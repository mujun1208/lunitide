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

func TestDesktopTypeFindAfterThenTypeAndSubmit(t *testing.T) {
	mediaSleep = func(time.Duration) {}
	t.Cleanup(func() { mediaSleep = time.Sleep })

	var typed []string
	var keys []string
	var clicked []string
	invoke := func(_ context.Context, _, tool string, args json.RawMessage, _ bool) (Result, error) {
		switch tool {
		case ccapp.ToolWindowFocus:
			return result("focused"), nil
		case ccapp.ToolObserveUI:
			raw, _ := json.Marshal(map[string]any{"nodes": []mediaUINode{
				{Role: "button", Name: "发送", Y: 500, H: 28},
			}})
			return Result{Output: string(raw)}, nil
		case ccapp.ToolKeyboardType:
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
	payload, _ := json.Marshal(map[string]any{
		"text": "330102199001011234", "after": "证件号码", "submit": true, "window": "协议",
	})
	res, err := executeDesktopType(context.Background(), invoke, "s1", payload, true, true)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(res.Output, "typed after") || !strings.Contains(res.Output, "submitted") {
		t.Fatalf("output %q", res.Output)
	}
	if len(typed) < 2 || typed[0] != "证件号码" || typed[1] != "330102199001011234" {
		t.Fatalf("typed %v", typed)
	}
	joined := strings.Join(keys, ",")
	if !strings.Contains(joined, "ctrl+f") || !strings.Contains(joined, "enter") {
		t.Fatalf("keys %v", keys)
	}
	if len(clicked) == 0 || clicked[len(clicked)-1] != "发送" {
		t.Fatalf("clicked %v", clicked)
	}
}

func TestDesktopTypeNamedEditThenSubmit(t *testing.T) {
	mediaSleep = func(time.Duration) {}
	t.Cleanup(func() { mediaSleep = time.Sleep })

	var typed []string
	var clicked []string
	invoke := func(_ context.Context, _, tool string, args json.RawMessage, _ bool) (Result, error) {
		switch tool {
		case ccapp.ToolObserveUI:
			raw, _ := json.Marshal(map[string]any{"nodes": []mediaUINode{
				{Role: "edit", Name: "证件号码", Y: 80, H: 24},
				{Role: "button", Name: "发送", Y: 500, H: 28},
			}})
			return Result{Output: string(raw)}, nil
		case ccapp.ToolKeyboardType:
			var a struct {
				Text string `json:"text"`
			}
			_ = json.Unmarshal(args, &a)
			typed = append(typed, a.Text)
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
	if !strings.Contains(res.Output, "submitted") {
		t.Fatalf("output %q", res.Output)
	}
	if len(typed) == 0 || typed[len(typed)-1] != "330102199001011234" {
		t.Fatalf("typed %v", typed)
	}
	if len(clicked) == 0 || clicked[len(clicked)-1] != "发送" {
		t.Fatalf("clicked %v", clicked)
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
	}, "s1", payload, true, false)
	if err == nil || !strings.Contains(err.Error(), "无法执行") {
		t.Fatalf("confined %v", err)
	}
}
