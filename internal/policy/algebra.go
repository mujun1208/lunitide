// Package policy implements the M9 slice-2 policy merge algebra accepted by
// ADR-013 (D-3): four-level tighten-only merging Platform → Organization →
// TeamSpace → Project. Every dimension may only stay equal or grow stricter
// downwards; any relaxation is rejected with M9-009 - never silently clamped
// back to the parent value (02 技术设计 · PolicyCenter 与审批).
//
// Concept error taxonomy (04 错误目录, wire-alias only):
//
//	M9-007 SOD_VIOLATION               M9-010 POLICY_VERSION_STALE
//	M9-008 POLICY_PARENT_MISSING       M9-011 POLICY_EVALUATION_UNAVAILABLE
//	M9-009 POLICY_RELAXATION_DENIED
package policy

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"

	"github.com/lunitide/lunitide/internal/domain/token"
)

// ConceptError is an M9 concept-taxonomy error (code + name).
type ConceptError struct {
	Code string
	Name string
}

func (e *ConceptError) Error() string { return e.Code + " " + e.Name }

func (e *ConceptError) Is(target error) bool {
	var other *ConceptError
	if errors.As(target, &other) {
		return other.Code == e.Code
	}
	return false
}

var (
	ErrSoDViolation          = &ConceptError{"M9-007", "SOD_VIOLATION"}
	ErrParentMissing         = &ConceptError{"M9-008", "POLICY_PARENT_MISSING"}
	ErrRelaxationDenied      = &ConceptError{"M9-009", "POLICY_RELAXATION_DENIED"}
	ErrVersionStale          = &ConceptError{"M9-010", "POLICY_VERSION_STALE"}
	ErrEvaluationUnavailable = &ConceptError{"M9-011", "POLICY_EVALUATION_UNAVAILABLE"}
	ErrThresholdNotMet       = &ConceptError{"M9-012", "APPROVAL_THRESHOLD_NOT_MET"}
	ErrApprovalCandidate     = &ConceptError{"M9-013", "APPROVAL_CANDIDATE_INVALID"}
	ErrVoteRevoked           = &ConceptError{"M9-014", "APPROVAL_VOTE_REVOKED"}
)

// Code extracts the M9 concept code when err carries one.
func Code(err error) string {
	var ce *ConceptError
	if errors.As(err, &ce) {
		return ce.Code
	}
	return ""
}

// Level is a policy hierarchy tier (ADR-013). Merging walks strictly
// downwards one level at a time.
type Level int

const (
	LevelPlatform Level = iota
	LevelOrganization
	LevelTeamSpace
	LevelProject
)

func (l Level) String() string {
	switch l {
	case LevelPlatform:
		return "platform"
	case LevelOrganization:
		return "organization"
	case LevelTeamSpace:
		return "teamspace"
	case LevelProject:
		return "project"
	default:
		return fmt.Sprintf("level(%d)", int(l))
	}
}

// Kind declares how one constraint dimension merges and how "stricter" is
// proven for it (the per-dimension monotonicity the tighten-only guarantee
// is built on, ADR-013 decision 5):
//
//	allowlist  allowed set   intersection      child ⊆ parent
//	denylist   forbidden set union             parent ⊆ child
//	required   required set  union             parent ⊆ child
//	ceiling    numeric max   minimum           child ≤ parent
//	floor      numeric min   maximum           child ≥ parent
//	flag       boolean gate  AND               child implies parent
type Kind string

const (
	KindAllowlist Kind = "allowlist"
	KindDenylist  Kind = "denylist"
	KindRequired  Kind = "required"
	KindCeiling   Kind = "ceiling"
	KindFloor     Kind = "floor"
	KindFlag      Kind = "flag"
)

// Value is one normalized constraint dimension. Set lists are sorted and
// deduplicated at construction so digests are stable.
type Value struct {
	Kind   Kind     `json:"kind"`
	Set    []string `json:"set,omitempty"`
	Number float64  `json:"number,omitempty"`
	Bool   bool     `json:"bool,omitempty"`
}

func normalizeSet(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, v := range values {
		if v == "" {
			continue
		}
		if _, dup := seen[v]; dup {
			continue
		}
		seen[v] = struct{}{}
		out = append(out, v)
	}
	sort.Strings(out)
	return out
}

// Allowlist constructs an allowed-set constraint (merges by intersection).
func Allowlist(values ...string) Value { return Value{Kind: KindAllowlist, Set: normalizeSet(values)} }

// Denylist constructs a forbidden-set constraint (merges by union).
func Denylist(values ...string) Value { return Value{Kind: KindDenylist, Set: normalizeSet(values)} }

// Required constructs a required-set constraint (merges by union).
func Required(values ...string) Value { return Value{Kind: KindRequired, Set: normalizeSet(values)} }

// Ceiling constructs a numeric upper-bound constraint (merges by minimum).
func Ceiling(n float64) Value { return Value{Kind: KindCeiling, Number: n} }

// Floor constructs a numeric lower-bound constraint (merges by maximum).
func Floor(n float64) Value { return Value{Kind: KindFloor, Number: n} }

// Flag constructs a boolean gate constraint (merges by AND; true is stricter).
func Flag(on bool) Value { return Value{Kind: KindFlag, Bool: on} }

// Constraints is a normalized constraint set keyed by dimension name.
type Constraints map[string]Value

func subsetOf(sub, sup []string) bool {
	index := make(map[string]struct{}, len(sup))
	for _, v := range sup {
		index[v] = struct{}{}
	}
	for _, v := range sub {
		if _, ok := index[v]; !ok {
			return false
		}
	}
	return true
}

// relation reports how child compares to parent on one dimension when the
// child is required to be "equal or stricter" (the lattice order).
type relation int

const (
	relationEqual relation = iota
	relationStricter
	relationRelaxed
)

func relate(parent, child Value) relation {
	switch parent.Kind {
	case KindAllowlist:
		if len(child.Set) == len(parent.Set) && subsetOf(child.Set, parent.Set) {
			return relationEqual
		}
		if subsetOf(child.Set, parent.Set) {
			return relationStricter
		}
		return relationRelaxed
	case KindDenylist, KindRequired:
		if len(child.Set) == len(parent.Set) && subsetOf(parent.Set, child.Set) {
			return relationEqual
		}
		if subsetOf(parent.Set, child.Set) {
			return relationStricter
		}
		return relationRelaxed
	case KindCeiling:
		switch {
		case child.Number == parent.Number:
			return relationEqual
		case child.Number < parent.Number:
			return relationStricter
		default:
			return relationRelaxed
		}
	case KindFloor:
		switch {
		case child.Number == parent.Number:
			return relationEqual
		case child.Number > parent.Number:
			return relationStricter
		default:
			return relationRelaxed
		}
	case KindFlag:
		switch {
		case child.Bool == parent.Bool:
			return relationEqual
		case child.Bool && !parent.Bool:
			return relationStricter
		default:
			return relationRelaxed
		}
	default:
		return relationRelaxed
	}
}

// mergeDim folds child into parent under the tighten lattice. The folded
// value is the lattice meet: it can never be wider than either input.
func mergeDim(parent, child Value) Value {
	switch parent.Kind {
	case KindAllowlist:
		keep := make(map[string]struct{}, len(parent.Set))
		for _, v := range parent.Set {
			keep[v] = struct{}{}
		}
		out := make([]string, 0, len(child.Set))
		for _, v := range child.Set {
			if _, ok := keep[v]; ok {
				out = append(out, v)
			}
		}
		sort.Strings(out)
		return Value{Kind: KindAllowlist, Set: out}
	case KindDenylist, KindRequired:
		return Value{Kind: parent.Kind, Set: normalizeSet(append(append([]string{}, parent.Set...), child.Set...))}
	case KindCeiling:
		if child.Number < parent.Number {
			return child
		}
		return parent
	case KindFloor:
		if child.Number > parent.Number {
			return child
		}
		return parent
	case KindFlag:
		return Value{Kind: KindFlag, Bool: parent.Bool && child.Bool}
	default:
		return parent
	}
}

// Tighten merges child constraints into parent constraints under the
// tighten-only lattice (ADR-013 decision 1). The returned set is the
// per-dimension meet: equal or stricter than both inputs. If any declared
// child dimension is wider than the parent's, the merge is rejected with
// M9-009 naming the offending dimension (T-05 定位父规则) - there is no
// silent clamp-to-parent fallback.
func Tighten(parent, child Constraints) (Constraints, error) {
	out := make(Constraints, len(parent)+len(child))
	for k, v := range parent {
		out[k] = v
	}
	keys := make([]string, 0, len(child))
	for k := range child {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		c := child[k]
		p, inherited := parent[k]
		if !inherited {
			// A dimension the parent does not constrain is a fresh
			// restriction - always a tightening.
			out[k] = c
			continue
		}
		if p.Kind != c.Kind {
			// Cross-kind dimensions are incomparable: "equal or stricter"
			// cannot be proven, so publication is refused.
			return nil, fmt.Errorf("%w: dimension %q kind %s cannot tighten %s", ErrRelaxationDenied, k, c.Kind, p.Kind)
		}
		if relate(p, c) == relationRelaxed {
			return nil, fmt.Errorf("%w: dimension %q relaxes parent %s constraint", ErrRelaxationDenied, k, p.Kind)
		}
		out[k] = mergeDim(p, c)
	}
	return out, nil
}

// Node is one policy hierarchy node: a named version of constraints at a
// level, authored against a specific parent version.
type Node struct {
	ID                string
	Level             Level
	OrgID             string // empty only for the platform root
	Version           int64  // monotonic per node
	ExpectedParentVer int64  // parent version this draft was authored against
	Constraints       Constraints

	parent        *Node
	effective     Constraints
	digest        string
	chainVersions []string
}

// Digest returns the canonical-JSON SHA-256 fingerprint of the node's
// effective constraints (sorted keys, no whitespace - house convention).
func (n *Node) Digest() string { return n.digest }

// Effective returns the tightened merge of the whole ancestry chain.
func (n *Node) Effective() Constraints { return n.effective }

// ChainVersions returns "level:id@version" for every layer contributing to
// the effective constraints, root first (the 来源层版本链, ADR-013 decision 4).
func (n *Node) ChainVersions() []string { return n.chainVersions }

// Attach validates a child draft against its live parent and returns the
// attached node carrying effective constraints and their digest. It is the
// publication gate: parent missing → M9-008, authored-against version no
// longer current → M9-010 (T-06 竞态, the draft must re-prove), any relaxed
// dimension → M9-009.
func Attach(parent *Node, child Node) (*Node, error) {
	node := child
	node.Constraints = cloneConstraints(child.Constraints)
	if parent == nil {
		if node.Level != LevelPlatform {
			return nil, fmt.Errorf("%w: %s node %q has no parent", ErrParentMissing, node.Level, node.ID)
		}
		node.parent = nil
		node.effective = cloneConstraints(node.Constraints)
		node.chainVersions = []string{node.ref()}
		node.digest = Digest(node.effective)
		return &node, nil
	}
	if node.Level != parent.Level+1 {
		return nil, fmt.Errorf("%w: %s node %q cannot attach below %s parent", ErrParentMissing, node.Level, node.ID, parent.Level)
	}
	if node.ExpectedParentVer != parent.Version {
		return nil, fmt.Errorf("%w: node %q was authored against parent v%d but live parent is v%d", ErrVersionStale, node.ID, node.ExpectedParentVer, parent.Version)
	}
	merged, err := Tighten(parent.effective, node.Constraints)
	if err != nil {
		return nil, err
	}
	node.parent = parent
	node.effective = merged
	node.chainVersions = append(append([]string{}, parent.chainVersions...), node.ref())
	node.digest = Digest(merged)
	return &node, nil
}

func (n *Node) ref() string { return fmt.Sprintf("%s:%s@v%d", n.Level, n.ID, n.Version) }

func cloneConstraints(c Constraints) Constraints {
	out := make(Constraints, len(c))
	for k, v := range c {
		if v.Set != nil {
			set := make([]string, len(v.Set))
			copy(set, v.Set)
			v.Set = set
		}
		out[k] = v
	}
	return out
}

// Digest fingerprints a constraint set via canonical JSON SHA-256.
func Digest(c Constraints) string {
	raw, err := token.CanonicalJSON(map[string]any{"constraints": constraintsToAny(c)})
	if err != nil {
		// Constraints are plain JSON-safe values; failure is unreachable.
		return ""
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

func constraintsToAny(c Constraints) map[string]any {
	keys := make([]string, 0, len(c))
	for k := range c {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := make(map[string]any, len(c))
	for _, k := range keys {
		v := c[k]
		entry := map[string]any{"kind": string(v.Kind)}
		if v.Set != nil {
			set := make([]any, len(v.Set))
			for i, s := range v.Set {
				set[i] = s
			}
			entry["set"] = set
		}
		if v.Number != 0 {
			entry["number"] = v.Number
		}
		if v.Bool {
			entry["bool"] = true
		}
		out[k] = entry
	}
	return out
}

// Decision is a pinned authorization verdict (ADR-013 decision 4): it binds
// the effective digest and the source version chain. After any policy
// change the digest moves and replaying the old verdict fails with M9-010.
type Decision struct {
	Digest    string
	Versions  []string
	Effective Constraints
}

// Decide pins a verdict for an attached node.
func Decide(n *Node) *Decision {
	if n == nil {
		return nil
	}
	return &Decision{Digest: n.digest, Versions: append([]string{}, n.chainVersions...), Effective: cloneConstraints(n.effective)}
}

// ReplayAgainst refuses to replay a pinned decision once the live policy
// digest moved (策略变更后旧 digest 判定不可重放).
func (d *Decision) ReplayAgainst(currentDigest string) error {
	if d == nil || d.Digest == "" || d.Digest != currentDigest {
		return ErrVersionStale
	}
	return nil
}

// Direction classifies one simulated dimension change.
type Direction string

const (
	DirectionTightened Direction = "tightened"
	DirectionRelaxed   Direction = "relaxed"
	DirectionAdded     Direction = "added"
	DirectionRemoved   Direction = "removed"
	DirectionUnchanged Direction = "unchanged"
)

// DimChange is one row of a simulator diff (策略模拟器合并预演:
// 变更前后 digest 与逐维度 diff, ADR-013 consequences).
type DimChange struct {
	Key       string
	Before    *Value
	After     *Value
	Direction Direction
}

// Simulate diffs two constraint sets dimension by dimension. The caller can
// preflight a draft: any DirectionRelaxed row predicts the M9-009 refusal
// Tighten would raise.
func Simulate(before, after Constraints) []DimChange {
	keys := make(map[string]struct{}, len(before)+len(after))
	for k := range before {
		keys[k] = struct{}{}
	}
	for k := range after {
		keys[k] = struct{}{}
	}
	ordered := make([]string, 0, len(keys))
	for k := range keys {
		ordered = append(ordered, k)
	}
	sort.Strings(ordered)
	out := make([]DimChange, 0, len(ordered))
	for _, k := range ordered {
		b, hasB := before[k]
		a, hasA := after[k]
		row := DimChange{Key: k}
		if hasB {
			copyB := b
			row.Before = &copyB
		}
		if hasA {
			copyA := a
			row.After = &copyA
		}
		switch {
		case !hasB:
			row.Direction = DirectionAdded
		case !hasA:
			row.Direction = DirectionRemoved
		default:
			switch relate(b, a) {
			case relationEqual:
				row.Direction = DirectionUnchanged
			case relationStricter:
				row.Direction = DirectionTightened
			default:
				row.Direction = DirectionRelaxed
			}
		}
		out = append(out, row)
	}
	return out
}
