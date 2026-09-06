package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"github.com/lunitide/lunitide/internal/bridge"
	"github.com/lunitide/lunitide/internal/browserapp"
	"github.com/lunitide/lunitide/internal/buildinfo"
	"github.com/lunitide/lunitide/internal/conversationsapp"
	"github.com/lunitide/lunitide/internal/credentialsubmission"
	"github.com/lunitide/lunitide/internal/datadir"
	"github.com/lunitide/lunitide/internal/diagnosticapp"
	"github.com/lunitide/lunitide/internal/engineclient"
	"github.com/lunitide/lunitide/internal/hostbridge"
	"github.com/lunitide/lunitide/internal/ipc"
	"github.com/lunitide/lunitide/internal/secret"
	"github.com/lunitide/lunitide/internal/systemsettings"
	"github.com/lunitide/lunitide/internal/uitheme"
	"github.com/lunitide/lunitide/internal/webviewhost"
	"github.com/lunitide/lunitide/internal/desktopfiles"
	"github.com/lunitide/lunitide/internal/workspaceapp"
	"github.com/oklog/ulid/v2"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	showVersion := flag.Bool("version", false, "print version and exit")
	enginePath := flag.String("engine", "", "engine executable")
	pipe := flag.String("pipe", "", "development-only named pipe override")
	startHidden := flag.Bool("tray", false, "start hidden in the notification area")
	rpcHealth := flag.Bool("rpc-health", false, "connect to the running engine, print system.health, and exit")
	quitEngine := flag.Bool("quit", false, "stop the running engine (same as tray Exit) and exit")
	takeover := flag.Bool("takeover", false, "D11: wait for the previous desktop instance to exit, then become the gateway")
	flag.Parse()
	if *showVersion {
		fmt.Println(buildinfo.Version)
		return nil
	}
	executable, err := os.Executable()
	if err != nil {
		return err
	}
	if *enginePath == "" {
		*enginePath = filepath.Join(filepath.Dir(executable), "lunitide-engine.exe")
	}
	if *rpcHealth {
		return runRPCHealth(*pipe)
	}
	if *quitEngine {
		return runQuitEngine(*pipe)
	}
	already, releaseInstance := claimGatewayInstance()
	if already && *takeover {
		already, releaseInstance = claimGatewayInstanceRetry(time.Duration(gatewayTakeoverWait) * time.Second)
	}
	if already {
		if !*takeover && webviewhost.ActivateExistingMainWindow() {
			return nil
		}
		if *takeover {
			return errors.New("引擎重启失败：上一实例还没退出。请从托盘打开月汐。")
		}
		return errors.New("月汐已在运行，但没能唤起窗口。请从托盘打开。")
	}
	var releaseOnce sync.Once
	innerRelease := releaseInstance
	releaseInstance = func() { releaseOnce.Do(innerRelease) }
	defer releaseInstance()
	if *pipe == "" {
		*pipe = ipc.GatewayPipeName(os.Getenv("USERNAME"))
	}
	dataRoot, err := datadir.PrepareProduction()
	if err != nil {
		return err
	}
	defer dataRoot.Close()
	noncePath, err := dataRoot.FilePath(ipc.GatewayNonceFile)
	if err != nil {
		return err
	}
	nonce, err := ipc.LoadGatewayNonce(noncePath)
	if err != nil || len(nonce) != 32 {
		nonce = make([]byte, 32)
		if _, err := rand.Read(nonce); err != nil {
			return err
		}
		if err := ipc.SaveGatewayNonce(noncePath, nonce); err != nil {
			return err
		}
	}
	defer zeroBytes(nonce)
	reconnectWait := 800 * time.Millisecond
	if pidPath, pidErr := dataRoot.FilePath(ipc.GatewayEnginePIDFile); pidErr == nil {
		if savedPID, loadErr := ipc.LoadEnginePID(pidPath); loadErr == nil && processAlive(savedPID) {
			reconnectWait = 3 * time.Second
		}
	}
	reconnectCtx, cancelReconnect := context.WithTimeout(context.Background(), reconnectWait)
	existing, reconnectErr := engineclient.Connect(reconnectCtx, *pipe, 0, hex.EncodeToString(nonce))
	cancelReconnect()
	var command *exec.Cmd
	if reconnectErr == nil && existing != nil {
		command = nil
	} else {
		bootstrapReader, bootstrapWriter, _, bootErr := ipc.NewSessionBootstrap()
		if bootErr != nil {
			return bootErr
		}
		defer bootstrapReader.Close()
		defer bootstrapWriter.Close()
		command = exec.Command(*enginePath, "--pipe", *pipe, "--host-pid", fmt.Sprint(os.Getpid()))
		command.Stdin = bootstrapReader
		command.Stdout, command.Stderr = os.Stdout, os.Stderr
		configureEngineProcess(command)
		if err := command.Start(); err != nil {
			return err
		}
		if err := bootstrapReader.Close(); err != nil {
			return err
		}
		if err := ipc.WriteLaunchBootstrap(bootstrapWriter, nonce, `\\.\pipe\lunitide-secret-local`); err != nil {
			return fmt.Errorf("write Engine bootstrap secret: %w", err)
		}
		if err := bootstrapWriter.Close(); err != nil {
			return err
		}
	}
	// Engine watchdog: once the WebView host is up, an unexpected Engine exit
	// (crash, OOM, external kill) must not leave a zombie UI whose every
	// chat.start fails with ENGINE_EVENT_SOURCE_CLOSED. The watcher drops
	// Local\lunitide-gateway first, then starts a --takeover sibling, then
	// exits. Holding the mutex until WebView2 finishes teardown is how
	// 0.4.43 failed D11: the child waited 15s, gave up, and nothing
	// respawned the engine. Engine death before hostReady is a startup fail.
	hostCtx, stopHost := context.WithCancel(context.Background())
	defer stopHost()
	hostReady := make(chan struct{})
	engineDied := make(chan struct{})
	var engineDiedOnce sync.Once
	signalEngineDied := func() { engineDiedOnce.Do(func() { close(engineDied) }) }
	shuttingDown := &atomic.Bool{}
	shouldStopEngine := &atomic.Bool{}
	var enginePID atomic.Int32
	if command != nil && command.Process != nil {
		enginePID.Store(int32(command.Process.Pid))
	}
	// engineClient lets the watchdog distinguish root causes: engine crash
	// (client still healthy) vs client-side RPC failure (poison reason is
	// logged via engineclient.DiagnosticsSink) vs our own shutdown.
	var engineClient atomic.Pointer[engineclient.Client]
	if command != nil {
		go func() {
			waitErr := command.Wait()
			if !shuttingDown.Load() {
				// Capture the exit code: 2 means a Go runtime fatal error (concurrent
				// map write, OOM, deadlock) whose traceback now lands in the engine
				// log thanks to the engine-side stderr rebinding; 0xC0000005 would be
				// a native access violation; 1 is log.Fatal. This is the primary
				// forensic record for the "engine dies, desktop relaunches" symptom.
				code, detail := 0, "clean exit"
				if waitErr != nil {
					detail = waitErr.Error()
					var exitErr *exec.ExitError
					if errors.As(waitErr, &exitErr) {
						code = exitErr.ExitCode()
					}
				}
				if c := engineClient.Load(); c != nil {
					if broken := c.Broken(); broken != nil {
						hostLog("engine exited after RPC client failure: code=%d (%s); rpc: %v", code, detail, broken)
					} else {
						hostLog("engine exited unexpectedly while RPC client healthy: code=%d (%s)", code, detail)
					}
				} else {
					hostLog("engine exited unexpectedly before RPC connect: code=%d (%s)", code, detail)
				}
				signalEngineDied()
			}
		}()
	}
	go func() {
		select {
		case <-engineDied:
			select {
			case <-hostReady:
				hostLog("engine death detected after host ready; dropping gateway mutex then relaunching with --takeover")
				fmt.Fprintln(os.Stderr, "engine process exited unexpectedly; relaunching desktop")
				if self, err := os.Executable(); err == nil {
					if err := releaseGatewayThenRelaunch(self, os.Args[1:], releaseInstance, nil); err != nil {
						hostLog("engine death relaunch failed: %v", err)
					}
				}
				stopHost()
			default:
			}
		case <-hostCtx.Done():
		}
	}()
	defer func() {
		shuttingDown.Store(true)
		// G1: Task Manager / window crash must leave the detached engine.
		// D11: engine death (not tray death) relaunches desktop to respawn it.
		// Tray "退出" is the only path that stops the assistant — including
		// after a reconnect, when this process did not spawn the engine.
		if !shouldStopEngine.Load() {
			return
		}
		if command != nil {
			stopEngine(command)
		}
		if pid := int(enginePID.Load()); pid > 0 {
			stopEnginePID(pid, false)
		}
	}()
	webViewDataRoot, err := dataRoot.PrepareSubdirectory("WebView2")
	if err != nil {
		return err
	}
	defer webViewDataRoot.Close()
	browserWebViewDataRoot, err := dataRoot.PrepareSubdirectory("BrowserWebView2")
	if err != nil {
		return err
	}
	defer browserWebViewDataRoot.Close()
	browserManager, err := browserapp.New(browserWebViewDataRoot.Path(), webViewDataRoot.Path(), nil)
	if err != nil {
		return err
	}
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := browserManager.Shutdown(ctx); err != nil {
			fmt.Fprintln(os.Stderr, "isolated browser shutdown:", err)
		}
	}()
	secretService, err := secret.NewDPAPIService(dataRoot)
	if err != nil {
		return err
	}
	var client *engineclient.Client
	if existing != nil {
		client = existing
	} else {
		startupCtx, cancelStartup := context.WithTimeout(context.Background(), 15*time.Second)
		client, err = engineclient.Connect(startupCtx, *pipe, command.Process.Pid, hex.EncodeToString(nonce))
		cancelStartup()
	}
	zeroBytes(nonce)
	if err != nil {
		return err
	}
	// Forensics: the desktop is a GUI process whose stderr is lost. Route the
	// first RPC poison reason and the first WebView2 host failure into
	// host-*.log so "engine exited code=0, app relaunched" incidents carry
	// their root cause (stalled consumer, sequence mismatch, write failure,
	// renderer delivery failure, ...).
	engineclient.DiagnosticsSink = func(reason string) { hostLog("%s", reason) }
	webviewhost.HostDiagnosticsSink = func(message string) { hostLog("%s", message) }
	engineClient.Store(client)
	if pid, pidErr := client.ServerPID(); pidErr == nil && pid > 0 {
		enginePID.Store(int32(pid))
	}
	if path, pathErr := dataRoot.FilePath(ipc.GatewayEnginePIDFile); pathErr == nil {
		if pid := int(enginePID.Load()); pid > 0 {
			_ = ipc.SaveEnginePID(path, pid)
		}
	}
	// Shutdown ordering: the watchdog treats "engine exited while not
	// shutting down" as an unexpected death and relaunches the desktop.
	// Closing the RPC no longer exits the engine. Tray Exit sets
	// shouldStopEngine so the detached engine is stopped only then.
	defer func() {
		shuttingDown.Store(true)
		_ = client.Close()
	}()
	resolver := credentialsubmission.RPCResolver{Engine: client}
	coordinator, err := credentialsubmission.New(dataRoot, secretService, resolver, resolver)
	if err != nil {
		return err
	}
	defer coordinator.Close()
	credentialHandler := &credentialsubmission.HostHandler{Coordinator: coordinator, Engine: client, Secrets: secretService}
	cleanupCtx, stopCleanup := context.WithCancel(context.Background())
	defer stopCleanup()
	credentialHandler.StartCleanupWorker(cleanupCtx)
	id := ulid.Make().String()
	request := bridge.Request{Version: bridge.Version, Kind: "request", ID: id, TraceID: ulid.Make().String(), Method: "system.health", SentAt: time.Now().UTC(), Payload: json.RawMessage(`{}`), DeadlineMS: 3000}
	requestCtx, cancelRequest := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancelRequest()
	response, err := client.Call(requestCtx, request)
	if err != nil {
		return err
	}
	if !response.OK {
		return errors.New("engine health check failed")
	}
	rendererDir, err := webviewhost.DefaultRendererFolder()
	if err != nil {
		return err
	}
	themeHandler := &uitheme.Handler{}
	workspaceConfig, err := dataRoot.FilePath("workspace-root.json")
	if err != nil {
		return err
	}
	workspaceHandler := workspaceapp.New(workspaceConfig)
	conversationsHandler := conversationsapp.NewHostHandler()
	desktopFilesHandler := desktopfiles.New()
	gateway, err := hostbridge.New(webviewhost.TrustedOrigin, client, map[bridge.Method]hostbridge.Handler{
		bridge.MethodBrowserOpen:              browserManager,
		bridge.MethodBrowserClose:             browserManager,
		bridge.MethodProviderCredentialReveal: credentialHandler,
		bridge.MethodProviderCredentialSubmit: credentialHandler,
		bridge.MethodProviderCreate:                      credentialHandler,
		bridge.MethodProviderUpdate:                      credentialHandler,
		bridge.MethodProviderCredentialBackupAdd:         credentialHandler,
		bridge.MethodProviderDelete:                      credentialHandler,
		bridge.MethodDiagnosticsExport:        &diagnosticapp.HostHandler{},
		bridge.MethodSystemSettingsOpen:       &systemsettings.Handler{OpenMicrophone: webviewhost.OpenMicrophonePrivacySettings},
		bridge.MethodUiThemeSet:               themeHandler,
		bridge.MethodConversationsRootSelect:  conversationsHandler,
		bridge.MethodDesktopFilesPick:         desktopFilesHandler,
		bridge.MethodDesktopFilesReadChunk:    desktopFilesHandler,
		bridge.MethodWorkspaceRootSelect:      workspaceHandler,
		bridge.MethodWorkspaceRootClear:       workspaceHandler,
		bridge.MethodWorkspaceRootGet:         workspaceHandler,
		bridge.MethodWorkspaceList:            workspaceHandler,
		bridge.MethodWorkspaceRead:            workspaceHandler,
		bridge.MethodWorkspaceOpen:            workspaceHandler,
	})
	if err != nil {
		return err
	}
	pidPath, _ := dataRoot.FilePath(ipc.GatewayEnginePIDFile)
	go watchEngineHealth(hostCtx, shuttingDown, &engineClient, &enginePID, pidPath, *pipe, noncePath, gateway, func(rpcBroken bool, pid int) {
		hostLog("engine watch (rpcBroken=%v pid=%d); dropping gateway mutex then relaunching", rpcBroken, pid)
		signalEngineDied()
	})
	host, err := webviewhost.New(gateway, rendererDir, webViewDataRoot.Path())
	if err != nil {
		return err
	}
	host.SetStartHidden(*startHidden)
	themeHandler.Bind(host.SetTheme)
	fmt.Printf("Lunitide %s: Engine health check passed; starting WebView2 host\n", buildinfo.Version)
	close(hostReady)
	hostErr := make(chan error, 1)
	go func() { hostErr <- host.Run(hostCtx) }()
	runErr := <-hostErr
	if host.ForceQuitRequested() {
		shouldStopEngine.Store(true)
	}
	return runErr
}

func zeroBytes(value []byte) {
	for index := range value {
		value[index] = 0
	}
}

// stopEngine terminates the engine child on shutdown. The watchdog goroutine
// already reaps the process, so here we only kill a still-running one.
func stopEngine(command *exec.Cmd) {
	if command.Process == nil || command.ProcessState != nil {
		return
	}
	_ = command.Process.Kill()
}

func runRPCHealth(pipe string) error {
	if pipe == "" {
		pipe = ipc.GatewayPipeName(os.Getenv("USERNAME"))
	}
	dataRoot, err := datadir.PrepareProduction()
	if err != nil {
		return err
	}
	defer dataRoot.Close()
	noncePath, err := dataRoot.FilePath(ipc.GatewayNonceFile)
	if err != nil {
		return err
	}
	nonce, err := ipc.LoadGatewayNonce(noncePath)
	if err != nil || len(nonce) != 32 {
		return errors.New("引擎未就绪：没有可用的会话凭证。请先启动月汐。")
	}
	defer zeroBytes(nonce)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	client, err := engineclient.Connect(ctx, pipe, 0, hex.EncodeToString(nonce))
	if err != nil {
		return fmt.Errorf("无法连上已有引擎: %w", err)
	}
	defer client.Close()
	id := ulid.Make().String()
	request := bridge.Request{Version: bridge.Version, Kind: "request", ID: id, TraceID: ulid.Make().String(), Method: "system.health", SentAt: time.Now().UTC(), Payload: json.RawMessage(`{}`), DeadlineMS: 3000}
	response, err := client.Call(ctx, request)
	if err != nil {
		return err
	}
	if !response.OK {
		return errors.New("engine health check failed")
	}
	raw, err := json.Marshal(response.Payload)
	if err != nil || len(raw) == 0 || string(raw) == "null" {
		fmt.Println(`{"engine":"ready"}`)
		return nil
	}
	fmt.Println(string(raw))
	return nil
}

func runQuitEngine(pipe string) error {
	if pipe == "" {
		pipe = ipc.GatewayPipeName(os.Getenv("USERNAME"))
	}
	dataRoot, err := datadir.PrepareProduction()
	if err != nil {
		return err
	}
	defer dataRoot.Close()
	var enginePID int
	if path, pathErr := dataRoot.FilePath(ipc.GatewayEnginePIDFile); pathErr == nil {
		if pid, loadErr := ipc.LoadEnginePID(path); loadErr == nil {
			enginePID = pid
		}
	}
	noncePath, err := dataRoot.FilePath(ipc.GatewayNonceFile)
	if err == nil {
		if nonce, loadErr := ipc.LoadGatewayNonce(noncePath); loadErr == nil && len(nonce) == 32 {
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			client, connErr := engineclient.Connect(ctx, pipe, 0, hex.EncodeToString(nonce))
			cancel()
			if connErr == nil {
				if pid, pidErr := client.ServerPID(); pidErr == nil && pid > 0 {
					enginePID = int(pid)
				}
				_ = client.Close()
			}
			zeroBytes(nonce)
		}
	}
	if enginePID < 1 {
		return errors.New("没有正在运行的引擎可退出")
	}
	stopEnginePID(enginePID, false)
	fmt.Println("engine stopped")
	return nil
}

// hostLogMu guards the lazily-opened host log file. The desktop is a GUI
// process whose stdout/stderr are lost, so watchdog events (notably the
// engine exit code) are persisted to a rotating file under <data>/logs
// alongside the engine's own diagnostics.
var (
	hostLogMu   sync.Mutex
	hostLogFile *os.File
)

// hostLog appends a timestamped line to host-<launch>.log under the
// production data root. Failures are silent: diagnostics must never block
// the UI or the watchdog path.
func hostLog(format string, args ...any) {
	hostLogMu.Lock()
	defer hostLogMu.Unlock()
	if hostLogFile == nil {
		root, rootErr := datadir.PrepareProduction()
		if rootErr == nil {
			if logs, logsErr := root.PrepareSubdirectory("logs"); logsErr == nil {
				path := filepath.Join(logs.Path(), "host-"+time.Now().Format("20060102-150405")+".log")
				if f, openErr := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600); openErr == nil {
					hostLogFile = f
				}
				_ = logs.Close()
			}
			_ = root.Close()
		}
	}
	if hostLogFile == nil {
		return
	}
	fmt.Fprintf(hostLogFile, "%s %s\n", time.Now().UTC().Format("2006/01/02 15:04:05"), fmt.Sprintf(format, args...))
}
