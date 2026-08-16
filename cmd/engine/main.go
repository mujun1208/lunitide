package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/lunitide/lunitide/internal/agentorchestration"
	"github.com/lunitide/lunitide/internal/agentrunapp"
	"github.com/lunitide/lunitide/internal/app"
	"github.com/lunitide/lunitide/internal/artifactreview"
	"github.com/lunitide/lunitide/internal/scheduler"
	"github.com/lunitide/lunitide/internal/attachmentapp"
	"github.com/lunitide/lunitide/internal/buildinfo"
	"github.com/lunitide/lunitide/internal/compactionapp"
	"github.com/lunitide/lunitide/internal/datadir"
	"github.com/lunitide/lunitide/internal/domain/m8core"
	"github.com/lunitide/lunitide/internal/governanceapp"
	"github.com/lunitide/lunitide/internal/ipc"
	"github.com/lunitide/lunitide/internal/m7app"
	"github.com/lunitide/lunitide/internal/m8app"
	"github.com/lunitide/lunitide/internal/m9app"
	"github.com/lunitide/lunitide/internal/mcp6"
	"github.com/lunitide/lunitide/internal/memoryapp"
	"github.com/lunitide/lunitide/internal/messageapp"
	"github.com/lunitide/lunitide/internal/networkpolicy"
	"github.com/lunitide/lunitide/internal/ontologyapp"
	"github.com/lunitide/lunitide/internal/org"
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

	// M7 slice 3: CR revisions and immutable release packages.
	engine.SetM7ReleaseServices(m7app.NewReleaseService(store.AgentRuntimeRepository()))

	// M7 slice 4: the promotion saga (migration/deployment adapters stay
	// internal to the Promotion aggregate - M7-MIG-001).
	engine.SetM7PromotionServices(m7app.NewPromotionService(store.AgentRuntimeRepository()))
	engine.SetM7UpdateServices(m7app.NewUpdateService(store.AgentRuntimeRepository()))
	// M7 slices 6-8: read-only subagent runtime, tool-gap runtime and the
	// MCP settings plane (invoke stays on mcp6.invoke per the wire
	// contract). The frozen tool manifest is seeded read-only at startup.
	engine.SetM7RuntimeServices(
		m7app.NewSubagentService(store.AgentRuntimeRepository()),
		m7app.NewToolgapService(store.AgentRuntimeRepository()),
		m7app.NewMcpRuntimeService(store.AgentRuntimeRepository()),
	)
	// M6 MCP endpoint registry: production transport adapters live in
	// mcpgateway.go (frozen M5 GET client, self-host allowlist; stdio via
	// the 5B-isolated spawn engine). Extension supply / endpoint
	// persistence services stay unwired until their storage slices are
	// enabled; the handlers nil-guard them.
	mcp6Registry := mcp6.NewRegistry(mcpGatewayProbe, mcpGatewayInvoke, mcpEmptyLease{})
	mcp6Registry.SetDescribeFunc(mcpGatewayDescribe)
	engine.SetM6Services(nil, mcp6Registry, nil)
	// M8 slice 1: the governed long-term memory core (candidate/fact/
	// source-leaf/recall on the shared single-writer transaction).
	engine.SetM8MemoryServices(m8app.NewMemoryService(store.AgentRuntimeRepository(), "local-user"))
	// M8 slices 2-5: KB documents, handoff/tombstone/device sync and the
	// workflow bundle dispatch projection (single-writer transactions).
	engine.SetM8SliceServices(
		m8app.NewKBService(store.AgentRuntimeRepository(), "local-user"),
		m8app.NewHandoffService(store.AgentRuntimeRepository(), "local-user"),
		m8app.NewAutomationService(store.AgentRuntimeRepository()),
	)
	// M8 FR-18: unified plugin bundle runtime - capabilities hot-register
	// into the existing registries through the verification chain.
	engine.SetM8PluginService(m8app.NewPluginService(store.AgentRuntimeRepository(), "local-user"))
	// M8 FR-19: expert center - the persona read-only directory holds the
	// canonical six-section bodies addressed by persona_ref digest.
	personaRoot, err := dataRoot.PrepareSubdirectory("personas")
	if err != nil {
		log.Fatalf("prepare persona directory failed; engine not ready: %v", err)
	}
	engine.SetM8ExpertService(m8app.NewExpertService(
		store.AgentRuntimeRepository(), "local-user",
		m8app.NewFilePersonaStore(personaRoot.Path()),
	))
	// M8 FR-17: the write-collaboration gate stays disabled through M8 -
	// evaluate/status/confirm run the frozen-threshold evaluation and the
	// one-time-token decision lifecycle over the M7 subagent audit and the
	// M5/M6 EffectJournal (read-only aggregation, fail-closed).
	engine.SetM8CollabGateService(m8app.NewCollabGateService(
		store.AgentRuntimeRepository(),
		store.AgentRuntimeRepository().GateEvidence(),
		m8core.WriteCollabBinding(),
	))
	// M9.5 Moon Companion: offline SAPI TTS runtime. Synthesis stays
	// process-local (no network); machines without SAPI degrade to the
	// M95-001 subtitle-only mode at the bridge layer.
	engine.SetM9TtsService(tts.NewService(tts.NewPlatformEngine()))
	// M9 slice-1: org foundation - the org-admin bridge service derives the
	// verified org context from the persisted operator binding (ADR-011);
	// payloads never carry an org scope.
	orgRoot, err := dataRoot.PrepareSubdirectory("org")
	if err != nil {
		log.Fatalf("prepare org directory failed; engine not ready: %v", err)
	}
	engine.SetM9OrgAdminService(m9app.NewOrgAdminService(
		org.NewService(org.NewGate(store.OrgStorage()), nil),
		m9app.NewFileBindingStore(filepath.Join(orgRoot.Path(), "binding.json")),
	))
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
	// Web tools ride the same SSRF-pinned transport as the agent-run web
	// fetch (plain HTTP allowed for public read-only content).
	tools.SetWebFetcher(func(ctx context.Context, rawURL string) (networkpolicy.FetchResult, error) {
		return networkpolicy.Fetch(ctx, rawURL, networkpolicy.FetchOptions{Policy: networkpolicy.Policy{AllowHTTP: true}})
	})
	engine.SetToolRuntime(tools)
	defer tools.Close()
	// stdio MCP sessions sandbox under the tool workspaces tree (M6-MCP-004
	// gate opened 2026-08-16; per-endpoint subdirectory, per-call lifetime).
	mcpStdioRoot, err := toolRoot.PrepareSubdirectory("mcp-stdio")
	if err != nil {
		log.Fatal(err)
	}
	defer mcpStdioRoot.Close()
	mcpGatewaySetStdioWorkDir(mcpStdioRoot.Path())
	// P2-2 artifact acceptance log lives beside the tool workspaces
	// (single-user, low-volume, atomic file persistence).
	reviewRoot, err := dataRoot.PrepareSubdirectory("artifact-reviews")
	if err != nil {
		log.Fatal(err)
	}
	defer reviewRoot.Close()
	reviews, err := artifactreview.NewStore(reviewRoot.Path())
	if err != nil {
		log.Fatal(err)
	}
	engine.SetArtifactReviewStore(reviews)
	// P2-3 resident automation: cron scheduler beside the tool workspaces.
	// The headless executor is attached after the engine is fully wired so
	// scheduled runs reuse the single durable chat kernel.
	automationStore, err := scheduler.NewStore(dataRoot.Path())
	if err != nil {
		log.Fatal(err)
	}
	automationSched := scheduler.New(automationStore, nil, scheduler.NewPlatformNotifier())
	automationSched.SetExecutor(engine.AutomationHeadlessExecutor())
	engine.SetAutomationScheduler(automationSched)
	automationSched.Start(ctx)
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
