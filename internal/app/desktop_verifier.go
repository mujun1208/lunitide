package app

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/lunitide/lunitide/internal/llmadapter"
)

// Task-level desktop verifier.
//
// Every computer.act step already verifies itself (frame hash changed, named
// target found). None of that proves the *task* is done: the model can click
// the right things in the right order and still leave the message unsent or
// the file unsaved, then write "已完成". Before a desktop turn closes, this
// verifier takes one last screenshot and asks a screen-capable model whether
// the goal is visibly met. Its verdict either lets the turn close with an
// evidence line, sends the model back to work, or tells the user what is
// blocking. It never edits the model's text and never claims more than the
// screenshot shows.

type desktopVerdict struct {
	Verdict string `json:"verdict"`
	Reason  string `json:"reason"`
}

const (
	verdictDone    = "done"
	verdictNotDone = "not_done"
	verdictBlocked = "blocked"
	verdictUnclear = "unclear"
)

const desktopVerifierSystem = `You audit whether a Windows desktop task is finished. You receive the user's goal, the agent's closing claim, and the FINAL screenshot. Reply with exactly one JSON object and nothing else:
{"verdict":"done"|"not_done"|"blocked"|"unclear","reason":"<one short sentence in the user's language>"}

done      = the screenshot itself proves the goal (the file is open in front, the message appears in the sent history, the setting shows toggled, the text is visible in the document).
not_done  = the screenshot shows the task incomplete or that nothing happened (target app not in front, text still in the input box, dialog still open, wrong page).
blocked   = a login wall, permission/UAC prompt, save/open dialog, captcha or error dialog is in the way and the user must act.
unclear   = the screenshot cannot prove or disprove the goal.

Judge the screenshot, not the claim. If the claim says done but the screen does not show it, answer not_done.`

func desktopVerifierUserPrompt(goal, claim string) string {
	var b strings.Builder
	b.WriteString("Goal:\n")
	b.WriteString(strings.TrimSpace(goal))
	c := strings.TrimSpace(claim)
	if c != "" {
		if r := []rune(c); len(r) > 400 {
			c = string(r[:400]) + "…"
		}
		b.WriteString("\n\nAgent's closing claim:\n")
		b.WriteString(c)
	}
	b.WriteString("\n\nReply with one JSON object.")
	return b.String()
}

func parseDesktopVerdict(raw string) (desktopVerdict, bool) {
	js, err := firstJSONObject(raw)
	if err != nil {
		return desktopVerdict{}, false
	}
	var v desktopVerdict
	if json.Unmarshal([]byte(js), &v) != nil {
		return desktopVerdict{}, false
	}
	v.Verdict = strings.ToLower(strings.TrimSpace(v.Verdict))
	v.Verdict = strings.ReplaceAll(v.Verdict, "-", "_")
	v.Verdict = strings.ReplaceAll(v.Verdict, " ", "_")
	switch v.Verdict {
	case verdictDone, verdictNotDone, verdictBlocked, verdictUnclear:
	case "notdone", "incomplete", "not_finished":
		v.Verdict = verdictNotDone
	case "complete", "completed", "finished", "success":
		v.Verdict = verdictDone
	default:
		return desktopVerdict{}, false
	}
	v.Reason = strings.TrimSpace(v.Reason)
	if r := []rune(v.Reason); len(r) > 120 {
		v.Reason = string(r[:120]) + "…"
	}
	return v, true
}

// desktopVerifierApplies decides whether a closing desktop turn deserves the
// screenshot audit. Only turns that manipulated a screen (not just launched
// an app, which desktop.open already verifies by foreground window) and
// only once per turn. Companion (voice) turns skip it: the extra round trip
// would land after the spoken reply.
func desktopVerifierApplies(lastTools []string, companion, verified, usedDesktopTools bool) bool {
	if companion || verified || !usedDesktopTools {
		return false
	}
	return usedAnyTool(lastTools,
		"computer.act", "desktop.type", "browser.act",
		"cc.mouse_click", "cc.keyboard_type", "cc.paste",
		"cc.mouse_drag", "cc.mouse_scroll", "cc.menu_click", "cc.set_value",
	)
}

// desktopVerdictNudge is the system message that sends the model back to the
// screen when the audit says the task is not finished.
func desktopVerdictNudge(reason string) llmadapter.Message {
	r := strings.TrimSpace(reason)
	if r == "" {
		r = "最终截图未显示目标已达成"
	}
	return llmadapter.Message{Role: llmadapter.RoleSystem, Content: "屏幕核验结果：未完成 — " + r + "。最新截图已附上。要么继续 see→act→verify 把任务做完，要么如实告诉用户哪一步没做到，不要重复已完成的口径。"}
}

// desktopVerdictEvidenceLine is the one-line receipt appended to the reply so
// the user sees the audit, not just the model's claim.
func desktopVerdictEvidenceLine(v desktopVerdict) string {
	switch v.Verdict {
	case verdictDone:
		if v.Reason != "" {
			return "\n\n已核对最终截图：" + v.Reason
		}
		return "\n\n已核对最终截图：目标已在屏幕上确认。"
	case verdictBlocked:
		if v.Reason != "" {
			return "\n\n屏幕核验：" + v.Reason + " 需要你在电脑上处理后再继续。"
		}
		return "\n\n屏幕核验：有对话框或提示挡住了后续操作，需要你在电脑上处理后再继续。"
	}
	return ""
}

// verifyDesktopOutcome captures the final frame through computer.act observe
// (gated, audited) and asks the GUI/vision catalog for a verdict. ok=false
// means the audit could not run (no model, no frame, control off); callers
// must then close the turn exactly as before.
func (e *Engine) verifyDesktopOutcome(ctx context.Context, mode executionMode, sessionID, goal, claim string, companion bool) (desktopVerdict, []llmadapter.Image, bool) {
	if e == nil || e.ccctrl == nil || !e.computerControlEnabled() {
		return desktopVerdict{}, nil, false
	}
	gui, vision := e.guiLoopCatalogFlags(ctx)
	var exec guiExecutor
	switch {
	case gui:
		exec = guiExecGUI
	case vision:
		exec = guiExecVision
	default:
		return desktopVerdict{}, nil, false
	}
	res, err := e.executeUserToolWithCompanion(ctx, mode, sessionID, "computer.act", json.RawMessage(`{"action":"observe"}`), nil, companion)
	if err != nil || len(res.VisionData) == 0 {
		return desktopVerdict{}, nil, false
	}
	images := []llmadapter.Image{{MIME: res.VisionMIME, Data: res.VisionData}}
	raw, err := e.completeVisionJSON(ctx, exec, images, desktopVerifierSystem, desktopVerifierUserPrompt(goal, claim), 120)
	if err != nil {
		return desktopVerdict{}, images, false
	}
	v, ok := parseDesktopVerdict(raw)
	if !ok {
		return desktopVerdict{}, images, false
	}
	return v, images, true
}

func desktopVerdictThinking(v desktopVerdict) string {
	label := map[string]string{
		verdictDone:    "目标达成",
		verdictNotDone: "未完成",
		verdictBlocked: "被阻塞",
		verdictUnclear: "无法判定",
	}[v.Verdict]
	if v.Reason != "" {
		return fmt.Sprintf("屏幕核验：%s — %s\n", label, v.Reason)
	}
	return fmt.Sprintf("屏幕核验：%s\n", label)
}
