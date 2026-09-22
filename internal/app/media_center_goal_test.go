package app

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/lunitide/lunitide/internal/llmadapter"
)

func TestOwnedMediaCenterRewritesPlaybackAndBlocksOtherTools(t *testing.T) {
	goal := "帮我从网上找个电影，再我的媒体中心播放"
	raw := mediaArgsForGoal(goal, json.RawMessage(`{"target":"foreground","app":"汽水音乐","query":""}`))
	var fields map[string]any
	if err := json.Unmarshal(raw, &fields); err != nil {
		t.Fatal(err)
	}
	if fields["target"] != "center" || fields["app"] != nil {
		t.Fatalf("%s", raw)
	}
	if err := guardCurrentTurnTool(goal, "web.fetch"); err == nil {
		t.Fatal("web.fetch must not run for the owned media center")
	}
	if err := guardCurrentTurnTool(goal, "computer.act"); err == nil {
		t.Fatal("computer.act must not hunt for another player")
	}
	if err := guardCurrentTurnTool(goal, "media.play"); err != nil {
		t.Fatal(err)
	}
	messages := []llmadapter.Message{
		{Role: llmadapter.RoleUser, Content: goal},
		{Role: llmadapter.RoleAssistant, ToolCalls: []llmadapter.ToolCall{{ID: "current", Name: "media.play", Arguments: raw}}},
		{Role: llmadapter.RoleTool, ToolCallID: "current", Content: "已交给媒体中心播放。\nMEDIA_CENTER\nurl: https://archive.org/download/night/Night.mp4\nkind: video\ntitle: Night\n"},
	}
	if got := computerReceiptCloseout(messages, goal); got != "已经在媒体中心开始播放。" {
		t.Fatal(got)
	}
	if err := guardCurrentTurnToolHistory(goal, "web.search", messages); err == nil || !strings.Contains(err.Error(), "已经开始播放") {
		t.Fatal(err)
	}
}
