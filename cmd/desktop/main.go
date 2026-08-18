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
	"github.com/lunitide/lunitide/internal/credentialsubmission"
	"github.com/lunitide/lunitide/internal/datadir"
	"github.com/lunitide/lunitide/internal/diagnosticapp"
	"github.com/lunitide/lunitide/internal/engineclient"
	"github.com/lunitide/lunitide/internal/hostbridge"
	"github.com/lunitide/lunitide/internal/ipc"
	"github.com/lunitide/lunitide/internal/secret"
	"github.com/lunitide/lunitide/internal/secretlease"
	"github.com/lunitide/lunitide/internal/systemsettings"
	"github.com/lunitide/lunitide/internal/uitheme"
	"github.com/lunitide/lunitide/internal/webviewhost"
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
	pipeID := make([]byte, 16)
	if _, err := rand.Read(pipeID); err != nil {
		return err
	}
	if *pipe == "" {
		*pipe = `\\.\pipe\lunitide-engine-` + hex.EncodeToString(pipeID)
	}
	brokerID := make([]byte, 16)
	if _, err := rand.Read(brokerID); err != nil {
		return err
	}
	brokerPipe := `\\.\pipe\lunitide-secret-` + hex.EncodeToString(brokerID)
	brokerListener, err := ipc.ListenCurrentUser(brokerPipe)
	if err != nil {
		return err
	}
	defer brokerListener.Close()
	bootstrapReader, bootstrapWriter, nonce, err := ipc.NewSessionBootstrap()
	if err != nil {
		return err
	}
	defer zeroBytes(nonce)
	defer bootstrapReader.Close()
	defer bootstrapWriter.Close()
	command := exec.Command(*enginePath, "--pipe", *pipe, "--host-pid", fmt.Sprint(os.Getpid()))
	command.Stdin = bootstrapReader
	command.Stdout, command.Stderr = os.Stdout, os.Stderr
	configureEngineProcess(command)
	if err := command.Start(); err != nil {
		return err
	}
	// Engine watchdog: once the WebView host is up, an unexpected Engine exit
	// (crash, OOM, external kill) must not leave a zombie UI whose every
	// chat.start fails with ENGINE_EVENT_SOURCE_CLOSED. The watcher relaunches
	// a fresh desktop instance (SQLite state survives) and lets this one exit
	// through the normal shutdown path. Engine death before hostReady takes
	// the normal startup-failure path instead, so a crashing engine cannot
	// spawn a relaunch loop.
	hostCtx, stopHost := context.WithCancel(context.Background())
	defer stopHost()
	hostReady := make(chan struct{})
	engineDied := make(chan struct{})
	shuttingDown := &atomic.Bool{}
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
			hostLog("engine exited unexpectedly: code=%d (%s)", code, detail)
			close(engineDied)
		}
	}()
	go func() {
		select {
		case <-engineDied:
			select {
			case <-hostReady:
				hostLog("engine death detected after host ready; relaunching desktop")
				fmt.Fprintln(os.Stderr, "engine process exited unexpectedly; relaunching desktop")
				if self, err := os.Executable(); err == nil {
					relaunch := exec.Command(self, os.Args[1:]...)
					_ = relaunch.Start()
				}
				stopHost()
			default:
			}
		case <-hostCtx.Done():
		}
	}()
	defer func() {
		shuttingDown.Store(true)
		stopEngine(command)
	}()
	if err := bootstrapReader.Close(); err != nil {
		return err
	}
	if err := ipc.WriteLaunchBootstrap(bootstrapWriter, nonce, brokerPipe); err != nil {
		return fmt.Errorf("write Engine bootstrap secret: %w", err)
	}
	if err := bootstrapWriter.Close(); err != nil {
		return err
	}
	dataRoot, err := datadir.PrepareProduction()
	if err != nil {
		return err
	}
	defer dataRoot.Close()
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
	brokerKey := secretlease.DeriveKey(nonce)
	defer secret.Zero(brokerKey)
	broker, err := secretlease.NewServer(brokerListener, secretService, command.Process.Pid, brokerKey)
	if err != nil {
		return err
	}
	brokerCtx, stopBroker := context.WithCancel(context.Background())
	defer broker.Close()
	defer stopBroker()
	brokerErr := make(chan error, 1)
	go func() {
		if err := broker.Serve(brokerCtx); err != nil {
			brokerErr <- err
		}
	}()
	startupCtx, cancelStartup := context.WithTimeout(context.Background(), 15*time.Second)
	client, err := engineclient.Connect(startupCtx, *pipe, command.Process.Pid, hex.EncodeToString(nonce))
	cancelStartup()
	zeroBytes(nonce)
	if err != nil {
		return err
	}
	defer client.Close()
	resolver := credentialsubmission.RPCResolver{Engine: client}
	coordinator, err := credentialsubmission.New(dataRoot, secretService, resolver, resolver)
	if err != nil {
		return err
	}
	defer coordinator.Close()
	credentialHandler := &credentialsubmission.HostHandler{Coordinator: coordinator, Engine: client, Secrets: secretService}
	migrationCtx, cancelMigration := context.WithTimeout(context.Background(), 20*time.Second)
	migrationErr := credentialHandler.RunElectronCredentialAdoption(migrationCtx)
	cancelMigration()
	if migrationErr != nil {
		// The underlying discovery/platform error may contain a legacy path.
		// Pending items remain retryable on the next startup.
		fmt.Fprintln(os.Stderr, "Electron credential migration was deferred and will be retried on next startup")
	}
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
		return errors.New("Engine health check failed")
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
	gateway, err := hostbridge.New(webviewhost.TrustedOrigin, client, map[bridge.Method]hostbridge.Handler{
		bridge.MethodBrowserOpen:              browserManager,
		bridge.MethodBrowserClose:             browserManager,
		bridge.MethodProviderCredentialReveal: credentialHandler,
		bridge.MethodProviderCredentialSubmit: credentialHandler,
		bridge.MethodProviderCreate:           credentialHandler,
		bridge.MethodProviderUpdate:           credentialHandler,
		bridge.MethodProviderDelete:           credentialHandler,
		bridge.MethodDiagnosticsExport:        &diagnosticapp.HostHandler{},
		bridge.MethodSystemSettingsOpen:       &systemsettings.Handler{OpenMicrophone: webviewhost.OpenMicrophonePrivacySettings},
		bridge.MethodUiThemeSet:               themeHandler,
		bridge.MethodWorkspaceRootSelect:      workspaceHandler,
		bridge.MethodWorkspaceRootGet:         workspaceHandler,
		bridge.MethodWorkspaceList:            workspaceHandler,
		bridge.MethodWorkspaceRead:            workspaceHandler,
	})
	if err != nil {
		return err
	}
	host, err := webviewhost.New(gateway, rendererDir, webViewDataRoot.Path())
	if err != nil {
		return err
	}
	themeHandler.Bind(host.SetTheme)
	fmt.Printf("Lunitide %s: Engine health check passed; starting WebView2 host\n", buildinfo.Version)
	close(hostReady)
	hostErr := make(chan error, 1)
	go func() { hostErr <- host.Run(hostCtx) }()
	select {
	case err := <-brokerErr:
		stopHost()
		<-hostErr
		return fmt.Errorf("secret broker stopped unexpectedly: %w", err)
	case err := <-hostErr:
		return err
	}
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
