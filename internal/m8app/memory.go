// Package m8app hosts the M8 application services. memory.go is the
// slice-1 governed long-term memory core (T-8.1.x): candidate proposal,
// explicit token-bound confirmation (FR-02), leaf-source promotion
// (FR-03), inference-promotion refusal (FR-11) and explained recall
// (FR-04). Confidence, frequency and compaction never promote a candidate
// - the confirmation token is the only path to memory_facts.
package m8app

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/oklog/ulid/v2"

	"github.com/lunitide/lunitide/internal/audit"
	"github.com/lunitide/lunitide/internal/domain/m8core"
)

// M8 slice-1 error family (04 错误矩阵 M8-001~010 + global M8-027).
var (
	// ErrCandidateNotFound: candidate missing (M8-001, 404).
	ErrCandidateNotFound = errors.New("m8app: candidate not found")
	// ErrCandidateExpired: candidate past expiry (M8-002, 409).
	ErrCandidateExpired = errors.New("m8app: candidate expired")
	// ErrExplicitConfirmationRequired: promotion without confirmation
	// (M8-003, 428).
	ErrExplicitConfirmationRequired = errors.New("m8app: explicit confirmation required")
	// ErrConfirmTokenInvalid: token wrong, tampered or replayed (M8-004, 401).
	ErrConfirmTokenInvalid = errors.New("m8app: confirmation token invalid")
	// ErrPayloadDigestMismatch: candidate payload drifted after token
	// issuance (M8-005, 409).
	ErrPayloadDigestMismatch = errors.New("m8app: payload digest mismatch")
	// ErrInferencePromotionDenied: auto promotion of inferred/untrusted
	// candidates is forbidden (M8-006, 403; FR-11).
	ErrInferencePromotionDenied = errors.New("m8app: inference promotion denied")
	// ErrSourceLeafRequired: payload has no leaf coverage (M8-007, 422).
	ErrSourceLeafRequired = errors.New("m8app: source leaf required")
	// ErrSourceEvidenceUnavailable: leaf evidence failed verification
	// (M8-008, 409).
	ErrSourceEvidenceUnavailable = errors.New("m8app: source evidence unavailable")
	// ErrRecallScopeDenied: subject/scope policy refused the recall
	// (M8-009, 403).
	ErrRecallScopeDenied = errors.New("m8app: recall scope denied")
	// ErrExplanationUnavailable: recall explanation cannot be produced, so
	// no results are returned (M8-010, 503).
	ErrExplanationUnavailable = errors.New("m8app: recall explanation unavailable")
	// ErrPolicyUnavailable: policy engine unavailable - fail closed on
	// every write (M8-027, 503).
	ErrPolicyUnavailable = errors.New("m8app: policy unavailable")
	// ErrServiceUnavailable: unit of work missing.
	ErrServiceUnavailable = errors.New("m8app: unit of work unavailable")
	// ErrPayloadInvalid: payload document failed domain validation.
	ErrPayloadInvalid = errors.New("m8app: payload document invalid")
)

// MemoryTx is the slice-1 single-writer transaction: memory-core tables
// plus the shared audit ledger only.
type MemoryTx interface {
	PutCandidate(m8core.MemoryCandidate) error
	GetCandidate(id string) (m8core.MemoryCandidate, error)
	TransitionCandidate(id, from, to, confirmedAt string) error
	PutFact(m8core.MemoryFact) error
	PutSourceLeaves([]m8core.SourceLeaf) error
	PutRecallTrace(m8core.RecallTrace) error
	ListActiveFactsWithLeaves(scopeID string) ([]m8core.MemoryFact, map[string][]m8core.SourceLeaf, error)
	ListCandidatesByState(state string, limit int) ([]m8core.MemoryCandidate, error)
	AppendFeedbackEvent(FeedbackEvent) error
	AppendAuditEvent(audit.Event) (audit.Event, error)
}

// FeedbackEvent is one append-only row of the learning loop evidence ledger
// (migration 0065 feedback_events): feedback only ever forms new evidence,
// it never rewrites history in place.
type FeedbackEvent struct {
	EventID    string
	SubjectID  string
	Action     string // accept | reject | correct
	TargetType string
	TargetID   string
	Evidence   string // JSON
	CreatedAt  string
}

// Feedback actions accepted by RecordFeedback.
const (
	FeedbackAccept  = "accept"
	FeedbackReject  = "reject"
	FeedbackCorrect = "correct"
)

// LearningScope is the single-user scope every chat-originated preference
// candidate and chat-side preference snapshot reads from.
const LearningScope = "local"

// MemoryUnitOfWork is the slice-1 single-writer boundary.
type MemoryUnitOfWork interface {
	TransactMemory(ctx context.Context, fn func(MemoryTx) error) error
}

// PolicyProbe decides whether subject may read scope. An error means the
// policy engine itself is unavailable -> fail closed (M8-027).
type PolicyProbe func(ctx context.Context, subjectID, scopeID string) (bool, error)

// DefaultScopePolicy serves the local single-user engine: every scope is
// owned by the local subject, so recalls are allowed. The governed
// multi-scope policy engine plugs in via SetPolicyProbe without touching
// the service; the probe error path keeps M8-027 fail-closed.
func DefaultScopePolicy(ctx context.Context, subjectID, scopeID string) (bool, error) {
	return true, nil
}

// EvidenceVerifier re-checks one leaf evidence binding at confirm time.
// The default accepts well-formed refs; the provenance adapter (later
// slice) plugs full artifact digest verification here.
type EvidenceVerifier func(ctx context.Context, evidenceRef, wantDigest string) error

// DefaultEvidenceVerifier enforces ref shape only.
func DefaultEvidenceVerifier(ctx context.Context, evidenceRef, wantDigest string) error {
	if len(evidenceRef) < 1 || len(evidenceRef) > m8core.MaxEvidenceRef || !m8core.ValidHexDigest(wantDigest) {
		return fmt.Errorf("evidence ref/digest malformed")
	}
	return nil
}

// Clock mirrors m7app.Clock for deterministic tests.
type Clock interface{ Now() time.Time }

type systemClock struct{}

func (systemClock) Now() time.Time { return time.Now().UTC() }

// MemoryService implements the slice-1 use cases.
type MemoryService struct {
	uow      MemoryUnitOfWork
	clock    Clock
	subject  string // local requesting subject (single-user engine)
	policy   PolicyProbe
	verifyEv EvidenceVerifier
}

// NewMemoryService wires the slice-1 service with fail-closed defaults.
func NewMemoryService(uow MemoryUnitOfWork, localSubject string) *MemoryService {
	return &MemoryService{
		uow:      uow,
		clock:    systemClock{},
		subject:  localSubject,
		policy:   DefaultScopePolicy,
		verifyEv: DefaultEvidenceVerifier,
	}
}

// SetClock substitutes the clock (tests).
func (s *MemoryService) SetClock(c Clock) { s.clock = c }

// SetPolicyProbe substitutes the scope policy probe (fail-closed tests).
func (s *MemoryService) SetPolicyProbe(p PolicyProbe) { s.policy = p }

// SetEvidenceVerifier substitutes the leaf evidence verifier.
func (s *MemoryService) SetEvidenceVerifier(v EvidenceVerifier) { s.verifyEv = v }

// ProposeInput is the internal candidate-proposal command used by the
// provenance adapter path (chat bodies and ungated drafts may only ever
// produce quarantined pending candidates, never facts).
type ProposeInput struct {
	SubjectID string
	Doc       m8core.PayloadDoc
	Inferred  bool
	Trust     string
	TTL       time.Duration // zero -> DefaultTokenTTL
	Actor     string
}

// ProposeResult answers the created pending candidate and its one-time
// confirmation token for the UI confirmation journey.
type ProposeResult struct {
	Candidate    m8core.MemoryCandidate
	ConfirmToken string
}

// ProposeCandidate stores one pending candidate with a derived token.
func (s *MemoryService) ProposeCandidate(ctx context.Context, in ProposeInput) (ProposeResult, error) {
	if s == nil || s.uow == nil {
		return ProposeResult{}, ErrServiceUnavailable
	}
	if len(in.SubjectID) < 1 || len(in.SubjectID) > m8core.MaxSubjectID {
		return ProposeResult{}, fmt.Errorf("%w: subject invalid", ErrPayloadInvalid)
	}
	if !m8core.TrustAllowed(in.Trust) {
		return ProposeResult{}, fmt.Errorf("%w: trust %q", ErrPayloadInvalid, in.Trust)
	}
	if in.Doc.Sensitivity == "" {
		in.Doc.Sensitivity = m8core.SensPrivate
	}
	if err := in.Doc.Validate(); err != nil {
		if strings.Contains(err.Error(), "source leaf") {
			return ProposeResult{}, fmt.Errorf("%w: %v", ErrSourceLeafRequired, err)
		}
		return ProposeResult{}, fmt.Errorf("%w: %v", ErrPayloadInvalid, err)
	}
	payload, err := in.Doc.CanonicalPayload()
	if err != nil {
		return ProposeResult{}, err
	}
	digest, err := in.Doc.PayloadDigest()
	if err != nil {
		return ProposeResult{}, err
	}
	ttl := in.TTL
	if ttl <= 0 {
		ttl = m8core.DefaultTokenTTL
	}
	now := s.clock.Now().UTC()
	expires := now.Add(ttl).Format(time.RFC3339)
	cand := m8core.MemoryCandidate{
		CandidateID:   ulid.Make().String(),
		SubjectID:     in.SubjectID,
		Payload:       string(payload),
		PayloadDigest: digest,
		Inferred:      in.Inferred,
		Trust:         in.Trust,
		State:         m8core.CandPending,
		ExpiresAt:     expires,
		CreatedAt:     now.Format(time.RFC3339),
	}
	cand.ConfirmToken = m8core.DeriveConfirmToken(cand.CandidateID, cand.PayloadDigest, cand.SubjectID, cand.ExpiresAt)
	err = s.uow.TransactMemory(ctx, func(tx MemoryTx) error {
		if err := tx.PutCandidate(cand); err != nil {
			return err
		}
		_, err := tx.AppendAuditEvent(audit.Event{
			ID: ulid.Make().String(), Action: "memory.candidate.propose",
			ResourceType: "memory_candidate", ResourceID: cand.CandidateID,
			Actor: actorOr(in.Actor), AfterDigest: digest,
			CreatedAt: now.Format(time.RFC3339),
		})
		return err
	})
	if err != nil {
		return ProposeResult{}, err
	}
	return ProposeResult{Candidate: cand, ConfirmToken: cand.ConfirmToken}, nil
}

// ConfirmInput is the memory.confirmCandidate command.
type ConfirmInput struct {
	CandidateID string
	Token       string
	Action      string // confirm | reject
	EditedDoc   *m8core.PayloadDoc
	RequestID   string
	Actor       string
}

// FactRef identifies the created fact version.
type FactRef struct {
	FactID  string
	Version int64
}

// ConfirmResult is the confirm/reject outcome.
type ConfirmResult struct {
	CandidateID string
	State       string // confirmed | rejected
	Fact        *FactRef
}

// ConfirmCandidate enacts the explicit-only promotion (FR-02). Confirm
// creates the fact, every source leaf and the audit event inside one
// transaction; reject only writes state and audit. The token is one-time:
// any re-presentation after a terminal state answers ErrConfirmTokenInvalid
// (replay) or ErrCandidateExpired.
func (s *MemoryService) ConfirmCandidate(ctx context.Context, in ConfirmInput) (ConfirmResult, error) {
	if s == nil || s.uow == nil {
		return ConfirmResult{}, ErrServiceUnavailable
	}
	if len(in.CandidateID) != 26 || len(in.Token) != m8core.DigestHexLen || m8core.ValidHexDigest(in.Token) == false {
		return ConfirmResult{}, fmt.Errorf("%w: candidateId/token malformed", ErrConfirmTokenInvalid)
	}
	if in.Action != "confirm" && in.Action != "reject" {
		return ConfirmResult{}, fmt.Errorf("%w: action %q", ErrPayloadInvalid, in.Action)
	}
	var out ConfirmResult
	err := s.uow.TransactMemory(ctx, func(tx MemoryTx) error {
		cand, err := tx.GetCandidate(in.CandidateID)
		if errors.Is(err, sql.ErrNoRows) || errors.Is(err, m8core.ErrNotFound) {
			return fmt.Errorf("%w: %s", ErrCandidateNotFound, in.CandidateID)
		}
		if err != nil {
			return err
		}
		// M8-005: the token binds the payload digest as issued; a drifted
		// row means the candidate was mutated after issuance.
		want := m8core.DeriveConfirmToken(cand.CandidateID, cand.PayloadDigest, cand.SubjectID, cand.ExpiresAt)
		if want != in.Token || cand.ConfirmToken != in.Token {
			return fmt.Errorf("%w: binding mismatch", ErrConfirmTokenInvalid)
		}
		now := s.clock.Now().UTC()
		if m8core.CandTerminal(cand.State) {
			if cand.State == m8core.CandExpired {
				return fmt.Errorf("%w: %s", ErrCandidateExpired, cand.CandidateID)
			}
			// Single-use token: replay after confirmed/rejected is refused.
			return fmt.Errorf("%w: replay on %s", ErrConfirmTokenInvalid, cand.State)
		}
		expiresAt, perr := time.Parse(time.RFC3339, cand.ExpiresAt)
		if perr != nil {
			return fmt.Errorf("%w: expiry %q", ErrConfirmTokenInvalid, cand.ExpiresAt)
		}
		if now.After(expiresAt) {
			if err := tx.TransitionCandidate(cand.CandidateID, m8core.CandPending, m8core.CandExpired, ""); err != nil {
				return err
			}
			_, err := tx.AppendAuditEvent(audit.Event{
				ID: ulid.Make().String(), Action: "memory.candidate.expire",
				ResourceType: "memory_candidate", ResourceID: cand.CandidateID,
				Actor: actorOr(in.Actor), BeforeDigest: cand.PayloadDigest,
				CreatedAt: now.Format(time.RFC3339),
			})
			if err != nil {
				return err
			}
			return fmt.Errorf("%w: %s", ErrCandidateExpired, cand.CandidateID)
		}
		var doc m8core.PayloadDoc
		if err := json.Unmarshal([]byte(cand.Payload), &doc); err != nil {
			return fmt.Errorf("%w: %v", ErrPayloadInvalid, err)
		}
		if err := doc.Validate(); err != nil {
			if strings.Contains(err.Error(), "source leaf") {
				return fmt.Errorf("%w: %v", ErrSourceLeafRequired, err)
			}
			return fmt.Errorf("%w: %v", ErrPayloadInvalid, err)
		}
		if in.Action == "reject" {
			if err := tx.TransitionCandidate(cand.CandidateID, m8core.CandPending, m8core.CandRejected, now.Format(time.RFC3339)); err != nil {
				return err
			}
			_, err := tx.AppendAuditEvent(audit.Event{
				ID: ulid.Make().String(), Action: "memory.candidate.reject",
				ResourceType: "memory_candidate", ResourceID: cand.CandidateID,
				Actor: actorOr(in.Actor), BeforeDigest: cand.PayloadDigest,
				CorrelationID: in.RequestID, CreatedAt: now.Format(time.RFC3339),
			})
			if err != nil {
				return err
			}
			out = ConfirmResult{CandidateID: cand.CandidateID, State: m8core.CandRejected}
			return nil
		}
		// FR-11 hard guard: inferred + untrusted candidates may still be
		// promoted by THIS explicit token path only - never automatically.
		finalDoc := doc
		if in.EditedDoc != nil {
			edited := *in.EditedDoc
			if edited.Sensitivity == "" {
				edited.Sensitivity = doc.Sensitivity
			}
			if edited.ScopeID == "" {
				edited.ScopeID = doc.ScopeID
			}
			if edited.Leaves == nil {
				edited.Leaves = doc.Leaves
			}
			if err := edited.Validate(); err != nil {
				if strings.Contains(err.Error(), "source leaf") {
					return fmt.Errorf("%w: %v", ErrSourceLeafRequired, err)
				}
				return fmt.Errorf("%w: %v", ErrPayloadInvalid, err)
			}
			finalDoc = edited
		}
		finalDigest, err := finalDoc.PayloadDigest()
		if err != nil {
			return err
		}
		// M8-008: every leaf evidence ref is re-verified at confirm time.
		for i, l := range finalDoc.Leaves {
			if err := s.verifyEv(ctx, l.EvidenceRef, l.Digest); err != nil {
				return fmt.Errorf("%w: leaf %d: %v", ErrSourceEvidenceUnavailable, i, err)
			}
		}
		factID := ulid.Make().String()
		fact := m8core.MemoryFact{
			FactID:      factID,
			ScopeID:     finalDoc.ScopeID,
			Version:     1,
			Sensitivity: finalDoc.Sensitivity,
			State:       m8core.FactActive,
			CreatedAt:   now.Format(time.RFC3339),
		}
		if err := tx.PutFact(fact); err != nil {
			return err
		}
		leaves := make([]m8core.SourceLeaf, len(finalDoc.Leaves))
		for i, l := range finalDoc.Leaves {
			leaves[i] = m8core.SourceLeaf{
				ID:          ulid.Make().String(),
				FactID:      factID,
				FactVersion: 1,
				JSONPointer: l.JSONPointer,
				EvidenceRef: l.EvidenceRef,
				Digest:      l.Digest,
				CreatedAt:   now.Format(time.RFC3339),
			}
		}
		if err := tx.PutSourceLeaves(leaves); err != nil {
			return err
		}
		if err := tx.TransitionCandidate(cand.CandidateID, m8core.CandPending, m8core.CandConfirmed, now.Format(time.RFC3339)); err != nil {
			return err
		}
		_, err = tx.AppendAuditEvent(audit.Event{
			ID: ulid.Make().String(), Action: "memory.candidate.confirm",
			ResourceType: "memory_candidate", ResourceID: cand.CandidateID,
			Actor: actorOr(in.Actor), BeforeDigest: cand.PayloadDigest,
			AfterDigest: finalDigest, CorrelationID: in.RequestID,
			CreatedAt: now.Format(time.RFC3339),
		})
		if err != nil {
			return err
		}
		out = ConfirmResult{
			CandidateID: cand.CandidateID,
			State:       m8core.CandConfirmed,
			Fact:        &FactRef{FactID: factID, Version: 1},
		}
		return nil
	})
	if err != nil {
		return ConfirmResult{}, err
	}
	return out, nil
}

// AutoPromote is the explicit refusal path required by FR-11: frequency,
// confidence and compaction pipelines call it and always fail closed -
// with an audit trail proving the refusal.
func (s *MemoryService) AutoPromote(ctx context.Context, candidateID, source string) error {
	if s == nil || s.uow == nil {
		return ErrServiceUnavailable
	}
	_ = s.uow.TransactMemory(ctx, func(tx MemoryTx) error {
		_, err := tx.AppendAuditEvent(audit.Event{
			ID: ulid.Make().String(), Action: "memory.candidate.autopromote_refused",
			ResourceType: "memory_candidate", ResourceID: candidateID,
			Actor: actorOr(source), CreatedAt: s.clock.Now().UTC().Format(time.RFC3339),
		})
		return err
	})
	return ErrInferencePromotionDenied
}

// RecallInput is the recall.query command.
type RecallInput struct {
	ScopeID      string
	Query        string
	TopK         int
	IndexVersion string
}

// RecallHit is one explained hit.
type RecallHit struct {
	Source          string             `json:"source"`
	Version         int64              `json:"version"`
	Score           float64            `json:"score"`
	ScoreComponents map[string]float64 `json:"scoreComponents,omitempty"`
	Freshness       string             `json:"freshness"`
	// Content is the confirmed payload text for chat injection. It is omitted
	// from recall.query traces that still search fact identifiers only.
	Content string `json:"content,omitempty"`
}

// RecallExplanation is the mandatory FR-04 explanation block.
type RecallExplanation struct {
	Reasons    []string `json:"reasons"`
	Redactions []string `json:"redactions"`
	NotAdopted []string `json:"notAdopted"`
	Missing    bool     `json:"missing"`
}

// RecallResult is the recall.query response.
type RecallResult struct {
	TraceID      string
	Hits         []RecallHit
	Explanation  RecallExplanation
	IndexVersion string
}

// Recall answers an explained, policy-filtered read-only snapshot. Results
// are never returned without their persisted trace (M8-010).
func (s *MemoryService) Recall(ctx context.Context, in RecallInput) (RecallResult, error) {
	if s == nil || s.uow == nil {
		return RecallResult{}, ErrServiceUnavailable
	}
	if len(in.ScopeID) < 1 || len(in.ScopeID) > m8core.MaxScopeID || len(in.Query) < 1 || len(in.Query) > 2048 {
		return RecallResult{}, fmt.Errorf("%w: scope/query invalid", ErrPayloadInvalid)
	}
	if s.policy == nil {
		return RecallResult{}, ErrPolicyUnavailable
	}
	allowed, perr := s.policy(ctx, s.subject, in.ScopeID)
	if perr != nil {
		return RecallResult{}, fmt.Errorf("%w: %v", ErrPolicyUnavailable, perr)
	}
	if !allowed {
		_ = s.uow.TransactMemory(ctx, func(tx MemoryTx) error {
			_, err := tx.AppendAuditEvent(audit.Event{
				ID: ulid.Make().String(), Action: "memory.recall.refused",
				ResourceType: "memory_scope", ResourceID: in.ScopeID,
				Actor: actorOr(s.subject), CreatedAt: s.clock.Now().UTC().Format(time.RFC3339),
			})
			return err
		})
		return RecallResult{}, fmt.Errorf("%w: scope %q subject %q", ErrRecallScopeDenied, in.ScopeID, s.subject)
	}
	if in.TopK < 1 {
		in.TopK = m8core.RecallDefaultTopK
	}
	if in.TopK > m8core.RecallMaxTopK {
		in.TopK = m8core.RecallMaxTopK
	}
	if in.IndexVersion == "" {
		in.IndexVersion = "facts-v1"
	}
	now := s.clock.Now().UTC()
	terms := splitTerms(in.Query)
	var res RecallResult
	err := s.uow.TransactMemory(ctx, func(tx MemoryTx) error {
		facts, leaves, err := tx.ListActiveFactsWithLeaves(in.ScopeID)
		if err != nil {
			return err
		}
		type scored struct {
			fact  m8core.MemoryFact
			cov   float64
			fresh float64
			score float64
		}
		var candidates []scored
		redactions := map[string]bool{}
		var notAdopted []string
		for _, f := range facts {
			content, lerr := factContent(f, leaves)
			if lerr != nil {
				return lerr
			}
			matched := 0
			lc := strings.ToLower(content)
			for _, t := range terms {
				if strings.Contains(lc, t) {
					matched++
				}
			}
			if matched == 0 {
				continue
			}
			if f.Sensitivity == m8core.SensSensitive {
				// Restricted hits never enter hits and never leak names,
				// counts or summaries - only the rule id.
				redactions["policy:sensitivity=sensitive"] = true
				continue
			}
			cov := float64(matched) / float64(len(terms))
			age := now.Sub(parseTime(f.CreatedAt))
			fresh := 1.0 - age.Hours()/(365*24)
			if fresh < 0 {
				fresh = 0
			}
			sc := 0.8*cov + 0.2*fresh
			if sc < m8core.RecallScoreFloor {
				notAdopted = append(notAdopted, fmt.Sprintf("fact %s v%d: score %.3f below floor", f.FactID, f.Version, sc))
				continue
			}
			candidates = append(candidates, scored{fact: f, cov: cov, fresh: fresh, score: sc})
		}
		sort.SliceStable(candidates, func(i, j int) bool {
			if candidates[i].score != candidates[j].score {
				return candidates[i].score > candidates[j].score
			}
			return candidates[i].fact.FactID < candidates[j].fact.FactID
		})
		if len(candidates) > in.TopK {
			for _, c := range candidates[in.TopK:] {
				notAdopted = append(notAdopted, fmt.Sprintf("fact %s v%d: beyond topK", c.fact.FactID, c.fact.Version))
			}
			candidates = candidates[:in.TopK]
		}
		hits := make([]RecallHit, 0, len(candidates))
		reasons := make([]string, 0, len(candidates))
		for _, c := range candidates {
			source := "fact:" + c.fact.FactID
			if ls := leaves[c.fact.FactID]; len(ls) > 0 {
				source = ls[0].EvidenceRef
			}
			hits = append(hits, RecallHit{
				Source:          source,
				Version:         c.fact.Version,
				Score:           round3(c.score),
				ScoreComponents: map[string]float64{"keyword": round3(c.cov), "freshness": round3(c.fresh)},
				Freshness:       c.fact.CreatedAt,
			})
			reasons = append(reasons, fmt.Sprintf("fact %s v%d: keyword %.3f, freshness %.3f", c.fact.FactID, c.fact.Version, c.cov, c.fresh))
		}
		red := make([]string, 0, len(redactions))
		for r := range redactions {
			red = append(red, r)
		}
		sort.Strings(red)
		if notAdopted == nil {
			notAdopted = []string{}
		}
		traceID := ulid.Make().String()
		minHits, err := json.Marshal(hits)
		if err != nil {
			return err
		}
		if err := tx.PutRecallTrace(m8core.RecallTrace{
			ID:             traceID,
			QueryDigest:    m8core.DigestOf(in.Query),
			HitsJSON:       string(minHits),
			ReasonsJSON:    mustJSON(reasons),
			RedactionsJSON: mustJSON(red),
			CreatedAt:      now.Format(time.RFC3339),
		}); err != nil {
			// M8-010: never answer results whose explanation could not be
			// persisted.
			return fmt.Errorf("%w: trace persist failed: %v", ErrExplanationUnavailable, err)
		}
		if _, err := tx.AppendAuditEvent(audit.Event{
			ID: ulid.Make().String(), Action: "memory.recall",
			ResourceType: "memory_scope", ResourceID: in.ScopeID,
			Actor: actorOr(s.subject), AfterDigest: m8core.DigestOf(in.Query),
			CreatedAt: now.Format(time.RFC3339),
		}); err != nil {
			return err
		}
		res = RecallResult{
			TraceID:      traceID,
			Hits:         hits,
			Explanation:  RecallExplanation{Reasons: reasons, Redactions: red, NotAdopted: notAdopted, Missing: false},
			IndexVersion: in.IndexVersion,
		}
		return nil
	})
	if err != nil {
		return RecallResult{}, err
	}
	return res, nil
}

// factContent resolves the searchable text of one fact from its source
// leaves. Slice 1 keeps the fact payload inside the leaf digest chain;
// full content search lands with the KB index projection (Batch D).
func factContent(f m8core.MemoryFact, leaves map[string][]m8core.SourceLeaf) (string, error) {
	ls := leaves[f.FactID]
	parts := make([]string, 0, len(ls))
	for _, l := range ls {
		parts = append(parts, l.EvidenceRef+" "+l.JSONPointer)
	}
	// Evidence refs are identifiers, not content: the recall corpus for
	// slice 1 is the fact id + evidence refs; content search lands with
	// the KB index projection (Batch D).
	return f.FactID + " " + strings.Join(parts, " "), nil
}

func splitTerms(q string) []string {
	fields := strings.Fields(strings.ToLower(q))
	out := fields[:0]
	seen := map[string]bool{}
	for _, f := range fields {
		if len(f) < 2 || seen[f] {
			continue
		}
		seen[f] = true
		out = append(out, f)
	}
	if len(out) == 0 {
		return []string{strings.ToLower(strings.TrimSpace(q))}
	}
	return out
}

func parseTime(s string) time.Time {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return time.Time{}
	}
	return t
}

func round3(v float64) float64 {
	return float64(int(v*1000+0.5)) / 1000
}

func mustJSON(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}

func actorOr(actor string) string {
	if actor == "" {
		return "anonymous"
	}
	return actor
}

// FeedbackRecordInput is the feedback.record command: one chat-side
// accept/reject/correct signal. Only "correct" carries preference text and
// therefore only "correct" proposes a candidate - accept/reject stay pure
// evidence.
type FeedbackRecordInput struct {
	Action     string // accept | reject | correct
	TargetType string // e.g. "message"
	TargetID   string // e.g. message ULID
	Text       string // preference statement, required for correct
	Actor      string
}

// FeedbackRecordResult answers the persisted event and, for correct, the
// proposed pending preference candidate awaiting explicit confirmation.
type FeedbackRecordResult struct {
	EventID           string
	CandidateID       string
	ConfirmationToken string
}

// RecordFeedback appends one feedback event and, for corrections with
// text, proposes a governed preference candidate (inferred, untrusted):
// the FR-11 invariant is untouched - only the explicit token path ever
// promotes it into memory_facts and the chat-side snapshot.
func (s *MemoryService) RecordFeedback(ctx context.Context, in FeedbackRecordInput) (FeedbackRecordResult, error) {
	if s == nil || s.uow == nil {
		return FeedbackRecordResult{}, ErrServiceUnavailable
	}
	switch in.Action {
	case FeedbackAccept, FeedbackReject, FeedbackCorrect:
	default:
		return FeedbackRecordResult{}, fmt.Errorf("%w: action %q", ErrPayloadInvalid, in.Action)
	}
	if len(in.TargetType) < 1 || len(in.TargetType) > 64 || len(in.TargetID) < 1 || len(in.TargetID) > 128 {
		return FeedbackRecordResult{}, fmt.Errorf("%w: target invalid", ErrPayloadInvalid)
	}
	if len(in.Text) > 2048 {
		return FeedbackRecordResult{}, fmt.Errorf("%w: text too long", ErrPayloadInvalid)
	}
	if in.Action == FeedbackCorrect && strings.TrimSpace(in.Text) == "" {
		return FeedbackRecordResult{}, fmt.Errorf("%w: correct requires text", ErrPayloadInvalid)
	}
	now := s.clock.Now().UTC()
	eventID := ulid.Make().String()
	evidence, err := json.Marshal(map[string]string{"text": in.Text})
	if err != nil {
		return FeedbackRecordResult{}, err
	}
	event := FeedbackEvent{
		EventID:    eventID,
		SubjectID:  s.subject,
		Action:     in.Action,
		TargetType: in.TargetType,
		TargetID:   in.TargetID,
		Evidence:   string(evidence),
		CreatedAt:  now.Format(time.RFC3339),
	}
	if err := s.uow.TransactMemory(ctx, func(tx MemoryTx) error {
		return tx.AppendFeedbackEvent(event)
	}); err != nil {
		return FeedbackRecordResult{}, err
	}
	out := FeedbackRecordResult{EventID: eventID}
	if in.Action == FeedbackCorrect {
		doc := m8core.PayloadDoc{
			Content:     strings.TrimSpace(in.Text),
			ScopeID:     LearningScope,
			Sensitivity: m8core.SensPrivate,
			Leaves: []m8core.SourceLeafClaim{{
				JSONPointer: "/content",
				EvidenceRef: "feedback://" + eventID,
				Digest:      m8core.DigestOf(strings.TrimSpace(in.Text)),
			}},
		}
		prop, err := s.ProposeCandidate(ctx, ProposeInput{
			SubjectID: s.subject,
			Doc:       doc,
			Inferred:  true,
			Trust:     m8core.TrustUntrusted,
			Actor:     actorOr(in.Actor),
		})
		if err != nil {
			return FeedbackRecordResult{}, err
		}
		out.CandidateID = prop.Candidate.CandidateID
		out.ConfirmationToken = prop.ConfirmToken
	}
	return out, nil
}

// PendingCandidateView is one pending candidate projection for the UI.
type PendingCandidateView struct {
	CandidateID       string `json:"candidateId"`
	Content           string `json:"content"`
	ScopeID           string `json:"scopeId"`
	ConfirmationToken string `json:"confirmationToken"`
	CreatedAt         string `json:"createdAt"`
	ExpiresAt         string `json:"expiresAt"`
}

// ListPendingCandidates answers pending candidates (newest first) for the
// memory-center confirmation journey.
func (s *MemoryService) ListPendingCandidates(ctx context.Context, limit int) ([]PendingCandidateView, error) {
	if s == nil || s.uow == nil {
		return nil, ErrServiceUnavailable
	}
	if limit < 1 || limit > 100 {
		limit = 50
	}
	rows, err := s.listCandidates(ctx, m8core.CandPending, limit)
	if err != nil {
		return nil, err
	}
	out := make([]PendingCandidateView, 0, len(rows))
	for _, c := range rows {
		var doc m8core.PayloadDoc
		if err := json.Unmarshal([]byte(c.Payload), &doc); err != nil {
			continue
		}
		out = append(out, PendingCandidateView{
			CandidateID:       c.CandidateID,
			Content:           doc.Content,
			ScopeID:           doc.ScopeID,
			ConfirmationToken: c.ConfirmToken,
			CreatedAt:         c.CreatedAt,
			ExpiresAt:         c.ExpiresAt,
		})
	}
	return out, nil
}

func (s *MemoryService) listCandidates(ctx context.Context, state string, limit int) ([]m8core.MemoryCandidate, error) {
	var rows []m8core.MemoryCandidate
	err := s.uow.TransactMemory(ctx, func(tx MemoryTx) error {
		var err error
		rows, err = tx.ListCandidatesByState(state, limit)
		return err
	})
	return rows, err
}

// ConfirmedSnapshot answers the confirmed (explicitly promoted) candidate
// contents of one scope for chat-side preference injection, bounded by
// maxItems and maxBytes (injected-tokens budget: preferences must never
// crowd out the conversation context).
func (s *MemoryService) ConfirmedSnapshot(ctx context.Context, scopeID string, maxItems, maxBytes int) ([]string, error) {
	if s == nil || s.uow == nil {
		return nil, ErrServiceUnavailable
	}
	if len(scopeID) < 1 || len(scopeID) > m8core.MaxScopeID {
		return nil, fmt.Errorf("%w: scope invalid", ErrPayloadInvalid)
	}
	if maxItems < 1 {
		maxItems = 8
	}
	if maxBytes < 64 {
		maxBytes = 2048
	}
	rows, err := s.listCandidates(ctx, m8core.CandConfirmed, 200)
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, maxItems)
	used := 0
	for _, c := range rows {
		if len(out) >= maxItems || used >= maxBytes {
			break
		}
		var doc m8core.PayloadDoc
		if err := json.Unmarshal([]byte(c.Payload), &doc); err != nil || doc.ScopeID != scopeID {
			continue
		}
		content := strings.TrimSpace(doc.Content)
		if content == "" {
			continue
		}
		// Never cut a UTF-8 rune in half: a preference that no longer
		// fits the budget wholesale is skipped, not truncated.
		if remaining := maxBytes - used; len(content) > remaining {
			continue
		}
		out = append(out, content)
		used += len(content)
	}
	return out, nil
}
