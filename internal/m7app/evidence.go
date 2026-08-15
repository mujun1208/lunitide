// M7 slice 2 application services (T-7.2.2..T-7.2.5): the trace engine
// with stale propagation, the gate evaluator with checkpoints, and the
// review service. All services share the agent-runtime single-writer
// transaction (EvidenceTx) so state changes, evidence rows and (later)
// audit events commit atomically.
package m7app

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/lunitide/lunitide/internal/domain/m7flow"
	"github.com/oklog/ulid/v2"
)

var (
	// ErrEvidenceServiceUnavailable: unit of work missing.
	ErrEvidenceServiceUnavailable = errors.New("m7app: evidence unit of work unavailable")
	// ErrEdgeNotFound / ErrStaleNotFound / ErrGateNotFound.
	ErrEdgeNotFound  = errors.New("m7app: trace edge not found")
	ErrStaleNotFound = errors.New("m7app: stale mark not found")
	ErrGateNotFound  = errors.New("m7app: gate evaluation not found")
	// ErrBadDepth / ErrBadDirection: trace.query guards.
	ErrBadDepth     = errors.New("m7app: trace depth must be 1..10")
	ErrBadDirection = errors.New("m7app: trace direction must be up or down")
	// ErrUnknownNodeType: endpoint type not in the traceable registry.
	ErrUnknownNodeType = errors.New("m7app: unknown trace node type")
	// ErrDuplicateEdge: identical endpoints+relation already stored with a
	// different digest.
	ErrDuplicateEdge = errors.New("m7app: trace edge already exists")
	// ErrTaskNotFound / ErrTaskTransition: dev-task guards.
	ErrTaskNotFound   = errors.New("m7app: dev task not found")
	ErrTaskTransition = errors.New("m7app: illegal dev task transition")
	// ErrEvidenceNotFound: generic evidence row miss.
	ErrEvidenceNotFound = errors.New("m7app: evidence not found")
	// ErrBadNodeRef: malformed trace endpoint.
	ErrBadNodeRef = errors.New("m7app: malformed trace node reference")
)

// EvidenceTx is the slice-2 single-writer transaction (sqlite agentRuntimeTx
// satisfies it alongside WorkflowTx). The two workflow reads (GetStageRun,
// LatestInputSnapshot) are declared here so gate evaluation and checkpoints
// read stage state on the *same* transaction — Transact does not support
// nesting, so re-entering TransactM7 from inside TransactEvidence would
// deadlock on the SQLite write lock.
type EvidenceTx interface {
	// Workflow reads shared with WorkflowTx.
	GetStageRun(id string) (m7flow.StageRun, error)
	LatestInputSnapshot(stageRunID string) (m7flow.InputSnapshot, error)
	// Trace edges.
	NodeExists(nodeType, nodeID string) (bool, error)
	FindEdge(fromType, fromID, relation, toType, toID string) (m7flow.TraceEdge, error)
	PutEdge(m7flow.TraceEdge) error
	EdgesFrom(fromType, fromID string, limit int) ([]m7flow.TraceEdge, error)
	EdgesTo(toType, toID string, limit int) ([]m7flow.TraceEdge, error)
	// Stale propagation.
	PutStaleMark(m7flow.StaleMark) error
	FindStaleMark(subjectType, subjectID string) (m7flow.StaleMark, error)
	GetStaleMark(id string) (m7flow.StaleMark, error)
	PutStaleResolution(m7flow.StaleResolution) error
	StaleResolutions(markID string) ([]m7flow.StaleResolution, error)
	// Gate evaluations and checkpoints.
	PutGateEvaluation(m7flow.GateEvaluation) error
	FindGateEvaluation(stageRunID, gateKey, inputDigest string) (m7flow.GateEvaluation, error)
	LatestGateEvaluation(stageRunID, gateKey string) (m7flow.GateEvaluation, error)
	PutCheckpoint(m7flow.Checkpoint) error
	MaxCheckpointSequence(stageRunID string) (int64, error)
	// Reviews.
	PutReview(m7flow.Review) error
	LatestApprovedReview(subjectType, subjectID string, subjectVersion int64) (m7flow.Review, error)
	// Dev tasks.
	PutDevTask(m7flow.DevTask) error
	GetDevTask(id string) (m7flow.DevTask, error)
	UpdateDevTaskState(id string, expectedVersion int64, to, stateReason string) (m7flow.DevTask, error)
	TaskStatesForRun(stageRunID string) (map[string]int, error)
	// Test/scan evidence.
	PutTestRun(m7flow.TestRun) error
	PutScanRun(m7flow.ScanRun) error
	TestResultsForTask(taskRef string) (map[string]int, error)
	ScanResultsForTask(taskRef string) (map[string]int, error)
}

// EvidenceUnitOfWork mirrors WorkflowUnitOfWork for slice 2.
type EvidenceUnitOfWork interface {
	TransactEvidence(ctx context.Context, fn func(EvidenceTx) error) error
}

// TraceService implements trace.add / trace.query / stale marking (T-7.2.2,
// T-7.2.3).
type TraceService struct {
	uow   EvidenceUnitOfWork
	clock Clock
}

func NewTraceService(uow EvidenceUnitOfWork) *TraceService {
	return &TraceService{uow: uow, clock: systemClock{}}
}

// SetClock substitutes the wall clock (tests).
func (s *TraceService) SetClock(c Clock) { s.clock = c }

// AddEdge validates both endpoints (TRC-001), rejects conflicting duplicates
// and stores the edge. Re-adding the identical edge answers the stored one.
func (s *TraceService) AddEdge(ctx context.Context, e m7flow.TraceEdge) (m7flow.TraceEdge, error) {
	if s == nil || s.uow == nil {
		return m7flow.TraceEdge{}, ErrEvidenceServiceUnavailable
	}
	if !m7flow.LegalRelation(e.Relation) {
		return m7flow.TraceEdge{}, m7flow.ErrBadRelation
	}
	if e.FromType == "" || e.FromID == "" || e.ToType == "" || e.ToID == "" ||
		len(e.FromDigest) != 64 || len(e.ToDigest) != 64 {
		return m7flow.TraceEdge{}, ErrBadNodeRef
	}
	var out m7flow.TraceEdge
	err := s.uow.TransactEvidence(ctx, func(tx EvidenceTx) error {
		for _, ref := range []struct{ typ, id string }{
			{e.FromType, e.FromID}, {e.ToType, e.ToID},
		} {
			ok, err := tx.NodeExists(ref.typ, ref.id)
			if err != nil {
				return err
			}
			if !ok {
				return fmt.Errorf("%w: %s/%s", m7flow.ErrDanglingEdge, ref.typ, ref.id)
			}
		}
		if existing, err := tx.FindEdge(e.FromType, e.FromID, e.Relation, e.ToType, e.ToID); err == nil {
			if existing.FromDigest == e.FromDigest && existing.ToDigest == e.ToDigest {
				out = existing
				return nil // idempotent
			}
			return ErrDuplicateEdge
		} else if !errors.Is(err, sql.ErrNoRows) && !errors.Is(err, m7flow.ErrNotFound) {
			return err
		}
		if e.ID == "" {
			e.ID = ulid.Make().String()
		}
		e.CreatedAt = s.clock.Now().UTC()
		out = e
		return tx.PutEdge(e)
	})
	return out, err
}

// EdgePage is one trace.query result page.
type EdgePage struct {
	Edges      []m7flow.TraceEdge
	NextCursor string
}

// Query walks the trace graph up or down from a root for at most depth hops
// (1..10), breadth-first with cursor pagination.
func (s *TraceService) Query(ctx context.Context, rootType, rootID, direction string, depth int, cursor string) (EdgePage, error) {
	if s == nil || s.uow == nil {
		return EdgePage{}, ErrEvidenceServiceUnavailable
	}
	if depth < 1 || depth > 10 {
		return EdgePage{}, ErrBadDepth
	}
	if direction != "up" && direction != "down" {
		return EdgePage{}, ErrBadDirection
	}
	const pageSize = 50
	visited := map[string]bool{}
	var collected []m7flow.TraceEdge
	frontier := []struct{ typ, id string }{{rootType, rootID}}
	var walkErr error
	err := s.uow.TransactEvidence(ctx, func(tx EvidenceTx) error {
		for hop := 0; hop < depth && len(frontier) > 0; hop++ {
			var next []struct{ typ, id string }
			for _, node := range frontier {
				edges, err := walkStep(tx, node, direction, pageSize*depth)
				if err != nil {
					walkErr = err
					return nil
				}
				for _, e := range edges {
					if cursor != "" && e.ID <= cursor {
						continue
					}
					collected = append(collected, e)
					var peer struct{ typ, id string }
					if direction == "down" {
						peer.typ, peer.id = e.ToType, e.ToID
					} else {
						peer.typ, peer.id = e.FromType, e.FromID
					}
					if !visited[peer.typ+"/"+peer.id] {
						visited[peer.typ+"/"+peer.id] = true
						next = append(next, peer)
					}
				}
			}
			frontier = next
		}
		return nil
	})
	if walkErr != nil {
		return EdgePage{}, walkErr
	}
	if err != nil {
		return EdgePage{}, err
	}
	page := EdgePage{Edges: collected}
	if len(page.Edges) > pageSize {
		page.NextCursor = page.Edges[pageSize-1].ID
		page.Edges = page.Edges[:pageSize]
	}
	return page, nil
}

func walkStep(tx EvidenceTx, node struct{ typ, id string }, direction string, limit int) ([]m7flow.TraceEdge, error) {
	if direction == "down" {
		return tx.EdgesFrom(node.typ, node.id, limit)
	}
	return tx.EdgesTo(node.typ, node.id, limit)
}

// MarkStale records one stale mark for a subject caused by an edge (the
// digest-drift edge). Idempotent per subject.
func (s *TraceService) MarkStale(ctx context.Context, subjectType, subjectID, causeEdgeID string) (m7flow.StaleMark, error) {
	if s == nil || s.uow == nil {
		return m7flow.StaleMark{}, ErrEvidenceServiceUnavailable
	}
	var out m7flow.StaleMark
	err := s.uow.TransactEvidence(ctx, func(tx EvidenceTx) error {
		if existing, err := tx.FindStaleMark(subjectType, subjectID); err == nil {
			out = existing
			return nil
		} else if !errors.Is(err, sql.ErrNoRows) && !errors.Is(err, m7flow.ErrNotFound) {
			return err
		}
		out = m7flow.StaleMark{
			ID: ulid.Make().String(), SubjectType: subjectType, SubjectID: subjectID,
			CauseEdge: causeEdgeID, DetectedAt: s.clock.Now().UTC(),
		}
		return tx.PutStaleMark(out)
	})
	return out, err
}

// ResolveStale closes a stale mark with an append-only resolution row
// (recaptured / reevaluated / waived).
func (s *TraceService) ResolveStale(ctx context.Context, markID, resolutionType, reevaluationID, resolvedBy string) (m7flow.StaleResolution, error) {
	if s == nil || s.uow == nil {
		return m7flow.StaleResolution{}, ErrEvidenceServiceUnavailable
	}
	switch resolutionType {
	case m7flow.ResolveRecaptured, m7flow.ResolveReevaluated, m7flow.ResolveWaived:
	default:
		return m7flow.StaleResolution{}, m7flow.ErrBadResolution
	}
	var out m7flow.StaleResolution
	err := s.uow.TransactEvidence(ctx, func(tx EvidenceTx) error {
		if _, err := tx.GetStaleMark(markID); err != nil {
			return ErrStaleNotFound
		}
		out = m7flow.StaleResolution{
			ID: ulid.Make().String(), StaleMarkID: markID,
			ResolutionType: resolutionType, ReevaluationID: reevaluationID,
			ResolvedBy: resolvedBy, ResolvedAt: s.clock.Now().UTC(),
		}
		return tx.PutStaleResolution(out)
	})
	return out, err
}

// OutstandingStale reports unresolved stale marks for a subject (a mark with
// no resolution rows yet).
func (s *TraceService) OutstandingStale(ctx context.Context, subjectType, subjectID string) (bool, error) {
	if s == nil || s.uow == nil {
		return false, ErrEvidenceServiceUnavailable
	}
	var outstanding bool
	err := s.uow.TransactEvidence(ctx, func(tx EvidenceTx) error {
		mark, err := tx.FindStaleMark(subjectType, subjectID)
		if errors.Is(err, sql.ErrNoRows) || errors.Is(err, m7flow.ErrNotFound) {
			return nil
		}
		if err != nil {
			return err
		}
		res, err := tx.StaleResolutions(mark.ID)
		if err != nil {
			return err
		}
		outstanding = len(res) == 0
		return nil
	})
	return outstanding, err
}

// GateService implements workflow.evaluateGate and checkpoint creation
// (T-7.2.4). Stage-run reads go through the shared evidence transaction —
// the workflow unit of work is never re-entered from inside a transaction.
type GateService struct {
	uow   EvidenceUnitOfWork
	clock Clock
}

func NewGateService(uow EvidenceUnitOfWork) *GateService {
	return &GateService{uow: uow, clock: systemClock{}}
}

// SetClock substitutes the wall clock (tests).
func (s *GateService) SetClock(c Clock) { s.clock = c }

// Evaluate recomputes one gate server-side. The decision order is fixed:
// outstanding stale blocks first (TRC-002), then input completeness and the
// gate rules decide FAIL with findings (GAT-001), otherwise PASS. Identical
// inputs answer the stored evaluation (idempotent) — "inputs" means the full
// decision state (snapshot, reviews, tasks, tests, scans, stale), not just
// the captured snapshot, so new evidence always re-evaluates.
func (s *GateService) Evaluate(ctx context.Context, stageRunID, gateKey string) (m7flow.GateEvaluation, error) {
	if s == nil || s.uow == nil {
		return m7flow.GateEvaluation{}, ErrEvidenceServiceUnavailable
	}
	var out m7flow.GateEvaluation
	err := s.uow.TransactEvidence(ctx, func(tx EvidenceTx) error {
		run, err := tx.GetStageRun(stageRunID)
		if err != nil {
			return ErrStageRunNotFound
		}
		snapshotDigest, inputDigest := gateInputDigest(tx, run)
		if prior, err := tx.FindGateEvaluation(stageRunID, gateKey, inputDigest); err == nil {
			out = prior
			return nil
		} else if !errors.Is(err, sql.ErrNoRows) && !errors.Is(err, m7flow.ErrNotFound) {
			return err
		}
		decision, findings := s.evaluateRules(ctx, tx, run, gateKey, snapshotDigest)
		out = m7flow.GateEvaluation{
			ID: ulid.Make().String(), StageRunID: stageRunID, GateKey: gateKey,
			InputDigest: inputDigest, Decision: decision, Findings: findings,
			CreatedAt: s.clock.Now().UTC(),
		}
		return tx.PutGateEvaluation(out)
	})
	return out, err
}

// gateInputDigest folds every state a gate decision depends on into one
// digest: the latest snapshot plus review/task/test/scan/stale facts. It is
// shared by evaluation (idempotence key) and checkpoints (drift check).
func gateInputDigest(tx EvidenceTx, run m7flow.StageRun) (snapshotDigest, inputDigest string) {
	var latestErr error
	snapshotDigest, latestErr = latestSnapshotDigest(tx, run.ID)
	factors := map[string]any{
		"stageRunID": run.ID,
		"state":      run.State,
		"snapshot":   snapshotDigest,
		"snapshotOK": latestErr == nil,
	}
	if rev, err := tx.LatestApprovedReview("stage_run", run.ID, 0); err == nil {
		factors["approvedReview"] = rev.ID
	}
	factors["taskStates"], _ = tx.TaskStatesForRun(run.ID)
	factors["testResults"], _ = tx.TestResultsForTask(run.ID)
	factors["scanResults"], _ = tx.ScanResultsForTask(run.ID)
	if stale, err := tx.FindStaleMark("stage_run", run.ID); err == nil {
		resolutions, _ := tx.StaleResolutions(stale.ID)
		factors["staleResolutions"] = len(resolutions)
	}
	return snapshotDigest, m7flow.Digest256(factors)
}

func latestSnapshotDigest(tx EvidenceTx, stageRunID string) (string, error) {
	snap, err := tx.LatestInputSnapshot(stageRunID)
	if err != nil {
		return "", err
	}
	return snap.Digest, nil
}

// evaluateRules applies the fixed decision order and the per-gate rules.
func (s *GateService) evaluateRules(ctx context.Context, tx EvidenceTx, run m7flow.StageRun, gateKey, snapshotDigest string) (string, []m7flow.Finding) {
	// 1. Outstanding stale blocks (TRC-002).
	if stale, err := tx.FindStaleMark("stage_run", run.ID); err == nil {
		if res, rerr := tx.StaleResolutions(stale.ID); rerr == nil && len(res) == 0 {
			return m7flow.GateBlocked, []m7flow.Finding{{
				Code: "M7-TRC-002", Message: "存在未清除的过期标记，需重捕获或重评后重试",
				Severity: "blocker", Ref: stale.ID,
			}}
		}
	}
	findings := []m7flow.Finding{}
	// 2. Input completeness (GAT-001).
	if snapshotDigest == "" {
		findings = append(findings, m7flow.Finding{
			Code: "M7-GAT-001", Message: "阶段输入快照缺失，无法求值门禁", Severity: "error",
		})
	}
	switch gateKey {
	case "stage.exit":
		// Exit needs an approved review bound to this run.
		if _, err := tx.LatestApprovedReview("stage_run", run.ID, 0); err != nil {
			findings = append(findings, m7flow.Finding{
				Code: "M7-GAT-001", Message: "阶段退出需要一次通过评审（waiting_review → approved）", Severity: "error",
			})
		}
	case "dev.integration":
		// Every dev task closed and tests green.
		states, _ := tx.TaskStatesForRun(run.ID)
		open := states[m7flow.TaskDraft] + states[m7flow.TaskReady] + states[m7flow.TaskInProgress] +
			states[m7flow.TaskBlocked] + states[m7flow.TaskInReview] + states[m7flow.TaskReopened]
		if open > 0 {
			findings = append(findings, m7flow.Finding{
				Code: "M7-GAT-001", Message: "存在未关闭的开发任务", Severity: "error",
			})
		}
		results, _ := tx.TestResultsForTask(run.ID)
		if results["pass"] == 0 || results["fail"]+results["error"]+results["timeout"] > 0 {
			findings = append(findings, m7flow.Finding{
				Code: "M7-GAT-001", Message: "测试证据不足或存在失败", Severity: "error",
			})
		}
	case "verify.security":
		scans, _ := tx.ScanResultsForTask(run.ID)
		if scans["pass"] == 0 {
			findings = append(findings, m7flow.Finding{
				Code: "M7-GAT-001", Message: "缺少通过的安全扫描证据", Severity: "error",
			})
		}
	case "release.package":
		scans, _ := tx.ScanResultsForTask(run.ID)
		if scans["pass"] == 0 {
			findings = append(findings, m7flow.Finding{
				Code: "M7-GAT-001", Message: "封装前缺少通过的安全扫描证据", Severity: "error",
			})
		}
	default:
		findings = append(findings, m7flow.Finding{
			Code: "M7-GAT-002", Message: "未知门禁键，策略拒绝求值", Severity: "error", Ref: gateKey,
		})
	}
	if len(findings) > 0 {
		return m7flow.GateFail, findings
	}
	return m7flow.GatePass, findings
}

// CreateCheckpoint appends the next checkpoint for a stage run. Only a PASS
// stage.exit evaluation over the *current* input digest may create one
// (CHK-001); the sequence is per-run monotonic.
func (s *GateService) CreateCheckpoint(ctx context.Context, stageRunID string) (m7flow.Checkpoint, error) {
	if s == nil || s.uow == nil {
		return m7flow.Checkpoint{}, ErrEvidenceServiceUnavailable
	}
	var out m7flow.Checkpoint
	err := s.uow.TransactEvidence(ctx, func(tx EvidenceTx) error {
		latest, err := tx.LatestGateEvaluation(stageRunID, "stage.exit")
		if err != nil {
			return fmt.Errorf("%w: no stage.exit evaluation", m7flow.ErrCheckpointDenied)
		}
		if latest.Decision != m7flow.GatePass {
			return fmt.Errorf("%w: stage.exit=%s", m7flow.ErrCheckpointDenied, latest.Decision)
		}
		run, err := tx.GetStageRun(stageRunID)
		if err != nil {
			return fmt.Errorf("%w: %v", m7flow.ErrCheckpointDenied, err)
		}
		snapshotDigest, inputDigest := gateInputDigest(tx, run)
		if latest.InputDigest != inputDigest {
			return fmt.Errorf("%w: inputs changed after evaluation", m7flow.ErrCheckpointDenied)
		}
		if stale, err := tx.FindStaleMark("stage_run", stageRunID); err == nil {
			if res, rerr := tx.StaleResolutions(stale.ID); rerr == nil && len(res) == 0 {
				return fmt.Errorf("%w: stale outstanding", m7flow.ErrCheckpointDenied)
			}
		}
		seq, err := tx.MaxCheckpointSequence(stageRunID)
		if err != nil {
			return err
		}
		out = m7flow.Checkpoint{
			ID: ulid.Make().String(), StageRunID: stageRunID,
			SnapshotDigest: snapshotDigest, TraceRoot: stageRunID,
			Sequence: seq + 1, CreatedAt: s.clock.Now().UTC(),
		}
		return tx.PutCheckpoint(out)
	})
	return out, err
}

// ReviewService implements review.submit (T-7.2.5).
type ReviewService struct {
	uow   EvidenceUnitOfWork
	trace *TraceService
	clock Clock
}

func NewReviewService(uow EvidenceUnitOfWork, trace *TraceService) *ReviewService {
	return &ReviewService{uow: uow, trace: trace, clock: systemClock{}}
}

// SetClock substitutes the wall clock (tests).
func (s *ReviewService) SetClock(c Clock) { s.clock = c }

// SubmitReview stores one immutable review and its trace edge. The reviewer
// must differ from the subject author (REV-001) and the subject must exist
// (TRC-001 — the auto edge must not dangle). History is append-only: a
// change of mind is a new row.
func (s *ReviewService) SubmitReview(ctx context.Context, rev m7flow.Review, authorID string) (m7flow.Review, string, error) {
	if s == nil || s.uow == nil {
		return m7flow.Review{}, "", ErrEvidenceServiceUnavailable
	}
	if rev.Verdict != m7flow.VerdictApprove && rev.Verdict != m7flow.VerdictReject {
		return m7flow.Review{}, "", m7flow.ErrBadVerdict
	}
	if rev.ReviewerID == authorID {
		return m7flow.Review{}, "", m7flow.ErrSelfReview
	}
	if rev.SubjectVersion < 1 {
		// reviews.subject_version is 1-based; subjects without a version
		// notion (stage_run, review) pin version 1.
		rev.SubjectVersion = 1
	}
	var out m7flow.Review
	var edgeID string
	err := s.uow.TransactEvidence(ctx, func(tx EvidenceTx) error {
		if ok, err := tx.NodeExists(rev.SubjectType, rev.SubjectID); err != nil {
			return err
		} else if !ok {
			return fmt.Errorf("%w: %s/%s", m7flow.ErrDanglingEdge, rev.SubjectType, rev.SubjectID)
		}
		if rev.ID == "" {
			rev.ID = ulid.Make().String()
		}
		rev.CreatedAt = s.clock.Now().UTC()
		if err := tx.PutReview(rev); err != nil {
			return err
		}
		edge := m7flow.TraceEdge{
			ID: ulid.Make().String(),
			FromType: "review", FromID: rev.ID,
			FromDigest: m7flow.Digest256(map[string]any{
				"verdict": rev.Verdict, "subjectType": rev.SubjectType,
				"subjectID": rev.SubjectID, "subjectVersion": rev.SubjectVersion,
			}),
			Relation: m7flow.RelReviews,
			ToType:   rev.SubjectType, ToID: rev.SubjectID,
			ToDigest: m7flow.Digest256(map[string]any{"id": rev.SubjectID, "version": rev.SubjectVersion}),
			CreatedAt: rev.CreatedAt,
		}
		if err := tx.PutEdge(edge); err != nil {
			return err
		}
		out, edgeID = rev, edge.ID
		return nil
	})
	return out, edgeID, err
}

// CreateDevTask opens a task under a stage run with a canonical acceptance
// digest.
func (s *TraceService) CreateDevTask(ctx context.Context, task m7flow.DevTask) (m7flow.DevTask, error) {
	if s == nil || s.uow == nil {
		return m7flow.DevTask{}, ErrEvidenceServiceUnavailable
	}
	var out m7flow.DevTask
	err := s.uow.TransactEvidence(ctx, func(tx EvidenceTx) error {
		if task.ID == "" {
			task.ID = ulid.Make().String()
		}
		if task.State == "" {
			task.State = m7flow.TaskDraft
		}
		if task.LockVersion == 0 {
			task.LockVersion = 1
		}
		if task.CreatedAt.IsZero() {
			task.CreatedAt = s.clock.Now().UTC()
		}
		out = task
		return tx.PutDevTask(task)
	})
	return out, err
}

// TransitionDevTask applies one canonical task transition under the
// optimistic lock.
func (s *TraceService) TransitionDevTask(ctx context.Context, id string, expectedVersion int64, to, stateReason string) (m7flow.DevTask, error) {
	if s == nil || s.uow == nil {
		return m7flow.DevTask{}, ErrEvidenceServiceUnavailable
	}
	var out m7flow.DevTask
	err := s.uow.TransactEvidence(ctx, func(tx EvidenceTx) error {
		task, err := tx.GetDevTask(id)
		if err != nil {
			return ErrTaskNotFound
		}
		if expectedVersion > 0 && task.LockVersion != expectedVersion {
			return fmt.Errorf("%w: expected %d, current %d", ErrVersionConflict, expectedVersion, task.LockVersion)
		}
		if !m7flow.LegalTaskTransition(task.State, to) {
			return fmt.Errorf("%w: %s -> %s", ErrTaskTransition, task.State, to)
		}
		out, err = tx.UpdateDevTaskState(id, task.LockVersion, to, stateReason)
		return err
	})
	return out, err
}

// AttachTestRun appends one test-run evidence row bound to a task reference.
func (s *TraceService) AttachTestRun(ctx context.Context, taskRef, result, reportDigest string) (m7flow.TestRun, error) {
	if s == nil || s.uow == nil {
		return m7flow.TestRun{}, ErrEvidenceServiceUnavailable
	}
	tr := m7flow.TestRun{
		ID: ulid.Make().String(), TaskRef: taskRef, Result: result,
		ReportDigest: reportDigest, CreatedAt: s.clock.Now().UTC(),
	}
	err := s.uow.TransactEvidence(ctx, func(tx EvidenceTx) error { return tx.PutTestRun(tr) })
	return tr, err
}

// AttachScanRun appends one scan-run evidence row bound to a task reference.
func (s *TraceService) AttachScanRun(ctx context.Context, taskRef, scanner, severityGate, reportDigest string) (m7flow.ScanRun, error) {
	if s == nil || s.uow == nil {
		return m7flow.ScanRun{}, ErrEvidenceServiceUnavailable
	}
	sr := m7flow.ScanRun{
		ID: ulid.Make().String(), TaskRef: taskRef, Scanner: scanner,
		SeverityGate: severityGate, ReportDigest: reportDigest,
		CreatedAt: s.clock.Now().UTC(),
	}
	err := s.uow.TransactEvidence(ctx, func(tx EvidenceTx) error { return tx.PutScanRun(sr) })
	return sr, err
}
