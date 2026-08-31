package app

import (
	"context"
	"strings"
	"testing"

	"github.com/lunitide/lunitide/internal/people"
)

func TestParseAgentMentionsAndClaimTask(t *testing.T) {
	agents := []people.Contact{
		{SubjectID: "01ARZ3NDEKTSV4RRFFQ69G5FAV", Nickname: "PPT专家", OrgName: people.AgentOrgName},
		{SubjectID: "01ARZ3NDEKTSV4RRFFQ69G5FAW", Nickname: "报告编写专家", OrgName: people.AgentOrgName},
	}
	got := parseAgentMentions("@PPT 看一下封面", agents)
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
	if peopleAgentSessionID("short") != "" {
		t.Fatal("non-ULID thread must not be a workspace session")
	}
	if got := peopleAgentSessionID("01ARZ3NDEKTSV4RRFFQ69G5FAV"); got != "01ARZ3NDEKTSV4RRFFQ69G5FAV" {
		t.Fatalf("session = %q", got)
	}
	if peopleAgentAllowedTool("user.ask") || peopleAgentAllowedTool("computer.act") || peopleAgentAllowedTool("desktop.open") {
		t.Fatal("colleague chat must refuse hang/desktop tools")
	}
	if !peopleAgentAllowedTool("pptx.gen") || !peopleAgentAllowedTool("skill.invoke") || !peopleAgentAllowedTool("web.search") {
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
	for _, needle := range []string{"独立智能体", "PPT专家", "desktop=true", "skill.invoke", "pptx.gen"} {
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
