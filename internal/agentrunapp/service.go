// Package agentrunapp implements the M4-B application services for the
// reliable single-agent runtime: capability manifest and the
// agent.run.start/get/resume/cancel/reconcile RPC surface. Every mutation
// runs in a single SQLite transaction that carries the domain state change,
// the run event, the audit record and the idempotency record together, so a
// crash can never persist half of a logical operation (PRD M4-FR-01).
package agentrunapp

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/lunitide/lunitide/internal/commandworker"
	"github.com/lunitide/lunitide/internal/domain/agentrun"
	"github.com/lunitide/lunitide/internal/providerapp"
	"github.com/oklog/ulid/v2"
)

var (
	ErrIdempotencyKeyRequired = errors.New("idempotency key is required")
	ErrIdempotencyConflict    = errors.New("idempotency key reused with different request")
)

// Tx is the agent runtime transaction extended with idempotency and audit
// writes so one logical RPC commits atomically.
type Tx interface {
	agentrun.Tx
	Idempotency(op, key string, now time.Time) (providerapp.Record, bool, error)
	PutIdempotency(providerapp.Record) error
	PutAudit(providerapp.Audit) error
}

// UnitOfWork is the single-writer boundary for agent runtime mutations.
type UnitOfWork interface {
	TransactAgentRuntime(ctx context.Context, fn func(Tx) error) error
}

type Clock interface{ Now() time.Time }
type systemClock struct{}

func (systemClock) Now() time.Time { return time.Now().UTC() }

// Service implements the M4-B agent.run.* and capability.list use cases.
type Service struct {
	uow   UnitOfWork
	clock Clock

	// M4-F command execution: runCommand executes resolved specs (default
	// commandworker.Run); cmdCancels holds the per-job kill switches for
	// command.cancel. Both are process-local; after a crash the database
	// rows are reconciled to outcome_unknown.
	runCommand CommandRunner
	cmdMu      sync.Mutex
	cmdCancels map[string]context.CancelFunc
	// cmdRunning counts the launched commands that have not yet written
	// their result. A finished command writes it from its own goroutine, in
	// its own transaction, after the call that started it has long returned
	// — so without this there is no moment at which the caller can say the
	// work is over, and nothing that owns the database can be shut down
	// without cutting a write in half.
	cmdRunning sync.WaitGroup

	// M4-G web access: fetchWeb retrieves URLs through the SSRF-pinned
	// transport (default defaultWebFetch).
	fetchWeb WebFetcher
}

func New(u UnitOfWork) *Service {
	return &Service{uow: u, clock: systemClock{}, runCommand: commandworker.Run, fetchWeb: defaultWebFetch}
}

// SetCommandRunner substitutes the process executor. Tests use it to run
// command jobs without spawning real processes.
func (s *Service) SetCommandRunner(r CommandRunner) {
	s.runCommand = r
}

// DrainCommands blocks until every launched command has written its result.
//
// A command reports its outcome from its own goroutine in its own
// transaction, long after the call that started it returned, so "the request
// finished" and "the database is idle" are different moments. Anything that
// closes the store has to wait for the second one: a connection still inside
// a transaction is not closed by sql.DB.Close, which on Windows leaves the
// database file open and undeletable, and in production would mean tearing
// down storage underneath a half-written result.
func (s *Service) DrainCommands() {
	if s == nil {
		return
	}
	s.cmdRunning.Wait()
}

func (s *Service) available() error {
	if s == nil || s.uow == nil {
		return errors.New("agent runtime unit of work unavailable")
	}
	return nil
}

// ToolDescriptor describes one tool the runtime knows about. Descriptors are
// versioned; a tool is only invocable when Enabled is true, which flips when
// the owning M4 slice lands (M4-D fs, M4-E changeset, M4-F command,
// M4-G web, M4-H plan/evidence).
type ToolDescriptor struct {
	Name           string `json:"name"`
	Version        string `json:"version"`
	Kind           string `json:"kind"`
	Risk           string `json:"risk"`
	Enabled        bool   `json:"enabled"`
	RequiresReview bool   `json:"requiresReview"`
}

// CapabilityManifest is the versioned tool registry snapshot returned by
// capability.list. Digest is the SHA-256 of the canonical JSON encoding of
// the descriptor list, so clients can detect registry drift.
type CapabilityManifest struct {
	ManifestVersion string           `json:"manifestVersion"`
	Digest          string           `json:"digest"`
	Items           []ToolDescriptor `json:"items"`
}

// ManifestVersion tracks the M4 tool registry. Bump when descriptors change.
const ManifestVersion = "m4-capability-v6"

// registry is the authoritative M4 tool descriptor set. All tools are
// disabled until their slice ships; capability.list must never advertise an
// invocable tool that has no executor behind it.
var registry = []ToolDescriptor{
	{Name: "fs.tree", Version: "1.0.0", Kind: "fs", Risk: "low", Enabled: true, RequiresReview: false},
	{Name: "fs.stat", Version: "1.0.0", Kind: "fs", Risk: "low", Enabled: true, RequiresReview: false},
	{Name: "fs.read", Version: "1.0.0", Kind: "fs", Risk: "low", Enabled: true, RequiresReview: false},
	{Name: "fs.readMany", Version: "1.0.0", Kind: "fs", Risk: "low", Enabled: true, RequiresReview: false},
	{Name: "fs.glob", Version: "1.0.0", Kind: "fs", Risk: "low", Enabled: true, RequiresReview: false},
	{Name: "fs.grep", Version: "1.0.0", Kind: "fs", Risk: "low", Enabled: true, RequiresReview: false},
	{Name: "changeset.preview", Version: "1.0.0", Kind: "changeset", Risk: "medium", Enabled: true, RequiresReview: false},
	{Name: "changeset.apply", Version: "1.0.0", Kind: "changeset", Risk: "high", Enabled: true, RequiresReview: true},
	{Name: "changeset.revert", Version: "1.0.0", Kind: "changeset", Risk: "high", Enabled: true, RequiresReview: true},
	{Name: "command.start", Version: "1.0.0", Kind: "command", Risk: "critical", Enabled: true, RequiresReview: true},
	{Name: "command.get", Version: "1.0.0", Kind: "command", Risk: "low", Enabled: true, RequiresReview: false},
	{Name: "command.cancel", Version: "1.0.0", Kind: "command", Risk: "medium", Enabled: true, RequiresReview: false},
	{Name: "web.fetch", Version: "1.0.0", Kind: "web", Risk: "medium", Enabled: true, RequiresReview: false},
	{Name: "web.search", Version: "1.0.0", Kind: "web", Risk: "medium", Enabled: true, RequiresReview: false},
	{Name: "run.plan.put", Version: "1.0.0", Kind: "plan", Risk: "low", Enabled: true, RequiresReview: false},
	{Name: "evidence.list", Version: "1.0.0", Kind: "evidence", Risk: "low", Enabled: true, RequiresReview: false},
}

// Capability returns the current capability manifest. It is a pure read and
// needs no transaction.
func (s *Service) Capability() CapabilityManifest {
	body, _ := json.Marshal(registry)
	sum := sha256.Sum256(body)
	return CapabilityManifest{
		ManifestVersion: ManifestVersion,
		Digest:          hex.EncodeToString(sum[:]),
		Items:           append([]ToolDescriptor(nil), registry...),
	}
}

func requestDigest(request any) (string, error) {
	body, err := json.Marshal(request)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(body)
	return hex.EncodeToString(sum[:]), nil
}

// replay unwraps a previously stored idempotent response.
func replay(record providerapp.Record, digest string, out any) error {
	if record.Digest != digest {
		return ErrIdempotencyConflict
	}
	return json.Unmarshal(record.Response, out)
}

func (s *Service) putAudit(tx Tx, action, aggregateID, actor, digest string, meta []byte, now time.Time) error {
	eventSum := sha256.Sum256([]byte("agent-run-audit\x00" + digest + "\x00" + aggregateID + "\x00" + action))
	var id ulid.ULID
	copy(id[:], eventSum[:16])
	return tx.PutAudit(providerapp.Audit{ID: id.String(), Action: action, AggregateID: aggregateID, Actor: actor, Metadata: meta, CreatedAt: now})
}

// appendRunEvent records one ordered run event in the writer transaction.
// The sequence is derived from the events already persisted for the run.
func appendRunEvent(tx Tx, runID, eventType string, payload any, now time.Time) error {
	events, err := tx.ListEvents(runID)
	if err != nil {
		return err
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	return tx.AppendEvent(agentrun.RunEvent{
		ID:        ulid.Make().String(),
		RunID:     runID,
		Sequence:  int64(len(events)) + 1,
		EventType: eventType,
		Payload:   body,
		CreatedAt: now,
	})
}

func runEventPayload(run agentrun.AgentRun) map[string]any {
	return map[string]any{
		"schemaVersion": 1,
		"runId":         run.ID,
		"sessionId":     run.SessionID,
		"status":        string(run.Status),
		"version":       run.Version,
	}
}

// Start creates a run in queued state and immediately transitions it to
// running inside one transaction (PRD M4 user story: 创建 queued 运行并转为
// running). Replaying the same idempotency key with the same request returns
// the original run without creating a duplicate.
func (s *Service) Start(ctx context.Context, key, actor string, request any, sessionID string, budget agentrun.Budget) (agentrun.AgentRun, error) {
	if !providerapp.ValidIdempotencyKey(key) {
		return agentrun.AgentRun{}, ErrIdempotencyKeyRequired
	}
	if err := s.available(); err != nil {
		return agentrun.AgentRun{}, err
	}
	if err := budget.Validate(); err != nil {
		return agentrun.AgentRun{}, fmt.Errorf("%w: %v", agentrun.ErrInvalid, err)
	}
	digest, err := requestDigest(request)
	if err != nil {
		return agentrun.AgentRun{}, err
	}
	var result agentrun.AgentRun
	err = s.uow.TransactAgentRuntime(ctx, func(tx Tx) error {
		now := s.clock.Now().UTC()
		record, found, err := tx.Idempotency("agent.run.start", key, now)
		if err != nil {
			return err
		}
		if found {
			return replay(record, digest, &result)
		}
		run := agentrun.AgentRun{
			ID:        ulid.Make().String(),
			SessionID: sessionID,
			Status:    agentrun.RunQueued,
			Budget:    budget,
			Version:   1,
			CreatedAt: now,
			UpdatedAt: now,
		}
		if err := tx.PutRun(run); err != nil {
			return err
		}
		run, err = tx.TransitionRun(run.ID, 1, agentrun.RunRunning, now)
		if err != nil {
			return err
		}
		turn := agentrun.AgentTurn{ID: ulid.Make().String(), RunID: run.ID, TurnNo: 1, Status: agentrun.TurnRunning, Version: 1, CreatedAt: now, UpdatedAt: now}
		if err = tx.PutTurn(turn); err != nil {
			return err
		}
		step := agentrun.AgentStep{ID: ulid.Make().String(), TurnID: turn.ID, StepNo: 1, Kind: agentrun.StepModel, Status: agentrun.StepRunning, CreatedAt: now, UpdatedAt: now}
		if err = tx.PutStep(step); err != nil {
			return err
		}
		if err := appendRunEvent(tx, run.ID, "AgentRunStartCompleted", runEventPayload(run), now); err != nil {
			return err
		}
		meta, _ := json.Marshal(map[string]any{"sessionId": run.SessionID, "version": run.Version})
		if err := s.putAudit(tx, "agent.run.started", run.ID, actor, digest, meta, now); err != nil {
			return err
		}
		response, err := json.Marshal(run)
		if err != nil {
			return err
		}
		if err := tx.PutIdempotency(providerapp.Record{Operation: "agent.run.start", Key: key, Digest: digest, Response: response, CreatedAt: now, ExpiresAt: now.Add(24 * time.Hour)}); err != nil {
			return err
		}
		result = run
		return nil
	})
	return result, err
}

// Get loads a run by ID.
func (s *Service) Get(ctx context.Context, runID string) (agentrun.AgentRun, error) {
	if err := s.available(); err != nil {
		return agentrun.AgentRun{}, err
	}
	var result agentrun.AgentRun
	err := s.uow.TransactAgentRuntime(ctx, func(tx Tx) error {
		run, err := tx.GetRun(runID)
		if err != nil {
			return err
		}
		result = run
		return nil
	})
	return result, err
}

// transition covers resume and cancel: an idempotent, CAS-guarded domain
// transition with event + audit in the same transaction.
func (s *Service) transition(ctx context.Context, op, auditAction, eventType, key, actor string, request any, runID string, expectedVersion int64, requiredFrom, to agentrun.RunStatus) (agentrun.AgentRun, error) {
	if !providerapp.ValidIdempotencyKey(key) {
		return agentrun.AgentRun{}, ErrIdempotencyKeyRequired
	}
	if err := s.available(); err != nil {
		return agentrun.AgentRun{}, err
	}
	digest, err := requestDigest(request)
	if err != nil {
		return agentrun.AgentRun{}, err
	}
	var result agentrun.AgentRun
	err = s.uow.TransactAgentRuntime(ctx, func(tx Tx) error {
		now := s.clock.Now().UTC()
		record, found, err := tx.Idempotency(op, key, now)
		if err != nil {
			return err
		}
		if found {
			return replay(record, digest, &result)
		}
		if requiredFrom != "" {
			run, err := tx.GetRun(runID)
			if err != nil {
				return err
			}
			if run.Status != requiredFrom {
				return fmt.Errorf("%w: %s requires %s, got %s", agentrun.ErrInvalidTransition, op, requiredFrom, run.Status)
			}
		}
		run, err := tx.TransitionRun(runID, expectedVersion, to, now)
		if err != nil {
			return err
		}
		if err := appendRunEvent(tx, run.ID, eventType, runEventPayload(run), now); err != nil {
			return err
		}
		meta, _ := json.Marshal(map[string]any{"status": string(run.Status), "version": run.Version})
		if err := s.putAudit(tx, auditAction, run.ID, actor, digest, meta, now); err != nil {
			return err
		}
		response, err := json.Marshal(run)
		if err != nil {
			return err
		}
		if err := tx.PutIdempotency(providerapp.Record{Operation: op, Key: key, Digest: digest, Response: response, CreatedAt: now, ExpiresAt: now.Add(24 * time.Hour)}); err != nil {
			return err
		}
		result = run
		return nil
	})
	return result, err
}

// Resume moves a paused_budget run back to running. A paused_review run must
// only resume through review.decide so the approval digest cannot be bypassed.
func (s *Service) Resume(ctx context.Context, key, actor string, request any, runID string, expectedVersion int64) (agentrun.AgentRun, error) {
	return s.transition(ctx, "agent.run.resume", "agent.run.resumed", "AgentRunResumeCompleted", key, actor, request, runID, expectedVersion, agentrun.RunPausedBudget, agentrun.RunRunning)
}

// Cancel moves a running run to cancelled (first-terminal-wins).
func (s *Service) Cancel(ctx context.Context, key, actor string, request any, runID string, expectedVersion int64) (agentrun.AgentRun, error) {
	return s.transition(ctx, "agent.run.cancel", "agent.run.cancelled", "AgentRunCancelCompleted", key, actor, request, runID, expectedVersion, "", agentrun.RunCancelled)
}

// ReconcileResult pairs the run snapshot with the number of effect journal
// entries that were resolved from prepared to outcome_unknown.
type ReconcileResult struct {
	Run               agentrun.AgentRun `json:"run"`
	ReconciledEffects int               `json:"reconciledEffects"`
}

// Reconcile resolves dangling prepared effects after a crash. A prepared
// effect has no receipt, so the system cannot prove the external world saw
// it: it is resolved to outcome_unknown and never blindly retried (PRD M4
// 崩溃后按 journal/receipt 对账, MUST NOT 盲重试). The run row itself keeps
// its status; only a version CAS guards the caller's view.
func (s *Service) Reconcile(ctx context.Context, key, actor string, request any, runID string, expectedVersion int64) (ReconcileResult, error) {
	if !providerapp.ValidIdempotencyKey(key) {
		return ReconcileResult{}, ErrIdempotencyKeyRequired
	}
	if err := s.available(); err != nil {
		return ReconcileResult{}, err
	}
	digest, err := requestDigest(request)
	if err != nil {
		return ReconcileResult{}, err
	}
	var result ReconcileResult
	err = s.uow.TransactAgentRuntime(ctx, func(tx Tx) error {
		now := s.clock.Now().UTC()
		record, found, err := tx.Idempotency("agent.run.reconcile", key, now)
		if err != nil {
			return err
		}
		if found {
			return replay(record, digest, &result)
		}
		run, err := tx.GetRun(runID)
		if err != nil {
			return err
		}
		if run.Version != expectedVersion {
			return agentrun.ErrVersionConflict
		}
		effects, err := tx.ListEffects(runID)
		if err != nil {
			return err
		}
		reconciled := 0
		for _, effect := range effects {
			if effect.Status != agentrun.EffectPrepared {
				continue
			}
			resolved, err := effect.Resolve(agentrun.EffectOutcomeUnknown, "", now)
			if err != nil {
				return err
			}
			if err := tx.PutEffect(resolved); err != nil {
				return err
			}
			reconciled++
		}
		if err := appendRunEvent(tx, run.ID, "AgentRunReconcileCompleted", map[string]any{
			"schemaVersion":     1,
			"runId":             run.ID,
			"status":            string(run.Status),
			"version":           run.Version,
			"reconciledEffects": reconciled,
		}, now); err != nil {
			return err
		}
		meta, _ := json.Marshal(map[string]any{"reconciledEffects": reconciled, "version": run.Version})
		if err := s.putAudit(tx, "agent.run.reconciled", run.ID, actor, digest, meta, now); err != nil {
			return err
		}
		result = ReconcileResult{Run: run, ReconciledEffects: reconciled}
		response, err := json.Marshal(result)
		if err != nil {
			return err
		}
		return tx.PutIdempotency(providerapp.Record{Operation: "agent.run.reconcile", Key: key, Digest: digest, Response: response, CreatedAt: now, ExpiresAt: now.Add(24 * time.Hour)})
	})
	return result, err
}
