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
	"github.com/lunitide/lunitide/internal/doctext"
)

func typingFocusMissed(err error) bool {
	if err == nil {
		return false
	}
	return errors.Is(err, ccapp.ErrCcInputFiltered) || strings.Contains(err.Error(), "input rejected") || strings.Contains(err.Error(), "焦点不在输入框")
}

func isSendControlName(name string) bool {
	if isWindowCloseControlName(name) {
		return false
	}
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

func isWindowCloseControlName(name string) bool {
	n := foldMedia(name)
	n = strings.TrimRight(n, "….")
	switch n {
	case "关闭", "close", "关闭窗口", "关闭文档", "关闭文件", "关闭程序", "退出", "exit":
		return true
	}
	return strings.HasPrefix(n, "关闭") || strings.HasPrefix(n, "close ")
}

// IsChromeCloseControl reports a title-bar / document close affordance that
// computer-control must not click unless the user asked to close.
func IsChromeCloseControl(name string, y, w, h int) bool {
	if !isWindowCloseControlName(name) {
		return false
	}
	n := foldMedia(name)
	if n == "关闭窗口" || n == "关闭文档" || n == "关闭文件" || n == "关闭程序" || n == "退出" || n == "exit" {
		return true
	}
	if h > 0 && h <= 44 && w <= 96 && y < 56 {
		return true
	}
	return n == "关闭" || n == "close"
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

func pickComposerField(nodes []mediaUINode) *mediaUINode {
	var best *mediaUINode
	bestScore := 0
	for i := range nodes {
		n := &nodes[i]
		role := strings.ToLower(strings.TrimSpace(n.Role))
		name := foldMedia(n.Name)
		score := 0
		switch role {
		case "edit", "textbox", "textarea", "document":
			score = 40
		default:
			if strings.Contains(name, "发消息") || strings.Contains(name, "输入消息") {
				score = 30
			}
		}
		if score == 0 || isWindowCloseControlName(n.Name) {
			continue
		}
		if strings.Contains(name, "搜索") || strings.Contains(name, "search") {
			score -= 25
		}
		if strings.Contains(name, "发消息") || strings.Contains(name, "输入") {
			score += 30
		}
		if n.Y > 0 {
			score += n.Y / 80
		}
		if score > bestScore {
			bestScore = score
			best = n
		}
	}
	if best == nil || strings.TrimSpace(best.Name) == "" {
		return nil
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

func normalizeAfterLabel(s string) string {
	s = strings.TrimSpace(s)
	return strings.TrimRight(s, "：:")
}

func nodeIsLabelText(name, want string) bool {
	return labelsMatch(want, name)
}

func documentLabelSearchTerms(after string) []string {
	after = strings.TrimSpace(after)
	if after == "" {
		return nil
	}
	seen := map[string]bool{}
	var terms []string
	add := func(s string) {
		s = strings.TrimSpace(s)
		if s == "" || seen[s] {
			return
		}
		seen[s] = true
		terms = append(terms, s)
	}
	add(after)
	for _, alias := range labelAliases(after) {
		add(alias)
	}
	return terms
}

func documentLabelSearchTerm(after string) string {
	terms := documentLabelSearchTerms(after)
	if len(terms) == 0 {
		return after
	}
	best := terms[0]
	for _, term := range terms[1:] {
		if len([]rune(term)) > len([]rune(best)) {
			best = term
		}
	}
	return best
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

func desktopTypeVisible(nodes []mediaUINode, text string) bool {
	text = strings.TrimSpace(text)
	if text == "" {
		return false
	}
	for _, n := range nodes {
		if n.Value != "" && strings.Contains(n.Value, text) {
			return true
		}
	}
	return false
}

const desktopTypeVerifyTries = 3

func verifyDesktopTyped(ctx context.Context, invoke ccInvoker, session string, approved bool, text string) error {
	for i := 0; i < desktopTypeVerifyTries; i++ {
		mediaSleep(80 * time.Millisecond)
		nodes, _, _ := ccObserveNodes(ctx, invoke, session, approved)
		if desktopTypeVisible(nodes, text) {
			return nil
		}
	}
	return fmt.Errorf("无法执行：写完后界面上看不到「%s」，不能确认已写入", text)
}

func chatSearchWindow(window string) bool {
	switch strings.ToLower(strings.TrimSpace(window)) {
	case "微信", "wechat", "weixin":
		return true
	}
	return false
}

// sendChatBySearch follows the desktop WeChat sequence: Ctrl+F, paste the
// contact name, Enter to open that chat, paste the message, Enter to send.
// The following observe text is returned so the next step can read what is
// already on screen.
func sendChatBySearch(ctx context.Context, invoke ccInvoker, session, window, contact, text string, submit, approved bool) (Result, error) {
	if err := ccShortcut(ctx, invoke, session, approved, "ctrl", "f"); err != nil {
		return Result{}, fmt.Errorf("无法执行：没能打开%s的搜索", window)
	}
	mediaSleep(200 * time.Millisecond)
	if err := ccType(ctx, invoke, session, contact, approved); err != nil {
		return Result{}, fmt.Errorf("无法执行：没能搜索「%s」", contact)
	}
	mediaSleep(350 * time.Millisecond)
	if err := ccPress(ctx, invoke, session, "enter", approved); err != nil {
		return Result{}, fmt.Errorf("无法执行：没能打开和「%s」的会话", contact)
	}
	mediaSleep(450 * time.Millisecond)
	if err := ccType(ctx, invoke, session, text, approved); err != nil {
		return Result{}, fmt.Errorf("无法执行：没能在和「%s」的会话里输入", contact)
	}
	if submit {
		mediaSleep(150 * time.Millisecond)
		if err := ccPress(ctx, invoke, session, "enter", approved); err != nil {
			return Result{}, fmt.Errorf("无法执行：字已写入，没能发送")
		}
	}
	mediaSleep(250 * time.Millisecond)
	nodes, _, _ := ccObserveNodes(ctx, invoke, session, approved)
	seen := visibleChatLines(nodes, 12)
	shot, shotMIME, shotData := chatWindowShot(ctx, invoke, session, window, approved)
	if shot != "" && !strings.Contains(seen, shot) {
		if seen != "" {
			seen += "\n"
		}
		seen += shot
	}
	how := fmt.Sprintf("opened chat %q and typed %q", contact, text)
	if submit {
		how = fmt.Sprintf("opened chat %q and sent %q", contact, text)
	}
	if seen != "" {
		how += "\nvisible:\n" + seen
	}
	how += "\n对方已经显示在屏幕上的内容在 visible 里。根据这些内容继续回复，直到这一轮聊天结束。同名联系人用搜索后的第一个。"
	out := result(appendL0JSON(how, "chat", true, false, contact))
	out.VisionMIME = shotMIME
	out.VisionData = shotData
	return out, nil
}

var readChatImage = func(ctx context.Context, png []byte) (string, error) {
	got, err := doctext.ExtractImageOCR(ctx, png)
	if err != nil {
		return "", err
	}
	var b strings.Builder
	for _, page := range got.Pages {
		line := strings.TrimSpace(page.Text)
		if line == "" {
			continue
		}
		if b.Len() > 0 {
			b.WriteByte('\n')
		}
		b.WriteString(line)
	}
	return b.String(), nil
}

func chatWindowShot(ctx context.Context, invoke ccInvoker, session, window string, approved bool) (string, string, []byte) {
	res, err := ccCall(ctx, invoke, session, ccapp.ToolScreenCapture, map[string]any{"target": "window", "title": window}, approved)
	if err != nil || len(res.VisionData) == 0 {
		return "", "", nil
	}
	text, err := readChatImage(ctx, res.VisionData)
	if err != nil {
		text = ""
	}
	mime := res.VisionMIME
	if mime == "" {
		mime = "image/png"
	}
	return strings.TrimSpace(text), mime, res.VisionData
}

func composerSendsOnEnter(window string) bool {
	switch strings.ToLower(strings.TrimSpace(window)) {
	case "豆包", "doubao":
		return true
	}
	return false
}

func visibleChatLines(nodes []mediaUINode, limit int) string {
	if limit < 1 {
		limit = 1
	}
	var lines []string
	seen := map[string]bool{}
	for _, n := range nodes {
		for _, raw := range []string{n.Value, n.Name} {
			line := strings.TrimSpace(raw)
			if line == "" || seen[line] || utf8.RuneCountInString(line) < 2 {
				continue
			}
			if isWindowCloseControlName(line) {
				continue
			}
			seen[line] = true
			lines = append(lines, line)
			if len(lines) >= limit {
				return strings.Join(lines, "\n")
			}
		}
	}
	return strings.Join(lines, "\n")
}

func executeDesktopType(ctx context.Context, invoke ccInvoker, session string, args json.RawMessage, approved, unconfined bool) (Result, error) {
	var a struct {
		Text   string `json:"text"`
		Window string `json:"window"`
		After  string `json:"after"`
		Submit bool   `json:"submit"`
		Save   bool   `json:"save"`
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
	if err := requireDesktopAction(approved); err != nil {
		return Result{}, errors.New("无法执行：需要完整权限或用户批准才能在文档里输入")
	}

	window := strings.TrimSpace(a.Window)
	after := normalizeAfterLabel(a.After)
	ctx = withMediaInputWindow(ctx, window)

	if window != "" {
		if _, err := ccCall(ctx, invoke, session, ccapp.ToolWindowFocus, map[string]any{"title": window}, approved); err != nil {
			return Result{}, fmt.Errorf("无法执行：没能聚焦窗口「%s」", window)
		}
		mediaSleep(280 * time.Millisecond)
	}

	if after != "" && chatSearchWindow(window) {
		return sendChatBySearch(ctx, invoke, session, window, after, text, a.Submit, approved)
	}

	if after != "" {
		nodes, _, _ := ccObserveNodes(ctx, invoke, session, approved)
		if field := pickNamedEdit(nodes, after); field != nil {
			if isWindowCloseControlName(field.Name) {
				return Result{}, fmt.Errorf("无法执行：拒绝点击关闭按钮")
			}
			target := clipMediaName(field.Name)
			_ = ccClickName(ctx, invoke, session, target, 1, approved)
			mediaSleep(80 * time.Millisecond)
			if err := ccType(ctx, invoke, session, text, approved); err != nil {
				return Result{}, fmt.Errorf("无法执行：无法在「%s」后输入（%v）", after, err)
			}
		} else if label := pickDocumentLabel(nodes, after); label != nil && (label.W > 0 || label.H > 0) {
			if isWindowCloseControlName(label.Name) {
				return Result{}, fmt.Errorf("无法执行：拒绝点击关闭按钮")
			}
			x := label.X + label.W + 4
			y := label.Y + label.H/2
			if y == 0 {
				y = label.Y
			}
			if err := ccClickXY(ctx, invoke, session, x, y, 1, approved); err != nil {
				return Result{}, fmt.Errorf("无法执行：找不到「%s」", after)
			}
			mediaSleep(80 * time.Millisecond)
			if err := ccType(ctx, invoke, session, text, approved); err != nil {
				return Result{}, fmt.Errorf("无法执行：无法在「%s」后输入（%v）", after, err)
			}
		} else if label := pickDocumentLabel(nodes, after); label != nil {
			_ = ccClickName(ctx, invoke, session, clipMediaName(label.Name), 1, approved)
			mediaSleep(80 * time.Millisecond)
			_ = ccPress(ctx, invoke, session, "right", approved)
			_ = ccPress(ctx, invoke, session, "right", approved)
			if err := ccType(ctx, invoke, session, text, approved); err != nil {
				return Result{}, fmt.Errorf("无法执行：无法在「%s」后输入（%v）", after, err)
			}
		} else {
			return Result{}, fmt.Errorf("无法执行：界面上看不到可填写的「%s」。请先截图看清输入位置，不要假装已经写入", after)
		}
		if err := verifyDesktopTyped(ctx, invoke, session, approved, text); err != nil {
			return Result{}, err
		}
	} else {
		nodes, _, _ := ccObserveNodes(ctx, invoke, session, approved)
		field := pickComposerField(nodes)
		if field != nil {
			_ = ccClickName(ctx, invoke, session, clipMediaName(field.Name), 1, approved)
			mediaSleep(120 * time.Millisecond)
		}
		if err := ccType(ctx, invoke, session, text, approved); err != nil {
			if field != nil && typingFocusMissed(err) {
				x := field.X + field.W/2
				y := field.Y + field.H/2
				if x < 1 {
					x = field.X + 8
				}
				if y < 1 {
					y = field.Y + 8
				}
				if ccClickXY(ctx, invoke, session, x, y, 1, approved) == nil {
					mediaSleep(120 * time.Millisecond)
					err = ccType(ctx, invoke, session, text, approved)
				}
			}
			if err != nil {
				if typingFocusMissed(err) {
					return Result{}, fmt.Errorf("无法执行：焦点不在输入框。请先点开输入框，再输入「%s」", text)
				}
				return Result{}, fmt.Errorf("无法执行：无法输入文字（%v）", err)
			}
		}
	}

	saved := false
	if a.Save && !chatSearchWindow(window) {
		if err := ccShortcut(ctx, invoke, session, approved, "ctrl", "s"); err != nil {
			return Result{}, fmt.Errorf("无法执行：字已写入，没能保存")
		}
		saved = true
	}

	if a.Submit && composerSendsOnEnter(window) {
		mediaSleep(120 * time.Millisecond)
		if err := ccPress(ctx, invoke, session, "enter", approved); err != nil {
			return Result{}, fmt.Errorf("无法执行：已输入但没能发送（%v）", err)
		}
	} else if a.Submit {
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

	how := fmt.Sprintf("typed %q", text)
	if after != "" {
		how = fmt.Sprintf("typed %q after %q", text, after)
	}
	if a.Submit {
		how += " and submitted"
	}
	if saved {
		how += " and saved"
	}
	if window != "" {
		how += " in " + window
	}
	return result(appendL0JSON(how, "field", true, false, text)), nil
}
