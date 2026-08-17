// M10 memory nomination service: nominate wraps candidate proposal with a
// reason and source session; withdraw only touches the nominated state;
// MarkDecided is the handler-side hook called after the 0061 confirm/reject
// path settles the underlying candidate. The two-transaction Nominate
// trade-off is deliberate: a failed nomination insert leaves a plain
// pending candidate that still flows through the untouched explicit
// confirmation journey — never a nomination without a candidate.
package m8app

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/oklog/ulid/v2"

	"github.com/lunitide/lunitide/internal/audit"
	"github.com/lunitide/lunitide/internal/domain/m8core"
)

// M10 nomination error family (M10-ME-001~004).
var (
	// ErrNominationNotFound: nomination missing (M10-ME-001, 404).
	ErrNominationNotFound = errors.New("m8app: nomination not found")
	// ErrNominationTerminal: nomination already decided/withdrawn (M10-ME-002, 409).
	ErrNominationTerminal = errors.New("m8app: nomination already settled")
	// ErrNominationReasonInvalid: reason/nominator bounds (M10-ME-003, 422).
	ErrNominationReasonInvalid = errors.New("m8app: nomination reason invalid")
	// ErrNomineeStateConflict: candidate not pending (M10-ME-004, 409).
	ErrNomineeStateConflict = errors.New("m8app: nominee state conflict")
)

// NominationTx extends the slice-1 memory transaction with the migration
// 0071 table. The 0061 MemoryTx surface is untouched.
type NominationTx interface {
	MemoryTx
	PutNomination(m8core.Nomination) error
	GetNomination(id string) (m8core.Nomination, error)
	GetNominationByCandidate(candidateID string) (m8core.Nomination, error)
	ListNominationsByState(state string, limit int) ([]m8core.Nomination, error)
	ListNominationsWithCandidates(state string, limit int) ([]m8core.Nomination, []m8core.MemoryCandidate, error)
	TransitionNomination(id, from, to, decidedAt string) error
}

// NominationUnitOfWork is the single-writer boundary for nominations.
type NominationUnitOfWork interface {
	TransactNomination(ctx context.Context, fn func(NominationTx) error) error
}

// NominationService implements the M10 nomination use cases.
type NominationService struct {
	uow     NominationUnitOfWork
	memory  *MemoryService
	clock   Clock
}

// NewNominationService wires the nomination service over the slice-1
// memory service (candidate proposal) and the nomination unit of work.
func NewNominationService(uow NominationUnitOfWork, memory *MemoryService) *NominationService {
	return &NominationService{uow: uow, memory: memory, clock: systemClock{}}
}

// SetClock substitutes the clock (tests).
func (s *NominationService) SetClock(c Clock) { s.clock = c }

// NominateInput is the memory.nominate command.
type NominateInput struct {
	SubjectID       string
	Doc             m8core.PayloadDoc
	Reason          string
	Nominator       string
	SourceSessionID string
	Actor           string
}

// NominateResult answers the created nomination and its candidate token.
type NominateResult struct {
	Nomination       m8core.Nomination
	CandidateID      string
	ConfirmToken     string
}

// Nominate proposes the candidate (0061 path) then wraps it with the
// nomination row. See the package comment for the transaction trade-off.
func (s *NominationService) Nominate(ctx context.Context, in NominateInput) (NominateResult, error) {
	if s == nil || s.uow == nil || s.memory == nil {
		return NominateResult{}, ErrServiceUnavailable
	}
	if len(in.Reason) < 1 || len(in.Reason) > m8core.MaxReason {
		return NominateResult{}, ErrNominationReasonInvalid
	}
	nominator := in.Nominator
	if nominator == "" {
		nominator = "local-user"
	}
	if !m8core.NomValidate(m8core.Nomination{Nominator: nominator, Reason: in.Reason, State: m8core.NomNominated}) {
		return NominateResult{}, ErrNominationReasonInvalid
	}
	prop, err := s.memory.ProposeCandidate(ctx, ProposeInput{
		SubjectID: in.SubjectID,
		Doc:       in.Doc,
		Inferred:  true,
		Trust:     m8core.TrustUntrusted,
		Actor:     actorOr(in.Actor),
	})
	if err != nil {
		return NominateResult{}, err
	}
	now := s.clock.Now().UTC().Format(time.RFC3339)
	nom := m8core.Nomination{
		NominationID:    ulid.Make().String(),
		CandidateID:     prop.Candidate.CandidateID,
		Nominator:       nominator,
		Reason:          in.Reason,
		SourceSessionID: in.SourceSessionID,
		State:           m8core.NomNominated,
		CreatedAt:       now,
	}
	err = s.uow.TransactNomination(ctx, func(tx NominationTx) error {
		if err := tx.PutNomination(nom); err != nil {
			return err
		}
		_, err := tx.AppendAuditEvent(audit.Event{
			ID: ulid.Make().String(), Action: "memory.nomination.create",
			ResourceType: "memory_nomination", ResourceID: nom.NominationID,
			Actor: actorOr(in.Actor), AfterDigest: prop.Candidate.PayloadDigest,
			CreatedAt: now,
		})
		return err
	})
	if err != nil {
		return NominateResult{}, err
	}
	return NominateResult{Nomination: nom, CandidateID: prop.Candidate.CandidateID, ConfirmToken: prop.ConfirmToken}, nil
}

// NominationView is one nomination projection joined with its candidate.
type NominationView struct {
	NominationID      string `json:"nominationId"`
	CandidateID       string `json:"candidateId"`
	Nominator         string `json:"nominator"`
	Reason            string `json:"reason"`
	SourceSessionID   string `json:"sourceSessionId,omitempty"`
	State             string `json:"state"`
	Content           string `json:"content"`
	ScopeID           string `json:"scopeId"`
	ConfirmationToken string `json:"confirmationToken"`
	CreatedAt         string `json:"createdAt"`
	DecidedAt         string `json:"decidedAt,omitempty"`
}

// ListNominations answers nominations of one state (newest first) joined
// with the candidate content for the memory-hub confirmation journey.
func (s *NominationService) ListNominations(ctx context.Context, state string, limit int) ([]NominationView, error) {
	if s == nil || s.uow == nil {
		return nil, ErrServiceUnavailable
	}
	if state == "" {
		state = m8core.NomNominated
	}
	if limit < 1 || limit > 100 {
		limit = 50
	}
	var views []NominationView
	err := s.uow.TransactNomination(ctx, func(tx NominationTx) error {
		noms, cands, err := tx.ListNominationsWithCandidates(state, limit)
		if err != nil {
			return err
		}
		views = make([]NominationView, 0, len(noms))
		for i := range noms {
			v := NominationView{
				NominationID:      noms[i].NominationID,
				CandidateID:       noms[i].CandidateID,
				Nominator:         noms[i].Nominator,
				Reason:            noms[i].Reason,
				SourceSessionID:   noms[i].SourceSessionID,
				State:             noms[i].State,
				CreatedAt:         noms[i].CreatedAt,
				DecidedAt:         noms[i].DecidedAt,
			}
			if i < len(cands) {
				var doc m8core.PayloadDoc
				if jsonUnmarshal(cands[i].Payload, &doc) {
					v.Content = doc.Content
					v.ScopeID = doc.ScopeID
				}
				v.ConfirmationToken = cands[i].ConfirmToken
			}
			views = append(views, v)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return views, nil
}

// Withdraw marks a nominated nomination withdrawn (M10-ME-002 on terminal).
func (s *NominationService) Withdraw(ctx context.Context, nominationID, actor string) error {
	if s == nil || s.uow == nil {
		return ErrServiceUnavailable
	}
	now := s.clock.Now().UTC().Format(time.RFC3339)
	return s.uow.TransactNomination(ctx, func(tx NominationTx) error {
		nom, err := tx.GetNomination(nominationID)
		if errors.Is(err, sql.ErrNoRows) || errors.Is(err, m8core.ErrNotFound) {
			return fmt.Errorf("%w: %s", ErrNominationNotFound, nominationID)
		}
		if err != nil {
			return err
		}
		if m8core.NomTerminal(nom.State) {
			return fmt.Errorf("%w: %s", ErrNominationTerminal, nom.State)
		}
		if err := tx.TransitionNomination(nominationID, m8core.NomNominated, m8core.NomWithdrawn, now); err != nil {
			return err
		}
		_, err = tx.AppendAuditEvent(audit.Event{
			ID: ulid.Make().String(), Action: "memory.nomination.withdraw",
			ResourceType: "memory_nomination", ResourceID: nominationID,
			Actor: actorOr(actor), CreatedAt: now,
		})
		return err
	})
}

// MarkDecided settles the nomination after the underlying candidate was
// confirmed or rejected through the 0061 path. A missing nomination row is
// silently ignored: plain candidates (feedback-origin) never had one.
func (s *NominationService) MarkDecided(ctx context.Context, candidateID string) error {
	if s == nil || s.uow == nil {
		return ErrServiceUnavailable
	}
	now := s.clock.Now().UTC().Format(time.RFC3339)
	return s.uow.TransactNomination(ctx, func(tx NominationTx) error {
		nom, err := tx.GetNominationByCandidate(candidateID)
		if errors.Is(err, sql.ErrNoRows) || errors.Is(err, m8core.ErrNotFound) {
			return nil
		}
		if err != nil {
			return err
		}
		if m8core.NomTerminal(nom.State) {
			return nil
		}
		return tx.TransitionNomination(nom.NominationID, m8core.NomNominated, m8core.NomDecided, now)
	})
}

func jsonUnmarshal(payload string, doc *m8core.PayloadDoc) bool {
	return json.Unmarshal([]byte(payload), doc) == nil
}
