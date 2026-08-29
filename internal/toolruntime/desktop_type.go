package toolruntime

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/lunitide/lunitide/internal/ccapp"
)

func isSendControlName(name string) bool {
	n := foldMedia(name)
	if n == "" {
		return false
	}
	if strings.Contains(n, "发送到") || strings.Contains(n, "发送邮件") {
		return false
	}
	switch n {
	case "发送", "send", "submit", "确定", "确认", "提交", "完成", "ok", "yes":
		return true
	}
	return strings.HasPrefix(n, "发送") || n == "send message"
}

func pickSendControl(nodes []mediaUINode) *mediaUINode {
	var best *mediaUINode
	bestScore := 0
	for i := range nodes {
		n := &nodes[i]
		if !isSendControlName(n.Name) {
			continue
		}
		role := strings.ToLower(n.Role)
		if role != "" && role != "button" && role != "menuitem" && role != "link" {
			continue
		}
		score := 40
		if foldMedia(n.Name) == "发送" || foldMedia(n.Name) == "send" {
			score = 80
		}
		if n.Y >= 0 {
			score += n.Y / 200
		}
		if score >= bestScore {
			bestScore = score
			best = n
		}
	}
	return best
}

func pickNamedEdit(nodes []mediaUINode, want string) *mediaUINode {
	want = strings.TrimSpace(want)
	if want == "" {
		return nil
	}
	var best *mediaUINode
	bestScore := 0
	for i := range nodes {
		n := &nodes[i]
		role := strings.ToLower(strings.TrimSpace(n.Role))
		if role != "edit" && role != "textbox" && role != "combobox" {
			continue
		}
		score := mediaNameScore(n.Name, want)
		if score > bestScore {
			bestScore = score
			best = n
		}
	}
	if bestScore < 40 {
		return nil
	}
	return best
}

func nodeIsLabelText(name, want string) bool {
	g, w := foldMedia(name), foldMedia(want)
	if g == "" || w == "" {
		return false
	}
	g = strings.TrimRight(g, "：:")
	w = strings.TrimRight(w, "：:")
	return g == w
}

func pickDocumentLabel(nodes []mediaUINode, want string) *mediaUINode {
	want = strings.TrimSpace(want)
	if want == "" {
		return nil
	}
	var best *mediaUINode
	bestScore := 0
	for i := range nodes {
		n := &nodes[i]
		role := strings.ToLower(strings.TrimSpace(n.Role))
		if role == "button" || role == "menuitem" || role == "link" {
			continue
		}
		score := mediaNameScore(n.Name, want)
		if nodeIsLabelText(n.Name, want) {
			score += 20
		}
		if score > bestScore {
			bestScore = score
			best = n
		}
	}
	if bestScore < 40 {
		return nil
	}
	return best
}

func executeDesktopType(ctx context.Context, invoke ccInvoker, session string, args json.RawMessage, approved, unconfined bool) (Result, error) {
	var a struct {
		Text   string `json:"text"`
		Window string `json:"window"`
		After  string `json:"after"`
		Submit bool   `json:"submit"`
	}
	if strict(args, &a) != nil {
		return Result{}, errors.New("无法执行：参数无效")
	}
	text := strings.TrimSpace(a.Text)
	if text == "" || utf8.RuneCountInString(text) > 4096 {
		return Result{}, errors.New("无法执行：没有可输入的文字")
	}
	if invoke == nil {
		return Result{}, errors.New("无法执行：电脑控制未开启")
	}
	if !unconfined {
		return Result{}, errors.New("无法执行：需要完整磁盘访问才能在文档里输入")
	}

	window := strings.TrimSpace(a.Window)
	after := strings.TrimSpace(a.After)
	prevWin := mediaInputWindow
	if window != "" {
		mediaInputWindow = window
	}
	defer func() { mediaInputWindow = prevWin }()

	if window != "" {
		if _, err := ccCall(ctx, invoke, session, ccapp.ToolWindowFocus, map[string]any{"title": window}, approved); err != nil {
			return Result{}, fmt.Errorf("无法执行：没能聚焦窗口「%s」", window)
		}
		mediaSleep(280 * time.Millisecond)
	}

	if after != "" {
		nodes, _, _ := ccObserveNodes(ctx, invoke, session, approved)
		if field := pickNamedEdit(nodes, after); field != nil {
			target := clipMediaName(field.Name)
			_ = ccClickName(ctx, invoke, session, target, 1, approved)
			mediaSleep(80 * time.Millisecond)
			if err := ccType(ctx, invoke, session, text, approved); err != nil {
				return Result{}, fmt.Errorf("无法执行：无法在「%s」后输入（%v）", after, err)
			}
		} else if label := pickDocumentLabel(nodes, after); label != nil && (label.W > 0 || label.H > 0) {
			x := label.X + label.W + 4
			y := label.Y + label.H/2
			if y == 0 {
				y = label.Y
			}
			if err := ccClickXY(ctx, invoke, session, x, y, 1, approved); err != nil {
				return Result{}, fmt.Errorf("无法执行：找不到「%s」", after)
			}
			mediaSleep(80 * time.Millisecond)
			_ = ccPress(ctx, invoke, session, "end", approved)
			if err := ccType(ctx, invoke, session, text, approved); err != nil {
				return Result{}, fmt.Errorf("无法执行：无法在「%s」后输入（%v）", after, err)
			}
		} else if label := pickDocumentLabel(nodes, after); label != nil {
			_ = ccClickName(ctx, invoke, session, clipMediaName(label.Name), 1, approved)
			mediaSleep(80 * time.Millisecond)
			_ = ccPress(ctx, invoke, session, "end", approved)
			_ = ccPress(ctx, invoke, session, "right", approved)
			if err := ccType(ctx, invoke, session, text, approved); err != nil {
				return Result{}, fmt.Errorf("无法执行：无法在「%s」后输入（%v）", after, err)
			}
		} else {
			_ = ccShortcut(ctx, invoke, session, approved, "ctrl", "f")
			mediaSleep(220 * time.Millisecond)
			if err := ccType(ctx, invoke, session, after, approved); err != nil {
				return Result{}, fmt.Errorf("无法执行：找不到「%s」", after)
			}
			mediaSleep(80 * time.Millisecond)
			if err := ccPress(ctx, invoke, session, "enter", approved); err != nil {
				return Result{}, fmt.Errorf("无法执行：找不到「%s」", after)
			}
			mediaSleep(180 * time.Millisecond)
			_ = ccPress(ctx, invoke, session, "esc", approved)
			mediaSleep(80 * time.Millisecond)
			_ = ccPress(ctx, invoke, session, "end", approved)
			_ = ccPress(ctx, invoke, session, "right", approved)
			if err := ccType(ctx, invoke, session, text, approved); err != nil {
				return Result{}, fmt.Errorf("无法执行：无法在「%s」后输入（%v）", after, err)
			}
		}
	} else if err := ccType(ctx, invoke, session, text, approved); err != nil {
		return Result{}, fmt.Errorf("无法执行：无法输入文字（%v）", err)
	}

	if a.Submit {
		mediaSleep(120 * time.Millisecond)
		nodes, _, _ := ccObserveNodes(ctx, invoke, session, approved)
		if send := pickSendControl(nodes); send != nil {
			if err := ccClickName(ctx, invoke, session, clipMediaName(send.Name), 1, approved); err != nil {
				if pressErr := ccPress(ctx, invoke, session, "enter", approved); pressErr != nil {
					return Result{}, fmt.Errorf("无法执行：已输入但没能发送（%v）", err)
				}
			}
		} else if err := ccPress(ctx, invoke, session, "enter", approved); err != nil {
			return Result{}, fmt.Errorf("无法执行：已输入但没能发送（%v）", err)
		}
	}

	how := "typed"
	if after != "" {
		how = fmt.Sprintf("typed after %q", after)
	}
	if a.Submit {
		how += " and submitted"
	}
	if window != "" {
		how += " in " + window
	}
	return result(how), nil
}
