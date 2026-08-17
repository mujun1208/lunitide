// M10 memory nomination domain: a nomination is the enriched wrapper of a
// memory candidate — it records who nominated what, why and from which
// session, while confirmation/rejection keeps flowing through the 0061
// explicit token path (FR-02/FR-11 invariants untouched).
package m8core

// Nomination states (migration 0071 CHECK).
const (
	NomNominated = "nominated"
	NomDecided   = "decided"
	NomWithdrawn = "withdrawn"
)

// Field limits mirroring migration 0071 CHECKs.
const (
	MaxNominator = 128
	MaxReason    = 2048
)

// Nomination is one nomination row (migration 0071).
type Nomination struct {
	NominationID    string
	CandidateID     string
	Nominator       string
	Reason          string
	SourceSessionID string // optional
	State           string
	DecidedAt       string // UTC RFC3339, set on decided/withdrawn
	CreatedAt       string
}

// NomTerminal reports whether a nomination state is final.
func NomTerminal(state string) bool {
	return state == NomDecided || state == NomWithdrawn
}

// NomTransitionAllowed guards the single legal terminal transition:
// nominated -> decided / withdrawn. Terminal rows never move again.
func NomTransitionAllowed(from, to string) bool {
	if from != NomNominated {
		return false
	}
	return to == NomDecided || to == NomWithdrawn
}

// NomValidate checks the nomination invariants.
func NomValidate(n Nomination) bool {
	if len(n.Nominator) < 1 || len(n.Nominator) > MaxNominator {
		return false
	}
	if len(n.Reason) < 1 || len(n.Reason) > MaxReason {
		return false
	}
	switch n.State {
	case NomNominated, NomDecided, NomWithdrawn:
	default:
		return false
	}
	return true
}
