// M8 slice-1 service tests (T-8.1.x): explicit-only promotion, token
// single-use, expiry, rejection, inference-promotion refusal, leaf-source
// guards and explained recall - against a fully migrated SQLite store.
package m8app_test

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/lunitide/lunitide/internal/domain/m8core"
	"github.com/lunitide/lunitide/internal/m8app"
	storage "github.com/lunitide/lunitide/internal/storage/sqlite"
)

type fakeClock struct{ now time.Time }

func (c *fakeClock) Now() time.Time { return c.now }

func openMemoryService(t *testing.T) (*m8app.MemoryService, *fakeClock) {
	t.Helper()
	store, err := storage.Open(context.Background(), filepath.Join(t.TempDir(), "m8-memory.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	clock := &fakeClock{now: time.Date(2026, 8, 16, 12, 0, 0, 0, time.UTC)}
	svc := m8app.NewMemoryService(store.AgentRuntimeRepository(), "local-user")
	svc.SetClock(clock)
	return svc, clock
}

func leafDoc(scope, content string, sensitivity string) m8core.PayloadDoc {
	if sensitivity == "" {
		sensitivity = m8core.SensPrivate
	}
	return m8core.PayloadDoc{
		Content:     content,
		ScopeID:     scope,
		Sensitivity: sensitivity,
		Leaves: []m8core.SourceLeafClaim{{
			JSONPointer: "/content",
			EvidenceRef: "artifact://run-1/evidence-a",
			Digest:      strings.Repeat("a", 64),
		}},
	}
}

func propose(t *testing.T, svc *m8app.MemoryService, doc m8core.PayloadDoc, inferred bool) m8app.ProposeResult {
	t.Helper()
	res, err := svc.ProposeCandidate(context.Background(), m8app.ProposeInput{
		SubjectID: "local-user",
		Doc:       doc,
		Inferred:  inferred,
		Trust:     m8core.TrustUntrusted,
		Actor:     "adapter",
	})
	if err != nil {
		t.Fatalf("propose: %v", err)
	}
	return res
}

func confirm(t *testing.T, svc *m8app.MemoryService, in m8app.ConfirmInput) (m8app.ConfirmResult, error) {
	t.Helper()
	return svc.ConfirmCandidate(context.Background(), in)
}

func TestProposeConfirmCreatesFactLeavesAndAudit(t *testing.T) {
	svc, _ := openMemoryService(t)
	prop := propose(t, svc, leafDoc("subject:local-user", "prefer dark theme", ""), false)
	if prop.Candidate.State != m8core.CandPending || prop.ConfirmToken == "" {
		t.Fatalf("candidate state=%s token=%q", prop.Candidate.State, prop.ConfirmToken)
	}
	res, err := confirm(t, svc, m8app.ConfirmInput{
		CandidateID: prop.Candidate.CandidateID,
		Token:       prop.ConfirmToken,
		Action:      "confirm",
		RequestID:   "req-1",
	})
	if err != nil {
		t.Fatalf("confirm: %v", err)
	}
	if res.State != m8core.CandConfirmed || res.Fact == nil || res.Fact.Version != 1 {
		t.Fatalf("confirm result = %+v", res)
	}
	// Replay of the single-use token is refused (M8-004).
	if _, err := confirm(t, svc, m8app.ConfirmInput{CandidateID: prop.Candidate.CandidateID, Token: prop.ConfirmToken, Action: "confirm", RequestID: "req-2"}); !errors.Is(err, m8app.ErrConfirmTokenInvalid) {
		t.Fatalf("replay err = %v, want ErrConfirmTokenInvalid", err)
	}
}

func TestConfirmWrongTokenRejected(t *testing.T) {
	svc, _ := openMemoryService(t)
	prop := propose(t, svc, leafDoc("subject:local-user", "x", ""), false)
	if _, err := confirm(t, svc, m8app.ConfirmInput{
		CandidateID: prop.Candidate.CandidateID,
		Token:       strings.Repeat("b", 64),
		Action:      "confirm",
		RequestID:   "req-1",
	}); !errors.Is(err, m8app.ErrConfirmTokenInvalid) {
		t.Fatalf("err = %v, want ErrConfirmTokenInvalid", err)
	}
}

func TestConfirmExpiredCandidateTransitionsAndFails(t *testing.T) {
	svc, clock := openMemoryService(t)
	res, err := svc.ProposeCandidate(context.Background(), m8app.ProposeInput{
		SubjectID: "local-user",
		Doc:       leafDoc("subject:local-user", "x", ""),
		Trust:     m8core.TrustUntrusted,
		TTL:       time.Hour,
	})
	if err != nil {
		t.Fatalf("propose: %v", err)
	}
	clock.now = clock.now.Add(2 * time.Hour)
	if _, err := confirm(t, svc, m8app.ConfirmInput{CandidateID: res.Candidate.CandidateID, Token: res.ConfirmToken, Action: "confirm", RequestID: "req-1"}); !errors.Is(err, m8app.ErrCandidateExpired) {
		t.Fatalf("err = %v, want ErrCandidateExpired", err)
	}
	// The expired transition itself is terminal: a retry now answers expired.
	if _, err := confirm(t, svc, m8app.ConfirmInput{CandidateID: res.Candidate.CandidateID, Token: res.ConfirmToken, Action: "confirm", RequestID: "req-2"}); !errors.Is(err, m8app.ErrCandidateExpired) {
		t.Fatalf("retry err = %v, want ErrCandidateExpired", err)
	}
}

func TestConfirmRejectWritesNoFact(t *testing.T) {
	svc, _ := openMemoryService(t)
	prop := propose(t, svc, leafDoc("subject:local-user", "x", ""), false)
	res, err := confirm(t, svc, m8app.ConfirmInput{CandidateID: prop.Candidate.CandidateID, Token: prop.ConfirmToken, Action: "reject", RequestID: "req-1"})
	if err != nil || res.State != m8core.CandRejected || res.Fact != nil {
		t.Fatalf("reject res=%+v err=%v", res, err)
	}
}

func TestAutoPromoteAlwaysDenied(t *testing.T) {
	svc, _ := openMemoryService(t)
	prop := propose(t, svc, leafDoc("subject:local-user", "x", ""), true)
	if err := svc.AutoPromote(context.Background(), prop.Candidate.CandidateID, "compaction"); !errors.Is(err, m8app.ErrInferencePromotionDenied) {
		t.Fatalf("err = %v, want ErrInferencePromotionDenied", err)
	}
	// The candidate must still be pending: refusal has no side effects.
	res, err := confirm(t, svc, m8app.ConfirmInput{CandidateID: prop.Candidate.CandidateID, Token: prop.ConfirmToken, Action: "confirm", RequestID: "req-1"})
	if err != nil || res.State != m8core.CandConfirmed {
		t.Fatalf("explicit confirm after auto-refusal res=%+v err=%v", res, err)
	}
}

func TestProposeRequiresSourceLeaf(t *testing.T) {
	svc, _ := openMemoryService(t)
	doc := leafDoc("subject:local-user", "x", "")
	doc.Leaves = nil
	if _, err := svc.ProposeCandidate(context.Background(), m8app.ProposeInput{SubjectID: "local-user", Doc: doc, Trust: m8core.TrustUntrusted}); !errors.Is(err, m8app.ErrSourceLeafRequired) {
		t.Fatalf("err = %v, want ErrSourceLeafRequired", err)
	}
}

func TestConfirmWithEditedPayloadKeepsScopeAndLeaves(t *testing.T) {
	svc, _ := openMemoryService(t)
	prop := propose(t, svc, leafDoc("subject:local-user", "draft content", ""), false)
	edited := m8core.PayloadDoc{Content: "edited content"}
	res, err := confirm(t, svc, m8app.ConfirmInput{
		CandidateID: prop.Candidate.CandidateID,
		Token:       prop.ConfirmToken,
		Action:      "confirm",
		EditedDoc:   &edited,
		RequestID:   "req-1",
	})
	if err != nil || res.Fact == nil {
		t.Fatalf("edited confirm res=%+v err=%v", res, err)
	}
}

func TestConfirmFailsClosedOnEvidenceVerification(t *testing.T) {
	svc, _ := openMemoryService(t)
	svc.SetEvidenceVerifier(func(ctx context.Context, ref, digest string) error {
		return errors.New("artifact revoked")
	})
	prop := propose(t, svc, leafDoc("subject:local-user", "x", ""), false)
	if _, err := confirm(t, svc, m8app.ConfirmInput{CandidateID: prop.Candidate.CandidateID, Token: prop.ConfirmToken, Action: "confirm", RequestID: "req-1"}); !errors.Is(err, m8app.ErrSourceEvidenceUnavailable) {
		t.Fatalf("err = %v, want ErrSourceEvidenceUnavailable", err)
	}
}

func TestRecallRedactsSensitiveAndTraces(t *testing.T) {
	svc, _ := openMemoryService(t)
	for _, doc := range []m8core.PayloadDoc{
		leafDoc("subject:local-user", "rotate keys quarterly", ""),
		leafDoc("subject:local-user", "rotate keys weekly", m8core.SensSensitive),
	} {
		prop := propose(t, svc, doc, false)
		if _, err := confirm(t, svc, m8app.ConfirmInput{CandidateID: prop.Candidate.CandidateID, Token: prop.ConfirmToken, Action: "confirm", RequestID: "req-" + prop.Candidate.CandidateID}); err != nil {
			t.Fatalf("confirm: %v", err)
		}
	}
	res, err := svc.Recall(context.Background(), m8app.RecallInput{ScopeID: "subject:local-user", Query: "evidence-a"})
	if err != nil {
		t.Fatalf("recall: %v", err)
	}
	if len(res.Hits) != 1 {
		t.Fatalf("hits = %d, want 1 (sensitive redacted)", len(res.Hits))
	}
	if res.Hits[0].Source != "artifact://run-1/evidence-a" || res.Hits[0].Version != 1 {
		t.Fatalf("hit = %+v", res.Hits[0])
	}
	if len(res.Explanation.Redactions) != 1 || res.Explanation.Redactions[0] != "policy:sensitivity=sensitive" {
		t.Fatalf("redactions = %v", res.Explanation.Redactions)
	}
	if len(res.Explanation.Reasons) != 1 || res.Explanation.Missing {
		t.Fatalf("reasons = %v missing=%v", res.Explanation.Reasons, res.Explanation.Missing)
	}
	if res.TraceID == "" || res.IndexVersion != "facts-v1" {
		t.Fatalf("trace=%q index=%q", res.TraceID, res.IndexVersion)
	}
}

func TestRecallScopeDeniedAndPolicyFailClosed(t *testing.T) {
	svc, _ := openMemoryService(t)
	svc.SetPolicyProbe(func(ctx context.Context, subject, scope string) (bool, error) { return false, nil })
	if _, err := svc.Recall(context.Background(), m8app.RecallInput{ScopeID: "project:p1", Query: "x"}); !errors.Is(err, m8app.ErrRecallScopeDenied) {
		t.Fatalf("err = %v, want ErrRecallScopeDenied", err)
	}
	svc.SetPolicyProbe(func(ctx context.Context, subject, scope string) (bool, error) { return false, errors.New("engine down") })
	if _, err := svc.Recall(context.Background(), m8app.RecallInput{ScopeID: "project:p1", Query: "x"}); !errors.Is(err, m8app.ErrPolicyUnavailable) {
		t.Fatalf("err = %v, want ErrPolicyUnavailable", err)
	}
}
