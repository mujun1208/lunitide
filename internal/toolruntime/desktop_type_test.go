package toolruntime

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/lunitide/lunitide/internal/ccapp"
)

func TestPickSendAndNamedEdit(t *testing.T) {
	if !isSendControlName("发送") || isSendControlName("发送到微信") {
		t.Fatal("send name filter")
	}
	nodes := []mediaUINode{
		{Role: "button", Name: "取消", Y: 10},
		{Role: "button", Name: "发送", Y: 400},
		{Role: "edit", Name: "证件号码", Y: 80, H: 24},
	}
	if got := pickSendControl(nodes); got == nil || got.Name != "发送" {
		t.Fatalf("send %+v", got)
	}
	if got := pickNamedEdit(nodes, "证件号码"); got == nil || got.Name != "证件号码" {
		t.Fatalf("edit %+v", got)
	}
	if pickNamedEdit(nodes, "不存在的字段") != nil {
		t.Fatal("missing field must fail")
	}
}

func TestWindowCloseIsNotASendControl(t *testing.T) {
	if !isWindowCloseControlName("关闭") || !isWindowCloseControlName("关闭窗口") {
		t.Fatal("close names")
	}
	if isSendControlName("关闭") || isSendControlName("关闭窗口") {
		t.Fatal("close must not count as send")
	}
	nodes := []mediaUINode{
		{Role: "button", Name: "关闭", Y: 8, W: 28, H: 28},
		{Role: "button", Name: "发送", Y: 400, W: 64, H: 28},
	}
	if got := pickSendControl(nodes); got == nil || got.Name != "发送" {
		t.Fatalf("send %+v", got)
	}
}

func TestDesktopTypeFindAfterThenTypeAndSubmit(t *testing.T) {
	mediaSleep = func(time.Duration) {}
	t.Cleanup(func() { mediaSleep = time.Sleep })

	var typed []string
	var keys []string
	invoke := func(_ context.Context, _, tool string, args json.RawMessage, _ bool) (Result, error) {
		switch tool {
		case ccapp.ToolWindowFocus:
			return result("focused"), nil
		case ccapp.ToolObserveUI:
			raw, _ := json.Marshal(map[string]any{"nodes": []mediaUINode{
				{Role: "button", Name: "发送", Y: 500, H: 28},
			}})
			return Result{Output: string(raw)}, nil
		case ccapp.ToolKeyboardType, ccapp.ToolPaste:
			var a struct {
				Text string `json:"text"`
			}
			_ = json.Unmarshal(args, &a)
			typed = append(typed, a.Text)
			return result("ok"), nil
		case ccapp.ToolKeyboardShortcut:
			var a struct {
				Keys []string `json:"keys"`
			}
			_ = json.Unmarshal(args, &a)
			keys = append(keys, strings.Join(a.Keys, "+"))
			return result("ok"), nil
		default:
			return result("ok"), nil
		}
	}
	payload, _ := json.Marshal(map[string]any{
		"text": "330102199001011234", "after": "证件号码", "submit": true, "window": "协议",
	})
	_, err := executeDesktopType(context.Background(), invoke, "s1", payload, true, true)
	if err == nil || !strings.Contains(err.Error(), "无法执行") {
		t.Fatalf("Ctrl+F must not count as success, got %v", err)
	}
	if len(typed) != 0 {
		t.Fatalf("must not type into Find: %v", typed)
	}
	if strings.Contains(strings.Join(keys, ","), "ctrl+f") {
		t.Fatalf("must not press Ctrl+F: %v", keys)
	}
}

func TestDesktopTypeNamedEditThenSubmit(t *testing.T) {
	mediaSleep = func(time.Duration) {}
	t.Cleanup(func() { mediaSleep = time.Sleep })

	var typed []string
	var clicked []string
	field := ""
	invoke := func(_ context.Context, _, tool string, args json.RawMessage, _ bool) (Result, error) {
		switch tool {
		case ccapp.ToolObserveUI:
			raw, _ := json.Marshal(map[string]any{"nodes": []mediaUINode{
				{Role: "edit", Name: "证件号码", Value: field, Y: 80, H: 24},
				{Role: "button", Name: "发送", Y: 500, H: 28},
			}})
			return Result{Output: string(raw)}, nil
		case ccapp.ToolKeyboardType, ccapp.ToolPaste:
			var a struct {
				Text string `json:"text"`
			}
			_ = json.Unmarshal(args, &a)
			typed = append(typed, a.Text)
			field = a.Text
			return result("ok"), nil
		case ccapp.ToolMouseClick:
			var a struct {
				Name string `json:"name"`
			}
			_ = json.Unmarshal(args, &a)
			clicked = append(clicked, a.Name)
			return result("ok"), nil
		default:
			return result("ok"), nil
		}
	}
	payload, _ := json.Marshal(map[string]any{
		"text": "330102199001011234", "after": "证件号码", "submit": true,
	})
	res, err := executeDesktopType(context.Background(), invoke, "s1", payload, true, true)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(res.Output, `typed "330102199001011234" after "证件号码"`) || !strings.Contains(res.Output, "submitted") {
		t.Fatalf("output %q", res.Output)
	}
	if !strings.Contains(res.Output, `"l0"`) || !strings.Contains(res.Output, `"kind":"field"`) {
		t.Fatalf("desktop.type success must attach field l0: %q", res.Output)
	}
	if len(typed) == 0 || typed[len(typed)-1] != "330102199001011234" {
		t.Fatalf("typed %v", typed)
	}
	if len(clicked) == 0 || clicked[len(clicked)-1] != "发送" {
		t.Fatalf("clicked %v", clicked)
	}
}

func TestDesktopTypeRejectsUnverifiedWrite(t *testing.T) {
	mediaSleep = func(time.Duration) {}
	t.Cleanup(func() { mediaSleep = time.Sleep })

	var typed []string
	invoke := func(_ context.Context, _, tool string, args json.RawMessage, _ bool) (Result, error) {
		switch tool {
		case ccapp.ToolObserveUI:
			raw, _ := json.Marshal(map[string]any{"nodes": []mediaUINode{
				{Role: "edit", Name: "证件号码", Y: 80, H: 24},
			}})
			return Result{Output: string(raw)}, nil
		case ccapp.ToolKeyboardType, ccapp.ToolPaste:
			var a struct {
				Text string `json:"text"`
			}
			_ = json.Unmarshal(args, &a)
			typed = append(typed, a.Text)
			return result("ok"), nil
		default:
			return result("ok"), nil
		}
	}
	payload, _ := json.Marshal(map[string]any{"text": "204040", "after": "证件号码"})
	_, err := executeDesktopType(context.Background(), invoke, "s1", payload, true, true)
	if err == nil || !strings.Contains(err.Error(), "不能确认已写入") {
		t.Fatalf("unverified write must fail, got %v", err)
	}
	if len(typed) != 1 || typed[0] != "204040" {
		t.Fatalf("typed %v", typed)
	}
}

func TestDesktopTypeFindAfterUsesIdCardAlias(t *testing.T) {
	if got := documentLabelSearchTerm("证件号码"); got != "身份证号码" {
		t.Fatalf("search term %q", got)
	}
	if !labelsMatch("证件号码", "身份证号码：") {
		t.Fatal("label alias match")
	}
}

func TestPickDocumentLabelMatchesIDCardAliases(t *testing.T) {
	nodes := []mediaUINode{
		{Role: "text", Name: "身份证号码：", Y: 120, H: 18, W: 80},
	}
	if got := pickDocumentLabel(nodes, "证件号码"); got == nil || got.Name != "身份证号码：" {
		t.Fatalf("label %+v", got)
	}
}

func TestDesktopTypeFindAfterIDCardNumber(t *testing.T) {
	mediaSleep = func(time.Duration) {}
	t.Cleanup(func() { mediaSleep = time.Sleep })

	var typed []string
	invoke := func(_ context.Context, _, tool string, args json.RawMessage, _ bool) (Result, error) {
		switch tool {
		case ccapp.ToolWindowFocus:
			return result("focused"), nil
		case ccapp.ToolObserveUI:
			return Result{Output: `{"nodes":[]}`}, nil
		case ccapp.ToolKeyboardType, ccapp.ToolPaste:
			var a struct {
				Text string `json:"text"`
			}
			_ = json.Unmarshal(args, &a)
			typed = append(typed, a.Text)
			return result("ok"), nil
		default:
			return result("ok"), nil
		}
	}
	payload, _ := json.Marshal(map[string]any{
		"text": "204040", "after": "身份证号码", "window": "协议",
	})
	_, err := executeDesktopType(context.Background(), invoke, "s1", payload, true, true)
	if err == nil || !strings.Contains(err.Error(), "无法执行") {
		t.Fatalf("empty UIA tree must fail, got %v", err)
	}
	if len(typed) != 0 {
		t.Fatalf("must not type: %v", typed)
	}
}

func TestDesktopTypeFailsLoudlyWithoutCC(t *testing.T) {
	payload, _ := json.Marshal(map[string]any{"text": "hello"})
	_, err := executeDesktopType(context.Background(), nil, "s1", payload, true, true)
	if err == nil || !strings.Contains(err.Error(), "无法执行") {
		t.Fatalf("got %v", err)
	}
	_, err = executeDesktopType(context.Background(), func(context.Context, string, string, json.RawMessage, bool) (Result, error) {
		return result("ok"), nil
	}, "s1", payload, false, true)
	if err == nil || !strings.Contains(err.Error(), "无法执行") {
		t.Fatalf("unapproved %v", err)
	}
}
