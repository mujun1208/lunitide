package app

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/lunitide/lunitide/internal/ccapp"
)

// G1: native gui-plus protocol.
//
// Qwen gui-plus models are trained on Alibaba's Computer System Prompt and
// answer with <tool_call>{"name":"computer_use","arguments":{...}}</tool_call>.
// Without that exact prompt the model replies with prose ("建议长按屏幕…"),
// which no parser can drive a desktop with. When the loop runs on a bound
// GUI-kind model it therefore speaks the native protocol verbatim and maps
// the native action set onto computer.act (ccapp/guimap.go). Vision fallback
// models keep the internal JSON grammar; each parser accepts the other
// format as a fallback so either executor survives a model swap.

// guiNativeSystemPrompt is Alibaba's Computer System Prompt, verbatim from
// the gui-plus documentation. The declared 1000x1000 screen is what the
// model's coordinates refer to.
const guiNativeSystemPrompt = `# Tools

You may call one or more functions to assist with the user query.

You are provided with function signatures within <tools></tools> XML tags:
<tools>
{"type": "function", "function": {"name": "computer_use", "description": "Use a mouse and keyboard to interact with a computer, and take screenshots.\n* This is an interface to a desktop GUI. You do not have access to a terminal or applications menu. You must click on desktop icons to start applications.\n* Some applications may take time to start or process actions, so you may need to wait and take successive screenshots to see the results of your actions. E.g. if you click on Firefox and a window doesn't open, try wait and taking another screenshot.\n* The screen's resolution is 1000x1000.\n* Make sure to click any buttons, links, icons, etc with the cursor tip in the center of the element. Don't click boxes on their edges unless asked.", "parameters": {"properties": {"action": {"description": "The action to perform. The available actions are:\n* ` + "`key`" + `: Performs key down presses on the arguments passed in order, then performs key releases in reverse order.\n* ` + "`type`" + `: Type a string of text on the keyboard.\n* ` + "`mouse_move`" + `: Move the cursor to a specified (x, y) pixel coordinate on the screen.\n* ` + "`left_click`" + `: Click the left mouse button at a specified (x, y) pixel coordinate on the screen.\n* ` + "`left_click_drag`" + `: Click and drag the cursor to a specified (x, y) pixel coordinate on the screen.\n* ` + "`right_click`" + `: Click the right mouse button at a specified (x, y) pixel coordinate on the screen.\n* ` + "`middle_click`" + `: Click the middle mouse button at a specified (x, y) pixel coordinate on the screen.\n* ` + "`double_click`" + `: Double-click the left mouse button at a specified (x, y) pixel coordinate on the screen.\n* ` + "`triple_click`" + `: Triple-click the left mouse button at a specified (x, y) pixel coordinate on the screen (simulated as double-click since it's the closest action).\n* ` + "`scroll`" + `: Performs a scroll of the mouse scroll wheel.\n* ` + "`hscroll`" + `: Performs a horizontal scroll (mapped to regular scroll).\n* ` + "`wait`" + `: Wait specified seconds for the change to happen.\n* ` + "`terminate`" + `: Terminate the current task and report its completion status.\n* ` + "`answer`" + `: Answer a question.\n* ` + "`interact`" + `: Resolve the blocking window by interacting with the user.", "enum": ["key", "type", "mouse_move", "left_click", "left_click_drag", "right_click", "middle_click", "double_click", "triple_click", "scroll", "hscroll", "wait", "terminate", "answer", "interact"], "type": "string"}, "keys": {"description": "Required only by ` + "`action=key`" + `.", "type": "array"}, "text": {"description": "Required only by ` + "`action=type`" + `, ` + "`action=answer`" + ` and ` + "`action=interact`" + `.", "type": "string"}, "coordinate": {"description": "(x, y): The x (pixels from the left edge) and y (pixels from the top edge) coordinates to move the mouse to. Required only by ` + "`action=mouse_move`" + ` and ` + "`action=left_click_drag`" + `.", "type": "array"}, "pixels": {"description": "The amount of scrolling to perform. Positive values scroll up, negative values scroll down. Required only by ` + "`action=scroll`" + ` and ` + "`action=hscroll`" + `.", "type": "number"}, "time": {"description": "The seconds to wait. Required only by ` + "`action=wait`" + `.", "type": "number"}, "status": {"description": "The status of the task. Required only by ` + "`action=terminate`" + `.", "type": "string", "enum": ["success", "failure"]}}, "required": ["action"], "type": "object"}}}
</tools>

For each function call, return a json object with function name and arguments within <tool_call></tool_call> XML tags:
<tool_call>
{"name": <function-name>, "arguments": <args-json-object>}
</tool_call>

# Response format

Response format for every step:
1) Action: a short imperative describing what to do in the UI.
2) A single <tool_call>...</tool_call> block containing only the JSON: {"name": <function-name>, "arguments": <args-json-object>}.

Rules:
- Output exactly in the order: Action, <tool_call>.
- Be brief: one for Action.
- Do not output anything else outside those two parts.
- If finishing, use action=terminate in the tool call.`

// guiNativeUserPrompt builds the native user turn. It starts with "Goal:\n"
// so completeGUIStep's system/user split keeps working.
func guiNativeUserPrompt(goal, locked string, history []guiLoopStep) string {
	var b strings.Builder
	b.WriteString("Goal:\n")
	b.WriteString(strings.TrimSpace(goal))
	if locked != "" {
		b.WriteString("\nLocked window: ")
		b.WriteString(locked)
		b.WriteString(". Act only inside it.")
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
	b.WriteString("\n\nContinue with the next single tool call.")
	return b.String()
}

// parseGuiNativeStep wraps one parsed native tool call as a loop action.
// Signal actions (terminate/answer/interact) map onto the existing done/fail
// semantics; executable actions keep their native name and carry the parsed
// payload for buildGUILoopArgs to map.
func parseGuiNativeStep(raw string) (guiLoopAction, error) {
	na, err := ccapp.ParseGuiNativeToolCall(raw)
	if err != nil {
		return guiLoopAction{}, err
	}
	a := guiLoopAction{Native: &na}
	switch ccapp.GuiNativeSignal(na) {
	case "done":
		a.Action = "done"
		a.Reason = strings.TrimSpace(na.Text)
		if a.Reason == "" && na.Action == "terminate" {
			a.Reason = "模型报告任务完成"
		}
		return a, nil
	case "fail":
		a.Action = "fail"
		a.Reason = strings.TrimSpace(na.Text)
		if a.Reason == "" {
			a.Reason = "模型报告任务失败"
		}
		return a, nil
	case "ask":
		a.Action = "fail"
		a.Reason = "需要用户先处理：" + strings.TrimSpace(na.Text)
		return a, nil
	}
	a.Action = na.Action
	if len(na.Coordinate) == 2 {
		x, y := na.Coordinate[0], na.Coordinate[1]
		a.X, a.Y = &x, &y
	}
	a.Text = na.Text
	a.Keys = na.Keys
	if na.Pixels != nil {
		a.Scroll = ccampGuiNativeScrollNotchesForDescription(na)
	}
	if na.Time != nil {
		a.Ms = ccapp.GuiNativeWaitMs(na.Time)
	}
	return a, nil
}

// ccampGuiNativeScrollNotchesForDescription mirrors the notch conversion so
// describeGUIAction reports what will actually run.
func ccampGuiNativeScrollNotchesForDescription(na ccapp.GuiNativeAction) int {
	raw, err := ccapp.MapGuiNativeAction(na, 1000, 1000, nil)
	if err != nil {
		return 0
	}
	var m struct {
		Scroll int `json:"scroll"`
	}
	if json.Unmarshal(raw, &m) != nil {
		return 0
	}
	return m.Scroll
}

// parseGUILoopStep parses one model reply into a loop action, trying the
// native gui-plus grammar and the internal JSON grammar in the order that
// matches the executor. Either parser accepting the reply is enough.
func parseGUILoopStep(raw string, emptyTree bool, wantFrame string, nativeFirst bool) (guiLoopAction, error) {
	if nativeFirst {
		if a, err := parseGuiNativeStep(raw); err == nil {
			return a, nil
		}
		return parseGUILoopAction(raw, emptyTree, wantFrame)
	}
	if a, err := parseGUILoopAction(raw, emptyTree, wantFrame); err == nil {
		return a, nil
	}
	return parseGuiNativeStep(raw)
}
