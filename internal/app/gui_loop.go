package app

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/lunitide/lunitide/internal/ccapp"
	"github.com/lunitide/lunitide/internal/domain/provider"
	"github.com/lunitide/lunitide/internal/llmadapter"
	"github.com/lunitide/lunitide/internal/secretlease"
	"github.com/lunitide/lunitide/internal/toolruntime"
)

// GUI visual execution loop.
//
// The single-shot SoM pick in gui_fallback.go answers "which mark do I click
// once". Real desktop work needs see → act → verify repeated until the goal
// is met, the way Operator / UI-TARS / Computer Use drive a screen. This
// loop keeps every action on the governed computer.act path (risk gates,
// audit, fence, frameId freshness) and lets a GUI model — or, when none is
// configured, any vision-capable model including the chat model itself —
// pick the next action from the latest screenshot.

// maxGUILoopSteps bounds one loop. Each step costs one observe + one model
// call; eight covers "open menu → pick item → confirm → verify" with slack.
const maxGUILoopSteps = 8

// maxGUILoopRunsPerTurn lets the loop re-enter once more if the main model
// makes progress and then gets stuck again in the same turn.
const maxGUILoopRunsPerTurn = 2

// maxGUILoopConsecutiveFails stops the loop when the screen refuses to
// cooperate instead of burning the whole budget on the same failing click.
const maxGUILoopConsecutiveFails = 2

type guiLoopAction struct {
	Action  string   `json:"action"`
	MarkID  string   `json:"markId"`
	X       *float64 `json:"x"`
	Y       *float64 `json:"y"`
	Text    string   `json:"text"`
	Keys    []string `json:"keys"`
	Key     string   `json:"key"`
	Scroll  int      `json:"scroll"`
	Ms      int      `json:"ms"`
	Reason  string   `json:"reason"`
	FrameID string   `json:"frameId"`
}

type guiLoopStep struct {
	Action string          `json:"action"`
	Args   json.RawMessage `json:"args,omitempty"`
	Result string          `json:"result"`
	OK     bool            `json:"ok"`
}

// guiLoopRuntime extends guiFallbackRuntime with the general executor. Exec
// runs any computer.act payload (not just click) through the same gate.
type guiLoopRuntime struct {
	guiFallbackRuntime
	Exec     func(args json.RawMessage, allowPixels bool) (toolruntime.Result, error)
	MaxSteps int
}

const guiLoopSystemPrompt = `You operate a Windows desktop by looking at screenshots. Each turn you see the latest screenshot and must reply with exactly one JSON object describing the single next action. No prose.

Actions:
{"action":"click","markId":"B3"}                       click a painted mark (preferred when marks exist)
{"action":"click","x":<0-1000>,"y":<0-1000>,"frameId":"<given>"}   click a point; x,y are per-mille of the screenshot width/height
{"action":"double_click", ...same targeting...}
{"action":"right_click", ...same targeting...}
{"action":"type","text":"..."}                          type into the focused field (CJK / long text is pasted automatically)
{"action":"key","keys":["ctrl","s"]}                   press a shortcut
{"action":"key","key":"enter"}                          press one key (enter, esc, tab, backspace, delete, up, down...)
{"action":"scroll","scroll":-3,"x":..,"y":..}           negative scrolls down, positive up, at an optional point
{"action":"wait","ms":800}                              let the UI settle
{"action":"done","reason":"..."}                        the goal is visibly achieved on this screenshot
{"action":"fail","reason":"..."}                        cannot continue (login wall, missing app, ambiguous target)

Rules: one action per reply. Prefer a markId over coordinates. Never invent a mark that is not painted. Say done only when the screenshot proves the goal. Say fail instead of guessing when the target is not visible. Do not type passwords or payment details.`

func guiLoopUserPrompt(goal, frameID string, marks bool, history []guiLoopStep) string {
	var b strings.Builder
	b.WriteString("Goal:\n")
	b.WriteString(strings.TrimSpace(goal))
	b.WriteString("\n\nCurrent frameId: ")
	b.WriteString(strings.TrimSpace(frameID))
	if marks {
		b.WriteString("\nMarks like B1/E2 are painted on the screenshot; use markId.")
	} else {
		b.WriteString("\nNo marks are painted; use x,y per-mille coordinates with this frameId.")
	}
	if len(history) > 0 {
		b.WriteString("\n\nSteps already taken this run:")
		for i, h := range history {
			status := "ok"
			if !h.OK {
				status = "FAILED"
			}
			fmt.Fprintf(&b, "\n%d. %s → %s: %s", i+1, h.Action, status, clipGUIResult(h.Result, 160))
		}
		b.WriteString("\nIf the last step failed, choose a different target or action; never repeat the same failing step.")
	}
	b.WriteString("\n\nReply with one JSON object.")
	return b.String()
}

func clipGUIResult(s string, n int) string {
	s = strings.TrimSpace(strings.ReplaceAll(s, "\n", " "))
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}

func parseGUILoopAction(raw string, emptyTree bool, wantFrame string) (guiLoopAction, error) {
	js, err := firstJSONObject(raw)
	if err != nil {
		return guiLoopAction{}, err
	}
	var a guiLoopAction
	if json.Unmarshal([]byte(js), &a) != nil {
		return guiLoopAction{}, fmt.Errorf("invalid action json")
	}
	a.Action = strings.ToLower(strings.TrimSpace(a.Action))
	switch a.Action {
	case "done", "fail", "ask":
		if a.Action == "ask" {
			a.Action = "fail"
		}
		return a, nil
	case "click", "double_click", "right_click", "left_click", "dblclick", "rightclick":
		if strings.TrimSpace(a.MarkID) != "" {
			a.MarkID = strings.ToUpper(strings.TrimSpace(a.MarkID))
			if !validSOMMarkID(a.MarkID) {
				return guiLoopAction{}, fmt.Errorf("invalid markId")
			}
			if emptyTree {
				return guiLoopAction{}, fmt.Errorf("no marks painted")
			}
			return a, nil
		}
		if a.X == nil || a.Y == nil {
			return guiLoopAction{}, fmt.Errorf("click needs markId or x,y")
		}
		if *a.X < 0 || *a.X > 1000 || *a.Y < 0 || *a.Y > 1000 {
			return guiLoopAction{}, fmt.Errorf("xy out of range")
		}
		if strings.TrimSpace(a.FrameID) != strings.TrimSpace(wantFrame) {
			return guiLoopAction{}, fmt.Errorf("frameId mismatch")
		}
		return a, nil
	case "type", "paste":
		if strings.TrimSpace(a.Text) == "" {
			return guiLoopAction{}, fmt.Errorf("type needs text")
		}
		return a, nil
	case "key", "hotkey", "shortcut", "press":
		if len(a.Keys) == 0 && strings.TrimSpace(a.Key) == "" {
			return guiLoopAction{}, fmt.Errorf("key needs keys or key")
		}
		return a, nil
	case "scroll":
		if a.Scroll == 0 {
			a.Scroll = -3
		}
		return a, nil
	case "wait":
		if a.Ms <= 0 {
			a.Ms = 800
		}
		if a.Ms > 5000 {
			a.Ms = 5000
		}
		return a, nil
	}
	return guiLoopAction{}, fmt.Errorf("unknown action %q", a.Action)
}

func buildGUILoopArgs(a guiLoopAction, emptyTree bool, visW, visH int) (json.RawMessage, error) {
	switch a.Action {
	case "click", "double_click", "right_click", "left_click", "dblclick", "rightclick":
		action := a.Action
		switch action {
		case "left_click":
			action = "click"
		case "dblclick":
			action = "double_click"
		case "rightclick":
			action = "right_click"
		}
		if a.MarkID != "" {
			return json.Marshal(map[string]any{"action": action, "id": a.MarkID})
		}
		x, err := mapGUICoord(*a.X, visW)
		if err != nil {
			return nil, err
		}
		y, err := mapGUICoord(*a.Y, visH)
		if err != nil {
			return nil, err
		}
		return json.Marshal(map[string]any{"action": action, "x": x, "y": y, "frameId": strings.TrimSpace(a.FrameID)})
	case "type", "paste":
		action := "type"
		if a.Action == "paste" || ccapp.PreferPasteText(a.Text) {
			action = "paste"
		}
		return json.Marshal(map[string]any{"action": action, "text": a.Text})
	case "key", "hotkey", "shortcut", "press":
		if len(a.Keys) > 0 {
			return json.Marshal(map[string]any{"action": "key", "keys": a.Keys})
		}
		return json.Marshal(map[string]any{"action": "key", "key": strings.TrimSpace(a.Key)})
	case "scroll":
		m := map[string]any{"action": "scroll", "scroll": a.Scroll}
		if a.X != nil && a.Y != nil {
			if x, err := mapGUICoord(*a.X, visW); err == nil {
				if y, err := mapGUICoord(*a.Y, visH); err == nil {
					m["x"], m["y"] = x, y
				}
			}
		}
		return json.Marshal(m)
	case "wait":
		return json.Marshal(map[string]any{"action": "wait", "ms": a.Ms})
	}
	return nil, fmt.Errorf("not an executable action: %s", a.Action)
}

// pickGUILoopExecutor widens pickGUIFallback: a vision model may also work
// on an empty tree (per-mille coordinates), and the chat model itself is a
// valid vision executor when nothing else is configured.
func pickGUILoopExecutor(in guiFallbackIn) guiExecutor {
	if !shouldAttemptGUIFallback(in) {
		return guiExecNone
	}
	if in.guiCatalog {
		return guiExecGUI
	}
	if in.visionCatalog {
		return guiExecVision
	}
	return guiExecNone
}

func guiLoopStepsSummary(steps []guiLoopStep) string {
	if len(steps) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("屏幕执行步骤：")
	for i, s := range steps {
		status := "✓"
		if !s.OK {
			status = "✗"
		}
		fmt.Fprintf(&b, "\n%d. %s %s %s", i+1, status, s.Action, clipGUIResult(s.Result, 120))
	}
	return b.String()
}

func guiLoopDisplayArgs(steps []guiLoopStep) json.RawMessage {
	if len(steps) == 0 {
		return json.RawMessage(`{"action":"gui_loop","steps":[]}`)
	}
	items := make([]json.RawMessage, 0, len(steps))
	for _, s := range steps {
		if len(s.Args) > 0 {
			items = append(items, s.Args)
		}
	}
	raw, err := json.Marshal(map[string]any{"action": "gui_loop", "steps": items})
	if err != nil {
		return json.RawMessage(`{"action":"gui_loop"}`)
	}
	return raw
}

// runGUILoop drives see → act → verify until done/fail/budget. used=false
// means the loop never engaged (gates closed, observe impossible, no model);
// the caller then keeps the main model's own result.
func runGUILoop(in guiFallbackIn, rt guiLoopRuntime) (toolruntime.Result, json.RawMessage, bool) {
	if !shouldAttemptGUIFallback(in) {
		return toolruntime.Result{}, nil, false
	}
	if rt.Complete == nil || rt.Observe == nil || rt.Exec == nil {
		return toolruntime.Result{}, nil, false
	}
	maxSteps := rt.MaxSteps
	if maxSteps <= 0 {
		maxSteps = maxGUILoopSteps
	}
	frameID, nodes, visW, visH := strings.TrimSpace(rt.FrameID), rt.Nodes, rt.VisW, rt.VisH
	images := rt.Images
	if frameID == "" || len(images) == 0 {
		var err error
		frameID, nodes, visW, visH, images, err = rt.Observe()
		if err != nil || strings.TrimSpace(frameID) == "" {
			return toolruntime.Result{}, nil, false
		}
		frameID = strings.TrimSpace(frameID)
	}
	in.nodeCount = nodes
	exec := pickGUILoopExecutor(in)
	if exec == guiExecNone {
		return toolruntime.Result{}, nil, false
	}
	var steps []guiLoopStep
	var lastVision llmadapter.Image
	if len(images) > 0 {
		lastVision = images[len(images)-1]
	}
	finish := func(ok bool, headline string) (toolruntime.Result, json.RawMessage, bool) {
		var b strings.Builder
		if ok {
			b.WriteString("ok:true\n")
		} else {
			b.WriteString("ok:false\n")
		}
		b.WriteString(headline)
		if s := guiLoopStepsSummary(steps); s != "" {
			b.WriteString("\n")
			b.WriteString(s)
		}
		if !ok {
			b.WriteString("\n请根据最新截图判断下一步，或请用户指出要点哪里。")
		}
		res := toolruntime.Result{Output: b.String()}
		if len(lastVision.Data) > 0 {
			res.VisionData, res.VisionMIME = lastVision.Data, lastVision.MIME
		}
		return res, guiLoopDisplayArgs(steps), true
	}
	consecutiveFails := 0
	for step := 0; step < maxSteps; step++ {
		var frameImages []llmadapter.Image
		if len(images) > 0 {
			frameImages = images[len(images)-1:]
		}
		raw, err := rt.Complete(exec, frameImages, guiLoopSystemPrompt+"\n\n"+guiLoopUserPrompt(rt.Goal, frameID, nodes > 0, steps))
		if err != nil {
			if len(steps) == 0 {
				return guiFallbackFailResult("未能从屏幕读出下一步"), nil, true
			}
			return finish(false, "屏幕模型无响应，已停止。")
		}
		act, err := parseGUILoopAction(raw, nodes == 0, frameID)
		if err != nil {
			steps = append(steps, guiLoopStep{Action: "invalid", Result: "模型回复不是合法动作: " + clipGUIResult(raw, 80), OK: false})
			consecutiveFails++
			if consecutiveFails >= maxGUILoopConsecutiveFails {
				return finish(false, "屏幕模型连续给出无效动作。")
			}
			continue
		}
		switch act.Action {
		case "done":
			reason := strings.TrimSpace(act.Reason)
			if reason == "" {
				reason = "屏幕显示目标已达成"
			}
			if len(steps) == 0 {
				// Nothing was done: the screen already showed the goal. Report
				// that honestly instead of claiming an action.
				return finish(true, "未执行新动作；屏幕已显示目标状态："+reason)
			}
			return finish(true, "屏幕执行完成："+reason)
		case "fail":
			reason := strings.TrimSpace(act.Reason)
			if reason == "" {
				reason = "屏幕上找不到可继续的目标"
			}
			return finish(false, "屏幕执行停止："+reason)
		}
		skipped := false
		if act.MarkID != "" && (rt.HasHit == nil || !rt.HasHit(act.MarkID)) {
			steps = append(steps, guiLoopStep{Action: act.Action + " " + act.MarkID, Result: "编号不在本帧观察里", OK: false})
			consecutiveFails++
			if consecutiveFails >= maxGUILoopConsecutiveFails {
				return finish(false, "屏幕模型连续选择了不存在的编号。")
			}
			// The next decision needs a new frame. Continuing here reused the
			// same node list, so the model clicked numbers that were never on screen.
			skipped = true
		}
		if !skipped {
			args, err := buildGUILoopArgs(act, nodes == 0, visW, visH)
			if err != nil {
				steps = append(steps, guiLoopStep{Action: act.Action, Result: err.Error(), OK: false})
				consecutiveFails++
				if consecutiveFails >= maxGUILoopConsecutiveFails {
					return finish(false, "无法把屏幕读号收成动作。")
				}
			} else {
				res, execErr := rt.Exec(args, nodes == 0)
				out := strings.TrimSpace(res.Output)
				ok := execErr == nil && !strings.Contains(out, "ok:false")
				if execErr != nil {
					out = execErr.Error()
				}
				steps = append(steps, guiLoopStep{Action: describeGUIAction(act), Args: args, Result: out, OK: ok})
				if len(res.VisionData) > 0 {
					lastVision = llmadapter.Image{MIME: res.VisionMIME, Data: res.VisionData}
				}
				if !ok {
					consecutiveFails++
					if consecutiveFails >= maxGUILoopConsecutiveFails {
						return finish(false, "屏幕动作连续失败，已停止。")
					}
				} else {
					consecutiveFails = 0
				}
			}
		}
		// Verify: fresh frame before the next decision. A stale frame would
		// let the model click coordinates from a screen that no longer exists.
		nf, nn, nw, nh, nimgs, oerr := rt.Observe()
		if oerr != nil || strings.TrimSpace(nf) == "" {
			return finish(false, "动作后无法重新观察屏幕。")
		}
		frameID, nodes, visW, visH, images = strings.TrimSpace(nf), nn, nw, nh, nimgs
		if len(images) > 0 {
			lastVision = images[len(images)-1]
		}
	}
	return finish(false, fmt.Sprintf("已用完 %d 步仍未确认完成。", maxSteps))
}

func describeGUIAction(a guiLoopAction) string {
	switch a.Action {
	case "type":
		return "type " + clipGUIResult(a.Text, 24)
	case "key", "hotkey", "shortcut", "press":
		if len(a.Keys) > 0 {
			return "key " + strings.Join(a.Keys, "+")
		}
		return "key " + a.Key
	case "scroll":
		return fmt.Sprintf("scroll %d", a.Scroll)
	case "wait":
		return fmt.Sprintf("wait %dms", a.Ms)
	}
	if a.MarkID != "" {
		return a.Action + " " + a.MarkID
	}
	if a.X != nil && a.Y != nil {
		return fmt.Sprintf("%s (%.0f,%.0f)‰", a.Action, *a.X, *a.Y)
	}
	return a.Action
}

// guiLoopCatalogFlags reports which executors exist. Unlike the single-shot
// flags, the vision catalog here includes the active chat model when it can
// see images, so a user with only one multimodal model still gets a loop.
func (e *Engine) guiLoopCatalogFlags(ctx context.Context) (gui, vision bool) {
	if e == nil || e.providers == nil {
		return false, false
	}
	items, err := e.providers.List(ctx, provider.Filter{})
	if err != nil {
		return false, false
	}
	return len(e.preferBoundCatalog(ctx, "gui", provider.CatalogForKind(items, provider.KindGUI))) > 0,
		len(e.preferBoundCatalog(ctx, "vision", provider.VisionDescribeCatalog(items, ""))) > 0
}

// completeGUIStep asks the GUI catalog (then vision, including the chat
// model when it can see) for one JSON action, with the action grammar in
// the system prompt.
func (e *Engine) completeGUIStep(ctx context.Context, exec guiExecutor, images []llmadapter.Image, prompt string) (string, error) {
	system, user := prompt, ""
	if i := strings.Index(prompt, "\n\nGoal:\n"); i > 0 {
		system, user = prompt[:i], prompt[i+2:]
	}
	return e.completeVisionJSON(ctx, exec, images, system, user, 160)
}

// completeVisionJSON asks the GUI catalog (then vision, which includes the
// chat model when it can see) for a short JSON answer about a screenshot.
// Shared by the GUI loop and the task verifier so both fall back the same way.
func (e *Engine) completeVisionJSON(ctx context.Context, exec guiExecutor, images []llmadapter.Image, system, user string, maxTokens int) (string, error) {
	if e == nil || e.providers == nil {
		return "", fmt.Errorf("no providers")
	}
	if len(images) == 0 {
		return "", fmt.Errorf("no screenshot")
	}
	items, err := e.providers.List(ctx, provider.Filter{})
	if err != nil {
		return "", err
	}
	var catalog []provider.CatalogEntry
	switch exec {
	case guiExecGUI:
		catalog = e.preferBoundCatalog(ctx, "gui", provider.CatalogForKind(items, provider.KindGUI))
		// A bound GUI model that errors should not strand the loop: fall
		// through to vision candidates afterwards.
		catalog = append(catalog, e.preferBoundCatalog(ctx, "vision", provider.VisionDescribeCatalog(items, ""))...)
	case guiExecVision:
		catalog = e.preferBoundCatalog(ctx, "vision", provider.VisionDescribeCatalog(items, ""))
	default:
		return "", fmt.Errorf("no gui executor")
	}
	if len(catalog) == 0 {
		return "", fmt.Errorf("empty gui catalog")
	}
	var msgs []llmadapter.Message
	if strings.TrimSpace(user) != "" {
		msgs = []llmadapter.Message{{Role: llmadapter.RoleSystem, Content: system}, {Role: llmadapter.RoleUser, Content: user}}
	} else {
		msgs = []llmadapter.Message{{Role: llmadapter.RoleUser, Content: system}}
	}
	if maxTokens <= 0 {
		maxTokens = 160
	}
	req := llmadapter.Request{
		Messages:         msgs,
		Images:           images,
		MaxTokens:        maxTokens,
		MaxAttempts:      1,
		DisableReasoning: true,
	}
	var last error
	for _, entry := range catalog {
		req.Model = entry.Model.ModelID
		var text string
		leaseErr := e.withProviderLease(ctx, entry.Provider, secretlease.OperationChat, func(op context.Context, secret []byte) error {
			op = withCallPurpose(op, "gui")
			a, adapterErr := e.adapterForModel(op, entry.Provider, entry.Model)
			if adapterErr != nil {
				return adapterErr
			}
			out, completeErr := a.Complete(op, secret, req)
			if completeErr != nil {
				return completeErr
			}
			if out.FinishReason == "content_filter" {
				return fmt.Errorf("gui step refused: %s", out.FinishReason)
			}
			text = strings.TrimSpace(out.Message.Content)
			if text == "" {
				return fmt.Errorf("empty gui step")
			}
			return nil
		})
		if leaseErr == nil && text != "" {
			return text, nil
		}
		last = leaseErr
	}
	if last == nil {
		last = fmt.Errorf("gui step failed")
	}
	return "", last
}
