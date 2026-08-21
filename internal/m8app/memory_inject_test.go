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
