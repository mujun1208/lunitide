// M10 expert scenario-card service: create/list/archive over the FR-19
// expert transaction. Cards never mutate: create stores the canonical
// scenario JSON with its SHA-256 digest; delete is the soft archive only.
package m8app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/oklog/ulid/v2"

	"github.com/lunitide/lunitide/internal/audit"
	"github.com/lunitide/lunitide/internal/domain/m8core"
)

// M10 expert scenario error family (M10-EX-001~003).
var (
	// ErrScenarioNotFound: card missing or already archived (M10-EX-001, 404).
	ErrScenarioNotFound = errors.New("m8app: scenario card not found")
	// ErrScenarioDuplicate: UNIQUE(expert_id,title) conflict (M10-EX-002, 409).
	ErrScenarioDuplicate = errors.New("m8app: scenario card title already exists")
	// ErrScenarioInvalid: title/summary/phase/scenario bounds (M10-EX-003, 422).
	ErrScenarioInvalid = errors.New("m8app: scenario card invalid")
)

// ScenarioTx extends the FR-19 expert transaction with migration 0072.
type ScenarioTx interface {
	ExpertTx
	GetScenarioCard(id string) (m8core.ScenarioCard, error)
	GetScenarioCardByTitle(expertID, title string) (m8core.ScenarioCard, bool, error)
	PutScenarioCard(m8core.ScenarioCard) error
	ListScenarioCards(expertID, state string) ([]m8core.ScenarioCard, error)
	ArchiveScenarioCard(id, updatedAt string) error
}

// ScenarioUnitOfWork is the single-writer boundary for scenario cards.
type ScenarioUnitOfWork interface {
	TransactScenario(ctx context.Context, fn func(ScenarioTx) error) error
}

// ScenarioService implements the M10 scenario-card use cases.
type ScenarioService struct {
	uow   ScenarioUnitOfWork
	clock Clock
}

// NewScenarioService wires the scenario-card service.
func NewScenarioService(uow ScenarioUnitOfWork) *ScenarioService {
	return &ScenarioService{uow: uow, clock: systemClock{}}
}

// SetClock substitutes the clock (tests).
func (s *ScenarioService) SetClock(c Clock) { s.clock = c }

// ScenarioCreateInput is the expert.scenario.create command.
type ScenarioCreateInput struct {
	ExpertID string
	Title    string
	Summary  string
	PhaseKey string
	Scenario json.RawMessage // object; canonicalized server-side
	Actor    string
}

// ScenarioCreateResult is the expert.scenario.create outcome.
type ScenarioCreateResult struct {
	ScenarioCardID string `json:"scenarioCardId"`
	ExpertID       string `json:"expertId"`
	Title          string `json:"title"`
	PhaseKey       string `json:"phaseKey"`
	Digest         string `json:"digest"`
}

// ScenarioView is one card projection for the expert-center drawer.
type ScenarioView struct {
	ScenarioCardID string `json:"scenarioCardId"`
	ExpertID       string `json:"expertId"`
	Title          string `json:"title"`
	Summary        string `json:"summary"`
	PhaseKey       string `json:"phaseKey"`
	State          string `json:"state"`
	CreatedAt      string `json:"createdAt"`
	UpdatedAt      string `json:"updatedAt"`
}

// canonicalScenario marshals the scenario payload via map re-encoding so
// keys are sorted and whitespace-free; non-object payloads are rejected.
func canonicalScenario(raw json.RawMessage) (string, string, error) {
	if len(raw) < 2 || len(raw) > m8core.MaxScenarioJSON {
		return "", "", fmt.Errorf("scenario size out of bounds")
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil || len(m) == 0 {
		return "", "", fmt.Errorf("scenario must be a JSON object")
	}
	// json.Marshal sorts map keys alphabetically: canonical form.
	canonical, err := json.Marshal(m)
	if err != nil || len(canonical) > m8core.MaxScenarioJSON {
		return "", "", fmt.Errorf("scenario canonicalization failed")
	}
	sum := sha256.Sum256(canonical)
	return string(canonical), hex.EncodeToString(sum[:]), nil
}

// CreateScenario validates and stores one card under an existing expert.
func (s *ScenarioService) CreateScenario(ctx context.Context, in ScenarioCreateInput) (ScenarioCreateResult, error) {
	if s == nil || s.uow == nil {
		return ScenarioCreateResult{}, ErrServiceUnavailable
	}
	canonical, digest, err := canonicalScenario(in.Scenario)
	if err != nil {
		return ScenarioCreateResult{}, fmt.Errorf("%w: %v", ErrScenarioInvalid, err)
	}
	card := m8core.ScenarioCard{
		ExpertID:     in.ExpertID,
		Title:        in.Title,
		Summary:      in.Summary,
		PhaseKey:     in.PhaseKey,
		ScenarioJSON: canonical,
		ScenarioDigest: digest,
		State:        m8core.ScenarioActive,
	}
	if !m8core.ScenarioValidate(card) {
		return ScenarioCreateResult{}, ErrScenarioInvalid
	}
	now := s.clock.Now().UTC().Format(time.RFC3339)
	card.ScenarioCardID = ulid.Make().String()
	card.CreatedAt = now
	card.UpdatedAt = now
	err = s.uow.TransactScenario(ctx, func(tx ScenarioTx) error {
		if _, err := tx.GetExpert(in.ExpertID); errors.Is(err, m8core.ErrNotFound) {
			return ErrExpertNotFound
		} else if err != nil {
			return err
		}
		if _, has, err := tx.GetScenarioCardByTitle(in.ExpertID, in.Title); err != nil {
			return err
		} else if has {
			return ErrScenarioDuplicate
		}
		if err := tx.PutScenarioCard(card); err != nil {
			return err
		}
		_, err := tx.AppendAuditEvent(audit.Event{
			ID: ulid.Make().String(), Action: "expert.scenario.create",
			ResourceType: "expert_scenario_card", ResourceID: card.ScenarioCardID,
			Actor: actorOr(in.Actor), AfterDigest: digest, CreatedAt: now,
		})
		return err
	})
	if err != nil {
		return ScenarioCreateResult{}, err
	}
	return ScenarioCreateResult{
		ScenarioCardID: card.ScenarioCardID, ExpertID: card.ExpertID,
		Title: card.Title, PhaseKey: card.PhaseKey, Digest: digest,
	}, nil
}

// ListScenarios answers the cards of one expert; empty state lists both
// active and archived, otherwise only the requested state (newest first).
func (s *ScenarioService) ListScenarios(ctx context.Context, expertID, state string) ([]ScenarioView, error) {
	if s == nil || s.uow == nil {
		return nil, ErrServiceUnavailable
	}
	switch state {
	case "", m8core.ScenarioActive, m8core.ScenarioArchived:
	default:
		return nil, ErrScenarioInvalid
	}
	var views []ScenarioView
	err := s.uow.TransactScenario(ctx, func(tx ScenarioTx) error {
		cards, err := tx.ListScenarioCards(expertID, state)
		if err != nil {
			return err
		}
		views = make([]ScenarioView, 0, len(cards))
		for _, c := range cards {
			views = append(views, ScenarioView{
				ScenarioCardID: c.ScenarioCardID, ExpertID: c.ExpertID,
				Title: c.Title, Summary: c.Summary, PhaseKey: c.PhaseKey,
				State: c.State, CreatedAt: c.CreatedAt, UpdatedAt: c.UpdatedAt,
			})
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return views, nil
}

// DeleteScenario archives a card (soft delete); archived cards answer
// M10-EX-001 so the active journey never observes them twice.
func (s *ScenarioService) DeleteScenario(ctx context.Context, scenarioCardID, actor string) error {
	if s == nil || s.uow == nil {
		return ErrServiceUnavailable
	}
	now := s.clock.Now().UTC().Format(time.RFC3339)
	return s.uow.TransactScenario(ctx, func(tx ScenarioTx) error {
		card, err := tx.GetScenarioCard(scenarioCardID)
		if errors.Is(err, m8core.ErrNotFound) {
			return ErrScenarioNotFound
		}
		if err != nil {
			return err
		}
		if m8core.ScenarioTerminal(card.State) {
			return ErrScenarioNotFound
		}
		if err := tx.ArchiveScenarioCard(scenarioCardID, now); err != nil {
			return err
		}
		_, err = tx.AppendAuditEvent(audit.Event{
			ID: ulid.Make().String(), Action: "expert.scenario.archive",
			ResourceType: "expert_scenario_card", ResourceID: scenarioCardID,
			Actor: actorOr(actor), CreatedAt: now,
		})
		return err
	})
}
