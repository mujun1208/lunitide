package main

import (
	"context"
	"encoding/json"
	"errors"
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

	"github.com/lunitide/lunitide/internal/agentorchestration"
	"github.com/lunitide/lunitide/internal/agentrunapp"
	"github.com/lunitide/lunitide/internal/app"
	"github.com/lunitide/lunitide/internal/artifactreview"
	"github.com/lunitide/lunitide/internal/attachmentapp"
	"github.com/lunitide/lunitide/internal/brapp"
	"github.com/lunitide/lunitide/internal/buildinfo"
	"github.com/lunitide/lunitide/internal/ccapp"
	"github.com/lunitide/lunitide/internal/compactionapp"
	"github.com/lunitide/lunitide/internal/conversationsapp"
	"github.com/lunitide/lunitide/internal/datadir"
	"github.com/lunitide/lunitide/internal/domain/m8core"
	"github.com/lunitide/lunitide/internal/governanceapp"
	"github.com/lunitide/lunitide/internal/identity"
	"github.com/lunitide/lunitide/internal/ipc"
	"github.com/lunitide/lunitide/internal/m6app"
	"github.com/lunitide/lunitide/internal/m7app"
	"github.com/lunitide/lunitide/internal/m8app"
	"github.com/lunitide/lunitide/internal/m9app"
	"github.com/lunitide/lunitide/internal/mcapp"
	"github.com/lunitide/lunitide/internal/mcp6"
	"github.com/lunitide/lunitide/internal/meetings"
	"github.com/lunitide/lunitide/internal/memoryapp"
	"github.com/lunitide/lunitide/internal/messageapp"
	"github.com/lunitide/lunitide/internal/networkpolicy"
	"github.com/lunitide/lunitide/internal/ontologyapp"
	"github.com/lunitide/lunitide/internal/org"
	"github.com/lunitide/lunitide/internal/people"
	"github.com/lunitide/lunitide/internal/planningapp"
	"github.com/lunitide/lunitide/internal/projectapp"
	"github.com/lunitide/lunitide/internal/providerapp"
	"github.com/lunitide/lunitide/internal/queueapp"
	"github.com/lunitide/lunitide/internal/scheduler"
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
	store, err := storage.OpenSecure(ctx, dataRoot, "lunitide.db")
	if err != nil {
		log.Fatal(err)
	}
	defer store.Close()
	providerService := providerapp.New(store, store)
	projectService := projectapp.New(store, store)
	sessionService := sessionapp.New(store, store)
	projectService.SetArtifactChecker(store)
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
	skillService.SetCategoryStore(store, store)
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
	// M6 S5C: skill-import + complexity routing share the agent-runtime
	// single-writer transaction. Extension/catalog/delegation/merge stay
	// unwired until their storage slices are enabled; those handlers
	// nil-guard to STORAGE_UNAVAILABLE.
	engine.SetM6GovernanceServices(
		m6app.NewSkillImportService(store.AgentRuntimeRepository()),
		m6app.NewRoutingService(store.AgentRuntimeRepository()),
	)
	// P1-1: persistent stdio MCP sessions. The idle reaper runs until
	// shutdown; pooled children die with the engine process anyway (5B
	// job object), the reaper just bounds live servers while running.
	mcpStdioPool.Start(ctx)
	defer mcpStdioPool.Close()
	// M8 slice 1: the governed long-term memory core (candidate/fact/
	// source-leaf/recall on the shared single-writer transaction).
	memorySvc := m8app.NewMemoryService(store.AgentRuntimeRepository(), "local-user")
	engine.SetM8MemoryServices(memorySvc)
	// M10: the memory nomination workflow over the slice-1 core.
	engine.SetM10NominationService(m8app.NewNominationService(store.AgentRuntimeRepository(), memorySvc))
	// M10: expert scenario cards over the FR-19 expert core.
	engine.SetM10ScenarioService(m8app.NewScenarioService(store.AgentRuntimeRepository()))
	// M10: queued user input (run.queue*).
	engine.SetQueueService(queueapp.New(store))
	// M10 wave-3: MCP market (mc.*) over the shared single-writer tx.
	engine.SetMcMarketService(mcapp.New(store.AgentRuntimeRepository()))
	// M10 wave-3: browser multi-mode (br.*) with the CDP profile root.
	browserProfiles, err := dataRoot.PrepareSubdirectory("browser-profiles")
	if err != nil {
		log.Fatalf("prepare browser profile directory failed; engine not ready: %v", err)
	}
	engine.SetBrMultiModeService(brapp.New(store.AgentRuntimeRepository(), browserProfiles.Path()))
	// M10 wave-4: computer control (cc.*) over the shared single-writer tx.
	ccSvc := ccapp.New(store.AgentRuntimeRepository())
	engine.SetCcControlService(ccSvc)
	// M10: memory operations (stats/facts/traces/growth/settings/export/purge).
	engine.SetMemoryOpsService(m8app.NewMemoryOpsService(store))
	// M8 slices 2-5: KB documents, handoff/tombstone/device sync and the
	// workflow bundle dispatch projection (single-writer transactions).
	engine.SetM8SliceServices(
		m8app.NewKBService(store.AgentRuntimeRepository(), "local-user"),
		m8app.NewHandoffService(store.AgentRuntimeRepository(), "local-user"),
		m8app.NewAutomationService(store.AgentRuntimeRepository()),
	)
	// M8 FR-18: unified plugin bundle runtime - capabilities hot-register
	// into the existing registries through the verification chain.
	pluginSvc := m8app.NewPluginService(store.AgentRuntimeRepository(), "local-user")
	engine.SetM8PluginService(pluginSvc)
	if err := m8app.EnsureBuiltinPlugins(ctx, pluginSvc); err != nil {
		log.Printf("builtin plugin seed: %v", err)
	}
	// M8 FR-19: expert center - the persona read-only directory holds the
	// canonical six-section bodies addressed by persona_ref digest.
	personaRoot, err := dataRoot.PrepareSubdirectory("personas")
	if err != nil {
		log.Fatalf("prepare persona directory failed; engine not ready: %v", err)
	}
	expertSvc := m8app.NewExpertService(
		store.AgentRuntimeRepository(), "local-user",
		m8app.NewFilePersonaStore(personaRoot.Path()),
	)
	engine.SetM8ExpertService(expertSvc)
	engine.SetSessionExpertStore(store)
	if err := m8app.EnsureBuiltinExperts(ctx, expertSvc); err != nil {
		log.Printf("builtin expert seed: %v", err)
	}
	// M8 FR-17: the write-collaboration gate stays disabled through M8 -
	// evaluate/status/confirm run the frozen-threshold evaluation and the
	// one-time-token decision lifecycle over the M7 subagent audit and the
	// M5/M6 EffectJournal (read-only aggregation, fail-closed).
	engine.SetM8CollabGateService(m8app.NewCollabGateService(
		store.AgentRuntimeRepository(),
		store.AgentRuntimeRepository().GateEvidence(),
		m8core.WriteCollabBinding(),
	))
	// M9.5 Moon Companion TTS runtime: the router fans synthesis out to
	// the free Microsoft Edge cloud neural voices, offline SAPI / OneCore,
	// and a local reference-timbre (voice-clone) service. Machines without
	// SAPI still expose the cloud and ref engines.
	engine.SetM9TtsService(tts.NewService(tts.NewRouterEngine(tts.NewPlatformEngine())))
	// Local speech recognition, the companion's other ear. Nothing is
	// downloaded or started here: wiring the service only makes voice.status
	// answerable, and the engine and its model arrive when a user asks for
	// them. A machine that never opens the companion never pays for this.
	if voiceRoot, err := dataRoot.PrepareSubdirectory("voice"); err != nil {
		log.Printf("voice directory unavailable; local recognition stays off: %v", err)
	} else {
		voiceService := app.NewVoiceService(voiceRoot.Path(), "")
		engine.SetVoiceService(voiceService)
		defer voiceService.Close()
	}
	// MiniCPM-o 4.5 Q4 duplex. Weights download on demand; the server is
	// spawned only when this channel is selected.
	if omniRoot, err := dataRoot.PrepareSubdirectory("omni"); err != nil {
		log.Printf("omni directory unavailable; MiniCPM-o stays off: %v", err)
	} else {
		omniService := app.NewOmniService(omniRoot.Path())
		engine.SetOmniService(omniService)
		defer omniService.Close()
	}
	// M9 slice-1: org foundation - the org-admin bridge service derives the
	// verified org context from the persisted operator binding (ADR-011);
	// payloads never carry an org scope.
	orgRoot, err := dataRoot.PrepareSubdirectory("org")
	if err != nil {
		log.Fatalf("prepare org directory failed; engine not ready: %v", err)
	}
	orgAdmin := m9app.NewOrgAdminService(
		org.NewService(org.NewGate(store.OrgStorage()), nil),
		m9app.NewFileBindingStore(filepath.Join(orgRoot.Path(), "binding.json")),
	)
	engine.SetM9OrgAdminService(orgAdmin)
	if err := m9app.EnsureDefaultOrgBinding(ctx, orgAdmin); err != nil {
		log.Fatalf("org auto-bootstrap failed; engine not ready: %v", err)
	}
	// This-PC person archive + LAN people messenger. Discovery stays off
	// until the user turns it on; file offers are never auto-accepted.
	ident := identity.New(store)
	if err := ident.Ensure(ctx); err != nil {
		log.Fatalf("local identity bootstrap failed; engine not ready: %v", err)
	}
	peopleRecv, err := dataRoot.PrepareSubdirectory("people-inbox")
	if err != nil {
		log.Fatalf("prepare people inbox failed; engine not ready: %v", err)
	}
	peopleStage, err := dataRoot.PrepareSubdirectory("people-staging")
	if err != nil {
		log.Fatalf("prepare people staging failed; engine not ready: %v", err)
	}
	peopleSvc := people.New(store, ident, peopleRecv.Path(), peopleStage.Path())
	engine.SetIdentityPeopleServices(ident, peopleSvc)
	peopleSvc.StartDiscoveryIfEnabled()
	defer peopleSvc.Close()
	meetingsSvc := meetings.New(store)
	engine.SetMeetingsService(meetingsSvc)
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
	// Full-access file tools read/write inside the user-selected workspace
	// root (same workspace-root.json the host picker writes). Re-resolved
	// per call; any parse/validation failure falls back to the sandbox.
	tools.SetFullAccessRootResolver(func() (string, error) {
		path, err := dataRoot.FilePath("workspace-root.json")
		if err != nil {
			return "", err
		}
		return readWorkspaceRoot(path)
	})
	convConfigPath, err := dataRoot.FilePath("conversations-root.json")
	if err != nil {
		log.Fatal(err)
	}
	convStore := conversationsapp.New(convConfigPath, toolRoot.Path())
	engine.SetConversationsStore(convStore)
	tools.SetSessionStorageRoot(func() (string, error) {
		root, configured, err := convStore.EffectiveRoot()
		if err != nil || !configured {
			return "", nil
		}
		return root, nil
	})
	engine.SetToolRuntime(tools)
	defer tools.Close()
	// M10 wave-4: the cc.* agent tools execute through the ccapp
	// service (three-layer interception, risk gate, audit ledger).
	tools.SetCcExecutor(ccSvc.ExecuteTool)
	// stdio MCP sessions sandbox under the tool workspaces tree (M6-MCP-004
	// gate opened 2026-08-16; per-endpoint subdirectory, per-call lifetime).
	mcpStdioRoot, err := toolRoot.PrepareSubdirectory("mcp-stdio")
	if err != nil {
		log.Fatal(err)
	}
	defer mcpStdioRoot.Close()
	mcpGatewaySetStdioWorkDir(mcpStdioRoot.Path())
	go func() {
		if n, err := skillService.EnsureBundledSkills(ctx); err != nil {
			log.Printf("bundled skills: %v", err)
		} else if n > 0 {
			log.Printf("bundled skills published: %d", n)
		}
		engine.SeedRecommendedMcpKit(ctx)
		engine.SeedPlaywrightMcp(ctx)
		engine.HydrateMcpGatewayFromSettings(ctx)
	}()
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
	engine.SetAssetStorage(store)
	engine.SetDeliverableStorage(store)
	projectAttachmentRoot, err := dataRoot.PrepareSubdirectory("project-attachments")
	if err != nil {
		log.Fatal(err)
	}
	defer projectAttachmentRoot.Close()
	engine.SetProjectAttachmentStorage(store, attachmentapp.NewDirFileStorage(projectAttachmentRoot.Path()))
	templateRoot, err := dataRoot.PrepareSubdirectory("asset-templates")
	if err != nil {
		log.Fatal(err)
	}
	defer templateRoot.Close()
	engine.SetTemplateFileStorage(attachmentapp.NewDirFileStorage(templateRoot.Path()))
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
			shutdownAfterSession(err, cancel)
			if authenticated {
				// The engine is single-use per desktop host: when the host's
				// RPC client hangs up (clean EOF or transport failure) the
				// engine exits code=0 by design. Logging the cause pairs the
				// engine exit with the host-side poison reason in host-*.log.
				log.Printf("authenticated RPC session ended (err=%v); engine exiting with its host", err)
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

// readWorkspaceRoot parses the host-written workspace-root.json. The heavy
// validation (fixed drive, no reparse point) already happened in the host
// picker; here we re-check existence and directory-ness so a stale config
// degrades to the sandbox instead of a confusing write error.
func readWorkspaceRoot(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	if len(data) > 4096 {
		return "", errors.New("invalid workspace root config")
	}
	var c struct {
		Path string `json:"path"`
	}
	if err := json.Unmarshal(data, &c); err != nil || c.Path == "" || len(c.Path) > 1024 {
		return "", errors.New("invalid workspace root config")
	}
	clean := filepath.Clean(c.Path)
	if !filepath.IsAbs(clean) || strings.HasPrefix(clean, `\\`) {
		return "", errors.New("invalid workspace root path")
	}
	info, err := os.Lstat(clean)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return "", errors.New("workspace root is not a plain directory")
	}
	return clean, nil
}
