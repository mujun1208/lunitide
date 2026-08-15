// Complexity routing and synthesis application service: decide (the
// audited complexity.decided entry), freeze (child manifests, result
// bundles) and synthesize (the root adoption record). Every write shares
// the agent-runtime single-writer transaction with its audit row.
package m6app

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/lunitide/lunitide/internal/complexity"
	"github.com/lunitide/lunitide/internal/domain/m6supply"
	"github.com/lunitide/lunitide/internal/providerapp"
	"github.com/oklog/ulid/v2"
)

var (
	// ErrManifestExists: the delegation already has a frozen manifest.
	ErrManifestExists = errors.New("m6app: child manifest already frozen")
	// ErrBundleExists: the (delegation, attempt) bundle already landed.
	ErrBundleExists = errors.New("m6app: result bundle already recorded")
	// ErrNoBundles: synthesis over an empty bundle set.
	ErrNoBundles = errors.New("m6app: no result bundles to synthesize")
)

// RoutingService implements decide/freeze/synthesize.
type RoutingService struct {
	uow   UnitOfWork
	clock Clock
}

func NewRoutingService(uow UnitOfWork) *RoutingService {
	return &RoutingService{uow: uow, clock: systemClock{}}
}

func (s *RoutingService) SetClock(c Clock) { s.clock = c }

func (s *RoutingService) available() error {
	if s == nil || s.uow == nil {
		return ErrServiceUnavailable
	}
	return nil
}

// Decide routes one conversation. The decision is idempotent on
// (inputDigest, routerVersion): a replay answers the stored row without a
// new audit event — deterministic routing must not double-report.
func (s *RoutingService) Decide(ctx context.Context, sessionID string, signals complexity.ConversationSignals) (m6supply.ComplexityDecision, error) {
	if err := s.available(); err != nil {
		return m6supply.ComplexityDecision{}, err
	}
	if len(sessionID) < 1 || len(sessionID) > 256 {
		return m6supply.ComplexityDecision{}, errors.New("m6app: sessionId length must be 1..256")
	}
	decision := complexity.Route(signals)
	reasonsJSON, err := json.Marshal(decision.ReasonCodes)
	if err != nil {
		return m6supply.ComplexityDecision{}, err
	}
	if err := m6supply.ValidateDecisionInput(decision.Tier, string(reasonsJSON), decision.Confidence); err != nil {
		return m6supply.ComplexityDecision{}, err
	}
	var out m6supply.ComplexityDecision
	err = s.uow.TransactM6(ctx, func(tx Tx) error {
		if existing, err := tx.FindM6ComplexityDecision(decision.InputDigest, decision.RouterVersion); err == nil {
			out = existing
			return nil
		} else if !errors.Is(err, m6supply.ErrNotFound) {
			return err
		}
		now := s.clock.Now().UTC()
		out = m6supply.ComplexityDecision{
			ID: ulid.Make().String(), SessionID: sessionID,
			InputDigest: decision.InputDigest, RouterVersion: decision.RouterVersion,
			Tier: decision.Tier, RoutedPath: decision.RoutedPath,
			ReasonCodes: string(reasonsJSON), Confidence: decision.Confidence,
			CreatedAt: now,
		}
		if err := tx.PutM6ComplexityDecision(out); err != nil {
			return err
		}
		return tx.PutAudit(providerapp.Audit{
			ID: ulid.Make().String(), Action: "complexity.decided",
			AggregateID: out.ID, Actor: delegationActor, CreatedAt: now,
			Metadata: marshalJSON(struct {
				SessionID     string   `json:"sessionId"`
				Tier          string   `json:"tier"`
				RoutedPath    string   `json:"routedPath"`
				ReasonCodes   []string `json:"reasonCodes"`
				Confidence    float64  `json:"confidence"`
				InputDigest   string   `json:"inputDigest"`
				RouterVersion string   `json:"routerVersion"`
			}{
				SessionID: sessionID, Tier: out.Tier, RoutedPath: out.RoutedPath,
				ReasonCodes: decision.ReasonCodes, Confidence: out.Confidence,
				InputDigest: out.InputDigest, RouterVersion: out.RouterVersion,
			}),
		})
	})
	return out, err
}

// FreezeManifestInput carries the frozen delegation context.
type FreezeManifestInput struct {
	DelegationID string
	TaskScope    string // JSON
	LockedInputs string // JSON
	Budget       string // JSON
	Capabilities string // JSON
}

// FreezeManifest pins the child context before the delegation spawns. The
// digest is computed here from the canonical payload — callers cannot
// self-assert a digest.
func (s *RoutingService) FreezeManifest(ctx context.Context, in FreezeManifestInput) (m6supply.ChildContextManifest, error) {
	if err := s.available(); err != nil {
		return m6supply.ChildContextManifest{}, err
	}
	for name, doc := range map[string]string{
		"taskScope": in.TaskScope, "lockedInputs": in.LockedInputs,
		"budget": in.Budget, "capabilities": in.Capabilities,
	} {
		if err := m6supply.ValidateJSONDoc(doc, 65536); err != nil {
			return m6supply.ChildContextManifest{}, errors.New("m6app: " + name + ": " + err.Error())
		}
	}
	digest, err := m6supply.DigestJSON(m6supply.ManifestPayload{
		TaskScope:    json.RawMessage(in.TaskScope),
		LockedInputs: json.RawMessage(in.LockedInputs),
		Budget:       json.RawMessage(in.Budget),
		Capabilities: json.RawMessage(in.Capabilities),
	})
	if err != nil {
		return m6supply.ChildContextManifest{}, err
	}
	var out m6supply.ChildContextManifest
	err = s.uow.TransactM6(ctx, func(tx Tx) error {
		if _, err := tx.GetM6Delegation(in.DelegationID); err != nil {
			if errors.Is(err, m6supply.ErrNotFound) {
				return ErrDelegationNotFound
			}
			return err
		}
		if _, err := tx.GetM6ChildManifestByDelegation(in.DelegationID); err == nil {
			return ErrManifestExists
		} else if !errors.Is(err, m6supply.ErrNotFound) {
			return err
		}
		now := s.clock.Now().UTC()
		out = m6supply.ChildContextManifest{
			ID: ulid.Make().String(), DelegationID: in.DelegationID,
			ManifestDigest: digest, TaskScope: in.TaskScope,
			LockedInputs: in.LockedInputs, BudgetJSON: in.Budget,
			Capabilities: in.Capabilities, CreatedAt: now,
		}
		return tx.PutM6ChildManifest(out)
	})
	return out, err
}

// BundleInput carries one child result bundle.
type BundleInput struct {
	DelegationID string
	ChildID      string
	Attempt      int64
	BaseHead     string
	Claims       string // JSON
	PatchDigest  string
	TestEvidence string // JSON
	Usage        string // JSON
	RiskNotes    string // JSON
}

// RecordBundle freezes what a child claims back. The result digest is
// computed from the canonical payload; (delegation, attempt) is UNIQUE.
func (s *RoutingService) RecordBundle(ctx context.Context, in BundleInput) (m6supply.ResultBundle, error) {
	if err := s.available(); err != nil {
		return m6supply.ResultBundle{}, err
	}
	if len(in.ChildID) < 1 || len(in.ChildID) > 256 {
		return m6supply.ResultBundle{}, errors.New("m6app: childId length must be 1..256")
	}
	if in.Attempt < 1 {
		return m6supply.ResultBundle{}, errors.New("m6app: attempt must be >= 1")
	}
	if len(in.BaseHead) < 1 || len(in.BaseHead) > 256 {
		return m6supply.ResultBundle{}, errors.New("m6app: baseHead length must be 1..256")
	}
	for name, doc := range map[string]string{
		"claims": in.Claims, "testEvidence": in.TestEvidence, "usage": in.Usage,
	} {
		if err := m6supply.ValidateJSONDoc(doc, 65536); err != nil {
			return m6supply.ResultBundle{}, errors.New("m6app: " + name + ": " + err.Error())
		}
	}
	if in.RiskNotes != "" {
		if err := m6supply.ValidateJSONDoc(in.RiskNotes, 16384); err != nil {
			return m6supply.ResultBundle{}, errors.New("m6app: riskNotes: " + err.Error())
		}
	}
	payload := m6supply.BundlePayload{
		ChildID: in.ChildID, Attempt: in.Attempt, BaseHead: in.BaseHead,
		Claims: json.RawMessage(in.Claims), PatchDigest: in.PatchDigest,
		TestEvidence: json.RawMessage(in.TestEvidence),
		Usage:        json.RawMessage(in.Usage),
	}
	if in.RiskNotes != "" {
		payload.RiskNotes = json.RawMessage(in.RiskNotes)
	}
	digest, err := m6supply.DigestJSON(payload)
	if err != nil {
		return m6supply.ResultBundle{}, err
	}
	var out m6supply.ResultBundle
	err = s.uow.TransactM6(ctx, func(tx Tx) error {
		if _, err := tx.GetM6Delegation(in.DelegationID); err != nil {
			if errors.Is(err, m6supply.ErrNotFound) {
				return ErrDelegationNotFound
			}
			return err
		}
		existing, err := tx.ListM6ResultBundles(in.DelegationID)
		if err != nil {
			return err
		}
		for _, b := range existing {
			if b.Attempt == in.Attempt {
				return ErrBundleExists
			}
		}
		now := s.clock.Now().UTC()
		out = m6supply.ResultBundle{
			ID: ulid.Make().String(), DelegationID: in.DelegationID,
			ChildID: in.ChildID, Attempt: in.Attempt, BaseHead: in.BaseHead,
			Claims: in.Claims, PatchDigest: in.PatchDigest,
			TestEvidence: in.TestEvidence, Usage: in.Usage, RiskNotes: in.RiskNotes,
			ResultDigest: digest, CreatedAt: now,
		}
		return tx.PutM6ResultBundle(out)
	})
	return out, err
}

// SynthesizeInput carries the root adoption decision.
type SynthesizeInput struct {
	RootID          string
	BarrierID       string
	Consistent      string // JSON
	Conflicts       string // JSON
	MissingEvidence string // JSON
	AdoptionReasons string // JSON
}

// Synthesize freezes the root's adoption record over the delegation
// bundles. Audited as synthesis.recorded.
func (s *RoutingService) Synthesize(ctx context.Context, in SynthesizeInput) (m6supply.SynthesisRecord, error) {
	if err := s.available(); err != nil {
		return m6supply.SynthesisRecord{}, err
	}
	for name, doc := range map[string]string{
		"consistent": in.Consistent, "conflicts": in.Conflicts,
		"missingEvidence": in.MissingEvidence, "adoptionReasons": in.AdoptionReasons,
	} {
		if err := m6supply.ValidateJSONDoc(doc, 65536); err != nil {
			return m6supply.SynthesisRecord{}, errors.New("m6app: " + name + ": " + err.Error())
		}
	}
	payload := m6supply.SynthesisPayload{
		RootID: in.RootID, Consistent: json.RawMessage(in.Consistent),
		Conflicts: json.RawMessage(in.Conflicts),
		MissingEvidence: json.RawMessage(in.MissingEvidence),
		AdoptionReasons: json.RawMessage(in.AdoptionReasons),
	}
	if in.BarrierID != "" {
		payload.BarrierID = in.BarrierID
	}
	digest, err := m6supply.DigestJSON(payload)
	if err != nil {
		return m6supply.SynthesisRecord{}, err
	}
	var out m6supply.SynthesisRecord
	err = s.uow.TransactM6(ctx, func(tx Tx) error {
		now := s.clock.Now().UTC()
		out = m6supply.SynthesisRecord{
			ID: ulid.Make().String(), RootID: in.RootID, BarrierID: in.BarrierID,
			SynthesisDigest: digest, Consistent: in.Consistent,
			Conflicts: in.Conflicts, MissingEvidence: in.MissingEvidence,
			AdoptionReasons: in.AdoptionReasons, CreatedAt: now,
		}
		if err := tx.PutM6SynthesisRecord(out); err != nil {
			return err
		}
		return tx.PutAudit(providerapp.Audit{
			ID: ulid.Make().String(), Action: "synthesis.recorded",
			AggregateID: out.ID, Actor: delegationActor, CreatedAt: now,
			Metadata: marshalJSON(struct {
				RootID          string `json:"rootId"`
				SynthesisDigest string `json:"synthesisDigest"`
			}{RootID: in.RootID, SynthesisDigest: digest}),
		})
	})
	return out, err
}
