package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/lunitide/lunitide/internal/app"
	"github.com/lunitide/lunitide/internal/buildinfo"
	"github.com/lunitide/lunitide/internal/datadir"
	"github.com/lunitide/lunitide/internal/governanceapp"
	"github.com/lunitide/lunitide/internal/ipc"
	"github.com/lunitide/lunitide/internal/memoryapp"
	"github.com/lunitide/lunitide/internal/messageapp"
	"github.com/lunitide/lunitide/internal/ontologyapp"
	"github.com/lunitide/lunitide/internal/planningapp"
	"github.com/lunitide/lunitide/internal/projectapp"
	"github.com/lunitide/lunitide/internal/providerapp"
	"github.com/lunitide/lunitide/internal/secret"
	"github.com/lunitide/lunitide/internal/secretlease"
	"github.com/lunitide/lunitide/internal/sessionapp"
	"github.com/lunitide/lunitide/internal/skillapp"
	"github.com/lunitide/lunitide/internal/stageapp"
	storage "github.com/lunitide/lunitide/internal/storage/sqlite"
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
	bootstrapSecret, brokerPipe, err := ipc.ReadLaunchBootstrap(os.Stdin)
	if err != nil {
		log.Fatal(err)
	}
	brokerKey := secretlease.DeriveKey(bootstrapSecret)
	cursorKey := messageapp.DeriveCursorKey(bootstrapSecret)
	authenticator := ipc.NewSessionAuthenticator(bootstrapSecret)
	leaseClient, err := secretlease.NewClient(brokerPipe, *hostPID, brokerKey)
	secret.Zero(brokerKey)
	if err != nil {
		log.Fatal(err)
	}
	defer leaseClient.Close()
	dataRoot, err := datadir.PrepareProduction()
	if err != nil {
		log.Fatal(err)
	}
	defer dataRoot.Close()
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	store, err := storage.OpenSecure(ctx, dataRoot, "lunitide.db")
	if err != nil {
		log.Fatal(err)
	}
	defer store.Close()
	// Electron 0.1-0.2.1 stored provider metadata under its roaming userData
	// directory. Import is intentionally internal/startup-only: the public
	// migration Bridge methods remain disabled until their DTO contract exists.
	statuses, migrationErr := store.RunDiscoveredElectronProviderMetadata(ctx)
	if migrationErr != nil {
		log.Printf("Electron provider metadata migration skipped: %v", migrationErr)
	}
	for _, status := range statuses {
		log.Printf("Electron provider metadata migration: state=%s processed=%d imported=%d duplicates=%d conflicts=%d", status.State, status.Processed, status.Imported, status.Duplicates, status.Conflicts)
	}
	listener, err := ipc.ListenCurrentUser(*pipe)
	if err != nil {
		log.Fatal(err)
	}
	defer listener.Close()
	go func() { <-ctx.Done(); listener.Close() }()
	fmt.Println("lunitide-engine ready")
	providerService := providerapp.New(store, store)
	projectService := projectapp.New(store, store)
	sessionService := sessionapp.New(store, store)
	messageService, err := messageapp.New(store, store, cursorKey)
	secret.Zero(cursorKey)
	if err != nil { log.Fatal(err) }
	stageService := stageapp.New(store, store)
	governanceService := governanceapp.New(store, store)
	planningService := planningapp.New(store, store, governanceService)
	memoryService := memoryapp.New(store, store)
	ontologyService := ontologyapp.New(store, store, store, store)
	skillService := skillapp.New(store, store)
	engine := app.NewEngineWithP3P4(providerService, projectService, sessionService, messageService, stageService, planningService, governanceService, memoryService, ontologyService, skillService, store.ContextReader(), store, buildinfo.Version, leaseClient)
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
			shutdownAfterSession(err, cancel)
			if authenticated {
				cancel()
			}
		}()
	}
}

func shutdownAfterSession(err error, cancel context.CancelFunc) {
	if errors.Is(err, ipc.ErrHandshakeACK) {
		cancel()
	}
}
