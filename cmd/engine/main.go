package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"

	"github.com/lunitide/lunitide/internal/bootstrap"
	"github.com/lunitide/lunitide/internal/buildinfo"
	"github.com/lunitide/lunitide/internal/datadir"
	"github.com/lunitide/lunitide/internal/ipc"
	"github.com/lunitide/lunitide/internal/mcp6"
	"github.com/lunitide/lunitide/internal/messageapp"
	"github.com/lunitide/lunitide/internal/secret"
	"github.com/lunitide/lunitide/internal/secretlease"
	"github.com/lunitide/lunitide/internal/tts"
)

func main() {
	showVersion := flag.Bool("version", false, "print version and exit")
	pipe := flag.String("pipe", "", "per-launch named pipe path (required)")
	hostPID := flag.Int("host-pid", 0, "expected Host process ID")
	flag.Parse()
	if *showVersion {
		fmt.Println(buildinfo.Version)
		return
	}
	if *hostPID < 1 {
		log.Fatal("valid host-pid is required")
	}
	if *pipe == "" {
		log.Fatal("pipe is required")
	}
	bootstrapSecret, _, err := ipc.ReadLaunchBootstrap(os.Stdin)
	if err != nil {
		log.Fatal(err)
	}
	cursorKey := messageapp.DeriveCursorKey(bootstrapSecret)
	authenticator := ipc.NewSessionAuthenticator(bootstrapSecret)
	dataRoot, err := datadir.PrepareProduction()
	if err != nil {
		log.Fatal(err)
	}
	defer dataRoot.Close()
	if pidPath, pidErr := dataRoot.FilePath(ipc.GatewayEnginePIDFile); pidErr == nil {
		if err := ipc.SaveEnginePID(pidPath, os.Getpid()); err != nil {
			log.Printf("write engine.pid: %v", err)
		}
	}
	secretService, err := secret.NewDPAPIService(dataRoot)
	if err != nil {
		log.Fatal(err)
	}
	leaseClient, err := secretlease.NewLocalClient(secretService)
	if err != nil {
		log.Fatal(err)
	}
	defer leaseClient.Close()
	// Engine diagnostics land in a rotating file under <data>/logs. The
	// desktop host is a GUI process, so pipe-inherited stdout/stderr are
	// lost; without this file engine crashes leave no trace at all.
	logsDir, err := dataRoot.PrepareSubdirectory("logs")
	if err != nil {
		log.Fatal(err)
	}
	defer logsDir.Close()
	logFile := setupEngineLog(logsDir.Path())
	defer logFile.Close()
	// The hosted GPT-SoVITS launcher (ref auto-host) writes its python
	// startup output next to the engine logs; the tree is killed when
	// the engine exits so no orphaned model server survives.
	tts.DefaultRefHost.SetLogDir(logsDir.Path())
	defer tts.DefaultRefHost.Stop()
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	// M6 MCP endpoint registry: production transport adapters live in
	// mcpgateway.go (package main, transport/platform-local; frozen M5 GET
	// client, self-host allowlist; stdio via the 5B-isolated spawn engine).
	// The registry is built here and injected into the composition root.
	mcp6Registry := mcp6.NewRegistry(mcpGatewayProbe, mcpGatewayInvoke, mcpEmptyLease{})
	mcp6Registry.SetDescribeFunc(mcpGatewayDescribe)
	// Composition root: store -> service -> engine wiring plus all the
	// startup reconciliation. WireEngine returns a cleanup closure that
	// releases every resource it opened in reverse order, preserving the
	// original defer stack ordering relative to main's own defers above.
	engine, cleanup, err := bootstrap.WireEngine(ctx, bootstrap.EngineDeps{
		DataRoot:        dataRoot,
		SecretService:   secretService,
		LeaseClient:     leaseClient,
		CursorKey:       cursorKey,
		Mcp6Registry:    mcp6Registry,
		StartStdioPool:  mcpStdioPool.Start,
		CloseStdioPool:  mcpStdioPool.Close,
		SetStdioWorkDir: mcpGatewaySetStdioWorkDir,
	})
	if err != nil {
		log.Fatal(err)
	}
	defer cleanup()
	// The externally reachable pipe and readiness marker come only after all
	// mandatory startup reconciliation has completed successfully.
	listener, err := ipc.ListenCurrentUser(*pipe)
	if err != nil {
		log.Fatal(err)
	}
	defer listener.Close()
	go func() { <-ctx.Done(); listener.Close() }()
	log.Printf("lunitide-engine %s ready on pipe %s", buildinfo.Version, *pipe)
	sessions := ipc.NewSessionGate(8)
	for {
		conn, err := listener.Accept()
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			log.Printf("accept RPC connection: %v", err)
			continue
		}
		// Reject untrusted clients before they can consume a bounded session slot.
		leave, ok := ipc.AdmitClient(conn, *hostPID, sessions)
		if !ok {
			_ = conn.Close()
			continue
		}
		go func() {
			defer leave()
			authenticated := false
			err := ipc.ServeSession(ctx, conn, *hostPID, authenticator, engine, func() { authenticated = true })
			if err != nil && ctx.Err() == nil {
				log.Printf("RPC session closed: %v", err)
			}
			bootstrap.ShutdownAfterSession(err, cancel)
			if authenticated {
				// Owner disconnect unloads this client only. Handshake ACK
				// failure still cancels the engine (shutdownAfterSession).
				log.Printf("authenticated RPC session ended (err=%v); engine staying up", err)
			}
		}()
	}
}

// setupEngineLog redirects the standard logger into a dated file under
// <data>/logs and prunes files older than the seven most recent. The
// returned closer is owned by main.
func setupEngineLog(dir string) io.Closer {
	name := filepath.Join(dir, "engine-"+time.Now().Format("20060102-150405")+".log")
	f, err := os.OpenFile(name, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
	if err != nil {
		// Diagnostics must never block startup: keep the default stderr sink.
		return nopCloser{}
	}
	log.SetOutput(f)
	log.SetFlags(log.LstdFlags | log.LUTC)
	// Runtime fatal errors (concurrent map write, OOM, deadlock) bypass the
	// log package and print straight to the OS stderr handle. The desktop host
	// is a GUI process with no usable console, so without rebinding both the
	// Go-level and Win32-level stderr handles those crashes leave no trace.
	os.Stderr = f
	redirectStderr(f)
	pruneEngineLogs(dir, 7)
	fmt.Fprintln(f, "lunitide-engine", buildinfo.Version, "starting")
	return f
}

func pruneEngineLogs(dir string, keep int) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	var names []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasPrefix(e.Name(), "engine-") && strings.HasSuffix(e.Name(), ".log") {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	for len(names) > keep {
		_ = os.Remove(filepath.Join(dir, names[0]))
		names = names[1:]
	}
}

type nopCloser struct{}

func (nopCloser) Close() error { return nil }
