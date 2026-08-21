package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sync"
	"time"

	"github.com/lunitide/lunitide/internal/agentorchestration"
	"github.com/lunitide/lunitide/internal/agentrunapp"
	"github.com/lunitide/lunitide/internal/artifactreview"
	"github.com/lunitide/lunitide/internal/attachmentapp"
	"github.com/lunitide/lunitide/internal/brapp"
	"github.com/lunitide/lunitide/internal/bridge"
	"github.com/lunitide/lunitide/internal/ccapp"
	"github.com/lunitide/lunitide/internal/compactionapp"
	"github.com/lunitide/lunitide/internal/contextapp"
	"github.com/lunitide/lunitide/internal/domain/attachment"
	"github.com/lunitide/lunitide/internal/domain/compaction"
	"github.com/lunitide/lunitide/internal/domain/handoff"
	"github.com/lunitide/lunitide/internal/domain/message"
	"github.com/lunitide/lunitide/internal/domain/project"
	"github.com/lunitide/lunitide/internal/domain/provider"
	"github.com/lunitide/lunitide/internal/domain/session"
	"github.com/lunitide/lunitide/internal/domain/stage"
	"github.com/lunitide/lunitide/internal/domain/token"
	"github.com/lunitide/lunitide/internal/gateway"
	"github.com/lunitide/lunitide/internal/handoffapp"
	"github.com/lunitide/lunitide/internal/m6app"
	"github.com/lunitide/lunitide/internal/m7app"
	"github.com/lunitide/lunitide/internal/m8app"
	"github.com/lunitide/lunitide/internal/m9app"
	"github.com/lunitide/lunitide/internal/mcapp"
	"github.com/lunitide/lunitide/internal/mcp6"
	"github.com/lunitide/lunitide/internal/messageapp"
	"github.com/lunitide/lunitide/internal/networkpolicy"
	"github.com/lunitide/lunitide/internal/providerapp"
	"github.com/lunitide/lunitide/internal/queueapp"
	"github.com/lunitide/lunitide/internal/scheduler"
	"github.com/lunitide/lunitide/internal/secret"
	"github.com/lunitide/lunitide/internal/secretlease"
	"github.com/lunitide/lunitide/internal/stageapp"
	"github.com/lunitide/lunitide/internal/terminalruntime"
	"github.com/lunitide/lunitide/internal/toolruntime"
	"github.com/lunitide/lunitide/internal/tts"
	"github.com/oklog/ulid/v2"
)

type ProviderService interface {
	Get(context.Context, string) (provider.Provider, error)
	List(context.Context, provider.Filter) ([]provider.Provider, error)
	CreateRequest(context.Context, string, string, any, provider.Provider) (provider.Provider, error)
	UpdateRequest(context.Context, string, string, any, string, int64, func(provider.Provider) (provider.Provider, error)) (provider.Provider, error)
	DeleteRequest(context.Context, string, string, any, string, int64) (provider.Provider, error)
}
type ProjectService interface {
	Create(context.Context, string, string, any, project.Project) (project.Project, error)
	List(context.Context, project.Filter) ([]project.Project, error)
	Get(context.Context, string) (project.Project, error)
	HasArtifacts(context.Context, string) (bool, error)
	Delete(context.Context, string) error
	Mutate(context.Context, string, string, string, string, int64, func(*project.Project) error) (project.Project, error)
}
type SessionService interface {
	Create(context.Context, string, string, any, session.Session) (session.Session, error)
	Update(context.Context, string, string, any, string, int64, string, bool) (session.Session, error)
	List(context.Context, session.Filter) ([]session.Session, error)
	Delete(context.Context, string) error
}
type MessageService interface {
	Append(context.Context, string, string, any, message.Message) (message.Message, error)
	AppendAssistant(context.Context, string, string, string, string, messageapp.AssistantUsage) (message.Message, error)
	List(context.Context, messageapp.PageRequest) (messageapp.Page, error)
}
type messageRewindService interface {
	Rewind(context.Context, string, string, string, string) (messageapp.RewindResult, error)
}
type StageService interface {
	Create(context.Context, string, string, any, stage.Stage) (stage.Stage, error)
	Update(context.Context, string, string, any, stageapp.UpdateInput) (stage.Stage, error)
	List(context.Context, stage.Filter) ([]stage.Stage, error)
}

type credentialLifecycleService interface {
	UpdateCredentialRequest(context.Context, string, string, any, string, int64, func(provider.Provider) (provider.Provider, error)) (provider.Provider, error)
	DeleteCoordinatedRequest(context.Context, string, string, any, string, int64, *secret.Ref) (provider.Provider, error)
	ClaimCredentialCleanup(context.Context, string, time.Time, time.Duration, int) ([]providerapp.ClaimedEvent, error)
	CompleteCredentialCleanup(context.Context, string, string, time.Time) error
	RetryCredentialCleanup(context.Context, string, string, time.Time, string) error
}

// CompactionSummaryReader reads the latest succeeded compaction summary for a session.
// Used by the context assembler to inject prior summaries into the model context.
type CompactionSummaryReader interface {
	GetLatestCompactionSummary(ctx context.Context, sessionID string) (string, error)
}

// compactionCoverageReader is the P2-2 hierarchical-context extension:
// implementers also answer the checkpoint's coverage end sequence so the
// assembler can stop projecting messages the summary already represents.
// Stores that do not implement it keep the flat (summary + full history)
// projection.
type compactionCoverageReader interface {
	GetLatestCompactionCheckpoint(ctx context.Context, sessionID string) (string, int64, error)
}
type electronCredentialMigrationService interface {
	PlanElectronCredentials(context.Context, []providerapp.ElectronCredentialTuple) ([]providerapp.ElectronCredentialPlan, error)
	AdoptElectronCredential(context.Context, string, providerapp.ElectronCredentialAdoption) (string, error)
	DispositionElectronCredential(context.Context, providerapp.ElectronCredentialTuple, string) error
}

type Engine struct {
	providers          ProviderService
	projects           ProjectService
	sessions           SessionService
	messages           MessageService
	stages             StageService
	planning           PlanningService
	governance         GovernanceService
	memories           MemoryService
	ontology           OntologyService
	skills             SkillService
	migration          MigrationService
	messageReader      contextapp.Reader
	tokenRepo          token.Repository
	compactionTrigger  *compactionapp.Trigger
	compactionExecutor *compactionapp.Executor
	summaryReader      CompactionSummaryReader
	handoffService     *handoffapp.Service
	attachmentService  *attachmentapp.Service
	version            string
	leases             LeaseClient
	network            networkpolicy.Options
	gateway            gateway.Options
	adapterFactory     func(context.Context, provider.Provider) (gateway.Adapter, error)
	adapterCacheMu     sync.Mutex
	adapterCache       map[string]gateway.Adapter
	browserLastURL     sync.Map
	streamsMu          sync.Mutex
	streams            map[string]*streamState
	maxStreams         int
	tools              *toolruntime.Runtime
	terminals          *terminalruntime.Runtime
	terminalsMu        sync.Mutex
	terminalOwners     map[string]*terminalOwner
	coordinator        *agentorchestration.Coordinator
	agentRuns          *agentrunapp.Service

	// M6 slice-1: extension supply chain + MCP endpoint gateway.
	m6ext         *m6app.ExtensionService
	mcp6Registry  *mcp6.Registry
	mcp6Endpoints *m6app.EndpointService

	// M6 slice-2: connector catalog + worker dispatch.
	m6catalog  *m6app.CatalogService
	m6dispatch *m6app.DispatchService

	// M6 slice-3: delegation fan-out/fan-in + join barriers.
	m6delegation *m6app.DelegationService
	m6barriers   *m6app.BarrierService

	// M6 slice-4: root-writer merge + final-tree gate + outbox.
	m6merge *m6app.MergeService

	// M7 slice-1: nine-stage versioned workflow backbone.
	m7workflow  *m7app.WorkflowService
	m7trace     *m7app.TraceService
	m7gate      *m7app.GateService
	m7review    *m7app.ReviewService
	m7release   *m7app.ReleaseService
	m7promotion *m7app.PromotionService
	m7update    *m7app.UpdateService
	m7subagent  *m7app.SubagentService
	m7toolgap   *m7app.ToolgapService
	m7mcp       *m7app.McpRuntimeService

	// P1-1: chat-layer subagent delegation tier (disabled/explicit/proactive).
	delegation delegationMode

	// M8 slice-1: governed long-term memory core.
	m8memory *m8app.MemoryService
	// M10: memory nomination workflow over the slice-1 core.
	m10nomination *m8app.NominationService
	// M10: memory operations (stats/facts/traces/growth/settings/export/purge).
	memoryOps *m8app.MemoryOpsService
	// M10: expert scenario cards over the FR-19 expert core.
	m10scenario *m8app.ScenarioService
	// M10: queued user input (run.queue* handlers).
	queue *queueapp.Service
	// M10 wave-3: MCP market surface (mc.* handlers).
	mcmarket *mcapp.Service
	// M10 wave-3: browser multi-mode surface (br.* handlers).
	brmulti *brapp.Service
	// M10 wave-4: computer-control surface (cc.* handlers + agent tools).
	ccctrl *ccapp.Service
	// M8 slice-2: versioned knowledge-base documents.
	m8kb *m8app.KBService
	// M8 slice-3/5: handoff, tombstone and device sync.
	m8handoff *m8app.HandoffService
	// M8 slice-4: workflow bundle dispatch projection.
	m8automation *m8app.AutomationService
	// M8 FR-18: unified plugin bundle runtime.
	m8plugin *m8app.PluginService
	// M8 FR-19: expert center and project-phase mounting.
	m8expert *m8app.ExpertService
	// Session-scoped expert collaboration pack (durable mounts).
	sessionExperts sessionExpertStore

	// M8 FR-17: the write-collaboration evaluation gate (default disabled).
	m8gate *m8app.CollabGateService

	// M9.5 Moon Companion: offline SAPI text-to-speech runtime.
	m9tts *tts.Service

	// M9 slice-1: org-admin bridge service (org.* methods, T-9.1.3).
	m9org *m9app.OrgAdminService

	// M6 S5C: governed skill import + complexity routing (0053).
	m6skills  *m6app.SkillImportService
	m6routing *m6app.RoutingService

	// P2-2: append-only artifact acceptance log (comment → revise → accept).
	artifactReviews *artifactreview.Store

	// P2-3: resident cron automation scheduler.
	automation *scheduler.Scheduler

	// Asset template library and project deliverables (migration 0082).
	assets                 AssetTemplateStore
	deliverables           DeliverableStore
	projectAttachments     ProjectAttachmentStore
	projectAttachmentFiles attachmentapp.FileStorage
}
type terminalOwner struct {
	emit     EventEmitter
	sequence uint64
}

type streamState struct {
	cancel    context.CancelFunc
	state     streamLifecycle
	companion bool
}

type streamLifecycle uint8

const (
	streamRunning streamLifecycle = iota
	streamCancelling
	// streamFinalizing means successful upstream completion claimed the right
	// to perform any durable assistant write and choose the terminal event.
	streamFinalizing
	streamTerminal
)

type LeaseClient interface {
	WithLease(context.Context, secretlease.Request, func([]byte) error) error
}

type runtimeHandler func(*Engine, context.Context, bridge.Request) bridge.Response

// RuntimeHandlers is both the runtime allow-list and the dispatch table.
// Contract tests compare its non-nil handlers with the public schema.
var RuntimeHandlers = map[bridge.Method]runtimeHandler{
	bridge.MethodAgentRunCancel:                handleAgentRunCancel,
	bridge.MethodAgentRunGet:                   handleAgentRunGet,
	bridge.MethodAgentRunReconcile:             handleAgentRunReconcile,
	bridge.MethodAgentRunResume:                handleAgentRunResume,
	bridge.MethodAgentRunStart:                 handleAgentRunStart,
	bridge.MethodCapabilityList:                handleCapabilityList,
	bridge.MethodChangesetApply:                handleChangesetApply,
	bridge.MethodChangesetPreview:              handleChangesetPreview,
	bridge.MethodChangesetRevert:               handleChangesetRevert,
	bridge.MethodCommandCancel:                 handleCommandCancel,
	bridge.MethodCommandGet:                    handleCommandGet,
	bridge.MethodCommandReviewRequest:          handleCommandReviewRequest,
	bridge.MethodCommandStart:                  handleCommandStart,
	bridge.MethodEvidenceList:                  handleEvidenceList,
	bridge.MethodFsGlob:                        handleFsGlob,
	bridge.MethodFsGrep:                        handleFsGrep,
	bridge.MethodFsRead:                        handleFsRead,
	bridge.MethodFsReadMany:                    handleFsReadMany,
	bridge.MethodFsStat:                        handleFsStat,
	bridge.MethodFsTree:                        handleFsTree,
	bridge.MethodReviewDecide:                  handleReviewDecide,
	bridge.MethodRunPlanPut:                    handleRunPlanPut,
	bridge.MethodWebFetch:                      handleWebFetch,
	bridge.MethodWebSearch:                     handleWebSearch,
	bridge.MethodWorkspaceGrant:                handleWorkspaceGrant,
	bridge.MethodWorkspaceLease:                handleWorkspaceLease,
	bridge.MethodWorkspaceRegister:             handleWorkspaceRegister,
	bridge.MethodBrowserAct:                    handleBrowserAct,
	bridge.MethodMcpInvoke:                     handleMcpInvoke,
	bridge.MethodRunSend:                       handleRunSend,
	bridge.MethodRunCancel:                     handleRunCancel,
	bridge.MethodRunQueueInput:                 handleRunQueueInput,
	bridge.MethodRunQueueList:                  handleRunQueueList,
	bridge.MethodRunQueueWithdraw:              handleRunQueueWithdraw,
	bridge.MethodRunQueueConsume:               handleRunQueueConsume,
	bridge.MethodMcMarketList:                  handleMcMarketList,
	bridge.MethodMcMarketDetail:                handleMcMarketDetail,
	bridge.MethodMcConfigValidate:              handleMcConfigValidate,
	bridge.MethodMcConfirmToken:                handleMcConfirmToken,
	bridge.MethodMcConnectorInstall:            handleMcConnectorInstall,
	bridge.MethodMcConnectorUninstall:          handleMcConnectorUninstall,
	bridge.MethodMcConnectorUpdate:             handleMcConnectorUpdate,
	bridge.MethodMcConnectorUsage:              handleMcConnectorUsage,
	bridge.MethodMcTombstoneCheck:              handleMcTombstoneCheck,
	bridge.MethodBrSettingsGet:                 handleBrSettingsGet,
	bridge.MethodBrSettingsUpdate:              handleBrSettingsUpdate,
	bridge.MethodBrModeDetect:                  handleBrModeDetect,
	bridge.MethodBrSessionConnect:              handleBrSessionConnect,
	bridge.MethodBrSessionList:                 handleBrSessionList,
	bridge.MethodBrSessionDisconnect:           handleBrSessionDisconnect,
	bridge.MethodBrNavigate:                    handleBrNavigate,
	bridge.MethodBrDataUsage:                   handleBrDataUsage,
	bridge.MethodBrDataClear:                   handleBrDataClear,
	bridge.MethodBrPermissionList:              handleBrPermissionList,
	bridge.MethodBrPermissionRequest:           handleBrPermissionRequest,
	bridge.MethodBrPermissionDecide:            handleBrPermissionDecide,
	bridge.MethodBrPermissionPolicy:            handleBrPermissionPolicy,
	bridge.MethodCcGetConfig:                   handleCcGetConfig,
	bridge.MethodCcUpdateConfig:                handleCcUpdateConfig,
	bridge.MethodCcGetAuditLog:                 handleCcGetAuditLog,
	bridge.MethodCcEmergencyStop:               handleCcEmergencyStop,
	bridge.MethodWorkspaceConvert:              handleWorkspaceConvert,
	bridge.MethodExtensionSearch:               handleExtensionSearch,
	bridge.MethodExtensionInstall:              handleExtensionInstall,
	bridge.MethodExtensionLifecycle:            handleExtensionLifecycle,
	bridge.MethodMcp6Register:                  handleMcp6Register,
	bridge.MethodMcp6Invoke:                    handleMcp6Invoke,
	bridge.MethodMcp6Revoke:                    handleMcp6Revoke,
	bridge.MethodMcp6PresetsList:               handleMcp6PresetsList,
	bridge.MethodToolsCommandPolicyGet:         handleToolsCommandPolicyGet,
	bridge.MethodToolsCommandPolicySet:         handleToolsCommandPolicySet,
	bridge.MethodToolsHooksPolicyGet:           handleToolsHooksPolicyGet,
	bridge.MethodToolsHooksPolicySet:           handleToolsHooksPolicySet,
	bridge.MethodToolsHooksEventsList:          handleToolsHooksEventsList,
	bridge.MethodWorkspaceArtifactReviewAppend: handleWorkspaceArtifactReviewAppend,
	bridge.MethodWorkspaceArtifactReviewList:   handleWorkspaceArtifactReviewList,
	bridge.MethodWorkspaceArtifactPreview:      handleWorkspaceArtifactPreview,
	bridge.MethodWorkspaceArtifactExport:       handleWorkspaceArtifactExport,
	bridge.MethodAutomationJobList:             handleAutomationJobList,
	bridge.MethodAutomationJobSet:              handleAutomationJobSet,
	bridge.MethodAutomationJobDelete:           handleAutomationJobDelete,
	bridge.MethodAutomationJobTrigger:          handleAutomationJobTrigger,
	bridge.MethodAutomationRunList:             handleAutomationRunList,
	bridge.MethodAutomationStatus:              handleAutomationStatus,
	bridge.MethodConnectorSnapshot:             handleConnectorSnapshot,
	bridge.MethodWorkerDispatch:                handleWorkerDispatch,
	bridge.MethodDelegationCreate:              handleDelegationCreate,
	bridge.MethodDelegationSettle:              handleDelegationSettle,
	bridge.MethodBarrierArrive:                 handleBarrierArrive,
	bridge.MethodMergeSubmit:                   handleMergeSubmit,
	bridge.MethodComplexityDecide:              handleComplexityDecide,
	bridge.MethodOpenapiParse:                  handleOpenapiParse,
	bridge.MethodSkillImportApprove:            handleSkillImportApprove,
	bridge.MethodSkillImportDiscover:           handleSkillImportDiscover,
	bridge.MethodSkillImportInspect:            handleSkillImportInspect,
	bridge.MethodSkillImportReject:             handleSkillImportReject,
	bridge.MethodSkillImportRevoke:             handleSkillImportRevoke,
	bridge.MethodSkillImportSubmit:             handleSkillImportSubmit,
	bridge.MethodAttachmentDelete:              handleAttachmentDelete,
	bridge.MethodAttachmentGet:                 handleAttachmentGet,
	bridge.MethodAttachmentIngest:              handleAttachmentIngest,
	bridge.MethodAttachmentList:                handleAttachmentList,
	bridge.MethodAttachmentUploadAbort:         handleAttachmentUploadAbort,
	bridge.MethodAttachmentUploadBegin:         handleAttachmentUploadBegin,
	bridge.MethodAttachmentUploadChunk:         handleAttachmentUploadChunk,
	bridge.MethodAttachmentUploadCommit:        handleAttachmentUploadCommit,
	bridge.MethodChatStart:                     handleChatStart,
	bridge.MethodChatToolApprove:               handleChatToolApprove,
	bridge.MethodContextStatus:                 handleContextStatus,
	bridge.MethodContextCompactPreview:         handleContextCompactPreview,
	bridge.MethodContextCompactCommit:          handleContextCompactCommit,
	bridge.MethodContextCompactCancel:          handleContextCompactCancel,
	bridge.MethodContextHandoffCreate:          handleContextHandoffCreate,
	bridge.MethodContextHandoffImport:          handleContextHandoffImport,
	bridge.MethodContextHandoffInspect:         handleContextHandoffInspect,
	bridge.MethodContextHandoffList:            handleContextHandoffList,
	bridge.MethodContextHandoffListImports:     handleContextHandoffListImports,
	bridge.MethodContextHandoffRevoke:          handleContextHandoffRevoke,
	bridge.MethodStreamCancel:                  handleStreamCancel,
	bridge.MethodSystemHealth:                  handleSystemHealth,
	bridge.MethodProviderCreate:                handleProviderCreate,
	bridge.MethodProviderDelete:                handleProviderDelete,
	bridge.MethodProviderGet:                   handleProviderGet,
	bridge.MethodProviderList:                  handleProviderList,
	bridge.MethodProviderModelSync:             handleProviderModelSync,
	bridge.MethodProviderTest:                  handleProviderTest,
	bridge.MethodProviderUpdate:                handleProviderUpdate,
	bridge.MethodProjectCreate:                 handleProjectCreate,
	bridge.MethodProjectDelete:                 handleProjectDelete,
	bridge.MethodProjectList:                   handleProjectList,
	bridge.MethodProjectUpdate:                 handleProjectUpdate,
	bridge.MethodProjectPublish:                handleProjectPublish,
	bridge.MethodProjectClose:                  handleProjectClose,
	bridge.MethodProjectReopen:                 handleProjectReopen,
	bridge.MethodProjectAdvanceStatus:          handleProjectAdvanceStatus,
	bridge.MethodProjectAttachmentIngest:       handleProjectAttachmentIngest,
	bridge.MethodProjectAttachmentList:         handleProjectAttachmentList,
	bridge.MethodSessionCreate:                 handleSessionCreate,
	bridge.MethodSessionDelete:                 handleSessionDelete,
	bridge.MethodSessionExpertsGet:             handleSessionExpertsGet,
	bridge.MethodSessionExpertsSet:             handleSessionExpertsSet,
	bridge.MethodSessionList:                   handleSessionList,
	bridge.MethodSessionUpdate:                 handleSessionUpdate,
	bridge.MethodMessageAppend:                 handleMessageAppend,
	bridge.MethodMessageList:                   handleMessageList,
	bridge.MethodMessageRewind:                 handleMessageRewind,
	bridge.MethodStageCreate:                   handleStageCreate,
	bridge.MethodStageList:                     handleStageList,
	bridge.MethodStageUpdate:                   handleStageUpdate,
	bridge.MethodPlanGet:                       handlePlanGet,
	bridge.MethodPlanList:                      handlePlanList,
	bridge.MethodPlanCreate:                    handlePlanCreate,
	bridge.MethodPlanActivate:                  handlePlanActivate,
	bridge.MethodPlanComplete:                  handlePlanComplete,
	bridge.MethodPlanPause:                     handlePlanPause,
	bridge.MethodPlanResume:                    handlePlanResume,
	bridge.MethodPlanTodoCreate:                handlePlanTodoCreate,
	bridge.MethodPlanRunStart:                  handlePlanRunStart,
	bridge.MethodPlanRunTree:                   handlePlanRunTree,
	bridge.MethodPlanRunSpawn:                  handlePlanRunSpawn,
	bridge.MethodPlanRunJoin:                   handlePlanRunJoin,
	bridge.MethodPlanRunCancel:                 handlePlanRunCancel,
	bridge.MethodNodeList:                      handleNodeList,
	bridge.MethodNodeCreate:                    handleNodeCreate,
	bridge.MethodNodeStart:                     handleNodeStart,
	bridge.MethodNodeComplete:                  handleNodeComplete,
	bridge.MethodNodeFail:                      handleNodeFail,
	bridge.MethodReviewList:                    handleReviewList,
	bridge.MethodReviewApprove:                 handleReviewApprove,
	bridge.MethodReviewReject:                  handleReviewReject,
	bridge.MethodMemoryGet:                     handleMemoryGet,
	bridge.MethodMemoryList:                    handleMemoryList,
	bridge.MethodMemorySearch:                  handleMemorySearch,
	bridge.MethodMemoryCreate:                  handleMemoryCreate,
	bridge.MethodMemoryUpdate:                  handleMemoryUpdate,
	bridge.MethodMemoryDelete:                  handleMemoryDelete,
	bridge.MethodOntologyNodeGet:               handleOntologyNodeGet,
	bridge.MethodOntologyNodeList:              handleOntologyNodeList,
	bridge.MethodOntologyNodeSearch:            handleOntologyNodeSearch,
	bridge.MethodOntologyNodeCreate:            handleOntologyNodeCreate,
	bridge.MethodOntologyNodeUpdate:            handleOntologyNodeUpdate,
	bridge.MethodOntologyNodeDelete:            handleOntologyNodeDelete,
	bridge.MethodOntologyEdgeList:              handleOntologyEdgeList,
	bridge.MethodOntologyEdgeCreate:            handleOntologyEdgeCreate,
	bridge.MethodOntologyEdgeUpdate:            handleOntologyEdgeUpdate,
	bridge.MethodOntologyEdgeDelete:            handleOntologyEdgeDelete,
	bridge.MethodSkillGet:                      handleSkillGet,
	bridge.MethodSkillInvoke:                   handleSkillInvoke,
	bridge.MethodSkillExecute:                  handleSkillExecute,
	bridge.MethodSkillList:                     handleSkillList,
	bridge.MethodSkillCategorySet:              handleSkillCategorySet,
	bridge.MethodSkillMatch:                    handleSkillMatch,
	bridge.MethodSkillCreate:                   handleSkillCreate,
	bridge.MethodSkillUpdate:                   handleSkillUpdate,
	bridge.MethodSkillDelete:                   handleSkillDelete,
	bridge.MethodSkillPublish:                  handleSkillPublish,
	bridge.MethodSkillDeprecate:                handleSkillDeprecate,
	bridge.MethodSkillDisable:                  handleSkillDisable,
	bridge.MethodSkillCatalogList:              handleSkillCatalogList,
	bridge.MethodSkillInstall:                  handleSkillInstall,
	bridge.MethodMigrationInspect:              handleMigrationInspect,
	bridge.MethodMigrationRun:                  handleMigrationRun,
	bridge.MethodMigrationStatus:               handleMigrationStatus,
	bridge.MethodTerminalStart:                 handleTerminalStart,
	bridge.MethodTerminalInput:                 handleTerminalInput,
	bridge.MethodTerminalResize:                handleTerminalResize,
	bridge.MethodTerminalClose:                 handleTerminalClose,
	bridge.MethodTemplateCreate:                handleTemplateCreate,
	bridge.MethodTemplateDelete:                handleTemplateDelete,
	bridge.MethodTemplateEnable:                handleTemplateEnable,
	bridge.MethodTemplateList:                  handleTemplateList,
	bridge.MethodTemplateVoid:                  handleTemplateVoid,
	bridge.MethodWorkflowCaptureInput:          handleWorkflowCaptureInput,
	bridge.MethodWorkflowCreateVersion:         handleWorkflowCreateVersion,
	bridge.MethodWorkflowPublish:               handleWorkflowPublish,
	bridge.MethodWorkflowStartStage:            handleWorkflowStartStage,
	bridge.MethodWorkflowTransitionStage:       handleWorkflowTransitionStage,
	bridge.MethodDevTaskCreate:                 handleDevTaskCreate,
	bridge.MethodDevTaskTransition:             handleDevTaskTransition,
	bridge.MethodEvidenceAttachScan:            handleEvidenceAttachScan,
	bridge.MethodEvidenceAttachTest:            handleEvidenceAttachTest,
	bridge.MethodReviewSubmit:                  handleReviewSubmit,
	bridge.MethodTraceAddEdge:                  handleTraceAddEdge,
	bridge.MethodTraceMarkStale:                handleTraceMarkStale,
	bridge.MethodTraceQuery:                    handleTraceQuery,
	bridge.MethodTraceResolveStale:             handleTraceResolveStale,
	bridge.MethodWorkflowCreateCheckpoint:      handleWorkflowCreateCheckpoint,
	bridge.MethodWorkflowEvaluateGate:          handleWorkflowEvaluateGate,
	bridge.MethodReleaseBuildPackage:           handleReleaseBuildPackage,
	bridge.MethodReleaseCreateRevision:         handleReleaseCreateRevision,
	bridge.MethodReleaseGetPackage:             handleReleaseGetPackage,
	bridge.MethodReleaseGetRevision:            handleReleaseGetRevision,
	bridge.MethodReleaseGetPromotion:           handleReleaseGetPromotion,
	bridge.MethodReleasePromote:                handleReleasePromote,
	bridge.MethodReleaseRollback:               handleReleaseRollback,
	bridge.MethodAppUpdateCheck:                handleAppUpdateCheck,
	bridge.MethodAppUpdateInstall:              handleAppUpdateInstall,
	bridge.MethodArchivePack:                   handleArchivePack,
	bridge.MethodArchiveUnpack:                 handleArchiveUnpack,
	bridge.MethodDbQuery:                       handleDbQuery,
	bridge.MethodDeliverableConfirmGate:        handleDeliverableConfirmGate,
	bridge.MethodDeliverableList:               handleDeliverableList,
	bridge.MethodDeliverableUpsert:             handleDeliverableUpsert,
	bridge.MethodDocumentParse:                 handleDocumentParse,
	bridge.MethodGitRead:                       handleGitRead,
	bridge.MethodHttpDownload:                  handleHttpDownload,
	bridge.MethodHttpRequest:                   handleHttpRequest,
	bridge.MethodMcpAdd:                        handleMcpAdd,
	bridge.MethodMcpHealth:                     handleMcpHealth,
	bridge.MethodMcpList:                       handleMcpList,
	bridge.MethodMcpMarketSearch:               handleMcpMarketSearch,
	bridge.MethodMcpToggle:                     handleMcpToggle,
	bridge.MethodSubagentJoin:                  handleSubagentJoin,
	bridge.MethodSubagentSpawn:                 handleSubagentSpawn,
	bridge.MethodSubagentTree:                  handleSubagentTree,
	bridge.MethodMemoryConfirmCandidate:        handleMemoryConfirmCandidate,
	bridge.MethodMemoryExport:                  handleMemoryExport,
	bridge.MethodMemoryFactsFlag:               handleMemoryFactsFlag,
	bridge.MethodMemoryFactsList:               handleMemoryFactsList,
	bridge.MethodMemoryGrowthDecide:            handleMemoryGrowthDecide,
	bridge.MethodMemoryGrowthList:              handleMemoryGrowthList,
	bridge.MethodMemoryNominate:                handleMemoryNominate,
	bridge.MethodMemoryNominationList:          handleMemoryNominationList,
	bridge.MethodMemoryNominationWithdraw:      handleMemoryNominationWithdraw,
	bridge.MethodMemoryPurge:                   handleMemoryPurge,
	bridge.MethodMemorySettingsGet:             handleMemorySettingsGet,
	bridge.MethodMemorySettingsUpdate:          handleMemorySettingsUpdate,
	bridge.MethodMemoryStats:                   handleMemoryOpsStats,
	bridge.MethodMemoryTracesList:              handleMemoryTracesList,
	bridge.MethodRecallQuery:                   handleRecallQuery,
	bridge.MethodFeedbackRecord:                handleFeedbackRecord,
	bridge.MethodFeedbackCandidates:            handleFeedbackCandidates,
	bridge.MethodKbUpsertDocument:              handleKBUpsertDocument,
	bridge.MethodHandoffAccept:                 handleHandoffAccept,
	bridge.MethodTombstoneDelete:               handleTombstoneDelete,
	bridge.MethodAutomationDispatch:            handleAutomationDispatch,
	bridge.MethodSyncPush:                      handleSyncPush,
	bridge.MethodPluginInstall:                 handlePluginInstall,
	bridge.MethodPluginList:                    handlePluginList,
	bridge.MethodPluginToggle:                  handlePluginToggle,
	bridge.MethodPluginUpgrade:                 handlePluginUpgrade,
	bridge.MethodPluginUninstall:               handlePluginUninstall,
	bridge.MethodPluginDevCreate:               handlePluginDevCreate,
	bridge.MethodPluginMarketSearch:            handlePluginMarketSearch,
	bridge.MethodPluginMarketDetail:            handlePluginMarketDetail,
	bridge.MethodExpertCreate:                  handleExpertCreate,
	bridge.MethodExpertCatalogList:             handleExpertCatalogList,
	bridge.MethodExpertInstall:                 handleExpertInstall,
	bridge.MethodExpertList:                    handleExpertList,
	bridge.MethodExpertDetail:                  handleExpertDetail,
	bridge.MethodExpertUpdate:                  handleExpertUpdate,
	bridge.MethodExpertToggle:                  handleExpertToggle,
	bridge.MethodExpertArchive:                 handleExpertArchive,
	bridge.MethodExpertMount:                   handleExpertMount,
	bridge.MethodExpertMountingGet:             handleExpertMountingGet,
	bridge.MethodExpertScenarioCreate:          handleExpertScenarioCreate,
	bridge.MethodExpertScenarioList:            handleExpertScenarioList,
	bridge.MethodExpertScenarioDelete:          handleExpertScenarioDelete,
	bridge.MethodCollabGateEvaluate:            handleCollabGateEvaluate,
	bridge.MethodCollabGateStatus:              handleCollabGateStatus,
	bridge.MethodCollabGateConfirm:             handleCollabGateConfirm,
	bridge.MethodTtsVoices:                     handleTtsVoices,
	bridge.MethodTtsSynthesize:                 handleTtsSynthesize,
	bridge.MethodTtsCancel:                     handleTtsCancel,
	bridge.MethodTtsRefAudios:                  handleTtsRefAudios,
	bridge.MethodTtsEnsureRefEngine:            handleTtsEnsureRefEngine,
	bridge.MethodOrgSummary:                    handleOrgSummary,
	bridge.MethodOrgCreate:                     handleOrgCreate,
	bridge.MethodOrgSwitch:                     handleOrgSwitch,
	bridge.MethodOrgActivate:                   handleOrgActivate,
	bridge.MethodOrgSuspend:                    handleOrgSuspend,
	bridge.MethodOrgSpaceList:                  handleOrgSpaceList,
	bridge.MethodOrgSpaceCreate:                handleOrgSpaceCreate,
	bridge.MethodOrgMemberList:                 handleOrgMemberList,
	bridge.MethodOrgMemberInvite:               handleOrgMemberInvite,
	bridge.MethodOrgMemberRevoke:               handleOrgMemberRevoke,
}

var internalRuntimeHandlers = map[bridge.Method]runtimeHandler{
	bridge.Method("internal.provider.create.with-credential"):      handleProviderCreateWithCredential,
	bridge.Method("internal.provider.update.with-credential"):      handleProviderUpdateWithCredential,
	bridge.Method("internal.provider.resolve"):                     handleProviderResolve,
	bridge.Method("internal.provider.credential-adoption.resolve"): handleCredentialAdoptionResolve,
	bridge.Method("internal.provider.delete.coordinated"):          handleProviderDeleteCoordinated,
	bridge.Method("internal.credential-cleanup.claim"):             handleCredentialCleanupClaim,
	bridge.Method("internal.credential-cleanup.complete"):          handleCredentialCleanupComplete,
	bridge.Method("internal.credential-cleanup.retry"):             handleCredentialCleanupRetry,
	bridge.Method("internal.provider.credential-binding.resolve"):  handleCredentialBindingResolve,
	bridge.Method("internal.electron-credential.plan"):             handleElectronCredentialPlan,
	bridge.Method("internal.electron-credential.adopt"):            handleElectronCredentialAdopt,
	bridge.Method("internal.electron-credential.disposition"):      handleElectronCredentialDisposition,
}

type providerDTO struct {
	ID              string                   `json:"id"`
	Name            string                   `json:"name"`
	Protocol        provider.Protocol        `json:"protocol"`
	BaseURL         string                   `json:"baseUrl"`
	Models          []provider.Model         `json:"models"`
	Status          provider.Status          `json:"status"`
	CredentialState provider.CredentialState `json:"credentialState"`
	CreatedAt       time.Time                `json:"createdAt"`
	UpdatedAt       time.Time                `json:"updatedAt"`
	Version         int64                    `json:"version"`
}

func NewEngine(providers ProviderService, version string) *Engine {
	return &Engine{providers: providers, version: version, streams: make(map[string]*streamState), maxStreams: 32, adapterCache: make(map[string]gateway.Adapter)}
}

func NewEngineWithProjects(providers ProviderService, projects ProjectService, version string, leases LeaseClient) *Engine {
	e := NewEngineWithGateway(providers, version, leases)
	e.projects = projects
	return e
}

func NewEngineWithSessions(providers ProviderService, projects ProjectService, sessions SessionService, version string, leases LeaseClient) *Engine {
	e := NewEngineWithProjects(providers, projects, version, leases)
	e.sessions = sessions
	return e
}

func NewEngineWithMessages(providers ProviderService, projects ProjectService, sessions SessionService, messages MessageService, version string, leases LeaseClient) *Engine {
	e := NewEngineWithSessions(providers, projects, sessions, version, leases)
	e.messages = messages
	return e
}

// NewEngineWithContextReader builds an Engine that can assemble durable
// session context for chat via the contextapp.Reader and token.Repository.
func NewEngineWithContextReader(providers ProviderService, projects ProjectService, sessions SessionService, messages MessageService, messageReader contextapp.Reader, tokenRepo token.Repository, version string, leases LeaseClient) *Engine {
	e := NewEngineWithMessages(providers, projects, sessions, messages, version, leases)
	e.messageReader = messageReader
	e.tokenRepo = tokenRepo
	return e
}

// NewEngineWithStages wires the stage service alongside the full context reader
// stack used by production.
func NewEngineWithStages(providers ProviderService, projects ProjectService, sessions SessionService, messages MessageService, stages StageService, messageReader contextapp.Reader, tokenRepo token.Repository, version string, leases LeaseClient) *Engine {
	e := NewEngineWithContextReader(providers, projects, sessions, messages, messageReader, tokenRepo, version, leases)
	e.stages = stages
	return e
}

// NewEngineWithP3P4 wires the P3/P4 services (planning, governance, memory,
// ontology, skills) on top of the full production stack.
func NewEngineWithP3P4(providers ProviderService, projects ProjectService, sessions SessionService, messages MessageService, stages StageService, planning PlanningService, governance GovernanceService, memories MemoryService, ontology OntologyService, skills SkillService, messageReader contextapp.Reader, tokenRepo token.Repository, version string, leases LeaseClient) *Engine {
	e := NewEngineWithStages(providers, projects, sessions, messages, stages, messageReader, tokenRepo, version, leases)
	e.planning = planning
	e.governance = governance
	e.memories = memories
	e.ontology = ontology
	e.skills = skills
	return e
}

// SetMigrationService wires the migration service into the engine.
func (e *Engine) SetMigrationService(m MigrationService) { e.migration = m }

func (e *Engine) SetToolRuntime(r *toolruntime.Runtime) { e.tools = r }

// SetArtifactReviewStore wires the P2-2 artifact acceptance log.
func (e *Engine) SetArtifactReviewStore(s *artifactreview.Store) { e.artifactReviews = s }

// SetAutomationScheduler wires the P2-3 resident cron scheduler. When the
// scheduler carries no executor yet (engine not ready), a later
// StartAutomationScheduler call attaches the headless chat executor.
func (e *Engine) SetAutomationScheduler(s *scheduler.Scheduler) { e.automation = s }

func (e *Engine) SetTerminalRuntime(r *terminalruntime.Runtime) {
	e.terminalsMu.Lock()
	e.terminals = r
	e.terminalOwners = make(map[string]*terminalOwner)
	e.terminalsMu.Unlock()
	if r != nil {
		go e.forwardTerminalEvents(r.Events())
	}
}

// SetCompactionServices wires the compaction trigger, executor, and summary reader
// into the engine. When set, chat.start will check token usage and automatically
// trigger compaction when the high watermark is exceeded. The assembler will also
// inject the latest succeeded checkpoint summary into the model context.
func (e *Engine) SetCompactionServices(trigger *compactionapp.Trigger, executor *compactionapp.Executor, summaryReader CompactionSummaryReader) {
	e.compactionTrigger = trigger
	e.compactionExecutor = executor
	e.summaryReader = summaryReader
	executor.SetTrigger(trigger)
}

// TriggerManualCompaction creates a manual compaction checkpoint for the
// specified source range and executes it synchronously. This is the user-facing
// entry point for manual compaction (ADR-005 §4: "Add checkpoint schema, state
// machine, source digests and manual compaction").
//
// The caller specifies the message sequence range [startSeq, endSeq] to compact.
// The method:
//  1. Creates a pending checkpoint with TriggerManual and source digest.
//  2. Executes the checkpoint (transitions pending → running → succeeded/failed).
//  3. Returns the execution result.
//
// Manual compaction never deletes source messages (ADR-005 §1). If a compaction
// is already in progress, the method returns an error.
func (e *Engine) TriggerManualCompaction(ctx context.Context, sessionID, provider, model string, startSeq, endSeq int64) (compactionapp.ManualTriggerResult, compactionapp.ExecuteResult, error) {
	if e.compactionTrigger == nil || e.compactionExecutor == nil {
		return compactionapp.ManualTriggerResult{}, compactionapp.ExecuteResult{}, errors.New("compaction services not configured")
	}

	triggerResult, err := e.compactionTrigger.TriggerManual(ctx, sessionID, provider, model, startSeq, endSeq)
	if err != nil {
		return triggerResult, compactionapp.ExecuteResult{}, err
	}
	if !triggerResult.Triggered {
		return triggerResult, compactionapp.ExecuteResult{}, nil
	}

	execResult, err := e.compactionExecutor.Execute(ctx, triggerResult.CheckpointID)
	return triggerResult, execResult, err
}

// RecoverCompaction reconciles orphaned compaction checkpoints left in pending
// or running state by a previous process crash. This is the restart recovery
// entry point (ADR-005 §5) and must be called once at engine startup before
// serving traffic.
//
// Recovery policy:
//   - Running checkpoints are marked failed with code "INTERRUPTED_BY_RESTART".
//   - Pending checkpoints are re-executed synchronously.
//
// Returns one RecoveryResult per orphaned checkpoint. If compaction services
// are not configured, returns nil with no error (no-op).
func (e *Engine) RecoverCompaction(ctx context.Context) ([]compactionapp.RecoveryResult, error) {
	if e.compactionExecutor == nil {
		return nil, nil
	}
	return e.compactionExecutor.RecoverOrphanedCheckpoints(ctx)
}

// ContextStatusResult describes the current context state for a session.
type ContextStatusResult struct {
	CanonicalLogicalTokens     int64   `json:"canonicalLogicalTokens"`
	CanonicalTokenizerID       string  `json:"canonicalTokenizerId"`
	CanonicalTokenizerRevision string  `json:"canonicalTokenizerRevision"`
	ModelContextWindow         int64   `json:"modelContextWindow"`
	ActiveCheckpointVersion    int64   `json:"activeCheckpointVersion"`
	BudgetUsage                float64 `json:"budgetUsage"`
	IsCompacting               bool    `json:"isCompacting"`
}

// ContextStatus returns the current context state for a session, including
// canonical logical tokens, estimated request tokens, model context window,
// active checkpoint version, budget usage ratio, and whether compaction is
// in progress (ADR-005 §4.2).
func (e *Engine) ContextStatus(ctx context.Context, sessionID string) (ContextStatusResult, error) {
	result := ContextStatusResult{}

	// Get the latest checkpoint to determine provider/model and compaction state.
	var latest *compaction.Checkpoint
	if e.compactionTrigger != nil {
		var err error
		latest, err = e.compactionTrigger.GetLatestCheckpoint(ctx, sessionID)
		if err != nil {
			return result, err
		}
	}

	// Active checkpoint version (0 if no succeeded checkpoint).
	if latest != nil && latest.Status == compaction.StatusSucceeded {
		result.ActiveCheckpointVersion = latest.Version
	}

	// IsCompacting: true if latest checkpoint is pending or running.
	if latest != nil && (latest.Status == compaction.StatusPending || latest.Status == compaction.StatusRunning) {
		result.IsCompacting = true
	}

	result.CanonicalTokenizerID = token.CanonicalTokenizerID
	result.CanonicalTokenizerRevision = token.CanonicalTokenizerRevision

	// Canonical logical usage is independent of the latest provider/model and is
	// available even before the first compaction checkpoint exists.
	if e.tokenRepo != nil {
		usage, err := e.tokenRepo.SumTokenLedgerBySession(ctx, sessionID, "", "", token.CanonicalTokenizerRevision)
		if err != nil {
			return result, err
		}
		result.CanonicalLogicalTokens = usage
	}

	// Context window comes from the latest checkpoint's provider/model.
	if latest != nil {
		// Look up context window from provider model config.
		if e.providers != nil {
			providers, err := e.providers.List(ctx, provider.Filter{})
			if err == nil {
				for _, p := range providers {
					if string(p.Protocol) == latest.Provider {
						for _, m := range p.Models {
							if m.ModelID == latest.Model && m.ContextWindow > 0 {
								result.ModelContextWindow = m.ContextWindow
								break
							}
						}
						break
					}
				}
			}
		}
	}

	// Budget usage: usage / (contextWindow * highWatermark).
	if result.ModelContextWindow > 0 {
		budget := float64(result.ModelContextWindow) * 0.80
		if budget > 0 {
			result.BudgetUsage = float64(result.CanonicalLogicalTokens) / budget
			if result.BudgetUsage > 1 {
				result.BudgetUsage = 1
			}
		}
	}

	return result, nil
}

// CompactPreview generates a draft compaction checkpoint for the session and
// returns the summary preview without changing the active checkpoint
// (ADR-005 §4.2). The checkpoint is executed to succeeded status; Commit must
// be called to confirm activation.
func (e *Engine) CompactPreview(ctx context.Context, sessionID, providerID, modelID, tokenizerRevision string, contextWindow int64) (*compactionapp.PreviewResult, error) {
	if e.compactionExecutor == nil {
		return nil, errors.New("compaction services not configured")
	}
	return e.compactionExecutor.Preview(ctx, sessionID, providerID, modelID, tokenizerRevision, contextWindow)
}

// CompactCommit activates a previewed checkpoint using CAS on baseVersion.
// Returns ErrVersionConflict if the baseVersion does not match the checkpoint's
// current version (ADR-005 §4.2).
func (e *Engine) CompactCommit(ctx context.Context, checkpointID string, baseVersion int64) (*compactionapp.CommitResult, error) {
	if e.compactionExecutor == nil {
		return nil, errors.New("compaction services not configured")
	}
	return e.compactionExecutor.Commit(ctx, checkpointID, baseVersion)
}

// CompactCancel cancels a pending compaction checkpoint by marking it failed
// with code "CANCELLED" (ADR-005 §4.2).
func (e *Engine) CompactCancel(ctx context.Context, checkpointID string) (*compactionapp.CancelResult, error) {
	if e.compactionExecutor == nil {
		return nil, errors.New("compaction services not configured")
	}
	return e.compactionExecutor.Cancel(ctx, checkpointID)
}

// CompactRetry retries a failed checkpoint by transitioning it back to pending
// and re-executing it (ADR-005 §5: "failed checkpoints can be retried").
func (e *Engine) CompactRetry(ctx context.Context, checkpointID string) (*compactionapp.RetryResult, *compactionapp.ExecuteResult, error) {
	if e.compactionExecutor == nil {
		return nil, nil, errors.New("compaction services not configured")
	}
	retryResult, execResult, err := e.compactionExecutor.Retry(ctx, checkpointID)
	return &retryResult, &execResult, err
}

// PreTurnCompactionResult describes the outcome of a synchronous pre-turn
// compaction. When Compacted is true, the current turn should re-assemble
// context using the new summary.
type PreTurnCompactionResult struct {
	// Err is set when compaction evaluation or activation failed. A normal
	// below-watermark result leaves Err nil and Compacted false.
	Err error
	// Compacted indicates whether compaction was triggered and completed.
	Compacted bool
	// CheckpointID is the ID of the new checkpoint, if compacted.
	CheckpointID string
	// Version is the new checkpoint version, if compacted.
	Version int64
	// LowWatermarkVerified is true when the post-compaction reusable context
	// was verified to be below the low watermark.
	LowWatermarkVerified bool
	// LowWatermarkUsageFraction is the remaining reusable context as a fraction
	// of the context window (0.0–1.0).
	LowWatermarkUsageFraction float64
	// Reason describes why compaction was or was not triggered.
	Reason string
}

// TriggerPreTurnCompaction synchronously triggers and executes compaction when
// the token usage exceeds the high watermark. This is called before context
// assembly so the current turn benefits from the compaction (ADR-005 §5:
// "pre-turn: budget check → generate compaction candidate → validate → CAS
// activate → re-assemble → send request").
//
// After successful compaction:
//  1. Previous succeeded checkpoints are marked as superseded (activation).
//  2. Low-watermark verification is performed (remaining context < 60%).
//
// If compaction fails or is not triggered, the caller proceeds with the
// existing context (fail-open for chat, fail-closed for assembly).
func (e *Engine) TriggerPreTurnCompaction(ctx context.Context, sessionID, provider, model, tokenizerRevision string, contextWindow int64) PreTurnCompactionResult {
	result := PreTurnCompactionResult{}

	if e.compactionTrigger == nil || e.compactionExecutor == nil {
		return result
	}

	// 1. Check if compaction should be triggered.
	triggerResult, err := e.compactionTrigger.CheckAndTrigger(ctx, sessionID, provider, model, tokenizerRevision, contextWindow)
	if err != nil || !triggerResult.Triggered {
		if err != nil {
			result.Err = err
			result.Reason = fmt.Sprintf("trigger error: %v", err)
		} else {
			result.Reason = triggerResult.Reason
		}
		return result
	}

	// 2. Synchronously execute the checkpoint (pending → running → succeeded/failed).
	execResult, err := e.compactionExecutor.Execute(ctx, triggerResult.CheckpointID)
	if err != nil || execResult.Status != compaction.StatusSucceeded {
		if err != nil {
			result.Err = err
			result.Reason = fmt.Sprintf("execute error: %v", err)
		} else {
			result.Err = fmt.Errorf("compaction execution failed: status %s", execResult.Status)
			result.Reason = fmt.Sprintf("execution failed: status %s", execResult.Status)
		}
		return result
	}

	// 3. Activate atomically; succeeded drafts remain invisible until this CAS.
	if err := e.compactionExecutor.ActivateAutomatic(ctx, triggerResult.CheckpointID, execResult.Version); err != nil {
		result.Err = err
		result.Reason = fmt.Sprintf("compaction succeeded but activation failed: %v", err)
		return result
	}

	// 4. Low-watermark verification (ADR-005 §5: "below 60%").
	verified, fraction, _ := e.compactionExecutor.VerifyLowWatermark(ctx, triggerResult.CheckpointID, contextWindow, 0.60)

	result.Compacted = true
	result.CheckpointID = triggerResult.CheckpointID
	result.Version = execResult.Version
	result.LowWatermarkVerified = verified
	result.LowWatermarkUsageFraction = fraction
	result.Reason = fmt.Sprintf("compaction completed: checkpoint %s, low-watermark verified=%v (%.1f%%)",
		triggerResult.CheckpointID, verified, fraction*100)
	return result
}

// SetupHandoffService wires the handoff capsule service into the engine.
// When set, the Engine can create, inspect, activate, and revoke cross-window
// handoff capsules (ADR-005 §5). The Engine, not the Renderer, validates and
// activates capsules.
func (e *Engine) SetHandoffService(s *handoffapp.Service) { e.handoffService = s }

// SetAttachmentService wires the attachment service into the engine.
// When set, the Engine can ingest, query, and delete user-supplied file
// attachments, and chat.start will inject parsed attachment excerpts as
// untrusted prior context (ADR-005 §7).
func (e *Engine) SetAttachmentService(s *attachmentapp.Service) { e.attachmentService = s }

// CreateHandoffCapsule creates a cross-window handoff capsule from a succeeded
// compaction checkpoint. The capsule carries the checkpoint's structured
// summary plus active task state and recent message IDs for cross-window
// continuation (ADR-005 §5).
func (e *Engine) CreateHandoffCapsule(ctx context.Context, req handoffapp.CreateCapsuleRequest) (handoff.Capsule, error) {
	if e.handoffService == nil {
		return handoff.Capsule{}, errors.New("handoff service not configured")
	}
	return e.handoffService.CreateCapsule(ctx, req)
}

// GetHandoffCapsule returns a capsule by ID for inspection. This allows the
// user to inspect the summary and jump to source Messages (ADR-005 §5).
func (e *Engine) GetHandoffCapsule(ctx context.Context, id string) (*handoff.Capsule, error) {
	if e.handoffService == nil {
		return nil, errors.New("handoff service not configured")
	}
	return e.handoffService.GetCapsule(ctx, id)
}

// InspectHandoffCapsule returns a capsule by ID together with its source
// checkpoint. The checkpoint summary lets the Renderer display the carried
// context; the checkpoint may be nil if the source was deleted (ADR-005 §5).
func (e *Engine) InspectHandoffCapsule(ctx context.Context, id string) (handoffapp.InspectCapsuleResult, error) {
	if e.handoffService == nil {
		return handoffapp.InspectCapsuleResult{}, errors.New("handoff service not configured")
	}
	return e.handoffService.InspectCapsule(ctx, id)
}

// ListHandoffCapsules returns capsules for a source session, ordered by
// creation time descending. A limit <= 0 or > 100 defaults to 50.
func (e *Engine) ListHandoffCapsules(ctx context.Context, sessionID string, limit int) ([]handoff.Capsule, error) {
	if e.handoffService == nil {
		return nil, errors.New("handoff service not configured")
	}
	return e.handoffService.ListCapsulesBySourceSession(ctx, sessionID, limit)
}

// ListActiveHandoffCapsules returns all active (non-terminal) capsules for a
// session. These are capsules that can still be activated or revoked.
func (e *Engine) ListActiveHandoffCapsules(ctx context.Context, sessionID string) ([]handoff.Capsule, error) {
	if e.handoffService == nil {
		return nil, errors.New("handoff service not configured")
	}
	return e.handoffService.ListActiveCapsules(ctx, sessionID)
}

// ActivateHandoffCapsule binds a capsule to a destination session and activates
// it. The Engine validates the capsule (digest binding, expiration) before
// activation (ADR-005 §5: "The Engine, not the Renderer, validates and
// activates capsules").
//
// On success, the caller is responsible for injecting the capsule's summary
// into the assembled prompt of the destination session via AssembleOptions.
func (e *Engine) ActivateHandoffCapsule(ctx context.Context, capsuleID, destSessionID string) (handoffapp.ActivateCapsuleResult, error) {
	if e.handoffService == nil {
		return handoffapp.ActivateCapsuleResult{}, errors.New("handoff service not configured")
	}
	return e.handoffService.ActivateCapsule(ctx, capsuleID, destSessionID)
}

// RevokeHandoffCapsule revokes an active capsule. Once revoked, a capsule
// cannot be activated (terminal state).
func (e *Engine) RevokeHandoffCapsule(ctx context.Context, id string) error {
	if e.handoffService == nil {
		return errors.New("handoff service not configured")
	}
	return e.handoffService.RevokeCapsule(ctx, id)
}

// ImportHandoffCapsule imports a capsule into a target session as
// provenance-linked untrusted prior context (ADR-005 §5). The Engine validates
// the capsule (digest binding, expiration, source existence) before recording
// the import. Repeat import of the same capsule into the same session is
// idempotent.
//
// On success, the capsule's summary becomes available as untrusted prior
// context for the target session. The chat send path injects imported
// capsule summaries into ContextEnvelope.HandoffCapsules.
func (e *Engine) ImportHandoffCapsule(ctx context.Context, capsuleID, targetSessionID string) (handoffapp.ImportCapsuleResult, error) {
	if e.handoffService == nil {
		return handoffapp.ImportCapsuleResult{}, errors.New("handoff service not configured")
	}
	return e.handoffService.ImportCapsule(ctx, capsuleID, targetSessionID)
}

// ListImportedHandoffCapsules returns all capsules imported into the target
// session, ordered by imported_at descending (ADR-005 §5).
func (e *Engine) ListImportedHandoffCapsules(ctx context.Context, targetSessionID string) ([]handoff.Capsule, error) {
	if e.handoffService == nil {
		return nil, errors.New("handoff service not configured")
	}
	return e.handoffService.ListImportedCapsules(ctx, targetSessionID)
}

// ListImportedHandoffCapsuleContexts returns all active capsules imported
// into the target session together with their source checkpoints. Capsules
// whose source checkpoint was deleted are returned with a nil Checkpoint so
// the caller can fail-closed. This is the primary read path used by
// chat.start to populate ContextEnvelope.HandoffCapsules (ADR-005 §5).
func (e *Engine) ListImportedHandoffCapsuleContexts(ctx context.Context, targetSessionID string) ([]handoffapp.CapsuleContext, error) {
	if e.handoffService == nil {
		return nil, nil
	}
	return e.handoffService.ListImportedCapsuleContexts(ctx, targetSessionID)
}

// IngestAttachment ingests a user-supplied file: validates content, writes to
// the controlled data directory, creates the metadata record, and parses text
// (ADR-005 §7). Returns the created attachment with parse status.
func (e *Engine) IngestAttachment(ctx context.Context, req attachmentapp.IngestFileRequest) (attachment.Attachment, error) {
	if e.attachmentService == nil {
		return attachment.Attachment{}, errors.New("attachment service not configured")
	}
	return e.attachmentService.IngestFile(ctx, req)
}
func (e *Engine) BeginAttachmentUpload(ctx context.Context, req attachmentapp.BeginUploadRequest) (string, time.Time, error) {
	if e.attachmentService == nil {
		return "", time.Time{}, errors.New("attachment service not configured")
	}
	return e.attachmentService.BeginUpload(ctx, req)
}
func (e *Engine) AppendAttachmentChunk(ctx context.Context, id string, offset int64, data []byte) (int64, error) {
	if e.attachmentService == nil {
		return 0, errors.New("attachment service not configured")
	}
	return e.attachmentService.UploadChunk(ctx, id, offset, data)
}
func (e *Engine) CommitAttachmentUpload(ctx context.Context, id, projectID, sessionID string) (attachment.Attachment, error) {
	if e.attachmentService == nil {
		return attachment.Attachment{}, errors.New("attachment service not configured")
	}
	return e.attachmentService.CommitUpload(ctx, id, projectID, sessionID)
}
func (e *Engine) AbortAttachmentUpload(ctx context.Context, id, projectID, sessionID string) error {
	if e.attachmentService == nil {
		return errors.New("attachment service not configured")
	}
	return e.attachmentService.AbortUpload(ctx, id, projectID, sessionID)
}

func (e *Engine) ListVisionImagesBySession(ctx context.Context, sessionID string) ([]attachmentapp.VisionImage, error) {
	if e.attachmentService == nil {
		return nil, nil
	}
	return e.attachmentService.ListVisionImagesBySession(ctx, sessionID)
}

func (e *Engine) GetVisionImage(ctx context.Context, id, sessionID string) (attachmentapp.VisionImage, error) {
	if e.attachmentService == nil {
		return attachmentapp.VisionImage{}, errors.New("attachment service not configured")
	}
	return e.attachmentService.GetVisionImage(ctx, id, sessionID)
}

// GetAttachment returns an attachment by ID for inspection.
func (e *Engine) GetAttachment(ctx context.Context, id string) (*attachment.Attachment, error) {
	if e.attachmentService == nil {
		return nil, errors.New("attachment service not configured")
	}
	return e.attachmentService.GetAttachment(ctx, id)
}

// ListAttachmentsByProject returns attachments for a project (ADR-005 §7).
func (e *Engine) ListAttachmentsByProject(ctx context.Context, projectID string, limit int) ([]attachment.Attachment, error) {
	if e.attachmentService == nil {
		return nil, errors.New("attachment service not configured")
	}
	return e.attachmentService.ListByProject(ctx, projectID, limit)
}

// ListAttachmentsBySession returns attachments for a session (ADR-005 §7).
func (e *Engine) ListAttachmentsBySession(ctx context.Context, sessionID string, limit int) ([]attachment.Attachment, error) {
	if e.attachmentService == nil {
		return nil, errors.New("attachment service not configured")
	}
	return e.attachmentService.ListBySession(ctx, sessionID, limit)
}

// DeleteAttachment soft-deletes an attachment and removes the underlying file
// from the data directory (ADR-005 §7).
func (e *Engine) DeleteAttachment(ctx context.Context, id string) error {
	if e.attachmentService == nil {
		return errors.New("attachment service not configured")
	}
	return e.attachmentService.DeleteAttachment(ctx, id)
}

func (e *Engine) ReconcileAttachmentFileCleanup(ctx context.Context) error {
	if e.attachmentService == nil {
		return errors.New("attachment service not configured")
	}
	return e.attachmentService.ReconcileFileCleanup(ctx)
}

// ListReadableAttachmentsBySession returns succeeded, non-deleted attachments
// for a session. This is the read path used by chat.start to populate
// ContextEnvelope.AttachmentExcerpts (ADR-005 §7).
func (e *Engine) ListReadableAttachmentsBySession(ctx context.Context, sessionID string) ([]attachment.Attachment, error) {
	if e.attachmentService == nil {
		return nil, nil
	}
	return e.attachmentService.ListReadableBySession(ctx, sessionID, 50)
}

// NewEngineWithGateway wires the existing policy connector and one-shot secret
// broker into provider diagnostics. Public requests never carry either.
func NewEngineWithGateway(providers ProviderService, version string, leases LeaseClient) *Engine {
	return &Engine{providers: providers, version: version, leases: leases, streams: make(map[string]*streamState), maxStreams: 32, adapterCache: make(map[string]gateway.Adapter),
		network: networkpolicy.Options{ConnectTimeout: 10 * time.Second, ResponseHeaderTimeout: 60 * time.Second, DisableOverallTimeout: true, IdleReadTimeout: 90 * time.Second, MaxResponseBytes: 1 << 20},
		gateway: gateway.Options{MaxModels: 50, MaxAttempts: 1, MaxRequestBytes: 5 << 20}}
}

// CancelAllStreams terminates every stream owned by this authenticated session.
func (e *Engine) CancelAllStreams() {
	e.streamsMu.Lock()
	defer e.streamsMu.Unlock()
	for _, stream := range e.streams {
		if stream.state == streamRunning {
			stream.state = streamCancelling
			stream.cancel()
		}
	}
}

func (e *Engine) cancelStream(id string) bool {
	e.streamsMu.Lock()
	defer e.streamsMu.Unlock()
	stream, ok := e.streams[id]
	if !ok || stream.state != streamRunning {
		return false
	}
	stream.state = streamCancelling
	stream.cancel()
	return true
}

// claimStreamFinalization linearizes successful upstream completion against
// cancellation. A false result means cancellation already won, so the caller
// must skip durable persistence.
func (e *Engine) claimStreamFinalization(state *streamState) bool {
	e.streamsMu.Lock()
	defer e.streamsMu.Unlock()
	if state.state != streamRunning {
		return false
	}
	state.state = streamFinalizing
	return true
}

func (e *Engine) selectTerminal(_ string, state *streamState, err error) bridge.EventType {
	e.streamsMu.Lock()
	defer e.streamsMu.Unlock()
	t := bridge.EventCompleted
	if state.state == streamCancelling {
		t = bridge.EventCancelled
	} else if err != nil {
		t = bridge.EventFailed
	}
	state.state = streamTerminal
	return t
}

func (e *Engine) finishTerminal(id string, state *streamState) {
	e.streamsMu.Lock()
	defer e.streamsMu.Unlock()
	if current, ok := e.streams[id]; ok && current == state {
		delete(e.streams, id)
	}
}

// SetAdapterFactoryForTest injects an adapter at the production Engine.Handle boundary.
func (e *Engine) SetAdapterFactoryForTest(factory func(context.Context, provider.Provider) (gateway.Adapter, error)) {
	e.adapterFactory = factory
}

func (e *Engine) SetAgentCoordinator(c *agentorchestration.Coordinator) { e.coordinator = c }

// SetAgentRunService wires the M4 reliable single-agent runtime service
// (capability.list + agent.run.*) into the engine.
func (e *Engine) SetAgentRunService(s *agentrunapp.Service) { e.agentRuns = s }

// SetM6Services wires the M6 slice-1 services: the extension supply chain,
// the in-memory MCP endpoint registry and its durable mirror.
func (e *Engine) SetM6Services(ext *m6app.ExtensionService, reg *mcp6.Registry, endpoints *m6app.EndpointService) {
	e.m6ext, e.mcp6Registry, e.mcp6Endpoints = ext, reg, endpoints
}

// SetM6ExecutionServices wires the M6 slice-2 services: the connector
// metadata catalog and the worker dispatch gateway.
func (e *Engine) SetM6ExecutionServices(catalog *m6app.CatalogService, dispatch *m6app.DispatchService) {
	e.m6catalog, e.m6dispatch = catalog, dispatch
}

// SetM6DelegationServices wires the M6 slice-3 services: the governed
// parent/child delegation fan-out and the join barriers.
func (e *Engine) SetM6DelegationServices(delegationSvc *m6app.DelegationService, barriers *m6app.BarrierService) {
	e.m6delegation, e.m6barriers = delegationSvc, barriers
}

// SetM6MergeServices wires the M6 slice-4 service: the root-writer merge
// walk and the final-tree gate (engine-internal loops; merge.submit is
// their only bridge entry).
func (e *Engine) SetM6MergeServices(mergeSvc *m6app.MergeService) {
	e.m6merge = mergeSvc
}

// SetM6GovernanceServices wires the M6 S5C services: the governed skill
// import pipeline and the complexity router (0053 domains).
func (e *Engine) SetM6GovernanceServices(skills *m6app.SkillImportService, routing *m6app.RoutingService) {
	e.m6skills, e.m6routing = skills, routing
}

// SetM7WorkflowServices wires the M7 slice-1 service: the nine-stage
// versioned workflow backbone (create/publish/start/transition/capture).
func (e *Engine) SetM7WorkflowServices(workflowSvc *m7app.WorkflowService) {
	e.m7workflow = workflowSvc
}

// SetM7EvidenceServices wires the M7 slice-2 services: the trace engine
// with stale propagation, the gate evaluator with checkpoints, and the
// review service.
func (e *Engine) SetM7EvidenceServices(traceSvc *m7app.TraceService, gateSvc *m7app.GateService, reviewSvc *m7app.ReviewService) {
	e.m7trace = traceSvc
	e.m7gate = gateSvc
	e.m7review = reviewSvc
}

// SetM7ReleaseServices wires the M7 slice-3 service: CR revisions and
// immutable release packages.
func (e *Engine) SetM7ReleaseServices(releaseSvc *m7app.ReleaseService) {
	e.m7release = releaseSvc
}

// SetM7PromotionServices wires the M7 slice-4 service: the promotion saga
// with internal migration/deployment adapter ports.
func (e *Engine) SetM7PromotionServices(promotionSvc *m7app.PromotionService) {
	e.m7promotion = promotionSvc
}

// SetM7UpdateServices wires the M7 slice-5 service: the AppUpdate split-track
// (check/install) and the audit-chain verification behind M7-DR-001.
func (e *Engine) SetM7UpdateServices(updateSvc *m7app.UpdateService) {
	e.m7update = updateSvc
}

// SetM7RuntimeServices wires the M7 slice 6-8 services: the read-only
// subagent runtime, the tool-gap runtime and the MCP settings plane.
func (e *Engine) SetM7RuntimeServices(subagentSvc *m7app.SubagentService, toolgapSvc *m7app.ToolgapService, mcpSvc *m7app.McpRuntimeService) {
	e.m7subagent = subagentSvc
	e.m7toolgap = toolgapSvc
	e.m7mcp = mcpSvc
}

// SetM8MemoryServices wires the M8 slice-1 governed long-term memory core.
func (e *Engine) SetM8MemoryServices(memorySvc *m8app.MemoryService) {
	e.m8memory = memorySvc
}

// SetM10NominationService wires the M10 memory nomination workflow.
func (e *Engine) SetM10NominationService(nominationSvc *m8app.NominationService) {
	e.m10nomination = nominationSvc
}

// SetMemoryOpsService wires the M10 memory-operations service.
func (e *Engine) SetMemoryOpsService(opsSvc *m8app.MemoryOpsService) {
	e.memoryOps = opsSvc
}

// SetM10ScenarioService wires the M10 expert scenario cards.
func (e *Engine) SetM10ScenarioService(scenarioSvc *m8app.ScenarioService) {
	e.m10scenario = scenarioSvc
}

// SetQueueService wires the M10 queued-input service.
func (e *Engine) SetQueueService(queueSvc *queueapp.Service) {
	e.queue = queueSvc
}

// SetMcMarketService wires the M10 wave-3 MCP-market service.
func (e *Engine) SetMcMarketService(mcSvc *mcapp.Service) {
	e.mcmarket = mcSvc
}

// SetBrMultiModeService wires the M10 wave-3 browser multi-mode service.
func (e *Engine) SetBrMultiModeService(brSvc *brapp.Service) {
	e.brmulti = brSvc
}

// SetCcControlService wires the M10 wave-4 computer-control service.
func (e *Engine) SetCcControlService(ccSvc *ccapp.Service) {
	e.ccctrl = ccSvc
}

// SetM8SliceServices wires the M8 slice-2/3/4/5 services (KB, handoff/
// tombstone/sync, automation).
func (e *Engine) SetM8SliceServices(kbSvc *m8app.KBService, handoffSvc *m8app.HandoffService, automationSvc *m8app.AutomationService) {
	e.m8kb = kbSvc
	e.m8handoff = handoffSvc
	e.m8automation = automationSvc
}

// SetM8PluginService wires the M8 FR-18 unified plugin runtime.
func (e *Engine) SetM8PluginService(pluginSvc *m8app.PluginService) {
	e.m8plugin = pluginSvc
}

// SetM8ExpertService wires the M8 FR-19 expert center.
func (e *Engine) SetM8ExpertService(expertSvc *m8app.ExpertService) {
	e.m8expert = expertSvc
}

type sessionExpertStore interface {
	ListSessionExpertIDs(ctx context.Context, sessionID string) ([]string, error)
	ReplaceSessionExpertIDs(ctx context.Context, sessionID string, expertIDs []string) error
}

func (e *Engine) SetSessionExpertStore(store sessionExpertStore) {
	e.sessionExperts = store
}

// SetM8CollabGateService wires the M8 FR-17 write-collaboration gate.
func (e *Engine) SetM8CollabGateService(gateSvc *m8app.CollabGateService) {
	e.m8gate = gateSvc
}

// SetM9OrgAdminService wires the M9 slice-1 org-admin bridge service.
func (e *Engine) SetM9OrgAdminService(orgSvc *m9app.OrgAdminService) {
	e.m9org = orgSvc
}

// SetM9TtsService wires the M9.5 Moon Companion SAPI runtime.
func (e *Engine) SetM9TtsService(ttsSvc *tts.Service) {
	e.m9tts = ttsSvc
}

func (e *Engine) Handle(ctx context.Context, request bridge.Request) bridge.Response {
	if _, err := ulid.ParseStrict(request.ID); err != nil {
		return bridge.Failure(ulid.Make().String(), ulid.Make().String(), "BRIDGE_SCHEMA_INVALID", "请求标识无效", false)
	}
	if _, err := ulid.ParseStrict(request.TraceID); err != nil {
		return bridge.Failure(request.ID, ulid.Make().String(), "BRIDGE_SCHEMA_INVALID", "追踪标识无效", false)
	}
	if request.Version != bridge.Version || request.Kind != "request" || len(request.IdempotencyKey) > 128 {
		return bridge.Failure(request.ID, request.TraceID, "BRIDGE_SCHEMA_INVALID", "请求协议无效", false)
	}
	if request.DeadlineMS < 1 || request.DeadlineMS > 30000 {
		return bridge.Failure(request.ID, request.TraceID, "BRIDGE_SCHEMA_INVALID", "请求超时参数无效", false)
	}
	now := time.Now().UTC()
	if request.SentAt.Before(now.Add(-5*time.Minute)) || request.SentAt.After(now.Add(5*time.Minute)) {
		return bridge.Failure(request.ID, request.TraceID, "BRIDGE_SCHEMA_INVALID", "请求时间无效", false)
	}
	deadline := request.SentAt.Add(time.Duration(request.DeadlineMS) * time.Millisecond)
	serverCap := now.Add(time.Duration(request.DeadlineMS) * time.Millisecond)
	if deadline.After(serverCap) {
		deadline = serverCap
	}
	if now.After(deadline) {
		return bridge.Failure(request.ID, request.TraceID, "REQUEST_DEADLINE_EXCEEDED", "请求已过期", true)
	}
	ctx, cancel := context.WithDeadline(ctx, deadline)
	defer cancel()
	handler, allowed := RuntimeHandlers[bridge.Method(request.Method)]
	if !allowed {
		handler, allowed = internalRuntimeHandlers[bridge.Method(request.Method)]
	}
	if !allowed || handler == nil {
		return bridge.Failure(request.ID, request.TraceID, "BRIDGE_METHOD_NOT_ALLOWED", "请求的方法不在白名单中", false)
	}
	// Last-resort panic guard: a handler crash degrades to one failed
	// request instead of killing the Engine process and the event pipe.
	return func() (resp bridge.Response) {
		defer func() {
			if rec := recover(); rec != nil {
				resp = bridge.Failure(request.ID, request.TraceID, "ENGINE_HANDLER_PANIC", "内部处理错误，请重试", true)
			}
		}()
		return handler(e, ctx, request)
	}()
}

func handleSystemHealth(e *Engine, _ context.Context, request bridge.Request) bridge.Response {
	if !emptyObject(request.Payload) {
		return bridge.Failure(request.ID, request.TraceID, "BRIDGE_SCHEMA_INVALID", "system.health 参数无效", false)
	}
	return bridge.Success(request.ID, map[string]any{"engine": "ready", "version": e.version, "protocol": bridge.Version})
}

func handleProviderList(e *Engine, ctx context.Context, request bridge.Request) bridge.Response {
	var payload struct {
		Protocol provider.Protocol `json:"protocol"`
	}
	if err := decodePayload(request.Payload, &payload); err != nil {
		return bridge.Failure(request.ID, request.TraceID, "BRIDGE_SCHEMA_INVALID", "provider.list 参数无效", false)
	}
	if payload.Protocol != "" && payload.Protocol != provider.ProtocolOpenAICompatible && payload.Protocol != provider.ProtocolAnthropic {
		return bridge.Failure(request.ID, request.TraceID, "BRIDGE_SCHEMA_INVALID", "供应商协议无效", false)
	}
	items, err := e.providers.List(ctx, provider.Filter{Protocol: payload.Protocol})
	if err != nil {
		return bridge.Failure(request.ID, request.TraceID, "STORAGE_UNAVAILABLE", "供应商数据暂时不可用", true)
	}
	publicItems := make([]providerDTO, len(items))
	for index, item := range items {
		publicItems[index] = publicProvider(item)
	}
	return bridge.Success(request.ID, map[string]any{"items": publicItems})
}

func publicProvider(item provider.Provider) providerDTO {
	return providerDTO{ID: item.ID, Name: item.Name, Protocol: item.Protocol, BaseURL: item.BaseURL, Models: item.Models, Status: item.Status, CredentialState: item.CredentialState, CreatedAt: item.CreatedAt, UpdatedAt: item.UpdatedAt, Version: item.Version}
}

func providerFailure(request bridge.Request, err error) bridge.Response {
	switch {
	case errors.Is(err, provider.ErrNotFound):
		return bridge.Failure(request.ID, request.TraceID, "PROVIDER_NOT_FOUND", "供应商不存在", false)
	case errors.Is(err, provider.ErrConflict):
		return bridge.Failure(request.ID, request.TraceID, "PROVIDER_VERSION_CONFLICT", "供应商已被修改，请刷新后重试", false)
	case errors.Is(err, provider.ErrCredentialReentryRequired):
		return bridge.Failure(request.ID, request.TraceID, "CREDENTIAL_REENTRY_REQUIRED", "地址或协议变更需要重新提交凭据", false)
	case errors.Is(err, providerapp.ErrIdempotencyKeyRequired):
		return bridge.Failure(request.ID, request.TraceID, "IDEMPOTENCY_KEY_REQUIRED", "写操作需要幂等键", false)
	case errors.Is(err, providerapp.ErrIdempotencyConflict):
		return bridge.Failure(request.ID, request.TraceID, "IDEMPOTENCY_CONFLICT", "幂等键已用于不同请求", false)
	case errors.Is(err, providerapp.ErrCredentialCleanupRequired):
		return bridge.Failure(request.ID, request.TraceID, "CREDENTIAL_CLEANUP_REQUIRED", "请先移除供应商凭据再删除", false)
	case errors.Is(err, providerapp.ErrStorageBusy):
		return bridge.Failure(request.ID, request.TraceID, "STORAGE_BUSY", "供应商数据正忙，请稍后重试", true)
	default:
		return bridge.Failure(request.ID, request.TraceID, "STORAGE_UNAVAILABLE", "供应商数据暂时不可用", true)
	}
}

func emptyObject(raw json.RawMessage) bool {
	var value map[string]json.RawMessage
	return decodePayload(raw, &value) == nil && value != nil && len(value) == 0
}

func decodePayload(raw json.RawMessage, target any) error {
	if bytes.Equal(bytes.TrimSpace(raw), []byte("null")) || len(bytes.TrimSpace(raw)) == 0 {
		return errors.New("payload must be a non-null JSON value")
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return errors.New("multiple JSON values")
	}
	return nil
}
