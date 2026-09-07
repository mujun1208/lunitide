package app

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/lunitide/lunitide/internal/bridge"
	"github.com/lunitide/lunitide/internal/messageapp"
	"github.com/lunitide/lunitide/internal/toolruntime"
)

func childProcessEvent(kind bridge.EventType, call, name, summary string) bridge.Event {
	return bridge.Event{Type: kind, Tool: &bridge.ToolEvent{CallID: call, Name: name, ArgsDigest: strings.Repeat("a", 64), Summary: summary}}
}
func TestMessageProcessSubagentsKeepIdentityAcrossLateStartAndLongReport(t *testing.T) {
	const id = "01ARZ3NDEKTSV4RRFFQ69G5FAV"
	for _, name := range []string{"subagent.spawn", "subagent.join"} {
		t.Run(name, func(t *testing.T) {
			p := messageProcess{}
			progress := subagentProgress{ID: id, Status: "running", Profile: "研究专家", Purpose: "核对天气数据来源", Stage: "searching", Tool: "web.search"}
			p.capture(childProcessEvent(bridge.EventToolOutput, "call-1", name, progress.JSONSummary()))
			if len(p.Tools) != 1 {
				t.Fatal("progress was discarded")
			}
			before := p.Tools[0]
			p.capture(childProcessEvent(bridge.EventToolStarted, "call-1", name, "开始子任务"))
			if p.Tools[0].Summary != before.Summary {
				t.Fatal("late start erased progress")
			}
			report, _ := json.Marshal(map[string]any{"subagentId": id, "status": "completed", "profile": "explore", "summary": strings.Repeat("已经完成来源核对，包含中文与\"<转义>。", 140), "spentTokens": 2000})
			if len(report) < 4096 {
				t.Fatal("test report must exceed original summary limit")
			}
			p.capture(childProcessEvent(bridge.EventToolCompleted, "call-1", name, string(report)))
			saved, ok := parseProcessSubagent(p.Tools[0].Summary, false)
			if !ok || saved.ID != id || saved.Status != "completed" || saved.Purpose != progress.Purpose || saved.Profile != progress.Profile {
				t.Fatalf("identity/status/title lost: %s", p.Tools[0].Summary)
			}
			if len(p.Tools[0].Summary) > 512 || !json.Valid([]byte(p.Tools[0].Summary)) || !p.valid() {
				t.Fatalf("invalid compact record: %s", p.Tools[0].Summary)
			}
			p.capture(childProcessEvent(bridge.EventToolOutput, "call-1", name, progress.JSONSummary()))
			again, _ := parseProcessSubagent(p.Tools[0].Summary, false)
			if again.Status != "completed" {
				t.Fatal("late progress reverted terminal status")
			}
		})
	}
}
func TestMessageProcessSubagentsPreserveLastValidTerminalWhenReportWasAlreadyClipped(t *testing.T) {
	p := messageProcess{}
	progress := subagentProgress{ID: "01ARZ3NDEKTSV4RRFFQ69G5FAV", Status: "failed", Purpose: "核对文档", Detail: "工具执行失败"}
	p.capture(childProcessEvent(bridge.EventToolOutput, "child", "subagent.spawn", progress.JSONSummary()))
	p.capture(childProcessEvent(bridge.EventToolCompleted, "child", "subagent.spawn", `{"subagentId":"01ARZ3NDEKTSV4RRFFQ69G5FAV","summary":"truncated`))
	saved, ok := parseProcessSubagent(p.Tools[0].Summary, false)
	if !ok || saved.Status != "failed" || saved.ID != progress.ID || saved.Purpose != progress.Purpose {
		t.Fatalf("corrupt final report overwrote progress: %s", p.Tools[0].Summary)
	}
}
func TestMessageProcessSubagentsIgnoreOtherToolsAndUnstructuredOutput(t *testing.T) {
	p := messageProcess{}
	for _, event := range []bridge.Event{
		childProcessEvent(bridge.EventToolOutput, "a", "workspace.read", `{"id":"01ARZ3NDEKTSV4RRFFQ69G5FAV","status":"completed","marker":"subagent_progress"}`),
		childProcessEvent(bridge.EventToolOutput, "b", "subagent.spawn", `{"id":"01ARZ3NDEKTSV4RRFFQ69G5FAV","status":"completed"}`),
		childProcessEvent(bridge.EventToolOutput, "c", "subagent.spawn", `{"id":"not-an-agent","status":"completed","marker":"subagent_progress"}`),
		childProcessEvent(bridge.EventToolOutput, "d", "subagent.join", `{"id":"01ARZ3NDEKTSV4RRFFQ69G5FAV","status":"unknown","marker":"subagent_progress"}`),
	} {
		p.capture(event)
	}
	if len(p.Tools) != 0 {
		t.Fatalf("untrusted progress captured: %+v", p.Tools)
	}
}
func TestMessageProcessSubagentReopenRetainsJoinTarget(t *testing.T) {
	e, _, sid, _ := messageEngine(t)
	ctx := context.Background()
	runtime, err := toolruntime.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer runtime.Close()
	e.tools = runtime
	msg, err := e.messages.AppendAssistant(ctx, "children-finished", "test", sid, "两项子任务已完成", messageapp.AssistantUsage{})
	if err != nil {
		t.Fatal(err)
	}
	p := messageProcess{}
	ids := []string{"01ARZ3NDEKTSV4RRFFQ69G5FAV", "01ARZ3NDEKTSV4RRFFQ69G5FAW"}
	for index, id := range ids {
		call := []string{"first", "second"}[index]
		progress := subagentProgress{ID: id, Status: "completed", Profile: "审查专家", Purpose: "核对数据与文档", Stage: "completed"}
		p.capture(childProcessEvent(bridge.EventToolOutput, call, "subagent.spawn", progress.JSONSummary()))
		p.capture(childProcessEvent(bridge.EventToolStarted, call, "subagent.spawn", "开始子任务"))
		report, _ := json.Marshal(map[string]any{"subagentId": id, "status": "completed", "summary": strings.Repeat("完整报告", 1024)})
		p.capture(childProcessEvent(bridge.EventToolCompleted, call, "subagent.spawn", string(report)))
	}
	e.saveMessageProcess(sid, msg.ID, p)
	fresh := NewEngine(nil, "reopened")
	fresh.tools = runtime
	fresh.messages = e.messages
	fresh.sessions = e.sessions
	response := fresh.Handle(ctx, validRequest("message.process", `{"sessionId":"`+sid+`","messageId":"`+msg.ID+`"}`))
	if !response.OK {
		t.Fatalf("reopen: %+v", response)
	}
	raw, _ := json.Marshal(response.Payload)
	var loaded messageProcess
	if err = json.Unmarshal(raw, &loaded); err != nil || !loaded.valid() || len(loaded.Tools) != 2 {
		t.Fatalf("invalid reopened process %s %v", raw, err)
	}
	for i, tool := range loaded.Tools {
		progress, ok := parseProcessSubagent(tool.Summary, false)
		if !ok || progress.ID != ids[i] || progress.Status != "completed" || progress.Purpose != "核对数据与文档" {
			t.Fatalf("wrong join target after reopen: %s", tool.Summary)
		}
	}
}

func TestMessageProcessSubagentJoinWithoutProgressKeepsCompleteFinalWireIdentity(t *testing.T) {
	const id = "01ARZ3NDEKTSV4RRFFQ69G5FAV"
	result, _ := json.Marshal(map[string]any{"subagentId": id, "status": "completed", "summary": strings.Repeat("\"<完整结果>汉", 2000), "spentTokens": 3000})
	if json.Valid([]byte(clipToolSummary(string(result)))) {
		t.Fatal("test must reproduce old final JSON truncation")
	}
	display := subagentDisplaySummary("subagent.join", string(result))
	if len(display) > 512 || !json.Valid([]byte(display)) {
		t.Fatalf("wire summary not bounded whole JSON: %s", display)
	}
	p := messageProcess{}
	p.capture(childProcessEvent(bridge.EventToolStarted, "join-1", "subagent.join", "收集结果"))
	p.capture(childProcessEvent(bridge.EventToolCompleted, "join-1", "subagent.join", display))
	got, ok := parseProcessSubagent(p.Tools[0].Summary, false)
	if !ok || got.ID != id || got.Status != "completed" {
		t.Fatalf("join target lost without earlier progress: %s", p.Tools[0].Summary)
	}
	if got := subagentDisplaySummary("workspace.read", string(result)); got != clipToolSummary(string(result)) {
		t.Fatal("ordinary tool behavior changed")
	}
}
