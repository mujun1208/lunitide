package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"sync"
	"time"

	"github.com/lunitide/lunitide/internal/bridge"
	"github.com/lunitide/lunitide/internal/compactionapp"
	"github.com/lunitide/lunitide/internal/contextapp"
	"github.com/lunitide/lunitide/internal/domain/message"
	"github.com/lunitide/lunitide/internal/domain/project"
	"github.com/lunitide/lunitide/internal/domain/provider"
	"github.com/lunitide/lunitide/internal/domain/session"
	"github.com/lunitide/lunitide/internal/domain/stage"
	"github.com/lunitide/lunitide/internal/domain/token"
	"github.com/lunitide/lunitide/internal/gateway"
	"github.com/lunitide/lunitide/internal/messageapp"
	"github.com/lunitide/lunitide/internal/networkpolicy"
	"github.com/lunitide/lunitide/internal/providerapp"
	"github.com/lunitide/lunitide/internal/secret"
	"github.com/lunitide/lunitide/internal/secretlease"
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
	Delete(context.Context, string) error
}
type SessionService interface {
	Create(context.Context, string, string, any, session.Session) (session.Session, error)
	List(context.Context, session.Filter) ([]session.Session, error)
	Delete(context.Context, string) error
}
type MessageService interface {
	Append(context.Context, string, string, any, message.Message) (message.Message, error)
	AppendAssistant(context.Context, string, string, string, string, messageapp.AssistantUsage) (message.Message, error)
	List(context.Context, messageapp.PageRequest) (messageapp.Page, error)
}
type StageService interface {
	Create(context.Context, string, string, any, stage.Stage) (stage.Stage, error)
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
type electronCredentialMigrationService interface {
	PlanElectronCredentials(context.Context, []providerapp.ElectronCredentialTuple) ([]providerapp.ElectronCredentialPlan, error)
	AdoptElectronCredential(context.Context, string, providerapp.ElectronCredentialAdoption) (string, error)
	DispositionElectronCredential(context.Context, providerapp.ElectronCredentialTuple, string) error
}

type Engine struct {
	providers      ProviderService
	projects       ProjectService
	sessions       SessionService
	messages       MessageService
	stages         StageService
	planning       PlanningService
	governance     GovernanceService
	memories       MemoryService
	ontology       OntologyService
	skills         SkillService
	migration      MigrationService
	messageReader  contextapp.Reader
	tokenRepo      token.Repository
	compactionTrigger    *compactionapp.Trigger
	compactionExecutor   *compactionapp.Executor
	summaryReader        CompactionSummaryReader
	version        string
	leases         LeaseClient
	network        networkpolicy.Options
	gateway        gateway.Options
	adapterFactory func(context.Context, provider.Provider) (gateway.Adapter, error)
	streamsMu      sync.Mutex
	streams        map[string]*streamState
	maxStreams     int
}

type streamState struct {
	cancel context.CancelFunc
	state  streamLifecycle
}

type streamLifecycle uint8

const (
	streamRunning streamLifecycle = iota
	streamCancelling
	streamTerminal
)

type LeaseClient interface {
	WithLease(context.Context, secretlease.Request, func([]byte) error) error
}

type runtimeHandler func(*Engine, context.Context, bridge.Request) bridge.Response

// RuntimeHandlers is both the runtime allow-list and the dispatch table.
// Contract tests compare its non-nil handlers with the public schema.
var RuntimeHandlers = map[bridge.Method]runtimeHandler{
	bridge.MethodChatStart:         handleChatStart,
	bridge.MethodStreamCancel:      handleStreamCancel,
	bridge.MethodSystemHealth:      handleSystemHealth,
	bridge.MethodProviderCreate:    handleProviderCreate,
	bridge.MethodProviderDelete:    handleProviderDelete,
	bridge.MethodProviderGet:       handleProviderGet,
	bridge.MethodProviderList:      handleProviderList,
	bridge.MethodProviderModelSync: handleProviderModelSync,
	bridge.MethodProviderTest:      handleProviderTest,
	bridge.MethodProviderUpdate:    handleProviderUpdate,
	bridge.MethodProjectCreate:     handleProjectCreate,
	bridge.MethodProjectDelete:     handleProjectDelete,
	bridge.MethodProjectList:       handleProjectList,
	bridge.MethodSessionCreate:     handleSessionCreate,
	bridge.MethodSessionDelete:     handleSessionDelete,
	bridge.MethodSessionList:       handleSessionList,
	bridge.MethodMessageAppend:     handleMessageAppend,
	bridge.MethodMessageList:       handleMessageList,
	bridge.MethodStageCreate:       handleStageCreate,
	bridge.MethodStageList:         handleStageList,
	bridge.MethodPlanGet:           handlePlanGet,
	bridge.MethodPlanList:          handlePlanList,
	bridge.MethodPlanCreate:        handlePlanCreate,
	bridge.MethodPlanActivate:      handlePlanActivate,
	bridge.MethodPlanComplete:      handlePlanComplete,
	bridge.MethodPlanPause:         handlePlanPause,
	bridge.MethodPlanResume:        handlePlanResume,
	bridge.MethodNodeList:          handleNodeList,
	bridge.MethodNodeCreate:        handleNodeCreate,
	bridge.MethodNodeStart:         handleNodeStart,
	bridge.MethodNodeComplete:      handleNodeComplete,
	bridge.MethodNodeFail:          handleNodeFail,
	bridge.MethodReviewList:        handleReviewList,
	bridge.MethodReviewApprove:     handleReviewApprove,
	bridge.MethodReviewReject:      handleReviewReject,
	bridge.MethodMemoryGet:         handleMemoryGet,
	bridge.MethodMemoryList:        handleMemoryList,
	bridge.MethodMemorySearch:      handleMemorySearch,
	bridge.MethodMemoryCreate:      handleMemoryCreate,
	bridge.MethodMemoryUpdate:      handleMemoryUpdate,
	bridge.MethodMemoryDelete:      handleMemoryDelete,
	bridge.MethodOntologyNodeGet:    handleOntologyNodeGet,
	bridge.MethodOntologyNodeList:   handleOntologyNodeList,
	bridge.MethodOntologyNodeSearch: handleOntologyNodeSearch,
	bridge.MethodOntologyNodeCreate: handleOntologyNodeCreate,
	bridge.MethodOntologyNodeUpdate: handleOntologyNodeUpdate,
	bridge.MethodOntologyNodeDelete: handleOntologyNodeDelete,
	bridge.MethodOntologyEdgeList:   handleOntologyEdgeList,
	bridge.MethodOntologyEdgeCreate: handleOntologyEdgeCreate,
	bridge.MethodOntologyEdgeUpdate: handleOntologyEdgeUpdate,
	bridge.MethodOntologyEdgeDelete: handleOntologyEdgeDelete,
	bridge.MethodSkillGet:          handleSkillGet,
	bridge.MethodSkillList:         handleSkillList,
	bridge.MethodSkillMatch:        handleSkillMatch,
	bridge.MethodSkillCreate:       handleSkillCreate,
	bridge.MethodSkillUpdate:       handleSkillUpdate,
	bridge.MethodSkillDelete:       handleSkillDelete,
	bridge.MethodSkillPublish:      handleSkillPublish,
	bridge.MethodSkillDeprecate:    handleSkillDeprecate,
	bridge.MethodSkillDisable:      handleSkillDisable,
	bridge.MethodMigrationInspect:  handleMigrationInspect,
	bridge.MethodMigrationRun:      handleMigrationRun,
	bridge.MethodMigrationStatus:   handleMigrationStatus,
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
	return &Engine{providers: providers, version: version, streams: make(map[string]*streamState), maxStreams: 32}
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

// SetCompactionServices wires the compaction trigger, executor, and summary reader
// into the engine. When set, chat.start will check token usage and automatically
// trigger compaction when the high watermark is exceeded. The assembler will also
// inject the latest succeeded checkpoint summary into the model context.
func (e *Engine) SetCompactionServices(trigger *compactionapp.Trigger, executor *compactionapp.Executor, summaryReader CompactionSummaryReader) {
	e.compactionTrigger = trigger
	e.compactionExecutor = executor
	e.summaryReader = summaryReader
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

// NewEngineWithGateway wires the existing policy connector and one-shot secret
// broker into provider diagnostics. Public requests never carry either.
func NewEngineWithGateway(providers ProviderService, version string, leases LeaseClient) *Engine {
	return &Engine{providers: providers, version: version, leases: leases, streams: make(map[string]*streamState), maxStreams: 32,
		network: networkpolicy.Options{ConnectTimeout: 5 * time.Second, ResponseHeaderTimeout: 10 * time.Second, OverallTimeout: 15 * time.Second, MaxResponseBytes: 1 << 20},
		gateway: gateway.Options{MaxModels: 50, MaxAttempts: 1, MaxRequestBytes: 64 << 10}}
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
	return handler(e, ctx, request)
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
