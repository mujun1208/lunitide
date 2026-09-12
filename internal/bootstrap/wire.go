package bootstrap

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/lunitide/lunitide/internal/agenthub"
	"github.com/lunitide/lunitide/internal/agentorchestration"
	"github.com/lunitide/lunitide/internal/agentrunapp"
	"github.com/lunitide/lunitide/internal/app"
	"github.com/lunitide/lunitide/internal/artifactreview"
	"github.com/lunitide/lunitide/internal/attachmentapp"
	"github.com/lunitide/lunitide/internal/brapp"
	"github.com/lunitide/lunitide/internal/buildinfo"
	"github.com/lunitide/lunitide/internal/ccapp"
	"github.com/lunitide/lunitide/internal/config"
	"github.com/lunitide/lunitide/internal/connectorapp"
	"github.com/lunitide/lunitide/internal/conversationsapp"
	"github.com/lunitide/lunitide/internal/datadir"
	"github.com/lunitide/lunitide/internal/datasourceapp"
	"github.com/lunitide/lunitide/internal/domain/m8core"
	"github.com/lunitide/lunitide/internal/governanceapp"
	"github.com/lunitide/lunitide/internal/identity"
	"github.com/lunitide/lunitide/internal/imapp"
	"github.com/lunitide/lunitide/internal/m6app"
	"github.com/lunitide/lunitide/internal/m7app"
	"github.com/lunitide/lunitide/internal/m8app"
	"github.com/lunitide/lunitide/internal/m9app"
	"github.com/lunitide/lunitide/internal/mcapp"
	"github.com/lunitide/lunitide/internal/mcp6"
	"github.com/lunitide/lunitide/internal/meetings"
	"github.com/lunitide/lunitide/internal/memoryapp"
	"github.com/lunitide/lunitide/internal/messageapp"
	"github.com/lunitide/lunitide/internal/mroapp"
	"github.com/lunitide/lunitide/internal/networkpolicy"
	"github.com/lunitide/lunitide/internal/ocrapp"
	"github.com/lunitide/lunitide/internal/officeapp"
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
	"github.com/lunitide/lunitide/internal/skillarchive"
	"github.com/lunitide/lunitide/internal/stageapp"
	storage "github.com/lunitide/lunitide/internal/storage/sqlite"
	"github.com/lunitide/lunitide/internal/terminalruntime"
	"github.com/lunitide/lunitide/internal/toolruntime"
	"github.com/lunitide/lunitide/internal/tts"
	"github.com/lunitide/lunitide/internal/widgetapp"
)

// EngineDeps carries the process-owned collaborators that main() constructs
// before the composition root runs. The MCP gateway callbacks and the stdio
// pool live in package main (transport/platform-local) and are injected here
// so WireEngine stays free of cmd/engine wiring.
type EngineDeps struct {
	DataRoot      *datadir.SecureRoot
	SecretService secret.Service
	LeaseClient   *secretlease.LocalClient
	// CursorKey is zeroed inside WireEngine once the message service owns it.
	CursorKey []byte
	// Mcp6Registry is pre-built by main with the transport-local probe/invoke/
	// describe callbacks; WireEngine only wires it into the engine.
	Mcp6Registry *mcp6.Registry
	// StartStdioPool / CloseStdioPool drive the persistent stdio MCP pool that
	// lives in package main. CloseStdioPool is registered on the cleanup stack.
	StartStdioPool func(context.Context)
	CloseStdioPool func()
	// SetStdioWorkDir points the stdio MCP sandbox at the tool-workspaces tree.
	SetStdioWorkDir func(string)
}

// WireEngine performs the store -> service -> engine composition that used to
// live inline in main(). It returns the fully wired engine plus a cleanup
// closure that releases every resource it opened, in reverse order (matching
// the original defer stack). On any startup failure it releases whatever it
// already opened and returns the error; the caller decides whether to fatal.
func WireEngine(ctx context.Context, deps EngineDeps) (*app.Engine, func(), error) {
	var closers []func()
	cleanup := func() {
		for i := len(closers) - 1; i >= 0; i-- {
			closers[i]()
		}
	}
	fail := func(e error) (*app.Engine, func(), error) {
		cleanup()
		return nil, nil, e
	}

	dataRoot := deps.DataRoot
	secretService := deps.SecretService
	leaseClient := deps.LeaseClient
	cursorKey := deps.CursorKey

	store, err := storage.OpenSecure(ctx, dataRoot, "lunitide.db")
	if err != nil {
		return fail(err)
	}
	closers = append(closers, func() { _ = store.Close() })
	if err := store.RecoverInterruptedToolOperations(ctx); err != nil {
		return fail(fmt.Errorf("tool operation recovery failed: %w", err))
	}
	if err := store.RecoverInterruptedCallAttempts(ctx); err != nil {
		return fail(fmt.Errorf("call attempt recovery failed: %w", err))
	}
	// Inspect persisted ownership before any startup seeder can add rows.
	personalData, organizationData, err := store.DesktopScopePresence(ctx)
	if err != nil {
		return fail(fmt.Errorf("read desktop data ownership: %w", err))
	}
	// Move the local identity Ed25519 private key out of the database column
	// and into the DPAPI credential store. Must be wired before identity
	// Ensure/Load runs below so the seal/migrate seam is active on first read.
	store.WithIdentitySecrets(secretService)
	// Resolve the durable identity before services or seeders bind their owner.
	// Ensure migrates the legacy local-user alias; retaining that alias in a
	// service afterwards makes its own existing knowledge fail ownership checks.
	ident := identity.New(store)
	if err := ident.Ensure(ctx); err != nil {
		return fail(fmt.Errorf("local identity bootstrap failed; engine not ready: %w", err))
	}
	localSubject := ident.SubjectID()
	providerService := providerapp.New(store, store)
	projectService := projectapp.New(store, store)
	sessionService := sessionapp.New(store, store)
	projectService.SetArtifactChecker(store)
	projectService.SetDeleter(store)
	sessionService.SetDeleter(store)
	messageService, err := messageapp.New(store, store, cursorKey)
	secret.Zero(cursorKey)
	if err != nil {
		return fail(err)
	}
	stageService := stageapp.New(store, store)
	governanceService := governanceapp.New(store, store)
	planningService := planningapp.New(store, store, governanceService)
	memoryService := memoryapp.New(store, store)
	ontologyService := ontologyapp.New(store, store, store, store)
	skillService := skillapp.New(store, store)
	skillService.SetCategoryStore(store, store)
	skillService.SetInvocationStore(store)
	engine := app.NewEngineWithP3P4(providerService, projectService, sessionService, messageService, stageService, planningService, governanceService, memoryService, ontologyService, skillService, store.ContextReader(), store, buildinfo.Version, leaseClient)
	engine.SetChatTurnJournal(store)
	engine.SetStorageReadiness(store)
	engine.SetToolOperationStore(store)
	engine.SetCallAttemptStore(store)
	engine.SetMessageGroupStore(store)
	coordinator, err := agentorchestration.New(store.AgentOrchestrationRepository(), agentorchestration.Limits{MaxDepth: 8, MaxConcurrency: 64}, nil)
	if err != nil {
		return fail(err)
	}
	if err = coordinator.ReconcileRestart(ctx); err != nil {
		return fail(fmt.Errorf("agent coordination restart recovery failed; engine not ready: %w", err))
	}
	engine.SetAgentCoordinator(coordinator)
	agentRuns := agentrunapp.New(store.AgentRuntimeRepository())
	engine.SetAgentRunService(agentRuns)
	// On-demand GPT-SoVITS engine download (nothing large ships in the
	// package): the pack is pulled into %LOCALAPPDATA%\Lunitide\gpt-sovits
	// when a manifest points at one, where the ref launcher discovers it.
	engine.SetRefEngineInstall(app.NewRefEngineInstall())

	// On-demand offline ONNX voice download (sherpa-onnx + Kokoro): two
	// digest-pinned bundles pulled into %LOCALAPPDATA%\Lunitide so the local
	// path is install-and-use with no Python and no reference audio.
	engine.SetOnnxEngineInstall(app.NewOnnxEngineInstall())

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
	releaseService := m7app.NewReleaseService(store.AgentRuntimeRepository())
	releaseService.SetProjectContent(store)
	engine.SetM7ReleaseServices(releaseService)

	// M7 slice 4: the promotion saga (migration/deployment adapters stay
	// internal to the Promotion aggregate - M7-MIG-001).
	publicationRoot, err := dataRoot.PrepareSubdirectory("release-publications")
	if err != nil {
		return fail(err)
	}
	closers = append(closers, func() { _ = publicationRoot.Close() })
	promotionService := m7app.NewPromotionService(store.AgentRuntimeRepository())
	promotionService.SetLocalPublication(publicationRoot.Path())
	store.SetProjectPublicationRoot(publicationRoot.Path())
	engine.SetM7PromotionServices(promotionService)
	engine.SetM7UpdateServices(m7app.NewUpdateService(store.AgentRuntimeRepository()))
	// W3: the general audit_events chain shares the M7-DR-001 promotion freeze.
	engine.SetAuditChainVerifier(store)
	// M7 slices 6-8: read-only subagent runtime, tool-gap runtime and the
	// MCP settings plane (invoke stays on mcp6.invoke per the wire
	// contract). The frozen tool manifest is seeded read-only at startup.
	if err := store.RepairLegacyMcpPresetLaunches(ctx); err != nil {
		return fail(fmt.Errorf("repair legacy MCP presets: %w", err))
	}
	engine.SetM7RuntimeServices(
		m7app.NewSubagentService(store.AgentRuntimeRepository()),
		m7app.NewToolgapService(store.AgentRuntimeRepository()),
		m7app.NewMcpRuntimeService(store.AgentRuntimeRepository()),
	)
	// M6 MCP endpoint registry: production transport adapters live in
	// mcpgateway.go (frozen M5 GET client, self-host allowlist; stdio via
	// the 5B-isolated spawn engine). Both mcp.add and legacy mcp6.register
	// use the M7 durable endpoint/security store and the same startup hydrate.
	// The separate M6 endpoint projection and extension supply stay unwired.
	engine.SetM6Services(nil, deps.Mcp6Registry, nil)
	// M6 S5C: skill-import + complexity routing share the agent-runtime
	// single-writer transaction. Extension/catalog/delegation/merge stay
	// unwired until their storage slices are enabled; those handlers
	// nil-guard to STORAGE_UNAVAILABLE.
	skillImports := m6app.NewSkillImportService(store.AgentRuntimeRepository())
	skillImports.SetSource(skillarchive.Loader{})
	engine.SetM6GovernanceServices(
		skillImports,
		m6app.NewRoutingService(store.AgentRuntimeRepository()),
	)
	// P1-1: persistent stdio MCP sessions. The idle reaper runs until
	// shutdown; pooled children die with the engine process anyway (5B
	// job object), the reaper just bounds live servers while running.
	deps.StartStdioPool(ctx)
	closers = append(closers, deps.CloseStdioPool)
	// M8 slice 1: the governed long-term memory core (candidate/fact/
	// source-leaf/recall on the shared single-writer transaction).
	memorySvc := m8app.NewMemoryService(store.AgentRuntimeRepository(), localSubject)
	memorySvc.SetFTS(store)
	engine.SetM8MemoryServices(memorySvc)
	// Phase-3 governance switches (M1/M2/S2), armed only by explicit env
	// override; unset keeps every frozen default in force.
	engine.SetGovernanceFlags(config.LoadGovernanceFlagsFromEnv())
	engine.SetOfficeFlags(config.LoadOfficeFlagsFromEnv())
	engine.SetPersistDir(dataRoot.Path())
	// M10: the memory nomination workflow over the slice-1 core.
	engine.SetM10NominationService(m8app.NewNominationService(store.AgentRuntimeRepository(), memorySvc))
	// M10: expert scenario cards over the FR-19 expert core.
	scenarioSvc := m8app.NewScenarioService(store.AgentRuntimeRepository())
	engine.SetM10ScenarioService(scenarioSvc)
	// M10: queued user input (run.queue*).
	engine.SetQueueService(queueapp.New(store))
	// M10 wave-3: MCP market (mc.*) over the shared single-writer tx.
	engine.SetMcMarketService(mcapp.New(store.AgentRuntimeRepository()))
	// M10 wave-3: browser multi-mode (br.*) with the CDP profile root.
	browserProfiles, err := dataRoot.PrepareSubdirectory("browser-profiles")
	if err != nil {
		return fail(fmt.Errorf("prepare browser profile directory failed; engine not ready: %w", err))
	}
	browserMulti := brapp.New(store.AgentRuntimeRepository(), browserProfiles.Path())
	engine.SetBrMultiModeService(browserMulti)
	closers = append(closers, func() {
		if err := browserMulti.Close(); err != nil {
			log.Printf("browser shutdown: %v", err)
		}
	})
	// M10 wave-4: computer control (cc.*) over the shared single-writer tx.
	ccSvc := ccapp.New(store.AgentRuntimeRepository())
	if count, err := ccSvc.ReconcilePendingIntents(ctx); err != nil {
		return fail(fmt.Errorf("computer-control intent recovery failed; engine not ready: %w", err))
	} else if count > 0 {
		log.Printf("computer-control recovery: %d unknown operation(s); control disabled pending operator review", count)
	}
	engine.SetCcControlService(ccSvc)
	// M10: memory operations (stats/facts/traces/growth/settings/export/purge).
	engine.SetMemoryOpsService(m8app.NewMemoryOpsService(store))
	// M8 slices 2-5: KB documents, handoff/tombstone/device sync and the
	// workflow bundle dispatch projection (single-writer transactions).
	kbSvc := m8app.NewKBService(store.AgentRuntimeRepository(), localSubject)
	growthSvc := m8app.NewGrowthService(store.AgentRuntimeRepository())
	engine.SetM8SliceServices(
		kbSvc,
		m8app.NewHandoffService(store.AgentRuntimeRepository(), localSubject),
		m8app.NewAutomationService(store.AgentRuntimeRepository()),
	)
	engine.SetExpertGrowthService(growthSvc)
	engine.SetMROService(mroapp.New(store))
	ds := datasourceapp.New(store)
	if err := store.RecoverDatasourceWrites(ctx); err != nil {
		return fail(fmt.Errorf("recover datasource write receipts: %w", err))
	}
	secretPath, err := dataRoot.FilePath("datasource-secrets.json")
	if err != nil {
		return fail(err)
	}
	secrets := datasourceapp.NewFileSecrets(secretPath)
	ds.SetSecrets(secrets.Put, secrets.Get)
	// Pure-Go PostgreSQL/MySQL probe + query drivers (CGO_ENABLED=0). Remote
	// connections stay read-only; local connections also get a read-write path.
	ds.SetPinger(datasourceapp.SQLPinger)
	ds.SetQuerier(datasourceapp.SQLQuerier)
	ds.SetWriteQuerier(datasourceapp.SQLWriteQuerier)
	// Auto-create the fixed database on a local connection so onboarding only
	// needs an account + password (remote hosts are left untouched).
	ds.SetProvisioner(datasourceapp.SQLProvisioner)
	engine.SetDatasourceService(ds)
	engine.SetCapabilityRoleStore(store)
	// M8 FR-18: unified plugin bundle runtime - capabilities hot-register
	// into the existing registries through the verification chain.
	pluginSvc := m8app.NewPluginService(store.AgentRuntimeRepository(), localSubject)
	engine.SetM8PluginService(pluginSvc)
	engine.SetCapabilityPackStore(store.AgentRuntimeRepository())
	if err := m8app.EnsureBuiltinPlugins(ctx, pluginSvc); err != nil {
		log.Printf("builtin plugin seed: %v", err)
	}
	// M8 FR-19: expert center - the persona read-only directory holds the
	// canonical six-section bodies addressed by persona_ref digest.
	personaRoot, err := dataRoot.PrepareSubdirectory("personas")
	if err != nil {
		return fail(fmt.Errorf("prepare persona directory failed; engine not ready: %w", err))
	}
	expertSvc := m8app.NewExpertService(
		store.AgentRuntimeRepository(), localSubject,
		m8app.NewFilePersonaStore(personaRoot.Path()),
	)
	expertSvc.SetSkillStore(store)
	closers = append(closers, expertSvc.StartPersonaMaintenance(ctx))
	engine.SetM8ExpertService(expertSvc)
	engine.SetSessionExpertStore(store)
	engine.SetExpertClaimStore(store)
	if err := m8app.EnsureBuiltinExperts(ctx, expertSvc); err != nil {
		log.Printf("builtin expert seed: %v", err)
	}
	if err := m8app.EnsureExpertFoundations(ctx, expertSvc, kbSvc, growthSvc); err != nil {
		log.Printf("expert foundation seed: %v", err)
	}
	if err := m8app.EnsureMROScenarios(ctx, expertSvc, scenarioSvc); err != nil {
		log.Printf("mro scenario seed: %v", err)
	}
	if err := m8app.EnsureOpsExpertScenarios(ctx, expertSvc, scenarioSvc); err != nil {
		log.Printf("ops scenario seed: %v", err)
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
		closers = append(closers, func() { voiceService.Close() })
	}
	// MiniCPM-o 4.5 Q4 duplex stays in-tree for leftover installs, but Setup
	// does not ship llama-omni-server / Comni / GGUF. Weights and runtime
	// download on demand; the process is spawned only if this channel is used.
	if omniRoot, err := dataRoot.PrepareSubdirectory("omni"); err != nil {
		log.Printf("omni directory unavailable; MiniCPM-o stays off: %v", err)
	} else {
		omniService := app.NewOmniService(omniRoot.Path())
		engine.SetOmniService(omniService)
		go omniService.WarmRuntime()
		closers = append(closers, func() { omniService.Close() })
	}
	// M9 slice-1: org foundation - the org-admin bridge service derives the
	// verified org context from the persisted operator binding (ADR-011);
	// payloads never carry an org scope.
	orgRoot, err := dataRoot.PrepareSubdirectory("org")
	if err != nil {
		return fail(fmt.Errorf("prepare org directory failed; engine not ready: %w", err))
	}
	orgAdmin := m9app.NewOrgAdminService(
		org.NewService(org.NewGate(store.OrgStorage()), nil),
		m9app.NewFileBindingStore(filepath.Join(orgRoot.Path(), "binding.json")),
	)
	engine.SetM9OrgAdminService(orgAdmin)
	if err := m9app.RestoreLegacyPersonalBinding(ctx, orgAdmin, personalData, organizationData); err != nil {
		return fail(fmt.Errorf("restore legacy desktop data view: %w", err))
	}
	if err := m9app.EnsureDefaultOrgBinding(ctx, orgAdmin); err != nil {
		return fail(fmt.Errorf("org auto-bootstrap failed; engine not ready: %w", err))
	}
	// This-PC person archive + LAN people messenger. Discovery stays off
	// until the user turns it on; file offers are never auto-accepted.
	peopleRecv, err := dataRoot.PrepareSubdirectory("people-inbox")
	if err != nil {
		return fail(fmt.Errorf("prepare people inbox failed; engine not ready: %w", err))
	}
	peopleStage, err := dataRoot.PrepareSubdirectory("people-staging")
	if err != nil {
		return fail(fmt.Errorf("prepare people staging failed; engine not ready: %w", err))
	}
	peopleSvc := people.New(store, ident, peopleRecv.Path(), peopleStage.Path())
	peopleSvc.StartDelivery()
	engine.SetIdentityPeopleServices(ident, peopleSvc)
	if err := engine.RegisterExpertAgentContacts(ctx); err != nil {
		log.Printf("expert agent roster: %v", err)
	}
	peopleSvc.StartDiscoveryIfEnabled()
	closers = append(closers, func() { peopleSvc.Close() })
	meetingsSvc := meetings.New(store)
	if meetingsRoot, err := dataRoot.PrepareSubdirectory("meetings-audio"); err != nil {
		log.Printf("meetings audio directory unavailable; long sessions cannot persist WAV: %v", err)
	} else {
		meetingsSvc.SetAudioRoot(meetingsRoot.Path())
	}
	engine.SetMeetingsService(meetingsSvc)
	imSvc := imapp.New(store).WithSecrets(secretService)
	engine.SetIMChannelsService(imSvc)
	engine.StartIMInbound(ctx)
	// M4-F: resolve command jobs left in queued/running by a previous crash
	// to outcome_unknown before serving traffic (unprovable side effects are
	// never blindly retried). Failure means unreconciled jobs remain, so
	// startup fails closed.
	if reconciled, err := agentRuns.ReconcileCommandJobs(ctx); err != nil {
		return fail(fmt.Errorf("command job reconciliation failed; engine not ready: %w", err))
	} else if reconciled > 0 {
		log.Printf("command job reconciliation: %d job(s) resolved to outcome_unknown", reconciled)
	}
	// Run generic recovery only after specialized effect dispatch. Prepared
	// changesets remain recoverable by an idempotent client retry; command
	// effects have already been reconciled above.
	if recovered, err := agentRuns.RunRecoveryScanner(ctx); err != nil {
		return fail(fmt.Errorf("durable run recovery failed; engine not ready: %w", err))
	} else if recovered.Runs+recovered.Steps+recovered.ToolCalls+recovered.Effects > 0 {
		log.Printf("durable run recovery: runs=%d steps=%d tools=%d effects=%d", recovered.Runs, recovered.Steps, recovered.ToolCalls, recovered.Effects)
	}
	if err := agentRuns.RecoverPlanExecutions(ctx); err != nil {
		return fail(fmt.Errorf("plan execution recovery failed; engine not ready: %w", err))
	}
	engine.SetupCompactionServices(store, store.CompactionMessageReader())
	engine.SetupHandoffService(store)
	toolRoot, err := dataRoot.PrepareSubdirectory("tool-workspaces")
	if err != nil {
		return fail(err)
	}
	closers = append(closers, func() { _ = toolRoot.Close() })
	tools, err := toolruntime.Open(toolRoot.Path())
	if err != nil {
		return fail(err)
	}
	// Web tools ride the same SSRF-pinned transport as the agent-run web
	// fetch (plain HTTP allowed for public read-only content).
	tools.SetWebFetcher(func(ctx context.Context, rawURL string) (networkpolicy.FetchResult, error) {
		return networkpolicy.Fetch(ctx, rawURL, networkpolicy.FetchOptions{Policy: networkpolicy.Policy{AllowHTTP: true}})
	})
	tools.SetWeatherFetcher(fetchWeather)
	// Full-access file tools read/write inside the user-selected workspace
	// root (same workspace-root.json the host picker writes). Re-resolved
	// per call; any parse/validation failure falls back to the sandbox.
	tools.SetFullAccessRootResolver(func() (string, error) {
		path, err := dataRoot.FilePath("workspace-root.json")
		if err != nil {
			return "", err
		}
		return ReadWorkspaceRoot(path)
	})
	convConfigPath, err := dataRoot.FilePath("conversations-root.json")
	if err != nil {
		return fail(err)
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
	engine.SetOCR(ocrapp.New(ocrapp.NewFileStore(filepath.Join(dataRoot.Path(), "ocr-routing.json"))))
	engine.SetWidgetStore(widgetapp.NewFileStore(filepath.Join(dataRoot.Path(), "widgets.json")))
	engine.SetConnectorStore(connectorapp.NewFileStore(filepath.Join(dataRoot.Path(), "connector-recipes.json")))
	closers = append(closers, func() { _ = tools.Close() })
	// M10 wave-4: the cc.* agent tools execute through the ccapp
	// service (three-layer interception, risk gate, audit ledger).
	tools.SetCcExecutor(ccSvc.ExecuteTool)
	tools.SetIMSend(func(ctx context.Context, kind, to, text string) (desktopApp, output string, err error) {
		k, err := imapp.ParseKind(kind)
		if err != nil {
			return "", "", err
		}
		ch, msg, err := imSvc.Send(ctx, k, to, text)
		if err != nil {
			return "", "", err
		}
		if strings.HasPrefix(msg, "desktop:") {
			return ch.DesktopApp, msg, nil
		}
		return "", msg, nil
	})
	// stdio MCP sessions sandbox under the tool workspaces tree (M6-MCP-004
	// gate opened 2026-08-16; per-endpoint subdirectory, per-call lifetime).
	mcpStdioRoot, err := toolRoot.PrepareSubdirectory("mcp-stdio")
	if err != nil {
		return fail(err)
	}
	closers = append(closers, func() { _ = mcpStdioRoot.Close() })
	deps.SetStdioWorkDir(mcpStdioRoot.Path())
	go func() {
		if n, err := skillService.EnsureBundledSkills(ctx); err != nil {
			log.Printf("bundled skills: %v", err)
		} else if n > 0 {
			log.Printf("bundled skills published: %d", n)
		}
		if n, err := skillService.EnsureComposeSkills(ctx); err != nil {
			log.Printf("compose skills: %v", err)
		} else if n > 0 {
			log.Printf("compose skills published: %d", n)
		}
		engine.SeedRecommendedMcpKit(ctx)
		engine.SeedPlaywrightMcp(ctx)
		engine.HydrateMcpGatewayFromSettings(ctx)
	}()
	// P2-2 artifact acceptance log lives beside the tool workspaces
	// (single-user, low-volume, atomic file persistence).
	reviewRoot, err := dataRoot.PrepareSubdirectory("artifact-reviews")
	if err != nil {
		return fail(err)
	}
	closers = append(closers, func() { _ = reviewRoot.Close() })
	reviews, err := artifactreview.NewStore(reviewRoot.Path())
	if err != nil {
		return fail(err)
	}
	engine.SetArtifactReviewStore(reviews)
	officeRoot, err := dataRoot.PrepareSubdirectory("office-studio")
	if err != nil {
		log.Printf("office-studio storage unavailable; existing chat remains available: %v", err)
	} else {
		closers = append(closers, func() { _ = officeRoot.Close() })
		officeService, officeErr := officeapp.New(store, officeRoot.Path())
		if officeErr != nil {
			log.Printf("office-studio unavailable; existing chat remains available: %v", officeErr)
		} else {
			policy, policyErr := config.OfficeStoragePolicy()
			if policyErr != nil {
				log.Printf("office storage configuration invalid; using default policy: %v", policyErr)
			} else {
				officeService.StoragePolicy = policy
			}
			engine.SetOfficeStudio(officeService)
		}
	}
	hubRoot, hubErr := dataRoot.PrepareSubdirectory("agent-hub")
	if hubErr != nil {
		log.Printf("agent-hub storage unavailable; existing chat remains available: %v", hubErr)
	} else {
		closers = append(closers, func() { _ = hubRoot.Close() })
		hub := agenthub.New(store, hubRoot.Path(), func(title, body string) error {
			return scheduler.NewPlatformNotifier().Notify(title, body)
		})
		hub.Threads = store.ThreadStore()
		hub.Recover()
		engine.SetAgentHub(hub)
	}
	engine.SetAssetStorage(store)
	engine.SetDataScopeStore(store)
	engine.SetDeliverableStorage(store)
	projectAttachmentRoot, err := dataRoot.PrepareSubdirectory("project-attachments")
	if err != nil {
		return fail(err)
	}
	closers = append(closers, func() { _ = projectAttachmentRoot.Close() })
	projectEvidenceFiles := attachmentapp.NewDirFileStorage(projectAttachmentRoot.Path())
	engine.SetProjectAttachmentStorage(store, projectEvidenceFiles)
	templateRoot, err := dataRoot.PrepareSubdirectory("asset-templates")
	if err != nil {
		return fail(err)
	}
	closers = append(closers, func() { _ = templateRoot.Close() })
	templateEvidenceFiles := attachmentapp.NewDirFileStorage(templateRoot.Path())
	engine.SetTemplateFileStorage(templateEvidenceFiles)
	store.SetProjectEvidenceFiles(projectEvidenceFiles, templateEvidenceFiles)
	templateStageRoot, err := dataRoot.PrepareSubdirectory("asset-staging")
	if err != nil {
		return fail(err)
	}
	closers = append(closers, func() { _ = templateStageRoot.Close() })
	engine.SetTemplateStageDirectory(templateStageRoot.Path())
	templateCleanupCtx, cancelTemplateCleanup := context.WithTimeout(ctx, 20*time.Second)
	templateCleanupErr := engine.ReconcileTemplateFiles(templateCleanupCtx, time.Now())
	cancelTemplateCleanup()
	if templateCleanupErr != nil {
		return fail(fmt.Errorf("template file reconciliation failed: %w", templateCleanupErr))
	}
	closers = append(closers, engine.StartTemplateMaintenance(ctx))
	// P2-3 resident automation: cron scheduler beside the tool workspaces.
	// The headless executor is attached after the engine is fully wired so
	// scheduled runs reuse the single durable chat kernel.
	automationStore, closeAutomation, err := scheduler.OpenAutomationRepository(dataRoot.Path())
	if err != nil {
		return fail(err)
	}
	closers = append(closers, closeAutomation)
	if err := automationStore.RecoverInterrupted(); err != nil {
		return fail(fmt.Errorf("automation recovery failed: %w", err))
	}
	automationSched := scheduler.New(automationStore, nil, scheduler.NewPlatformNotifier())
	closers = append(closers, automationSched.Close)
	automationSched.SetExecutor(engine.AutomationHeadlessExecutor())
	automationSched.SetContextualExecutor(engine.AutomationHeadlessContextualExecutor())
	engine.SetAutomationScheduler(automationSched)
	terminalRoot, err := toolRoot.PrepareSubdirectory("terminals")
	if err != nil {
		return fail(err)
	}
	closers = append(closers, func() { _ = terminalRoot.Close() })
	terminals, err := terminalruntime.New(terminalruntime.Config{Workspace: terminalRoot.Path(), AuditPath: terminalRoot.Path() + string(os.PathSeparator) + "audit.jsonl", MaxSessions: 4})
	if err != nil {
		return fail(err)
	}
	engine.SetTerminalRuntime(terminals)
	closers = append(closers, func() { terminals.Shutdown() })
	// Attachment service: prepare a DACL-protected subdirectory for file
	// content, then wire the service into the engine (ADR-005 §7). File
	// content lives outside SQLite; only metadata and parsed text are stored
	// in the database.
	attachmentRoot, err := dataRoot.PrepareSubdirectory("attachments")
	if err != nil {
		return fail(err)
	}
	closers = append(closers, func() { _ = attachmentRoot.Close() })
	engine.SetupAttachmentService(store, attachmentapp.NewDirFileStorage(attachmentRoot.Path()))
	if err := engine.ReconcileAttachmentFileCleanup(ctx); err != nil {
		return fail(fmt.Errorf("attachment file cleanup reconciliation failed; engine not ready: %w", err))
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
	if err := CompactionRecoveryError(recoveryResults, recoveryErr); err != nil {
		return fail(fmt.Errorf("compaction restart recovery failed; engine not ready: %w", err))
	}

	// Start only after all dependencies and recovery are ready. Shutdown joins
	// automation before closing terminal, attachment, tool, and database owners.
	closers = append(closers, automationSched.Close)
	automationSched.Start(ctx)
	closers = append(closers, engine.StopPlanExecutions)
	closers = append(closers, engine.StopPeopleAgentReplies)
	engine.SetCompanionArchiveStore(store)
	closers = append(closers, engine.StartCompanionArchives(ctx))
	return engine, cleanup, nil
}
