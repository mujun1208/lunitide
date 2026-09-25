package toolruntime

import (
	"context"
	"encoding/json"
	"fmt"
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

func TestDesktopTypeWeChatSearchesContactThenSends(t *testing.T) {
	mediaSleep = func(time.Duration) {}
	origRead := readChatImage
	readChatImage = func(context.Context, []byte) (string, error) { return "怎么这么晚还不睡", nil }
	t.Cleanup(func() {
		mediaSleep = time.Sleep
		readChatImage = origRead
	})

	var steps []string
	invoke := func(_ context.Context, _, tool string, args json.RawMessage, _ bool) (Result, error) {
		switch tool {
		case ccapp.ToolWindowFocus:
			steps = append(steps, "focus")
		case ccapp.ToolKeyboardShortcut:
			var a struct {
				Keys []string `json:"keys"`
			}
			_ = json.Unmarshal(args, &a)
			steps = append(steps, strings.Join(a.Keys, "+"))
		case ccapp.ToolPaste, ccapp.ToolKeyboardType:
			var a struct {
				Text string `json:"text"`
			}
			_ = json.Unmarshal(args, &a)
			steps = append(steps, "type:"+a.Text)
		case ccapp.ToolPress:
			var a struct {
				Key string `json:"key"`
			}
			_ = json.Unmarshal(args, &a)
			steps = append(steps, "key:"+a.Key)
		case ccapp.ToolObserveUI:
			raw, _ := json.Marshal(map[string]any{"nodes": []mediaUINode{
				{Role: "text", Name: "文件传输助手", Value: "你好"},
				{Role: "text", Name: "在的", Y: 200},
			}})
			return Result{Output: string(raw)}, nil
		case ccapp.ToolScreenCapture:
			return Result{Output: "captured", VisionMIME: "image/png", VisionData: []byte("png")}, nil
		}
		return result("ok"), nil
	}
	payload, _ := json.Marshal(map[string]any{
		"text": "你好", "after": "文件传输助手", "submit": true, "window": "微信",
	})
	out, err := executeDesktopType(context.Background(), invoke, "s1", payload, true, true)
	if err != nil {
		t.Fatal(err)
	}
	got := strings.Join(steps, ",")
	want := "focus,ctrl+f,type:文件传输助手,key:enter,type:你好,key:enter"
	if got != want {
		t.Fatalf("steps %s", got)
	}
	if !strings.Contains(out.Output, "文件传输助手") || !strings.Contains(out.Output, "你好") || !strings.Contains(out.Output, "在的") || !strings.Contains(out.Output, "怎么这么晚还不睡") {
		t.Fatal(out.Output)
	}
	if strings.Contains(out.Output, "供应商拒绝") {
		t.Fatal(out.Output)
	}
	if string(out.VisionData) != "png" {
		t.Fatal("wechat send must return the chat screenshot")
	}
}

func TestDesktopTypeDocumentSavePressesCtrlS(t *testing.T) {
	mediaSleep = func(time.Duration) {}
	t.Cleanup(func() { mediaSleep = time.Sleep })
	field := ""
	var keys []string
	invoke := func(_ context.Context, _, tool string, args json.RawMessage, _ bool) (Result, error) {
		switch tool {
		case ccapp.ToolObserveUI:
			raw, _ := json.Marshal(map[string]any{"nodes": []mediaUINode{
				{Role: "edit", Name: "证件号码", Value: field, Y: 80, H: 24},
			}})
			return Result{Output: string(raw)}, nil
		case ccapp.ToolPaste, ccapp.ToolKeyboardType:
			var a struct {
				Text string `json:"text"`
			}
			_ = json.Unmarshal(args, &a)
			field = a.Text
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
	payload, _ := json.Marshal(map[string]any{"text": "204040", "after": "证件号码", "window": "协议", "save": true})
	res, err := executeDesktopType(context.Background(), invoke, "s1", payload, true, true)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(strings.Join(keys, ","), "ctrl+f") {
		t.Fatalf("document save must not search: %v", keys)
	}
	if !strings.Contains(strings.Join(keys, ","), "ctrl+s") || !strings.Contains(res.Output, "saved") || !strings.Contains(res.Output, "204040") {
		t.Fatalf("keys %v output %s", keys, res.Output)
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

func TestDesktopTypeClicksComposerBeforeBareType(t *testing.T) {
	mediaSleep = func(time.Duration) {}
	t.Cleanup(func() { mediaSleep = time.Sleep })
	var clicked []string
	var keys []string
	typed := ""
	invoke := func(_ context.Context, _, tool string, args json.RawMessage, _ bool) (Result, error) {
		switch tool {
		case ccapp.ToolObserveUI:
			raw, _ := json.Marshal(map[string]any{"nodes": []mediaUINode{
				{Role: "edit", Name: "发消息", Y: 640, H: 36},
				{Role: "button", Name: "发送", Y: 640, H: 36},
			}})
			return Result{Output: string(raw)}, nil
		case ccapp.ToolKeyboardType, ccapp.ToolPaste:
			var a struct {
				Text string `json:"text"`
			}
			_ = json.Unmarshal(args, &a)
			typed = a.Text
			return result("ok"), nil
		case ccapp.ToolPress:
			var a struct {
				Key string `json:"key"`
			}
			_ = json.Unmarshal(args, &a)
			keys = append(keys, a.Key)
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
	payload, _ := json.Marshal(map[string]any{"text": "你好", "window": "豆包", "submit": true})
	res, err := executeDesktopType(context.Background(), invoke, "s1", payload, true, true)
	if err != nil {
		t.Fatal(err)
	}
	if typed != "你好" {
		t.Fatalf("typed %q", typed)
	}
	if len(clicked) == 0 || clicked[0] != "发消息" || strings.Contains(strings.Join(clicked, ","), "发送") {
		t.Fatalf("clicked %v", clicked)
	}
	if len(keys) == 0 || keys[len(keys)-1] != "enter" {
		t.Fatalf("keys %v", keys)
	}
	if !strings.Contains(res.Output, "submitted") {
		t.Fatalf("output %q", res.Output)
	}
}

func TestDesktopTypeRetriesAfterStaleFocus(t *testing.T) {
	mediaSleep = func(time.Duration) {}
	t.Cleanup(func() { mediaSleep = time.Sleep })
	pastes := 0
	var clicks []string
	invoke := func(_ context.Context, _, tool string, args json.RawMessage, _ bool) (Result, error) {
		switch tool {
		case ccapp.ToolObserveUI:
			raw, _ := json.Marshal(map[string]any{"nodes": []mediaUINode{
				{Role: "edit", Name: "发消息", X: 400, Y: 640, W: 480, H: 36},
				{Role: "button", Name: "发送", X: 900, Y: 640, W: 48, H: 36},
			}})
			return Result{Output: string(raw)}, nil
		case ccapp.ToolKeyboardType, ccapp.ToolPaste:
			pastes++
			var a struct {
				Text string `json:"text"`
			}
			_ = json.Unmarshal(args, &a)
			if pastes == 1 {
				return Result{}, fmt.Errorf("无法执行：焦点不在输入框")
			}
			if a.Text != "你好" {
				t.Fatalf("text %q", a.Text)
			}
			return result("ok"), nil
		case ccapp.ToolMouseClick:
			var a struct {
				Name string `json:"name"`
				X    int    `json:"x"`
				Y    int    `json:"y"`
			}
			_ = json.Unmarshal(args, &a)
			if a.Name != "" {
				clicks = append(clicks, a.Name)
			} else {
				clicks = append(clicks, fmt.Sprintf("%d,%d", a.X, a.Y))
			}
			if a.Name == "发消息" {
				return Result{}, fmt.Errorf("name click missed")
			}
			return result("ok"), nil
		default:
			return result("ok"), nil
		}
	}
	payload, _ := json.Marshal(map[string]any{"text": "你好", "window": "豆包", "submit": true})
	res, err := executeDesktopType(context.Background(), invoke, "s1", payload, true, true)
	if err != nil {
		t.Fatal(err)
	}
	if pastes < 2 {
		t.Fatalf("pastes %d clicks %v", pastes, clicks)
	}
	if !strings.Contains(strings.Join(clicks, " "), "640,658") && !strings.Contains(strings.Join(clicks, " "), "640,") {
		t.Fatalf("clicks %v", clicks)
	}
	if !strings.Contains(res.Output, "submitted") {
		t.Fatalf("output %q", res.Output)
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

func TestVerifyDesktopTypedRetriesThenSucceeds(t *testing.T) {
	mediaSleep = func(time.Duration) {}
	t.Cleanup(func() { mediaSleep = time.Sleep })
	observes := 0
	invoke := func(_ context.Context, _, tool string, _ json.RawMessage, _ bool) (Result, error) {
		if tool != ccapp.ToolObserveUI {
			return result("ok"), nil
		}
		observes++
		value := ""
		if observes >= 2 {
			value = "hello"
		}
		raw, _ := json.Marshal(map[string]any{"nodes": []mediaUINode{{Role: "edit", Value: value}}})
		return Result{Output: string(raw)}, nil
	}
	if err := verifyDesktopTyped(context.Background(), invoke, "s1", true, "hello"); err != nil {
		t.Fatal(err)
	}
	if observes != 2 {
		t.Fatalf("first miss then pass should poll twice, got %d", observes)
	}
}

func TestVerifyDesktopTypedFailsAfterThreeMisses(t *testing.T) {
	mediaSleep = func(time.Duration) {}
	t.Cleanup(func() { mediaSleep = time.Sleep })
	observes := 0
	invoke := func(_ context.Context, _, tool string, _ json.RawMessage, _ bool) (Result, error) {
		if tool != ccapp.ToolObserveUI {
			return result("ok"), nil
		}
		observes++
		raw, _ := json.Marshal(map[string]any{"nodes": []mediaUINode{{Role: "edit", Value: ""}}})
		return Result{Output: string(raw)}, nil
	}
	err := verifyDesktopTyped(context.Background(), invoke, "s1", true, "hello")
	if err == nil || !strings.Contains(err.Error(), "无法执行") || !strings.Contains(err.Error(), "不能确认已写入") {
		t.Fatalf("three misses must keep typed-fail copy, got %v", err)
	}
	if observes != 3 {
		t.Fatalf("want 3 polls, got %d", observes)
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
