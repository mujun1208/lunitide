package app

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"strings"
	"time"

	"github.com/lunitide/lunitide/internal/llmadapter"
	"github.com/lunitide/lunitide/internal/modelfit"
	"github.com/lunitide/lunitide/internal/storage/sqlite"
	"github.com/lunitide/lunitide/internal/toolruntime"
	"github.com/oklog/ulid/v2"
)

type ToolOperationStore interface {
	PutToolOperationIntent(context.Context, modelfit.ToolOperation) error
	MarkToolOperationRunning(context.Context, string, string, int, time.Time) error
	BindToolOperationTrack(ctx context.Context, ownerScope, id string, version int, externalID, evidenceRef string, artifactRefs []string, at time.Time) error
	FinishToolOperation(context.Context, string, string, int, modelfit.OperationState, string, time.Time) error
	RequestToolOperationCancel(context.Context, string, string, int, time.Time) error
	RecoverInterruptedToolOperations(context.Context) error
	GetToolOperation(context.Context, string, string) (modelfit.ToolOperation, error)
	ListToolOperations(context.Context, string, int) ([]modelfit.ToolOperation, error)
}

type CallAttemptStore interface {
	PutCallAttemptIntent(context.Context, sqlite.CallAttemptRecord) error
	MarkCallAttemptSent(context.Context, string, string, string) error
	FinishCallAttempt(context.Context, string, string, string, sqlite.CallAttemptReceipt) error
	ListCallAttempts(context.Context, string, string, int) ([]sqlite.CallAttemptRecord, error)
	SumCallAttemptsByOwner(context.Context, string, string) (sqlite.CallAttemptSum, error)
}

type continuityScopeKey struct{}

type continuityScope struct {
	Owner, Task, Turn, Purpose   string
	AutomationRunID, DispatchKey string
}

func withContinuityScope(ctx context.Context, scope continuityScope) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, continuityScopeKey{}, scope)
}

func continuityScopeFrom(ctx context.Context) continuityScope {
	if ctx == nil {
		return continuityScope{}
	}
	if scope, ok := ctx.Value(continuityScopeKey{}).(continuityScope); ok {
		return scope
	}
	return continuityScope{}
}

func withCallPurpose(ctx context.Context, purpose string) context.Context {
	scope := continuityScopeFrom(ctx)
	if strings.TrimSpace(purpose) != "" {
		scope.Purpose = purpose
	}
	if scope.Owner == "" {
		scope.Owner = "diagnostic"
	}
	return withContinuityScope(ctx, scope)
}

func withAutomationDispatch(ctx context.Context, runID, dispatchKey string) context.Context {
	scope := continuityScopeFrom(ctx)
	scope.AutomationRunID = strings.TrimSpace(runID)
	scope.DispatchKey = strings.TrimSpace(dispatchKey)
	if scope.Owner == "" {
		scope.Owner = "automation"
	}
	return withContinuityScope(ctx, scope)
}

type purposeAdapter struct {
	inner   llmadapter.Adapter
	purpose string
}

func (a purposeAdapter) Discover(ctx context.Context, secret []byte) (llmadapter.Discovery, error) {
	return a.inner.Discover(ctx, secret)
}

func (a purposeAdapter) Complete(ctx context.Context, secret []byte, req llmadapter.Request) (llmadapter.Response, error) {
	return a.inner.Complete(withCallPurpose(ctx, a.purpose), secret, req)
}

func (a purposeAdapter) Stream(ctx context.Context, secret []byte, req llmadapter.Request, emit func(llmadapter.Delta) error) (llmadapter.Response, error) {
	return a.inner.Stream(withCallPurpose(ctx, a.purpose), secret, req, emit)
}

func (e *Engine) SetToolOperationStore(store ToolOperationStore) {
	if e != nil {
		e.toolOps = store
	}
}

func (e *Engine) SetCallAttemptStore(store CallAttemptStore) {
	if e != nil {
		e.callAttempts = store
	}
}

type MessageGroupStore interface {
	PutProtocolMessageGroup(ctx context.Context, ownerScope, sessionID, turnID string, group modelfit.MessageGroup) error
	ListCompleteProtocolMessageGroups(ctx context.Context, ownerScope, sessionID, turnID string) ([]modelfit.MessageGroup, error)
	PutProtocolPrivate(ctx context.Context, ownerScope, ref string, cipher []byte, digest, credGen string) error
	GetProtocolPrivate(ctx context.Context, ownerScope, ref string) ([]byte, string, error)
}

func (e *Engine) SetMessageGroupStore(store MessageGroupStore) {
	if e != nil {
		e.messageGroups = store
	}
}

func (e *Engine) persistRememberedMessageGroups(sessionID string, turn *chatTurnCheckpoint) error {
	if e == nil || e.messageGroups == nil || turn == nil {
		return nil
	}
	groups := rememberMessageGroups(turn, turn.liveProtocol)
	if len(groups) == 0 {
		return nil
	}
	session := ""
	if looksLikeULID(sessionID) {
		session = sessionID
	}
	turnID := strings.TrimSpace(turn.StreamID)
	if !looksLikeULID(turnID) {
		turnID = ""
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	for _, g := range groups {
		if err := e.messageGroups.PutProtocolMessageGroup(ctx, ownerScope(sessionID), session, turnID, g); err != nil {
			return err
		}
	}
	return nil
}

func looksLikeULID(s string) bool {
	_, err := ulid.ParseStrict(s)
	return err == nil
}

func receiptSession(ctx context.Context, session string) string {
	if looksLikeULID(session) {
		return session
	}
	scope := continuityScopeFrom(ctx)
	if looksLikeULID(scope.Task) {
		return scope.Task
	}
	if looksLikeULID(scope.Owner) {
		return scope.Owner
	}
	return strings.TrimSpace(session)
}

func ownerScope(session string) string {
	if strings.TrimSpace(session) == "" {
		return "diagnostic"
	}
	return session
}

type toolOpRec struct {
	store          ToolOperationStore
	owner          string
	id             string
	version        int
	outcomeUnknown bool
}

// recordExistingToolCall persists intent/receipt around a call that already
// chose its own approval flag. It must not route through executeUserTool.
func (e *Engine) recordExistingToolCall(ctx context.Context, session, name string, args json.RawMessage, run func() (toolruntime.Result, error)) (out toolruntime.Result, err error) {
	rec, recErr := e.beginToolOperation(ctx, session, name, args)
	if recErr != nil {
		return toolruntime.Result{}, recErr
	}
	defer rec.finish(&out, &err)
	return run()
}

func (e *Engine) beginToolOperation(ctx context.Context, session, name string, args json.RawMessage) (*toolOpRec, error) {
	rec := &toolOpRec{}
	if e == nil || e.toolOps == nil {
		return rec, nil
	}
	now := time.Now().UTC()
	id := ulid.Make().String()
	owner := ownerScope(session)
	sess, turn := "", ""
	if looksLikeULID(session) {
		sess = session
	}
	if session != "" {
		if cp := e.loadTurnCheckpoint(session); looksLikeULID(cp.StreamID) {
			turn = cp.StreamID
		}
	}
	scope := continuityScopeFrom(ctx)
	if looksLikeULID(scope.Turn) && turn == "" {
		turn = scope.Turn
	}
	op := modelfit.ToolOperation{
		ID:              id,
		OwnerScope:      owner,
		SessionID:       sess,
		TurnID:          turn,
		AutomationRunID: scope.AutomationRunID,
		ToolName:        name,
		InputDigest:     argsDigestOrFallback(name, args),
		EffectClass:     modelfit.EffectClassForTool(name),
		State:           modelfit.OpPending,
		ExpectedVersion: 1,
		Attempt:         1,
		CreatedAt:       now,
		UpdatedAt:       now,
	}
	if err := e.toolOps.PutToolOperationIntent(ctx, op); err != nil {
		return nil, err
	}
	if err := e.toolOps.MarkToolOperationRunning(ctx, owner, id, 1, now); err != nil {
		return nil, err
	}
	if session != "" {
		cp := e.loadTurnCheckpoint(session)
		if cp.StreamID != "" || cp.Continuation != nil {
			pinContinuationIdentity(&cp, continuationIdentity{OperationRefs: []string{id}})
			_ = e.saveTurnCheckpoint(session, cp)
		}
	}
	rec.store = e.toolOps
	rec.owner = owner
	rec.id = id
	rec.version = 2
	return rec, nil
}

func (r *toolOpRec) bindTrack(externalID, evidenceRef string, artifactRefs []string) {
	if r == nil || r.store == nil {
		return
	}
	if strings.TrimSpace(externalID) == "" && strings.TrimSpace(evidenceRef) == "" && artifactRefs == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := r.store.BindToolOperationTrack(ctx, r.owner, r.id, r.version, strings.TrimSpace(externalID), strings.TrimSpace(evidenceRef), artifactRefs, time.Now().UTC()); err != nil {
		return
	}
	r.version++
}

func (r *toolOpRec) noteSubmitted() {
	r.bindTrack("", "submitted", nil)
}

func (r *toolOpRec) markUnknown() {
	if r != nil {
		r.outcomeUnknown = true
	}
}

func (r *toolOpRec) finish(out *toolruntime.Result, errp *error) {
	if r == nil || r.store == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	now := time.Now().UTC()
	var execErr error
	if errp != nil {
		execErr = *errp
	}
	if r.outcomeUnknown {
		_ = r.store.FinishToolOperation(ctx, r.owner, r.id, r.version, modelfit.OpUnknown, "outcome_unknown", now)
		return
	}
	if execErr != nil {
		if errors.Is(execErr, context.Canceled) || errors.Is(execErr, context.DeadlineExceeded) {
			_ = r.store.RequestToolOperationCancel(ctx, r.owner, r.id, r.version, now)
			return
		}
		state := modelfit.OpFailed
		if filesToolReturnsStatus("files.apply", execErr) || filesToolReturnsStatus("files.undo", execErr) {
			state = modelfit.OpPartial
		}
		_ = r.store.FinishToolOperation(ctx, r.owner, r.id, r.version, state, "tool_error", now)
		return
	}
	_ = r.store.FinishToolOperation(ctx, r.owner, r.id, r.version, modelfit.OpSucceeded, "", now)
	_ = out
}

type meteredAdapter struct {
	inner                llmadapter.Adapter
	store                CallAttemptStore
	provider, deployment string
	credentialGeneration string
	disableEfficiency    bool
}

func (e *Engine) withCallMeter(a llmadapter.Adapter, id, protocol, credentialGen string) llmadapter.Adapter {
	if e == nil || e.callAttempts == nil || a == nil {
		return a
	}
	return meteredAdapter{
		inner: a, store: e.callAttempts, provider: protocol, deployment: id,
		credentialGeneration: strings.TrimSpace(credentialGen),
		disableEfficiency:    e.gateway.DisableTokenEfficiency,
	}
}

func (a meteredAdapter) withEfficiency(req llmadapter.Request) llmadapter.Request {
	if req.Efficiency.PolicyVersion != "" || req.Efficiency.BytesBefore > 0 || req.Efficiency.BytesAfter > 0 {
		return req
	}
	if len(req.Messages) == 0 && len(req.Tools) == 0 {
		return req
	}
	return llmadapter.AttachEfficientRequest(req, llmadapter.Options{DisableTokenEfficiency: a.disableEfficiency})
}

func unwrapAdapter(a llmadapter.Adapter) llmadapter.Adapter {
	for i := 0; i < 8 && a != nil; i++ {
		switch x := a.(type) {
		case meteredAdapter:
			a = x.inner
		case purposeAdapter:
			a = x.inner
		case *videoProxyAdapter:
			a = x.Adapter
		default:
			return a
		}
	}
	return a
}

func adapterAs[T any](a llmadapter.Adapter) (T, bool) {
	t, ok := unwrapAdapter(a).(T)
	return t, ok
}

func (a meteredAdapter) Discover(ctx context.Context, secret []byte) (llmadapter.Discovery, error) {
	return a.inner.Discover(ctx, secret)
}

func (a meteredAdapter) Complete(ctx context.Context, secret []byte, req llmadapter.Request) (llmadapter.Response, error) {
	req = a.withEfficiency(req)
	return a.observe(ctx, req, func(emit func(llmadapter.Delta) error) (llmadapter.Response, error) {
		return a.inner.Complete(ctx, secret, req)
	}, nil)
}

func (a meteredAdapter) Stream(ctx context.Context, secret []byte, req llmadapter.Request, emit func(llmadapter.Delta) error) (llmadapter.Response, error) {
	req = a.withEfficiency(req)
	return a.observe(ctx, req, func(wrap func(llmadapter.Delta) error) (llmadapter.Response, error) {
		return a.inner.Stream(ctx, secret, req, wrap)
	}, emit)
}

func (a meteredAdapter) Embed(ctx context.Context, secret []byte, model string, texts []string) ([][]float32, error) {
	emb, ok := a.inner.(llmadapter.Embedder)
	if !ok {
		return nil, errors.New("adapter does not support embeddings")
	}
	var out [][]float32
	_, err := a.observe(ctx, llmadapter.Request{Model: model}, func(func(llmadapter.Delta) error) (llmadapter.Response, error) {
		var embedErr error
		out, embedErr = emb.Embed(ctx, secret, model, texts)
		return llmadapter.Response{}, embedErr
	}, nil)
	return out, err
}

func (a meteredAdapter) GenerateImage(ctx context.Context, secret []byte, model, prompt string) (llmadapter.MediaResult, error) {
	gen, ok := a.inner.(llmadapter.ImageGenerator)
	if !ok {
		return llmadapter.MediaResult{}, errors.New("adapter does not support image generation")
	}
	var out llmadapter.MediaResult
	_, err := a.observe(withCallPurpose(ctx, "image"), llmadapter.Request{Model: model}, func(func(llmadapter.Delta) error) (llmadapter.Response, error) {
		var genErr error
		out, genErr = gen.GenerateImage(ctx, secret, model, prompt)
		return llmadapter.Response{}, genErr
	}, nil)
	return out, err
}

func (a meteredAdapter) GenerateVideo(ctx context.Context, secret []byte, model, prompt string) (llmadapter.MediaResult, error) {
	gen, ok := a.inner.(llmadapter.VideoGenerator)
	if !ok {
		return llmadapter.MediaResult{}, errors.New("adapter does not support video generation")
	}
	var out llmadapter.MediaResult
	_, err := a.observe(withCallPurpose(ctx, "video"), llmadapter.Request{Model: model}, func(func(llmadapter.Delta) error) (llmadapter.Response, error) {
		var genErr error
		out, genErr = gen.GenerateVideo(ctx, secret, model, prompt)
		return llmadapter.Response{}, genErr
	}, nil)
	return out, err
}

func (a meteredAdapter) TestConnection(ctx context.Context, secret []byte, req llmadapter.Request) error {
	tester, ok := a.inner.(llmadapter.ConnectionTester)
	if !ok {
		return errors.New("adapter does not support connection tests")
	}
	_, err := a.observe(ctx, req, func(func(llmadapter.Delta) error) (llmadapter.Response, error) {
		return llmadapter.Response{}, tester.TestConnection(ctx, secret, req)
	}, nil)
	return err
}

func embedThrough(ctx context.Context, a llmadapter.Adapter, secret []byte, model string, texts []string) ([][]float32, error) {
	if m, ok := a.(meteredAdapter); ok {
		return m.Embed(ctx, secret, model, texts)
	}
	emb, ok := adapterAs[llmadapter.Embedder](a)
	if !ok {
		return nil, errors.New("adapter does not support embeddings")
	}
	return emb.Embed(ctx, secret, model, texts)
}

func generateImageThrough(ctx context.Context, a llmadapter.Adapter, secret []byte, model, prompt string) (llmadapter.MediaResult, error) {
	if m, ok := a.(meteredAdapter); ok {
		return m.GenerateImage(ctx, secret, model, prompt)
	}
	gen, ok := adapterAs[llmadapter.ImageGenerator](a)
	if !ok {
		return llmadapter.MediaResult{}, errors.New("adapter does not support image generation")
	}
	return gen.GenerateImage(ctx, secret, model, prompt)
}

func generateVideoThrough(ctx context.Context, a llmadapter.Adapter, secret []byte, model, prompt string) (llmadapter.MediaResult, error) {
	if m, ok := a.(meteredAdapter); ok {
		return m.GenerateVideo(ctx, secret, model, prompt)
	}
	if proxy, ok := a.(*videoProxyAdapter); ok {
		return proxy.GenerateVideo(ctx, secret, model, prompt)
	}
	gen, ok := adapterAs[llmadapter.VideoGenerator](a)
	if !ok {
		return llmadapter.MediaResult{}, errors.New("adapter does not support video generation")
	}
	return gen.GenerateVideo(ctx, secret, model, prompt)
}

func (a meteredAdapter) observe(ctx context.Context, req llmadapter.Request, run func(func(llmadapter.Delta) error) (llmadapter.Response, error), emit func(llmadapter.Delta) error) (llmadapter.Response, error) {
	scope := continuityScopeFrom(ctx)
	now := time.Now().UTC()
	rec := sqlite.CallAttemptRecord{
		ID:                   ulid.Make().String(),
		OwnerScope:           scope.Owner,
		TaskID:               clipMeterID(scope.Task),
		TurnID:               clipMeterID(scope.Turn),
		CallID:               ulid.Make().String(),
		AttemptID:            ulid.Make().String(),
		Purpose:              scope.Purpose,
		Provider:             a.provider,
		DeploymentRef:        a.deployment,
		Model:                req.Model,
		Status:               string(modelfit.CallIntent),
		Integrity:            string(modelfit.UsageUnknown),
		CostStatus:           "unknown",
		StartedAt:            now,
		PolicyVersion:        req.Efficiency.PolicyVersion,
		BytesBefore:          req.Efficiency.BytesBefore,
		BytesAfter:           req.Efficiency.BytesAfter,
		CredentialGeneration: a.credentialGeneration,
	}
	if rec.OwnerScope == "" {
		rec.OwnerScope = "diagnostic"
	}
	if rec.Purpose == "" {
		rec.Purpose = "unknown"
	}
	if err := a.store.PutCallAttemptIntent(ctx, rec); err != nil {
		log.Printf("model call intent not recorded: %v", err)
		return run(emit)
	}
	if err := a.store.MarkCallAttemptSent(ctx, rec.OwnerScope, rec.CallID, rec.AttemptID); err != nil {
		log.Printf("model call sent not recorded: %v", err)
	}
	acc := modelfit.UsageAccumulator{}
	wrap := emit
	if emit != nil {
		wrap = func(d llmadapter.Delta) error {
			if d.Usage != nil {
				acc.ObserveSnapshot(usageNumbers(*d.Usage))
			}
			return emit(d)
		}
	}
	resp, err := run(wrap)
	n := usageNumbers(resp.Usage)
	if acc.Updates > 0 && n.InputTokens == 0 && n.OutputTokens == 0 && n.TotalTokens == 0 {
		n = acc.Latest
	}
	reported := n.InputTokens > 0 || n.OutputTokens > 0 || n.TotalTokens > 0 || resp.Usage.CacheUsageReported
	status := modelfit.CallSucceeded
	if err != nil {
		status = modelfit.CallFailed
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			status = modelfit.CallCancelled
		}
	}
	attempt := modelfit.NewCallAttempt(modelfit.CallIdentity{
		OwnerScope: rec.OwnerScope, TaskID: rec.TaskID, TurnID: rec.TurnID,
		CallID: rec.CallID, AttemptID: rec.AttemptID,
	}, rec.Purpose).Receive(n, reported, status)
	finishCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if finErr := a.store.FinishCallAttempt(finishCtx, rec.OwnerScope, rec.CallID, rec.AttemptID, sqlite.CallAttemptReceipt{
		Status:            string(attempt.Status),
		Integrity:         string(attempt.UsageIntegrity),
		InputTokens:       attempt.Usage.InputTokens,
		OutputTokens:      attempt.Usage.OutputTokens,
		CachedInputTokens: attempt.Usage.CachedInputTokens,
		CacheWriteTokens:  attempt.Usage.CacheWriteTokens,
		CostStatus:        "unknown",
		EndedAt:           time.Now().UTC(),
	}); finErr != nil {
		log.Printf("model call receipt not recorded: %v", finErr)
	}
	return resp, err
}

func usageNumbers(u llmadapter.Usage) modelfit.UsageNumbers {
	return modelfit.UsageNumbers{
		InputTokens:       u.InputTokens,
		OutputTokens:      u.OutputTokens,
		TotalTokens:       u.TotalTokens,
		CachedInputTokens: u.CachedInputTokens,
		CacheWriteTokens:  u.CacheWriteInputTokens,
	}
}

func clipMeterID(s string) string {
	if len(s) > 64 {
		return s[:64]
	}
	return s
}
