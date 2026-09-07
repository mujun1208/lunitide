package app

import (
	"context"
	"encoding/json"
	"github.com/lunitide/lunitide/internal/domain/m8core"
	"github.com/lunitide/lunitide/internal/domain/memory"
	"github.com/lunitide/lunitide/internal/m8app"
	"strings"
	"testing"
)

func TestUserMemoryHiddenStopsAutomaticInject(t *testing.T) {
	mem, ops, _ := openAppMemory(t)
	e := NewEngine(nil, "test")
	e.SetM8MemoryServices(mem)
	e.SetMemoryOpsService(ops)
	ctx := context.Background()
	const sid = "01ARZ3NDEKTSV4RRFFQ69G5FAW"
	const mid = "01ARZ3NDEKTSV4RRFFQ69G5FAV"
	if err := e.maybeAutoNominateTurn(ctx, sid, "我喜欢爵士音乐", "好的", mid, false); err != nil {
		t.Fatal(err)
	}
	facts, _, err := ops.Facts(ctx, "active", m8app.LearningScope, 20, 0)
	if err != nil || len(facts) != 1 {
		t.Fatalf("facts %v %v", facts, err)
	}
	before, err := mem.PersonalPreferenceSnapshot(ctx, "local-user", m8app.LearningScope, 8, 4096)
	if err != nil || len(before) != 1 {
		t.Fatalf("before=%v %v", before, err)
	}
	if err := ops.FlagFact(ctx, facts[0].FactID, m8core.FlagHidden, "", true); err != nil {
		t.Fatal(err)
	}
	after, err := mem.PersonalPreferenceSnapshot(ctx, "local-user", m8app.LearningScope, 8, 4096)
	if err != nil || len(after) != 0 {
		t.Fatalf("hidden prefs=%v %v", after, err)
	}
	recall, err := mem.RecallForInject(ctx, m8app.RecallInput{ScopeID: m8app.LearningScope, SubjectID: "local-user", Query: "爵士音乐"})
	if err != nil || len(recall.Hits) != 0 {
		t.Fatalf("hidden recall=%v %v", recall, err)
	}
	if err := e.maybeAutoNominateTurn(ctx, sid, "我喜欢爵士音乐", "好的", mid, false); err != nil {
		t.Fatal(err)
	}
	facts, _, _ = ops.Facts(ctx, "", m8app.LearningScope, 20, 0)
	if len(facts) != 1 {
		t.Fatal("hidden memory recreated")
	}
	if err := ops.FlagFact(ctx, facts[0].FactID, m8core.FlagHidden, "", false); err != nil {
		t.Fatal(err)
	}
	after, err = mem.PersonalPreferenceSnapshot(ctx, "local-user", m8app.LearningScope, 8, 4096)
	if err != nil || len(after) != 1 {
		t.Fatalf("restored prefs=%v %v", after, err)
	}
}
func TestMemoryPendingScopedAndNoExpertLeak(t *testing.T) {
	mem, ops, _ := openAppMemory(t)
	e := NewEngine(nil, "test")
	e.SetM8MemoryServices(mem)
	e.SetMemoryOpsService(ops)
	ctx := context.Background()
	const sid = "01ARZ3NDEKTSV4RRFFQ69G5FAW"
	const other = "01ARZ3NDEKTSV4RRFFQ69G5FAX"
	const mid = "01ARZ3NDEKTSV4RRFFQ69G5FAV"
	if err := ops.SettingsUpdate(ctx, m8core.MemorySettings{SubjectID: "local-user", MemoryEnabled: true, CaptureMode: "manual", GrowthDays: 14}); err != nil {
		t.Fatal(err)
	}
	if err := e.maybeAutoNominateTurn(ctx, sid, "我喜欢爵士音乐", "", mid, false); err != nil {
		t.Fatal(err)
	}
	count := func(sessionID string) int {
		res := handleFeedbackCandidates(e, ctx, validRequest("feedback.candidates", `{"limit":3,"sessionId":"`+sessionID+`"}`))
		if !res.OK {
			t.Fatalf("%+v", res)
		}
		b, _ := json.Marshal(res.Payload)
		var out struct {
			Items []m8app.PendingCandidateView `json:"items"`
		}
		if err := json.Unmarshal(b, &out); err != nil {
			t.Fatal(err)
		}
		return len(out.Items)
	}
	if count(sid) != 1 || count(other) != 0 {
		t.Fatal("manual candidates cross sessions")
	}
	pending, _ := mem.ListPendingCandidates(ctx, 20)
	if _, err := mem.ConfirmCandidate(ctx, m8app.ConfirmInput{CandidateID: pending[0].CandidateID, Token: pending[0].ConfirmationToken, Action: "reject", RequestID: "reject-user-pref"}); err != nil {
		t.Fatal(err)
	}
	if err := e.maybeAutoNominateTurn(ctx, sid, "我喜欢爵士音乐", "", mid, false); err != nil {
		t.Fatal(err)
	}
	if count(sid) != 0 {
		t.Fatal("rejected preference prompted again")
	}
	confirmPref(t, mem, "用户：@PPT专家 你在吗\n要点：在的，给我一个主题")
	pack := e.prepareChatMemory(ctx, chatMemoryRequest{Query: "你好", SessionID: other, Companion: true})
	if strings.Contains(strings.Join(pack.Prefs, " "), "PPT") {
		t.Fatal("expert leaked")
	}
	items := []memory.Memory{{Key: sessionLastMemoryKey, Content: "PPT专家"}, {Key: "任务", Content: "同项目通用工作"}}
	if got := relevantSessionSummary(items, "你好"); len(got) != 1 {
		t.Fatal(got)
	}
	if got := relevantSessionSummary(items, "继续刚才的"); len(got) != 2 {
		t.Fatal(got)
	}
}
