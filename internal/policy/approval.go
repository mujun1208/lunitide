// approval.go implements the M9 slice-2 SoD + N-of-M approval state machine
// (T-9.2.2, ADR-013 decision 3 + 02 技术设计 · PolicyCenter 与审批):
//
//   - 禁止自批: the request initiator may never appear in the frozen
//     candidate set (M9-007).
//   - 候选冻结: the M candidates are frozen when the request is created;
//     votes from anyone else are refused (M9-013).
//   - 票据绑定: every vote binds the request digest and the policy digest it
//     was cast under; either moving invalidates the vote set (M9-010).
//   - 人类门槛: service identities cannot satisfy an approval that requires
//     human approval (T-04).
//   - 撤票重算: revoking a vote - or an identity expiring, or the policy
//     tightening - recomputes the tally and drops an already-APPROVED request
//     back to WAITING_APPROVAL before dispatch (T-08).
//   - 一主体一票: a subject holding several roles still casts exactly one
//     vote (T-09).
//
// Concept error taxonomy (04 错误目录, wire-alias only):
//
//	M9-007 SOD_VIOLATION             M9-013 APPROVAL_CANDIDATE_INVALID
//	M9-010 POLICY_VERSION_STALE      M9-014 APPROVAL_VOTE_REVOKED
//	M9-012 APPROVAL_THRESHOLD_NOT_MET
package policy

import (
	"fmt"
	"sort"
	"time"
)

// Approval states (projection source for WAITING_APPROVAL in the Execution
// Governance table).
const (
	StateWaitingApproval = "WAITING_APPROVAL"
	StateApproved        = "APPROVED"
)

// SubjectKind distinguishes human approval authority from service
// identities (ADR-012); only human votes satisfy a human-required threshold.
type SubjectKind string

const (
	SubjectHuman   SubjectKind = "human"
	SubjectService SubjectKind = "service"
)

// Subject is one approval authority referenced by a request. Candidates are
// always same-org principals - the org boundary is enforced upstream by the
// org store predicate, never re-derived here.
type Subject struct {
	ID        string
	Kind      SubjectKind
	ExpiresAt string // RFC3339; empty = no expiry
}

func (s Subject) expiredAt(now time.Time) bool {
	if s.ExpiresAt == "" {
		return false
	}
	t, err := time.Parse(time.RFC3339, s.ExpiresAt)
	if err != nil {
		return true // unparsable expiry fails closed
	}
	return !now.Before(t)
}

// Vote is one approval ballot bound to the request digest and the policy
// digest it was cast under.
type Vote struct {
	SubjectID     string
	RequestDigest string
	PolicyDigest  string
	GrantedAt     string
	Revoked       bool
}

// Request is an N-of-M approval request with its candidate set frozen at
// creation time.
type Request struct {
	ID            string
	OrgID         string
	InitiatorID   string
	N             int
	RequireHuman  bool
	RequestDigest string
	PolicyDigest  string

	candidates map[string]Subject
	votes      map[string]*Vote
	state      string
}

// NewRequest freezes the candidate set and enforces the SoD invariants.
// The initiator must not be a candidate (M9-007); candidates are deduplicated
// per subject (one subject, one vote, T-09); N must be reachable given the
// human-requirement (T-04).
func NewRequest(id, orgID, initiatorID string, n int, candidates []Subject, requireHuman bool, requestDigest, policyDigest string) (*Request, error) {
	if id == "" || orgID == "" || initiatorID == "" {
		return nil, ErrEvaluationUnavailable
	}
	frozen := make(map[string]Subject, len(candidates))
	for _, c := range candidates {
		if c.ID == "" || (c.Kind != SubjectHuman && c.Kind != SubjectService) {
			return nil, ErrEvaluationUnavailable
		}
		if c.ID == initiatorID {
			// 自批为零: initiator ∩ approvers = ∅ (ADR-013 decision 3).
			return nil, fmt.Errorf("%w: initiator %q may not approve their own request", ErrSoDViolation, initiatorID)
		}
		if _, dup := frozen[c.ID]; dup {
			continue // duplicate role rows collapse to one subject
		}
		frozen[c.ID] = c
	}
	if n < 1 {
		return nil, ErrEvaluationUnavailable
	}
	reachable := 0
	for _, c := range frozen {
		if !requireHuman || c.Kind == SubjectHuman {
			reachable++
		}
	}
	if n > reachable {
		// The threshold can never be met by this candidate set.
		return nil, fmt.Errorf("%w: n=%d exceeds %d eligible candidates", ErrThresholdNotMet, n, reachable)
	}
	return &Request{
		ID: id, OrgID: orgID, InitiatorID: initiatorID, N: n,
		RequireHuman: requireHuman, RequestDigest: requestDigest, PolicyDigest: policyDigest,
		candidates: frozen, votes: make(map[string]*Vote), state: StateWaitingApproval,
	}, nil
}

// State reports WAITING_APPROVAL or APPROVED.
func (r *Request) State() string { return r.state }

// Candidates returns the frozen candidate IDs, sorted.
func (r *Request) Candidates() []string {
	ids := make([]string, 0, len(r.candidates))
	for id := range r.candidates {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

// ApprovedCount tallies live votes that satisfy the human requirement.
func (r *Request) ApprovedCount(now time.Time) int {
	count := 0
	for id, v := range r.votes {
		if v.Revoked {
			continue
		}
		subject, ok := r.candidates[id]
		if !ok || subject.expiredAt(now) {
			continue
		}
		if r.RequireHuman && subject.Kind != SubjectHuman {
			continue
		}
		count++
	}
	return count
}

// Vote records one approval ballot. Non-candidates are refused (M9-013);
// stale digests are refused (M9-010); a subject voting again is idempotent -
// one subject always counts once (T-09). Recasting after revocation restores
// the ballot and recomputes the state.
func (r *Request) Vote(subjectID, requestDigest, policyDigest, grantedAt string, now time.Time) error {
	subject, ok := r.candidates[subjectID]
	if !ok {
		return fmt.Errorf("%w: subject %q is not in the frozen candidate set", ErrApprovalCandidate, subjectID)
	}
	if requestDigest != r.RequestDigest || policyDigest != r.PolicyDigest {
		return fmt.Errorf("%w: vote digests do not match the request", ErrVersionStale)
	}
	if subject.expiredAt(now) {
		return fmt.Errorf("%w: subject %q expired", ErrApprovalCandidate, subjectID)
	}
	if existing, seen := r.votes[subjectID]; seen && !existing.Revoked {
		return nil // idempotent: one subject, one vote
	}
	r.votes[subjectID] = &Vote{SubjectID: subjectID, RequestDigest: requestDigest, PolicyDigest: policyDigest, GrantedAt: grantedAt}
	r.recompute(now)
	return nil
}

// Revoke withdraws a live vote (M9-014 when absent) and recomputes the
// tally - an APPROVED request drops back to WAITING_APPROVAL (T-08).
func (r *Request) Revoke(subjectID string, now time.Time) error {
	v, ok := r.votes[subjectID]
	if !ok || v.Revoked {
		return fmt.Errorf("%w: no live vote from subject %q", ErrVoteRevoked, subjectID)
	}
	v.Revoked = true
	r.recompute(now)
	return nil
}

// RebindPolicy invalidates the vote set after the policy digest moved
// (策略收紧触发重算) and recomputes the tally under the new digest.
func (r *Request) RebindPolicy(newPolicyDigest string, now time.Time) {
	if newPolicyDigest == r.PolicyDigest {
		return
	}
	r.PolicyDigest = newPolicyDigest
	r.votes = make(map[string]*Vote)
	r.recompute(now)
}

func (r *Request) recompute(now time.Time) {
	if r.ApprovedCount(now) >= r.N {
		r.state = StateApproved
	} else {
		r.state = StateWaitingApproval
	}
}

// EnsureDispatchable is the pre-dispatch gate: the request must be APPROVED
// under the exact live policy digest, else M9-012 / M9-010.
func (r *Request) EnsureDispatchable(livePolicyDigest string, now time.Time) error {
	if r.PolicyDigest != livePolicyDigest {
		return fmt.Errorf("%w: request pinned policy %s, live policy %s", ErrVersionStale, r.PolicyDigest, livePolicyDigest)
	}
	r.recompute(now)
	if r.state != StateApproved {
		return fmt.Errorf("%w: state %s with %d/%d votes", ErrThresholdNotMet, r.state, r.ApprovedCount(now), r.N)
	}
	return nil
}
