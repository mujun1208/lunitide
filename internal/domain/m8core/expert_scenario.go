// M10 expert scenario-card domain: a scenario card is the structured play
// book an expert runs in one project phase (context/goals/steps/outputs).
// phase_key reuses the M7/M8 fixed nine-phase enum; delete is the soft
// active->archived transition only (scenario rows are never removed).
package m8core

// Scenario card states (migration 0072 CHECK).
const (
	ScenarioActive   = "active"
	ScenarioArchived = "archived"
)

// Field limits mirroring migration 0072 CHECKs.
const (
	MaxScenarioTitle   = 128
	MaxScenarioSummary = 2048
	MaxScenarioJSON    = 65536
)

// ScenarioCard is one expert_scenario_cards row (migration 0072).
type ScenarioCard struct {
	ScenarioCardID string
	ExpertID       string
	Title          string
	Summary        string
	PhaseKey       string
	ScenarioJSON   string
	ScenarioDigest string // sha256 of the canonical scenario JSON
	State          string
	CreatedAt      string
	UpdatedAt      string
}

// ScenarioTerminal reports whether a scenario card state is final.
func ScenarioTerminal(state string) bool { return state == ScenarioArchived }

// ScenarioValidate checks the card invariants (digest shape excluded: the
// service computes it and the migration CHECK guards storage).
func ScenarioValidate(c ScenarioCard) bool {
	if len(c.Title) < 1 || len(c.Title) > MaxScenarioTitle {
		return false
	}
	if len(c.Summary) < 1 || len(c.Summary) > MaxScenarioSummary {
		return false
	}
	if !ValidPhaseKey(c.PhaseKey) {
		return false
	}
	switch c.State {
	case ScenarioActive, ScenarioArchived:
	default:
		return false
	}
	return true
}
