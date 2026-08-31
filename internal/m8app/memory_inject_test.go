package m8app_test

import (
	"context"
	"strings"
	"testing"

	"github.com/lunitide/lunitide/internal/domain/m8core"
	"github.com/lunitide/lunitide/internal/m8app"
)

func TestRecallForInjectUsesConfirmedContentAndSkipsPending(t *testing.T) {
	svc, _ := openMemoryService(t)
	ctx := context.Background()
	pending := propose(t, svc, leafDoc(m8app.LearningScope, "pending must never inject", ""), true)
	_ = pending
	prop := propose(t, svc, leafDoc(m8app.LearningScope, "代码注释默认使用中文", ""), false)
	if _, err := confirm(t, svc, m8app.ConfirmInput{
		CandidateID: prop.Candidate.CandidateID, Token: prop.ConfirmToken,
		Action: "confirm", RequestID: "req-inject-1",
	}); err != nil {
		t.Fatal(err)
	}
	sens := propose(t, svc, leafDoc(m8app.LearningScope, "代码仓库的密钥轮换口令", m8core.SensSensitive), false)
	if _, err := confirm(t, svc, m8app.ConfirmInput{
		CandidateID: sens.Candidate.CandidateID, Token: sens.ConfirmToken,
		Action: "confirm", RequestID: "req-inject-2",
	}); err != nil {
		t.Fatal(err)
	}

	res, err := svc.RecallForInject(ctx, m8app.RecallInput{ScopeID: m8app.LearningScope, Query: "代码注释", TopK: 6})
	if err != nil {
		t.Fatal(err)
	}
	if res.TraceID == "" || res.IndexVersion != "confirmed-v1" {
		t.Fatalf("trace=%q index=%q", res.TraceID, res.IndexVersion)
	}
	if len(res.Hits) != 1 || !strings.Contains(res.Hits[0].Content, "代码注释") {
		t.Fatalf("hits = %+v, want confirmed 中文注释 payload", res.Hits)
	}
	if strings.Contains(res.Hits[0].Content, "pending") || strings.Contains(res.Hits[0].Content, "密钥") {
		t.Fatalf("pending or sensitive leaked: %+v", res.Hits)
	}
	if len(res.Explanation.Redactions) != 1 {
		t.Fatalf("redactions = %v", res.Explanation.Redactions)
	}
}

func TestRecallForInjectIsolatesSubject(t *testing.T) {
	svc, _ := openMemoryService(t)
	ctx := context.Background()
	mine := "01ARZ3NDEKTSV4RRFFQ69G5FAV"
	other := "01ARZ3NDEKTSV4RRFFQ69G5FAW"
	proposeOn := func(subject, content string) {
		t.Helper()
		prop, err := svc.ProposeCandidate(ctx, m8app.ProposeInput{
			SubjectID: subject,
			Doc: m8core.PayloadDoc{
				Content: content, ScopeID: m8app.LearningScope, Sensitivity: m8core.SensPrivate,
				Leaves: []m8core.SourceLeafClaim{{JSONPointer: "/content", EvidenceRef: "artifact://run-1/evidence-a", Digest: strings.Repeat("a", 64)}},
			},
			Trust: m8core.TrustUntrusted, Actor: "test",
		})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := confirm(t, svc, m8app.ConfirmInput{
			CandidateID: prop.Candidate.CandidateID, Token: prop.ConfirmToken,
			Action: "confirm", RequestID: "req-inject-" + prop.Candidate.CandidateID,
		}); err != nil {
			t.Fatal(err)
		}
	}
	proposeOn(mine, "继续刚才的深色主题")
	proposeOn(other, "继续刚才的别人笔记")
	res, err := svc.RecallForInject(ctx, m8app.RecallInput{
		ScopeID: m8app.LearningScope, Query: "继续刚才的", TopK: 6, SubjectID: mine,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Hits) != 1 || !strings.Contains(res.Hits[0].Content, "深色主题") {
		t.Fatalf("hits = %+v", res.Hits)
	}
	if strings.Contains(res.Hits[0].Content, "别人笔记") {
		t.Fatalf("foreign subject leaked: %+v", res.Hits)
	}
}

func TestConfirmedByIDIsolatesSubject(t *testing.T) {
	svc, _ := openMemoryService(t)
	ctx := context.Background()
	mine := "01ARZ3NDEKTSV4RRFFQ69G5FAV"
	other := "01ARZ3NDEKTSV4RRFFQ69G5FAW"
	propose := func(subject, content string) string {
		t.Helper()
		prop, err := svc.ProposeCandidate(ctx, m8app.ProposeInput{
			SubjectID: subject,
			Doc: m8core.PayloadDoc{
				Content: content, ScopeID: m8app.LearningScope, Sensitivity: m8core.SensPrivate,
				Leaves: []m8core.SourceLeafClaim{{JSONPointer: "/content", EvidenceRef: "artifact://run-1/evidence-a", Digest: strings.Repeat("a", 64)}},
			},
			Trust: m8core.TrustUntrusted, Actor: "test",
		})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := confirm(t, svc, m8app.ConfirmInput{
			CandidateID: prop.Candidate.CandidateID, Token: prop.ConfirmToken,
			Action: "confirm", RequestID: "req-get-" + prop.Candidate.CandidateID,
		}); err != nil {
			t.Fatal(err)
		}
		return prop.Candidate.CandidateID
	}
	mineID := propose(mine, "我的确认记忆")
	otherID := propose(other, "别人的确认记忆")
	got, err := svc.ConfirmedByIDFor(ctx, mineID, mine)
	if err != nil || !strings.Contains(got, "我的确认记忆") {
		t.Fatalf("mine get = %q err=%v", got, err)
	}
	hidden, err := svc.ConfirmedByIDFor(ctx, otherID, mine)
	if err == nil || hidden != "" {
		t.Fatalf("foreign get leaked: %q err=%v", hidden, err)
	}
}

func TestListPendingCandidatesForFiltersSubject(t *testing.T) {
	svc, _ := openMemoryService(t)
	ctx := context.Background()
	proposePending := func(subject, content string) {
		t.Helper()
		if _, err := svc.ProposeCandidate(ctx, m8app.ProposeInput{
			SubjectID: subject,
			Doc: m8core.PayloadDoc{
				Content: content, ScopeID: m8app.LearningScope, Sensitivity: m8core.SensPrivate,
				Leaves: []m8core.SourceLeafClaim{{JSONPointer: "/content", EvidenceRef: "artifact://run-1/evidence-a", Digest: strings.Repeat("a", 64)}},
			},
			Trust: m8core.TrustUntrusted, Actor: "test",
		}); err != nil {
			t.Fatal(err)
		}
	}
	mine := "01ARZ3NDEKTSV4RRFFQ69G5FAV"
	other := "01ARZ3NDEKTSV4RRFFQ69G5FAW"
	proposePending(mine, "我的待确认")
	proposePending(other, "别人的待确认")
	all, err := svc.ListPendingCandidates(ctx, 10)
	if err != nil || len(all) != 2 {
		t.Fatalf("unscoped pending = %+v err=%v", all, err)
	}
	mineOnly, err := svc.ListPendingCandidatesFor(ctx, mine, 10)
	if err != nil || len(mineOnly) != 1 || mineOnly[0].Content != "我的待确认" {
		t.Fatalf("mine pending = %+v err=%v", mineOnly, err)
	}
	if strings.Contains(mineOnly[0].Content, "别人") {
		t.Fatalf("foreign pending leaked: %+v", mineOnly)
	}
}
