package app

import (
	"strings"
	"testing"

	"github.com/lunitide/lunitide/internal/domain/agentrun"
	"github.com/lunitide/lunitide/internal/domain/provider"
	"github.com/lunitide/lunitide/internal/llmadapter"
)

func TestDropWindowRetryImages(t *testing.T) {
	req := &llmadapter.Request{Images: []llmadapter.Image{{MIME: "image/png", Data: []byte("shot")}}}
	dropWindowRetryImages(req)
	if len(req.Images) != 0 {
		t.Fatal("a screenshot must leave the retry, or the next call overflows again")
	}
}

func TestIsWindowOverflowError(t *testing.T) {
	if !isWindowOverflowError(&llmadapter.Error{Code: "CONTEXT_WINDOW_EXCEEDED"}) {
		t.Fatal("window code")
	}
	if !isWindowOverflowError(&llmadapter.Error{Code: "REQUEST_TOO_LARGE"}) {
		t.Fatal("too large")
	}
	if !isWindowOverflowError(&llmadapter.Error{Code: "HTTP_413", HTTPStatus: 413}) {
		t.Fatal("http 413")
	}
	if isWindowOverflowError(&llmadapter.Error{Code: "RESPONSE_TRUNCATED"}) {
		t.Fatal("length finish must continue the write, not shrink history")
	}
	if err := chatModelFinishError(llmadapter.FinishReasonWindow); !isWindowOverflowError(err) {
		t.Fatal("window finish must retry")
	}
	if err := chatModelFinishError(llmadapter.FinishReasonLength); !isReplyTruncatedError(err) {
		t.Fatal("length finish must be a truncated reply")
	}
	if isWindowOverflowError(&llmadapter.Error{Code: "RESPONSE_FILTERED"}) {
		t.Fatal("content filter is not a window retry")
	}
	if !isWindowOverflowError(agentrun.ErrContextWindow) {
		t.Fatal("admission window rejection must compact and retry")
	}
}

func TestShrinkMessagesForWindowRetryKeepsSystemAndTail(t *testing.T) {
	req := &llmadapter.Request{Messages: []llmadapter.Message{
		{Role: llmadapter.RoleSystem, Content: "rules"},
		{Role: llmadapter.RoleUser, Content: "old-1"},
		{Role: llmadapter.RoleAssistant, Content: "old-2"},
		{Role: llmadapter.RoleUser, Content: "old-3"},
		{Role: llmadapter.RoleAssistant, Content: "old-4"},
		{Role: llmadapter.RoleUser, Content: "old-5"},
		{Role: llmadapter.RoleAssistant, Content: "old-6"},
		{Role: llmadapter.RoleUser, Content: "old-7"},
		{Role: llmadapter.RoleAssistant, Content: "old-8"},
		{Role: llmadapter.RoleUser, Content: "current"},
	}}
	shrinkMessagesForWindowRetry(req)
	if req.Messages[0].Content != "rules" {
		t.Fatal("lost system")
	}
	if !strings.Contains(req.Messages[1].Content, "检查点") {
		t.Fatal("missing checkpoint note")
	}
	if req.Messages[2].Content == "old-1" {
		t.Fatal("should drop the oldest user turn")
	}
	last := req.Messages[len(req.Messages)-1]
	if last.Role != llmadapter.RoleUser || last.Content != "current" {
		t.Fatalf("lost current user: %+v", last)
	}
}

func TestApplyWindowRetryMessagesUsesCheckpointSummary(t *testing.T) {
	req := &llmadapter.Request{Messages: []llmadapter.Message{
		{Role: llmadapter.RoleSystem, Content: "rules"},
		{Role: llmadapter.RoleUser, Content: "old-1"},
		{Role: llmadapter.RoleAssistant, Content: "old-2"},
		{Role: llmadapter.RoleUser, Content: "current"},
	}}
	applyWindowRetryMessages(req, "先前约定：继续写周报")
	if req.Messages[0].Content != "rules" {
		t.Fatal("lost system")
	}
	if !strings.Contains(req.Messages[1].Content, "先前约定：继续写周报") {
		t.Fatalf("missing checkpoint summary: %+v", req.Messages)
	}
	last := req.Messages[len(req.Messages)-1]
	if last.Role != llmadapter.RoleUser || last.Content != "current" {
		t.Fatalf("lost current user: %+v", last)
	}
}

func TestWindowRetryShrinksHarderOnLaterAttempts(t *testing.T) {
	msgs := []llmadapter.Message{{Role: llmadapter.RoleSystem, Content: "rules"}}
	for i := 0; i < 10; i++ {
		msgs = append(msgs, llmadapter.Message{Role: llmadapter.RoleUser, Content: "u"})
		msgs = append(msgs, llmadapter.Message{Role: llmadapter.RoleAssistant, Content: strings.Repeat("很长", 3000)})
	}
	msgs = append(msgs, llmadapter.Message{Role: llmadapter.RoleUser, Content: "current"})
	req := &llmadapter.Request{Messages: msgs}
	applyWindowRetryMessagesKeep(req, "摘要", windowRetryKeep(3))
	clipWindowRetryPayloads(req, 3)
	nonSystem := 0
	for _, m := range req.Messages {
		if m.Role != llmadapter.RoleSystem {
			nonSystem++
		}
		if m.Role == llmadapter.RoleAssistant && len([]rune(m.Content)) > 1600 {
			t.Fatalf("assistant payload not clipped: %d", len([]rune(m.Content)))
		}
	}
	if nonSystem != 2 {
		t.Fatalf("third retry kept %d non-system messages", nonSystem)
	}
	if req.Messages[len(req.Messages)-1].Content != "current" {
		t.Fatal("lost the current request")
	}
}

func TestProviderModelContextWindow(t *testing.T) {
	p := provider.Provider{Models: []provider.Model{{ModelID: "glm-5.3", ContextWindow: 1000000}, {ModelID: "other", ContextWindow: 32000}}}
	if window, explicit := providerModelContextWindow(p, "glm-5.3"); window != 1000000 || !explicit {
		t.Fatalf("configured window = %d explicit=%v", window, explicit)
	}
	if window, explicit := providerModelContextWindow(p, "missing"); window != 128000 || explicit {
		t.Fatalf("fallback window = %d explicit=%v", window, explicit)
	}
}

func TestFoldKeepsOpenPageAndWrittenTextOnTheCard(t *testing.T) {
	path := `E:\Lunitide-Project\poc\it-crm\index.html`
	msgs := []llmadapter.Message{{
		Role:    llmadapter.RoleAssistant,
		Content: "已写内容如下\n水象段落尾标",
	}}
	for i := 0; i < 12; i++ {
		msgs = append(msgs, llmadapter.Message{Role: llmadapter.RoleAssistant, Content: strings.Repeat("很长的旧正文", 800)})
	}
	msgs = append(msgs, llmadapter.Message{Role: llmadapter.RoleUser, Content: "帮我新增一个商机\n\n" + openPageFileMark + path + "\n" + openPageFileInstruction})
	card := taskHandoffCard(msgs)
	if !strings.Contains(card, path) || !strings.Contains(card, "水象段落尾标") || !strings.Contains(card, "任务卡") {
		t.Fatalf("task card dropped the page or the writing: %s", card)
	}
}

func TestOpenPageReadKeepsTheTailWhenTheFileIsLong(t *testing.T) {
	path := `E:\Lunitide-Project\poc\it-crm\index.html`
	user := "帮我新增一个商机\n\n" + openPageFileMark + path + "\n" + openPageFileInstruction
	body := "头标记\n" + strings.Repeat("行", 5000) + "\n尾标记商机"
	req := &llmadapter.Request{Messages: []llmadapter.Message{
		{Role: llmadapter.RoleUser, Content: user},
		{Role: llmadapter.RoleTool, Content: body},
	}}
	fitModelRequest(req, 128000)
	got := ""
	for _, m := range req.Messages {
		if m.Role == llmadapter.RoleTool {
			got = m.Content
		}
	}
	if !strings.Contains(got, "头标记") || !strings.Contains(got, "尾标记商机") {
		t.Fatalf("the page being edited lost an end: %s", got)
	}
}

func TestOlderOpenPageReadKeepsTheTailAfterAFailedEdit(t *testing.T) {
	path := `E:\Lunitide-Project\poc\it-crm\index.html`
	user := "帮我新增一个商机\n\n" + openPageFileMark + path + "\n" + openPageFileInstruction
	body := "头标记\n" + strings.Repeat("行", 5000) + "\n尾标记商机"
	req := &llmadapter.Request{Messages: []llmadapter.Message{
		{Role: llmadapter.RoleUser, Content: user},
		{Role: llmadapter.RoleTool, Content: body},
		{Role: llmadapter.RoleTool, Content: "ok:false\nworkspace.edit failed"},
	}}
	fitModelRequest(req, 128000)
	got := req.Messages[1].Content
	if !strings.Contains(got, "头标记") || !strings.Contains(got, "尾标记商机") {
		t.Fatalf("the earlier page read lost an end: %s", got)
	}
}

func TestUnderBudgetKeepsTheThread(t *testing.T) {
	req := &llmadapter.Request{Messages: []llmadapter.Message{
		{Role: llmadapter.RoleAssistant, Content: "旧自测记录-早"},
		{Role: llmadapter.RoleAssistant, Content: "第二段还在"},
		{Role: llmadapter.RoleUser, Content: "把这份周报写完"},
	}}
	for i := 0; i < 14; i++ {
		req.Messages = append(req.Messages, llmadapter.Message{Role: llmadapter.RoleAssistant, Content: "近期一句"})
	}
	fitModelRequest(req, 128000)
	joined := ""
	for _, m := range req.Messages {
		joined += m.Content
	}
	if !strings.Contains(joined, "旧自测记录-早") || !strings.Contains(joined, "把这份周报写完") {
		t.Fatalf("short thread was folded before the window: %s", joined)
	}
}

func TestOverBudgetFoldsToATaskCard(t *testing.T) {
	msgs := []llmadapter.Message{{Role: llmadapter.RoleAssistant, Content: "旧自测记录-早 " + strings.Repeat("页", 4000)}}
	msgs = append(msgs, llmadapter.Message{
		Role: llmadapter.RoleAssistant,
		ToolCalls: []llmadapter.ToolCall{{
			Name:      "todo.write",
			Arguments: []byte(`{"todos":[{"content":"写入东航商机","status":"in_progress"}]}`),
		}},
	})
	msgs = append(msgs, llmadapter.Message{Role: llmadapter.RoleTool, Content: "edited index.html (1 replacement)"})
	for i := 0; i < 10; i++ {
		msgs = append(msgs, llmadapter.Message{Role: llmadapter.RoleAssistant, Content: strings.Repeat("很长的旧正文", 800)})
	}
	msgs = append(msgs, llmadapter.Message{Role: llmadapter.RoleUser, Content: "把这份周报写完"})
	req := &llmadapter.Request{Messages: msgs}
	fitModelRequest(req, 1000)
	joined := ""
	for _, m := range req.Messages {
		joined += m.Content + "\n"
	}
	if strings.Contains(joined, "旧自测记录-早") || !strings.Contains(joined, "任务卡") || !strings.Contains(joined, "把这份周报写完") || !strings.Contains(joined, "写入东航商机") || !strings.Contains(joined, "index.html") {
		t.Fatalf("over-budget fold lost the task card: %s", joined)
	}
}

func TestCompanionColdMaxMessages(t *testing.T) {
	if companionColdMaxMessages(true) != companionMaxMessages {
		t.Fatal("checkpoint should keep the voice window")
	}
	if companionColdMaxMessages(false) != companionMaxMessages/2 {
		t.Fatal("no checkpoint should tighten")
	}
}
