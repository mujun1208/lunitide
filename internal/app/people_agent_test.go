package app

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lunitide/lunitide/internal/identity"
	"github.com/lunitide/lunitide/internal/people"
	"github.com/lunitide/lunitide/internal/projectapp"
	"github.com/lunitide/lunitide/internal/sessionapp"
	sqlitestore "github.com/lunitide/lunitide/internal/storage/sqlite"
)

func TestParseAgentMentionsAndClaimTask(t *testing.T) {
	agents := []people.Contact{
		{SubjectID: "01ARZ3NDEKTSV4RRFFQ69G5FAV", Nickname: "PPT专家", OrgName: people.AgentOrgName},
		{SubjectID: "01ARZ3NDEKTSV4RRFFQ69G5FAW", Nickname: "报告编写专家", OrgName: people.AgentOrgName},
	}
	got := parseAgentMentions("[引用专家 PPT专家|01ARZ3NDEKTSV4RRFFQ69G5FAV] 看一下封面", agents)
	if len(got) != 1 || got[0].Nickname != "PPT专家" {
		t.Fatalf("token mention = %#v", got)
	}
	got = parseAgentMentions("@PPT 看一下封面", agents)
	if len(got) != 1 || got[0].Nickname != "PPT专家" {
		t.Fatalf("short mention = %#v", got)
	}
	got = parseAgentMentions("@报告编写专家 认领 周报", agents)
	if len(got) != 1 || got[0].Nickname != "报告编写专家" {
		t.Fatalf("full mention = %#v", got)
	}
	if key := parseClaimTaskKey("@PPT专家 认领 周报封面"); key != "周报封面" {
		t.Fatalf("claim key = %q", key)
	}
	if parseClaimTaskKey("随便聊聊") != "" {
		t.Fatal("plain text must not claim")
	}
}

func TestPeopleAgentSessionAndToolAllow(t *testing.T) {
	if peopleAgentAllowedTool("user.ask") || peopleAgentAllowedTool("computer.act") || peopleAgentAllowedTool("desktop.open") {
		t.Fatal("colleague chat must refuse hang/desktop tools")
	}
	if !peopleAgentAllowedTool("pptx.gen") || !peopleAgentAllowedTool("excel.gen") || !peopleAgentAllowedTool("skill.invoke") || !peopleAgentAllowedTool("skill.view") || !peopleAgentAllowedTool("web.search") {
		t.Fatal("colleague chat must keep specialist tools")
	}
	defs := peopleAgentToolDefinitions(engineToolDefinitions())
	for _, d := range defs {
		if d.Name == "user.ask" || d.Name == "desktop.open" {
			t.Fatalf("filtered tool leaked: %s", d.Name)
		}
	}
}

func TestPeopleAgentPromptUsesTools(t *testing.T) {
	e := NewEngine(nil, "test")
	got := e.peopleAgentPrompt(context.Background(), people.Contact{Nickname: "PPT专家"})
	for _, needle := range []string{"同事专家", "不是独立进程", "PPT专家", "desktop=true", "skill.invoke", "pptx.gen", "@同事昵称"} {
		if !strings.Contains(got, needle) {
			t.Fatalf("prompt missing %q: %s", needle, got)
		}
	}
}

func TestExtractDeliverablePaths(t *testing.T) {
	paths := extractDeliverablePaths("ok:true\npath: 半年汇报.pptx\n其他")
	if len(paths) != 1 || !strings.Contains(paths[0], "半年汇报.pptx") {
		t.Fatalf("paths = %#v", paths)
	}
	note := formatDeliverablePaths([]string{"桌面/半年汇报.pptx", "桌面/半年汇报.pptx"}, "已经写好了")
	if note != "文件：桌面/半年汇报.pptx" {
		t.Fatalf("note = %q", note)
	}
	if formatDeliverablePaths([]string{"桌面/半年汇报.pptx"}, "见 桌面/半年汇报.pptx") != "" {
		t.Fatal("already mentioned path must not repeat")
	}
}

func TestPeopleAgentNoModelSendsSystem(t *testing.T) {
	ctx := context.Background()
	store, err := sqlitestore.OpenTemplated(ctx, filepath.Join(t.TempDir(), "no-model.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	ident := identity.New(store)
	if err := ident.Ensure(ctx); err != nil {
		t.Fatal(err)
	}
	roster := people.New(store, ident, t.TempDir(), t.TempDir())
	t.Cleanup(roster.Close)
	peerID := "01ARZ3NDEKTSV4RRFFQ69G5FAV"
	agent := people.Contact{
		SubjectID: peerID, Nickname: "PPT专家", Avatar: "📊", Status: "online",
		OrgName: people.AgentOrgName,
	}
	if err := roster.UpsertAgentContact(ctx, agent); err != nil {
		t.Fatal(err)
	}
	thread, _, err := roster.OpenDirect(ctx, peerID)
	if err != nil {
		t.Fatal(err)
	}
	e := NewEngineWithSessions(nil, projectapp.New(store, store), sessionapp.New(store, store), "test", nil)
	e.SetIdentityPeopleServices(ident, roster)
	reply, err := e.completePeopleAgentTurn(ctx, agent, thread.ThreadID, "继续刚才的")
	if err == nil || reply != peopleAgentNoReplyUserError() {
		t.Fatalf("reply=%q err=%v", reply, err)
	}
	msgs, err := roster.ListMessages(ctx, thread.ThreadID, 20)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, msg := range msgs {
		if msg.Kind == "system" && msg.Body == peopleAgentNoReplyUserError() {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("system notice missing: %#v", msgs)
	}
}

func TestPeopleAgentUnknownMentionSendsSystem(t *testing.T) {
	ctx := context.Background()
	store, err := sqlitestore.OpenTemplated(ctx, filepath.Join(t.TempDir(), "no-target.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	ident := identity.New(store)
	if err := ident.Ensure(ctx); err != nil {
		t.Fatal(err)
	}
	roster := people.New(store, ident, t.TempDir(), t.TempDir())
	t.Cleanup(roster.Close)
	peerID := "01ARZ3NDEKTSV4RRFFQ69G5FAV"
	if err := roster.UpsertAgentContact(ctx, people.Contact{
		SubjectID: peerID, Nickname: "PPT专家", Avatar: "📊", Status: "online",
		OrgName: people.AgentOrgName,
	}); err != nil {
		t.Fatal(err)
	}
	thread, err := roster.CreateGroup(ctx, "同事聊天", ident.SubjectID(), []string{peerID})
	if err != nil {
		t.Fatal(err)
	}
	e := NewEngineWithSessions(nil, projectapp.New(store, store), sessionapp.New(store, store), "test", nil)
	e.SetIdentityPeopleServices(ident, roster)
	e.runPeopleAgentJob(ctx, peopleAgentJob{
		threadID:  thread.ThreadID,
		messageID: "01ARZ3NDEKTSV4RRFFQ69G5FAW",
		body:      "@不存在的人 继续刚才的",
	})
	msgs, err := roster.ListMessages(ctx, thread.ThreadID, 20)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, msg := range msgs {
		if msg.Kind == "system" && msg.Body == peopleAgentNoTargetUserError() {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("unknown @ must SendSystem: %#v", msgs)
	}
}

func TestPeopleAgentHandoffTargetsSkipSelfAndCapHops(t *testing.T) {
	ppt := people.Contact{SubjectID: "01ARZ3NDEKTSV4RRFFQ69G5FAV", Nickname: "PPT专家", OrgName: people.AgentOrgName}
	report := people.Contact{SubjectID: "01ARZ3NDEKTSV4RRFFQ69G5FAW", Nickname: "报告编写专家", OrgName: people.AgentOrgName}
	agents := []people.Contact{ppt, report}
	got := peopleAgentHandoffTargets("@报告编写专家 请补演讲备注", agents, ppt.SubjectID)
	if len(got) != 1 || got[0].SubjectID != report.SubjectID {
		t.Fatalf("handoff = %#v", got)
	}
	if got := peopleAgentHandoffTargets("@PPT专家 我自己接着做", agents, ppt.SubjectID); len(got) != 0 {
		t.Fatalf("self mention must not hand off: %#v", got)
	}
	thread := people.Thread{ThreadID: "t1", Kind: "group", Members: agents}
	job := peopleAgentJob{targetID: report.SubjectID, body: "忽略正文里的 @PPT专家"}
	targets := peopleAgentJobTargets(job, job.body, thread, agents)
	if len(targets) != 1 || targets[0].SubjectID != report.SubjectID {
		t.Fatalf("explicit target = %#v", targets)
	}
	if peopleAgentHandoffBody("PPT专家", "请补备注") != "【PPT专家 交接】\n请补备注" {
		t.Fatal("handoff body")
	}
}

func TestPeopleAgentHandoffEnqueuesNextHop(t *testing.T) {
	resetPeopleAgentQueueForTest()
	_, started, _ := enqueuePeopleAgentTurn("t1", "m0", "busy")
	if !started {
		t.Fatal("seed must start")
	}
	ppt := people.Contact{SubjectID: "01ARZ3NDEKTSV4RRFFQ69G5FAV", Nickname: "PPT专家", OrgName: people.AgentOrgName}
	report := people.Contact{SubjectID: "01ARZ3NDEKTSV4RRFFQ69G5FAW", Nickname: "报告编写专家", OrgName: people.AgentOrgName}
	thread := people.Thread{ThreadID: "t1", Kind: "group", Members: []people.Contact{ppt, report}}
	e := NewEngine(nil, "test")
	e.enqueuePeopleAgentHandoffs(thread, people.Message{MessageID: "m1", Body: "@报告编写专家 请补演讲备注"}, ppt, 0)
	job, ok := dequeuePeopleAgentTurn("t1")
	if !ok || job.targetID != report.SubjectID || job.hop != 1 || !strings.Contains(job.body, "交接") {
		t.Fatalf("queued handoff = %#v ok=%v", job, ok)
	}
	e.enqueuePeopleAgentHandoffs(thread, people.Message{MessageID: "m2", Body: "@PPT专家 再看一眼"}, report, peopleAgentMaxHandoffHops)
	if _, ok := dequeuePeopleAgentTurn("t1"); ok {
		t.Fatal("max hop must not enqueue")
	}
}

func TestPeopleAgentMembersSkipsHumans(t *testing.T) {
	thread := people.Thread{Kind: "group", Members: []people.Contact{
		{SubjectID: "01ARZ3NDEKTSV4RRFFQ69G5FA0", Nickname: "mu", OrgName: "月汐", Self: true},
		{SubjectID: "01ARZ3NDEKTSV4RRFFQ69G5FAV", Nickname: "PPT专家", OrgName: people.AgentOrgName},
		{SubjectID: "01ARZ3NDEKTSV4RRFFQ69G5FAW", Nickname: "同事", OrgName: "月汐", TrustState: "trusted"},
	}}
	got := peopleAgentMembers(thread)
	if len(got) != 1 || got[0].Nickname != "PPT专家" {
		t.Fatalf("agents = %#v", got)
	}
}

func TestPeopleAgentEmptyReplyDoesNotClaimMissingModel(t *testing.T) {
	kind, msg := classifyPeopleAgentFailure(true, nil, "")
	if kind != peopleFailEmpty || strings.Contains(msg, "启用一个对话模型") {
		t.Fatalf("got %d %q", kind, msg)
	}
	kind, msg = classifyPeopleAgentFailure(false, nil, "")
	if kind != peopleFailNoModel || msg != peopleAgentNoReplyUserError() {
		t.Fatalf("no catalog: %d %q", kind, msg)
	}
}

func TestPeopleAgentHistorySkipsCurrentUser(t *testing.T) {
	hist := peopleAgentHistoryMessages([]people.Message{
		{Kind: "text", SenderID: "u", Body: "周转件怎么算"},
		{Kind: "text", SenderID: "agent", Body: "先算周转率…"},
		{Kind: "text", SenderID: "u", Body: "做个模拟计算表"},
	}, "agent", "做个模拟计算表", 8)
	if len(hist) != 2 || hist[0].Content != "周转件怎么算" || hist[1].Content != "先算周转率…" {
		t.Fatalf("%+v", hist)
	}
}
