package app

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/lunitide/lunitide/internal/mcp6"
)

func browserToolForEndpoint(tools []mcp6.ReadyTool, endpointID, op string) (mcp6.ReadyTool, bool) {
	for _, candidate := range playwrightToolNames(op) {
		for _, entry := range tools {
			if entry.EndpointID != endpointID {
				continue
			}
			name := strings.ToLower(entry.Tool)
			if name == candidate || strings.HasSuffix(name, "_"+candidate) {
				return entry, true
			}
		}
	}
	return mcp6.ReadyTool{}, false
}

func browserEndpointScore(tools []mcp6.ReadyTool, endpoint string) int {
	score := 0
	for _, op := range []string{"navigate", "snapshot", "click", "type", "select", "press", "tabs", "back"} {
		if _, ok := browserToolForEndpoint(tools, endpoint, op); ok {
			score++
		}
	}
	return score
}

// Installed versions differ: classic Playwright uses ref, current releases
// advertise target, and some compatible servers advertise selector. Follow
// the admitted schema instead of sending all three (strict servers reject it).
func playwrightArgsForSchema(call browserActCall, schema json.RawMessage) (map[string]any, bool) {
	args, skip := playwrightArgs(call)
	if skip {
		return nil, true
	}
	var doc struct {
		Properties map[string]json.RawMessage `json:"properties"`
		Required   []string                   `json:"required"`
	}
	if json.Unmarshal(schema, &doc) != nil || len(doc.Properties) == 0 {
		return args, false
	}
	if ref, exists := args["ref"]; exists {
		delete(args, "ref")
		key := ""
		for _, candidate := range []string{"target", "ref", "selector"} {
			if _, ok := doc.Properties[candidate]; ok {
				key = candidate
				break
			}
		}
		if key == "" {
			return nil, true
		}
		args[key] = ref
	}
	for name := range args {
		if _, ok := doc.Properties[name]; !ok {
			delete(args, name)
		}
	}
	for _, required := range doc.Required {
		if _, ok := args[required]; !ok {
			return nil, true
		}
	}
	return args, false
}

func awaitBrowserReady(ctx context.Context, timeout, interval time.Duration, ready func() bool) bool {
	if ctx.Err() != nil {
		return false
	}
	if ready() {
		return true
	}
	deadline := time.NewTimer(timeout)
	defer deadline.Stop()
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return false
		case <-deadline.C:
			return false
		case <-ticker.C:
			if ready() {
				return true
			}
		}
	}
}

func (e *Engine) playwrightSnapshotForEndpoint(ctx context.Context, endpointID string) string {
	if e == nil || e.mcp6Registry == nil {
		return ""
	}
	entry, ok := browserToolForEndpoint(e.mcp6Registry.ReadyToolSnapshot(), endpointID, "snapshot")
	if !ok {
		return ""
	}
	out, err := e.invokeMcpTool(ctx, receiptSession(ctx, ""), endpointID, entry.Tool, json.RawMessage(`{}`))
	if err != nil || browserMCPResultError(out) != nil {
		return ""
	}
	return out
}

func browserMCPResultError(raw string) error {
	if value := strings.TrimSpace(raw); value == "" || value == "{}" || value == "null" {
		return fmt.Errorf("BROWSER_ACT_FAILED: 浏览器没有返回可核对的操作结果")
	}
	var result struct {
		IsError bool     `json:"isError"`
		Text    string   `json:"text"`
		Texts   []string `json:"texts"`
		Content []struct {
			Text string `json:"text"`
		} `json:"content"`
	}
	if json.Unmarshal([]byte(raw), &result) != nil || !result.IsError {
		return nil
	}
	detail := result.Text
	if detail == "" && len(result.Texts) > 0 {
		detail = result.Texts[0]
	}
	if detail == "" {
		for _, block := range result.Content {
			if block.Text != "" {
				detail = block.Text
				break
			}
		}
	}
	if strings.TrimSpace(detail) == "" {
		detail = "浏览器工具返回执行失败，未确认操作成功"
	}
	return fmt.Errorf("BROWSER_ACT_FAILED: %s", truncateUTF8Bytes(strings.TrimSpace(detail), 512))
}
