package policy

import (
	"errors"
	"testing"
	"time"
)

var baseTime = time.Date(2026, 8, 16, 12, 0, 0, 0, time.UTC)

func humans(ids ...string) []Subject {
	out := make([]Subject, 0, len(ids))
	for _, id := range ids {
		out = append(out, Subject{ID: id, Kind: SubjectHuman})
	}
	return out
}

func twoOfThree(t *testing.T) *Request {
	t.Helper()
	r, err := NewRequest("req-1", "01JDPOLICYORG00000001", "initiator-a", 2,
		humans("alice", "bob", "carol"), true, "digest-req-1", "policy-digest-1")
	if err != nil {
		t.Fatalf("NewRequest failed: %v", err)
	}
	return r
}

func castAll(t *testing.T, r *Request, now time.Time, ids ...string) {
	t.Helper()
	for _, id := range ids {
		if err := r.Vote(id, r.RequestDigest, r.PolicyDigest, now.Format(time.RFC3339), now); err != nil {
			t.Fatalf("Vote(%s) failed: %v", id, err)
		}
	}
}

func TestApproval(t *testing.T) {
	t.Run("requester self-approval is refused (SoD, T-07)", func(t *testing.T) {
		_, err := NewRequest("req-x", "01JDPOLICYORG00000001", "alice", 1, humans("alice", "bob"), true, "d", "p")
		if !errors.Is(err, ErrSoDViolation) || Code(err) != "M9-007" {
			t.Fatalf("want M9-007, got %v", err)
		}
	})

	t.Run("candidates freeze at creation (M9-013)", func(t *testing.T) {
		r := twoOfThree(t)
		if err := r.Vote("mallory", r.RequestDigest, r.PolicyDigest, baseTime.Format(time.RFC3339), baseTime); !errors.Is(err, ErrApprovalCandidate) {
			t.Fatalf("want M9-013 for non-candidate, got %v", err)
		}
		if got := r.Candidates(); len(got) != 3 || got[0] != "alice" {
			t.Fatalf("candidate set must stay frozen and sorted, got %v", got)
		}
	})

	t.Run("duplicate subject roles cast exactly one vote (T-09)", func(t *testing.T) {
		withDupes := append(humans("alice", "bob"), Subject{ID: "alice", Kind: SubjectHuman}, Subject{ID: "alice", Kind: SubjectHuman})
		r, err := NewRequest("req-d", "01JDPOLICYORG00000001", "init", 2, withDupes, true, "d", "p")
		if err != nil {
			t.Fatalf("NewRequest must dedupe candidates: %v", err)
		}
		castAll(t, r, baseTime, "alice", "alice", "bob")
		if got := r.ApprovedCount(baseTime); got != 2 {
			t.Fatalf("alice voting thrice must count once: want 2, got %d", got)
		}
	})

	t.Run("service identity cannot satisfy a human threshold (T-04)", func(t *testing.T) {
		mixed := []Subject{
			{ID: "svc-1", Kind: SubjectService},
			{ID: "svc-2", Kind: SubjectService},
			{ID: "human-1", Kind: SubjectHuman},
		}
		// n=2 is unreachable: only one human candidate exists.
		if _, err := NewRequest("req-s", "01JDPOLICYORG00000001", "init", 2, mixed, true, "d", "p"); !errors.Is(err, ErrThresholdNotMet) {
			t.Fatalf("unreachable human threshold must refuse creation, got %v", err)
		}
		r, err := NewRequest("req-s2", "01JDPOLICYORG00000001", "init", 2, append(mixed, Subject{ID: "human-2", Kind: SubjectHuman}), true, "d", "p")
		if err != nil {
			t.Fatalf("NewRequest failed: %v", err)
		}
		castAll(t, r, baseTime, "svc-1", "svc-2", "human-1")
		if got := r.ApprovedCount(baseTime); got != 1 {
			t.Fatalf("service votes must not count towards a human threshold, got %d", got)
		}
		if r.State() != StateWaitingApproval {
			t.Fatalf("want WAITING_APPROVAL, got %s", r.State())
		}
	})

	t.Run("n-of-m reaches APPROVED and dispatch gate passes", func(t *testing.T) {
		r := twoOfThree(t)
		castAll(t, r, baseTime, "alice")
		if r.State() != StateWaitingApproval {
			t.Fatalf("1/3 must stay waiting, got %s", r.State())
		}
		castAll(t, r, baseTime, "bob")
		if r.State() != StateApproved {
			t.Fatalf("2/3 must approve, got %s", r.State())
		}
		if err := r.EnsureDispatchable(r.PolicyDigest, baseTime); err != nil {
			t.Fatalf("dispatch gate must pass: %v", err)
		}
	})

	t.Run("revoking after approval falls back to WAITING_APPROVAL (T-08)", func(t *testing.T) {
		r := twoOfThree(t)
		castAll(t, r, baseTime, "alice", "bob")
		if r.State() != StateApproved {
			t.Fatal("precondition: approved")
		}
		if err := r.Revoke("bob", baseTime); err != nil {
			t.Fatalf("Revoke failed: %v", err)
		}
		if r.State() != StateWaitingApproval {
			t.Fatalf("2-of-3 revocation must return to WAITING_APPROVAL, got %s", r.State())
		}
		if err := r.EnsureDispatchable(r.PolicyDigest, baseTime); !errors.Is(err, ErrThresholdNotMet) || Code(err) != "M9-012" {
			t.Fatalf("want M9-012 at dispatch gate, got %v", err)
		}
		if err := r.Revoke("bob", baseTime); !errors.Is(err, ErrVoteRevoked) || Code(err) != "M9-014" {
			t.Fatalf("double revoke must fail with M9-014, got %v", err)
		}
		// Recasting after revocation restores the approval.
		castAll(t, r, baseTime, "bob", "carol")
		if r.State() != StateApproved {
			t.Fatalf("recast must re-approve, got %s", r.State())
		}
	})

	t.Run("votes are bound to request and policy digests (M9-010)", func(t *testing.T) {
		r := twoOfThree(t)
		if err := r.Vote("alice", "digest-req-2", r.PolicyDigest, baseTime.Format(time.RFC3339), baseTime); !errors.Is(err, ErrVersionStale) {
			t.Fatalf("wrong request digest must fail M9-010, got %v", err)
		}
		if err := r.Vote("alice", r.RequestDigest, "policy-other", baseTime.Format(time.RFC3339), baseTime); !errors.Is(err, ErrVersionStale) {
			t.Fatalf("wrong policy digest must fail M9-010, got %v", err)
		}
		castAll(t, r, baseTime, "alice", "bob")
		if err := r.EnsureDispatchable("policy-other", baseTime); !errors.Is(err, ErrVersionStale) {
			t.Fatalf("dispatch against moved policy must fail M9-010, got %v", err)
		}
	})

	t.Run("policy tightening recomputes and drops approved state", func(t *testing.T) {
		r := twoOfThree(t)
		castAll(t, r, baseTime, "alice", "bob")
		if r.State() != StateApproved {
			t.Fatal("precondition: approved")
		}
		r.RebindPolicy("policy-digest-2", baseTime)
		if r.State() != StateWaitingApproval {
			t.Fatalf("policy change must void votes and recompute, got %s", r.State())
		}
		// Old-digest votes cannot enter under the new policy version.
		if err := r.Vote("alice", r.RequestDigest, "policy-digest-1", baseTime.Format(time.RFC3339), baseTime); !errors.Is(err, ErrVersionStale) {
			t.Fatalf("stale-digest vote must fail M9-010, got %v", err)
		}
	})

	t.Run("expired identity recomputes the tally (T-03 adjacency)", func(t *testing.T) {
		expiry := baseTime.Add(2 * time.Hour).Format(time.RFC3339)
		candidates := []Subject{
			{ID: "alice", Kind: SubjectHuman, ExpiresAt: expiry},
			{ID: "bob", Kind: SubjectHuman},
			{ID: "carol", Kind: SubjectHuman},
		}
		r, err := NewRequest("req-e", "01JDPOLICYORG00000001", "init", 2, candidates, true, "d", "p")
		if err != nil {
			t.Fatalf("NewRequest failed: %v", err)
		}
		castAll(t, r, baseTime, "alice", "bob")
		if r.State() != StateApproved {
			t.Fatal("precondition: approved before expiry")
		}
		afterExpiry := baseTime.Add(3 * time.Hour)
		if got := r.ApprovedCount(afterExpiry); got != 1 {
			t.Fatalf("expired voter must drop out of the tally, got %d", got)
		}
		if err := r.EnsureDispatchable(r.PolicyDigest, afterExpiry); !errors.Is(err, ErrThresholdNotMet) {
			t.Fatalf("expired identity must block dispatch with M9-012, got %v", err)
		}
		// An already-expired subject cannot cast a new vote at all.
		if err := r.Vote("alice", r.RequestDigest, r.PolicyDigest, afterExpiry.Format(time.RFC3339), afterExpiry); !errors.Is(err, ErrApprovalCandidate) {
			t.Fatalf("expired subject must be refused, got %v", err)
		}
	})
}
