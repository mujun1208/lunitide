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

	"github.com/lunitide/lunitide/internal/agentorchestration"
	"github.com/lunitide/lunitide/internal/agentrunapp"
	"github.com/lunitide/lunitide/internal/app"
	"github.com/lunitide/lunitide/internal/attachmentapp"
	"github.com/lunitide/lunitide/internal/buildinfo"
	"github.com/lunitide/lunitide/internal/compactionapp"
	"github.com/lunitide/lunitide/internal/datadir"
	"github.com/lunitide/lunitide/internal/governanceapp"
	"github.com/lunitide/lunitide/internal/ipc"
	"github.com/lunitide/lunitide/internal/m7app"
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
	"github.com/lunitide/lunitide/internal/terminalruntime"
	"github.com/lunitide/lunitide/internal/toolruntime"
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
	providerService := providerapp.New(store, store)
	projectService := projectapp.New(store, store)
	sessionService := sessionapp.New(store, store)
	projectService.SetDeleter(store)
	sessionService.SetDeleter(store)
	messageService, err := messageapp.New(store, store, cursorKey)
	secret.Zero(cursorKey)
	if err != nil {
		log.Fatal(err)
	}
	stageService := stageapp.New(store, store)
	governanceService := governanceapp.New(store, store)
	planningService := planningapp.New(store, store, governanceService)
	memoryService := memoryapp.New(store, store)
	ontologyService := ontologyapp.New(store, store, store, store)
	skillService := skillapp.New(store, store)
	engine := app.NewEngineWithP3P4(providerService, projectService, sessionService, messageService, stageService, planningService, governanceService, memoryService, ontologyService, skillService, store.ContextReader(), store, buildinfo.Version, leaseClient)
	coordinator, err := agentorchestration.New(store.AgentOrchestrationRepository(), agentorchestration.Limits{MaxDepth: 8, MaxConcurrency: 64}, nil)
	if err != nil {
		log.Fatal(err)
	}
	if err = coordinator.ReconcileRestart(ctx); err != nil {
		log.Fatalf("agent coordination restart recovery failed; engine not ready: %v", err)
	}
	engine.SetAgentCoordinator(coordinator)
	agentRuns := agentrunapp.New(store.AgentRuntimeRepository())
	engine.SetAgentRunService(agentRuns)

	// M7 slice 1: the nine-stage versioned workflow backbone.
	engine.SetM7WorkflowServices(m7app.NewWorkflowService(store.AgentRuntimeRepository()))

	// M7 slice 2: evidence trace, gates and reviews share the agent-runtime
	// single-writer transaction.
	m7traceSvc := m7app.NewTraceService(store.AgentRuntimeRepository())
	engine.SetM7EvidenceServices(
		m7traceSvc,
		m7app.NewGateService(store.AgentRuntimeRepository()),
		m7app.NewReviewService(store.AgentRuntimeRepository(), m7traceSvc),
	)
	// M4-F: resolve command jobs left in queued/running by a previous crash
	// to outcome_unknown before serving traffic (unprovable side effects are
	// never blindly retried). Failure means unreconciled jobs remain, so
	// startup fails closed.
	if reconciled, err := agentRuns.ReconcileCommandJobs(ctx); err != nil {
		log.Fatalf("command job reconciliation failed; engine not ready: %v", err)
	} else if reconciled > 0 {
		log.Printf("command job reconciliation: %d job(s) resolved to outcome_unknown", reconciled)
	}
	// Run generic recovery only after specialized effect dispatch. Prepared
	// changesets remain recoverable by an idempotent client retry; command
	// effects have already been reconciled above.
	if recovered, err := agentRuns.RunRecoveryScanner(ctx); err != nil {
		log.Fatalf("durable run recovery failed; engine not ready: %v", err)
	} else if recovered.Runs+recovered.Steps+recovered.ToolCalls+recovered.Effects > 0 {
		log.Printf("durable run recovery: runs=%d steps=%d tools=%d effects=%d", recovered.Runs, recovered.Steps, recovered.ToolCalls, recovered.Effects)
	}
	engine.SetMigrationService(app.NewMigrationAdapter(store))
	engine.SetupCompactionServices(store, store.CompactionMessageReader())
	engine.SetupHandoffService(store)
	toolRoot, err := dataRoot.PrepareSubdirectory("tool-workspaces")
	if err != nil {
		log.Fatal(err)
	}
	defer toolRoot.Close()
	tools, err := toolruntime.Open(toolRoot.Path())
	if err != nil {
		log.Fatal(err)
	}
	engine.SetToolRuntime(tools)
	defer tools.Close()
	terminalRoot, err := toolRoot.PrepareSubdirectory("terminals")
	if err != nil {
		log.Fatal(err)
	}
	defer terminalRoot.Close()
	terminals, err := terminalruntime.New(terminalruntime.Config{Workspace: terminalRoot.Path(), AuditPath: terminalRoot.Path() + string(os.PathSeparator) + "audit.jsonl", MaxSessions: 4})
	if err != nil {
		log.Fatal(err)
	}
	engine.SetTerminalRuntime(terminals)
	defer terminals.Shutdown()
	// Attachment service: prepare a DACL-protected subdirectory for file
	// content, then wire the service into the engine (ADR-005 §7). File
	// content lives outside SQLite; only metadata and parsed text are stored
	// in the database.
	attachmentRoot, err := dataRoot.PrepareSubdirectory("attachments")
	if err != nil {
		log.Fatal(err)
	}
	defer attachmentRoot.Close()
	engine.SetupAttachmentService(store, attachmentapp.NewDirFileStorage(attachmentRoot.Path()))
	if err := engine.ReconcileAttachmentFileCleanup(ctx); err != nil {
		log.Fatalf("attachment file cleanup reconciliation failed; engine not ready: %v", err)
	}
	// Reconcile orphaned compaction checkpoints left in pending or running
	// state by a previous process crash (ADR-005 §5: "restart recovery must be
	// called once at engine startup before serving traffic"). Any error means
	// state may still contain an unreconciled orphan, so startup fails closed.
	recoveryResults, recoveryErr := engine.RecoverCompaction(ctx)
	for _, r := range recoveryResults {
		log.Printf("compaction recovery: checkpoint=%s session=%s version=%d action=%s status=%s err=%v",
			r.CheckpointID, r.SessionID, r.Version, r.Action, r.Status, r.Err)
	}
	if err := compactionRecoveryError(recoveryResults, recoveryErr); err != nil {
		log.Fatalf("compaction restart recovery failed; engine not ready: %v", err)
	}
	// The externally reachable pipe and readiness marker come only after all
	// mandatory startup reconciliation has completed successfully.
	listener, err := ipc.ListenCurrentUser(*pipe)
	if err != nil {
		log.Fatal(err)
	}
	defer listener.Close()
	go func() { <-ctx.Done(); listener.Close() }()
	fmt.Println("lunitide-engine ready")
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

func compactionRecoveryError(results []compactionapp.RecoveryResult, recoveryErr error) error {
	if recoveryErr != nil {
		return recoveryErr
	}
	for _, result := range results {
		if result.Err != nil {
			return fmt.Errorf("checkpoint %s reconciliation incomplete: %w", result.CheckpointID, result.Err)
		}
	}
	return nil
}

func shutdownAfterSession(err error, cancel context.CancelFunc) {
	if errors.Is(err, ipc.ErrHandshakeACK) {
		cancel()
	}
}
