# 月伴执行收口 + 火山定稿/插话 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking. Do not commit unless the user asks.

**Goal:** 语音执行句必须挂对工具并在「稍等」之后口播结果；火山认全句、不抢答、说话中可插话；打字与语音桌面动手同档。落地后对照规格综合 **4.9/5**。

**Architecture:** 不新开工具运行时。闲聊门改为「针 ∪ R1–R4」（**不含 R0 / unspecified**）。`runStream` 对已挂表的承诺句续轮 `wait`，三次仍不调则补「无法执行」。火山只改 `DefaultEndWindowMS=1200`。插话只打开已有 `considerBargeIn`。桌面预批把 `computer.act`/`desktop.type` 并进站立 CC。GUI SoM 兜底不动。

**Tech Stack:** Go `go test ./internal/app ./internal/voice/volcsauc`；前端 vitest：`companionSettings` / `companionLights` / `asrPath` / `voicePersonas`。

**Spec:** [docs/superpowers/specs/2026-09-05-companion-execute-followthrough-prd.md](../specs/2026-09-05-companion-execute-followthrough-prd.md) **第二部分（§0–§10）**。第一部分「星尘」已定稿，进产品走规格 V3–V4，**另开任务 / 另开提交**。本计划仍然 **不要** 做粒子月亮、不要改 `MoonSphere`/`Aurora`、不要加 `visualSkin`（那是视觉轨的事）。

## Global Constraints

- 不新 daemon；内核 SQLite；不改 VERSION / `v0.4.63`
- 不改 poison / 月伴 TTS 音色 / 专家表 / `CompanionStage.tsx` 视觉 / `SessionPage.tsx`
- 不接线 `companionOpeningAck`
- 不重写 `AnnotateCapture` / `assignNodeIDs` / GUI 兜底规则
- 云端/本机仍只按钮打断；不改 `speech.ts` 280/220
- 月伴仍剥 `command.run` / `im.send` / `cc.*`；不删现有 `browser.act` 广告
- 电脑控制不自开；R0 / 无针 unspecified 问候零工具
- 同一人不同时改 `SessionPage.tsx` 与 `CompanionStage.tsx` 视觉（本计划两者都不改视觉）
- 不在日常账号跑 `Test-Install.ps1`；不重打包进 0.4.63

## File map

| File | Responsibility |
|---|---|
| `internal/app/chat_companion_speech.go` | `companionWantsTools` + R1–R4；搜索失败口播 |
| `internal/app/chat_companion_fastpath_test.go` | 翻转天气闲聊旧测 |
| `internal/app/chat_intent.go` | `looksLikeCompanionWaitPromise` |
| `internal/app/chat_continue.go` | `pickTurnContinueKind` + `toolsAttached` + `wait` |
| `internal/app/chat_continue_test.go` | F-A3/A4/A5/A7 + 旧调用补参 |
| `internal/app/chat_run_stream.go` | 传入 `len(req.Tools)>0`；`wait` 催词；满次无法执行 |
| `internal/app/companion_parity_test.go` | 打字/语音允许集 |
| `internal/app/approval_profile.go` | 站立预批点/填 |
| `internal/app/approval_profile_test.go` | 翻转旧测 |
| `internal/voice/volcsauc/config.go` | `DefaultEndWindowMS=1200` |
| `internal/voice/volcsauc/config_test.go` | 新 F-C1（仓库原先无 400 断言） |
| `web/src/session/companion/companionSettings.ts` | 仅 volc barge-in |
| `web/src/session/companion/companionLights.ts` | 火山灯条 |
| `web/src/session/companion/asrPath.ts` | 火山路径文案 |
| `web/src/session/companion/voicePersonas.ts` | 火山卡 desc |
| `web/src/settings/VoicePathPicker.tsx` | 设置页火山说明 |

不改：`gui_fallback.go`、`speech.ts`、`sherpa.go`、`CompanionStage.tsx`、`SessionPage.tsx`、`companion_context.go`（A2 只验证）。

PowerShell 里用 `;` 连接命令，不要用 `&&`。

---

## Task 1: 闲聊门 — 针 ∪ R1–R4（F-A1 / F-A2）

**Files:** `internal/app/chat_companion_speech.go`, `internal/app/chat_companion_fastpath_test.go`

- [ ] 打开 `internal/app/chat_companion_fastpath_test.go`。把 `TestCompanionWantsTools` 里：

```go
	if companionWantsTools("今晚天气") {
		t.Fatal("bare weather chat must stay idle")
	}
```

改成：

```go
	if !companionWantsTools("今晚天气") {
		t.Fatal("今晚天气 is R1 and must attach tools")
	}
	if !companionWantsTools("今天合肥的天气怎么样") {
		t.Fatal("info question must attach tools")
	}
	if companionWantsTools("今晚月色如何") || companionWantsTools("你好") || companionWantsTools("我随便说说") {
		t.Fatal("idle / R0 must stay tool-less")
	}
```

保留原有「查一下天气 / 打开网页 / 帮我做手册解析」等正例。不要动 `TestCompanionTaskWorkflowInjectionSkipsIdle`（`chat_companion_office_test.go`）。

- [ ] 改 `TestCompanionFastPathCapsTokensAndKeepsVoice`（payload 仍是 `今晚天气`）。**删除**这两段反向断言：

```go
	if strings.Contains(system, "不要 web.fetch") {
		t.Fatalf("idle weather talk must not ship the tools weather clause: %q", system)
	}
	if len(req.Tools) != 0 {
		t.Fatalf("idle companion attached tools: %#v", req.Tools)
	}
```

换成：

```go
	if !strings.Contains(system, "不要只说等一下就停") {
		t.Fatalf("companion tools instruction missing: %q", system)
	}
	foundSearch := false
	for _, def := range req.Tools {
		if def.Name == "web.search" {
			foundSearch = true
		}
	}
	if !foundSearch {
		t.Fatalf("R1 weather must attach web.search: %#v", req.Tools)
	}
```

闲聊身份/禁止工作流/「不要原样复读 / 第一句 / 月汐」保持。**删除**「可用技能目录」反向断言：`wantsTools==true` 时 `chat.go` 不再把 catalog 清空，天气回合允许出现技能目录。

- [ ] 跑红测（预期 FAIL）：

```powershell
go test ./internal/app -run "TestCompanionWantsTools|TestCompanionFastPathCapsTokensAndKeepsVoice" -count=1
```

- [ ] 在 `companionWantsTools` 针循环之后、`return len(m8app.ConversationExpertsMatchingIntent(text)) > 0` 之前插入：

```go
	switch detectTaskRoute(text) {
	case RouteR1, RouteR2, RouteR3, RouteR4:
		return true
	}
	return len(m8app.ConversationExpertsMatchingIntent(text)) > 0
```

禁止写成 `route != RouteUnspecified`。不要加「天气」针。

- [ ] 同一 `-run` 必须绿。再跑：

```powershell
go test ./internal/app -run "TestCompanionWantsTools|TestCompanionFastPath|TestCompanionAttachesFullToolset|TestClassifyTaskRoute|TestSection8" -count=1
```

`TestCompanionAttachesFullToolset`（打开网页 / R3）必须仍绿。

- [ ] Commit only if the user asks: `Open companion tools for R1–R4 info and desktop turns.`

---

## Task 2: 承诺句续轮 `wait` + 满次无法执行（F-A3 / F-A4 / F-A5 / F-A7）

**Files:** `internal/app/chat_intent.go`, `internal/app/chat_continue.go`, `internal/app/chat_continue_test.go`, `internal/app/chat_run_stream.go`

- [ ] 在 `chat_intent.go` 的 `isCompanionLeadInOnly` 正上方加（`strings` 已导入）：

```go
func looksLikeCompanionWaitPromise(text string) bool {
	t := strings.TrimSpace(text)
	if t == "" || strings.Contains(t, "无法执行") {
		return false
	}
	if strings.Contains(t, "已经打开") || strings.Contains(t, "已经写入") {
		return false
	}
	for _, n := range []string{"稍等", "等一下", "帮你查", "我去查", "我来做", "我来执行"} {
		if strings.Contains(t, n) {
			return true
		}
	}
	return strings.Contains(t, "手头没有") && strings.Contains(t, "查")
}
```

空串不走 promise（由 `isCompanionLeadInOnly` 管）。不要用数字+气温排除。

- [ ] 把 `pickTurnContinueKind` 签名改成末尾多一个 `toolsAttached bool`。在 `leadin` 判断之前插入：

```go
	if companion && !usedTools && toolsAttached && nudges < maxContinueNudges &&
		(looksLikeCompanionWaitPromise(assistantAll) || isCompanionLeadInOnly(assistantAll)) {
		return "wait"
	}
```

完整函数头：

```go
func pickTurnContinueKind(stepText, assistantAll, toolOut string, lastTools []string, usedTools, usedDesktop, companion, disableReasoning bool, nudges int, userGoal string, toolsAttached bool) string {
```

顺序保持：ask → incomplete → open-only 停 → desktop → **wait** → leadin → `""`。

- [ ] 更新 `chat_continue_test.go` 里仅有的 4 个旧调用，最后都加 `true`（这些用例已 `usedTools`，wait 不会误触发）：

```go
	if got := pickTurnContinueKind("已经打开了。", "已经打开了。", "opened C:\\\\x\\\\汽水音乐.lnk", []string{"desktop.open"}, true, true, true, true, 0, "打开汽水", true); got != "" {
		t.Fatalf("open-only must not return desktop, got %q", got)
	}
	if got := pickTurnContinueKind("好，我来操作电脑。", "好，我来操作电脑。", "screenshot frameId=01ARZ3NDEKTSV4RRFFQ69G5FAV", []string{"computer.act"}, true, true, true, true, 0, "", true); got != "desktop" {
		t.Fatalf("screenshot + lead-in must keep desktop loop, got %q", got)
	}
	if got := pickTurnContinueKind("好，我帮你查一下。", "好，我帮你查一下。", "ok", []string{"web.search"}, true, false, true, true, 0, "", true); got != "leadin" {
		t.Fatalf("non-desktop lead-in must ask for a spoken result, got %q", got)
	}
	if got := pickTurnContinueKind("Word 里已经写上号码了。", "Word 里已经写上号码了。", `typed "204040"`, []string{"desktop.type"}, true, true, true, true, 0, "", true); got != "" {
		t.Fatalf("settled desktop result must stop, got %q", got)
	}
```

在同一测试函数末尾追加：

```go
	long := "合肥今天的天气我手头没有实时数据，没法给你准确温度。你要是不急，我可以帮你查一下，稍等。"
	if got := pickTurnContinueKind("", long, "", nil, false, false, true, true, 0, "今天合肥的天气怎么样", true); got != "wait" {
		t.Fatalf("long wait promise must continue, got %q", got)
	}
	if got := pickTurnContinueKind("", "稍等。", "", nil, false, false, true, true, 0, "你好", false); got != "" {
		t.Fatal("idle wait without tools must not loop")
	}
	if got := pickTurnContinueKind("", "手头没有那本书。", "", nil, false, false, true, true, 0, "今晚月色如何", true); got != "" {
		t.Fatal("book-missing chat must not wait")
	}
	if got := pickTurnContinueKind("", long, "", nil, false, false, true, true, 3, "今天合肥的天气怎么样", true); got != "" {
		t.Fatal("wait budget exhausted must stop kind")
	}
	if got := pickTurnContinueKind("", "", "", nil, false, false, true, true, 0, "今天合肥的天气怎么样", true); got != "wait" {
		t.Fatal("empty lead-in with tools attached must wait")
	}
```

- [ ] `chat_run_stream.go` 约 352 行改成：

```go
					continueKind := pickTurnContinueKind(stepText, assistantText.String(), toolOut, turn.LastTools, usedTools, usedDesktopTools, state.companion, req.DisableReasoning, nudges, turn.Goal, len(req.Tools) > 0)
```

`switch continueKind` 在 `case "incomplete":` 旁加：

```go
						case "wait":
							nudge = gateway.Message{Role: gateway.RoleSystem, Content: "立刻调用本轮已装备的工具执行。不要再承诺稍等。下一句必须是结果或无法执行。"}
```

在 `if continueKind != "" { ... continue }` 之后、`usedTools && isCompanionLeadInOnly` 合成句之前，插入满次收口（只在 companion、未用工具、已挂表、看起来像承诺时）：

```go
					if continueKind == "" && state.companion && !usedTools && len(req.Tools) > 0 &&
						(looksLikeCompanionWaitPromise(assistantText.String()) || isCompanionLeadInOnly(assistantText.String())) {
						close := "无法执行：这一轮没有完成查询。"
						assistantText.WriteString(close)
						if err := sendDeltaChunks(send, close); err != nil {
							return err
						}
						break
					}
```

- [ ] 跑：

```powershell
go test ./internal/app -run "TestShouldContinueDesktopTurn|TestCompanionWantsTools" -count=1
```

（`pickTurnContinueKind` 的断言在 `TestShouldContinueDesktopTurn` 里。）必须绿。再 `go test ./internal/app -count=1`。

- [ ] Commit only if asked: `Continue companion turns that promised to act but never called a tool.`

---

## Task 3: 搜索失败口播（F-A6）

**Files:** `internal/app/chat_companion_speech.go`, `internal/app/chat_companion_fastpath_test.go`

- [ ] 在 `TestCompanionToolLeadIn`（`chat_companion_fastpath_test.go` 约 353 行）末尾、最后一个 `filesystem.write` 断言之后加：

```go
	if got := companionToolResultSpeech("web.search", "ok:false"); !strings.Contains(got, "无法执行") {
		t.Fatalf("failed search must say 无法执行, got %q", got)
	}
	if got := companionToolResultSpeech("web.fetch", "ok:false\ntimeout"); !strings.Contains(got, "无法执行") {
		t.Fatalf("failed fetch must say 无法执行, got %q", got)
	}
```

- [ ] 跑红测：`go test ./internal/app -run TestCompanionToolLeadIn -count=1`

- [ ] 改 `companionToolResultSpeech`：在 `companionToolResultFailed(out)` 分支里，`return "这次没有完成。"` **之前**：

```go
		if name == "web.search" || name == "web.fetch" {
			return "无法执行：这次没有查到。"
		}
```

已含「无法执行」的 `out` 仍走上面的 `if strings.Contains(out, "无法执行")` 原路。

- [ ] `go test ./internal/app -run "Speech|CompanionWantsTools|CompanionFastPath" -count=1` 绿。
- [ ] Commit only if asked: `Speak 无法执行 when companion web search fails.`

---

## Task 4: 打字=语音桌面预批（F-E1–E4）

**Files:** `internal/app/approval_profile.go`, `internal/app/approval_profile_test.go`, create `internal/app/companion_parity_test.go`

- [ ] 把 `approval_profile_test.go` 第 38–40 行：

```go
	if companionToolPreapproved("desktop.type", false, true) || companionToolPreapproved("cc.mouse_click", false, true) || companionToolPreapproved("computer.act", false, true) {
		t.Fatal("pixel/keyboard desktop tools must still confirm even with CC on")
	}
```

改成：

```go
	if !companionToolPreapproved("desktop.type", false, true) || !companionToolPreapproved("computer.act", false, true) {
		t.Fatal("CC on must preapprove desktop.type and computer.act")
	}
	if companionToolPreapproved("desktop.type", false, false) || companionToolPreapproved("computer.act", false, false) {
		t.Fatal("CC off must not preapprove click/type")
	}
	if companionToolPreapproved("cc.mouse_click", false, true) {
		t.Fatal("raw cc.* stays gated")
	}
```

- [ ] 新建 `internal/app/companion_parity_test.go`：

```go
package app

import (
	"testing"

	"github.com/lunitide/lunitide/internal/gateway"
)

func TestCompanionParityDesktopAllow(t *testing.T) {
	defs := []gateway.ToolDefinition{
		{Name: "desktop.open"}, {Name: "desktop.type"}, {Name: "computer.act"},
		{Name: "command.run"}, {Name: "im.send"}, {Name: "user.ask"}, {Name: "web.search"},
	}
	has := func(list []gateway.ToolDefinition, name string) bool {
		for _, d := range list {
			if d.Name == name {
				return true
			}
		}
		return false
	}
	for _, goal := range []string{"帮我打开桌面道具", "打开记事本"} {
		voice := assembleRoutedTools(defs, goal, true, false)
		typed := assembleRoutedTools(defs, goal, false, false)
		if !has(voice, "desktop.open") || !has(typed, "desktop.open") {
			t.Fatalf("%q must allow desktop.open on both lanes", goal)
		}
		if has(voice, "command.run") {
			t.Fatalf("companion must strip command.run for %q", goal)
		}
	}
	offV := assembleRoutedTools(defs, "打开记事本", true, false)
	offT := assembleRoutedTools(defs, "打开记事本", false, false)
	if has(offV, "computer.act") || has(offT, "computer.act") {
		t.Fatal("ccOff must not ship computer.act")
	}
	onV := assembleRoutedTools(defs, "打开记事本", true, true)
	onT := assembleRoutedTools(defs, "打开记事本", false, true)
	if !has(onV, "computer.act") || !has(onT, "computer.act") {
		t.Fatal("ccOn R2 must ship computer.act on both lanes")
	}
	if companionGoalIsOpenOnly("打开记事本并写你好") {
		t.Fatal("open+type is not open-only")
	}
	if !companionGoalIsOpenOnly("打开记事本") {
		t.Fatal("plain open is open-only")
	}
}
```

- [ ] 跑红测：`go test ./internal/app -run "TestApprovalProfileDangerous|TestCompanionParityDesktopAllow" -count=1`

- [ ] 改 `ccStandingApprovedTool` 并更新注释（删掉「不能点/填」）：

```go
func ccStandingApprovedTool(name string) bool {
	switch strings.TrimSpace(name) {
	case "desktop.open", "media.play", "desktop.type", "computer.act":
		return true
	default:
		return false
	}
}
```

- [ ] 同一 `-run` 绿。
- [ ] Commit only if asked: `Preapprove companion desktop click and type when computer control is on.`

---

## Task 5: 火山等待窗 1200ms（F-C1）

**Files:** `internal/voice/volcsauc/config.go`, create `internal/voice/volcsauc/config_test.go`

- [ ] 新建 `config_test.go`（先红）：

```go
package volcsauc

import "testing"

func TestDefaultEndWindowMSMatchesSherpa(t *testing.T) {
	if DefaultEndWindowMS != 1200 {
		t.Fatalf("DefaultEndWindowMS=%d, want 1200", DefaultEndWindowMS)
	}
	cfg := Config{}
	req, _ := fullClientRequest(cfg)["request"].(map[string]any)
	if req["end_window_size"] != 1200 {
		t.Fatalf("end_window_size=%v", req["end_window_size"])
	}
}
```

注意：`fullClientRequest` 若未导出，把测试放在同一 package（已是 `package volcsauc`）即可。`end_window_size` 的 JSON 数字可能是 `int`；用 `%v` 比较 `1200`。若类型是 `int` 而字面量比较失败，改成：

```go
	n, ok := req["end_window_size"].(int)
	if !ok || n != 1200 {
		t.Fatalf("end_window_size=%T %v", req["end_window_size"], req["end_window_size"])
	}
```

- [ ] `go test ./internal/voice/volcsauc -run TestDefaultEndWindowMSMatchesSherpa -count=1` — 红。
- [ ] `config.go`：`DefaultEndWindowMS = 1200`。注释改为「对齐 sherpa 1.2s 说完再答；零值走 default」。`EndWindowMS` 字段注释把「产品默认 400」改成 1200。
- [ ] **不要改** `web/src/session/companion/volc/volcSpeech.test.ts`「commits a complete greeting within 400ms」。
- [ ] `go test ./internal/voice/volcsauc -count=1` 绿。
- [ ] Commit only if asked: `Align Volc SAUC end window with sherpa 1.2s endpointing.`

---

## Task 6: 仅火山插话打断（F-D1 / F-D2）

> **现码冲突（2026-09-05 复核）：** `companionVoiceBargeInEnabled` 现在是 `return false`，文件头写「client-side barge-in is retired on every path」防回声。用户锁仍要火山 cascade 说话中可插话。本任务是**有限恢复**：只对 `voicePath==='volc'` 返回 true；`acceptBargeIn` 里已有的 `looksLikePlaybackEcho` **必须保持**。不要删 echo 过滤，不要给 cloud/local 开麦。和星尘 `visualSkin` **不要同一提交**。

**Files:**  
`web/src/session/companion/companionSettings.ts`  
`web/src/session/companion/companionSettings.test.ts`  
`web/src/session/companion/companionLights.ts`  
`web/src/session/companion/companionLights.test.ts`  
`web/src/session/companion/asrPath.ts`  
`web/src/session/companion/asrPath.test.ts`  
`web/src/session/companion/voicePersonas.ts`  
`web/src/settings/VoicePathPicker.tsx`

- [ ] 把 `companionSettings.test.ts` 里 volc 期望改成 `true`：

```ts
    expect(companionVoiceBargeInEnabled(applyVoicePath(defaultCompanionSettings(), 'volc'))).toBe(true)
    expect(companionVoiceBargeInEnabled(applyVoicePath(defaultCompanionSettings(), 'cloud'))).toBe(false)
    expect(companionVoiceBargeInEnabled(applyVoicePath(defaultCompanionSettings(), 'local'))).toBe(false)
```

`local`/`cloud` + `voiceBargeIn: true` 仍 `false` 的两行保持。

- [ ] `companionLights.test.ts` 火山 ready 测：

```ts
    expect(report.lights[0]).toMatchObject({ label: '火山 seed-asr · 说完再答 · 可对着麦打断', state: 'on' })
```

- [ ] `asrPath.test.ts` 火山行：

```ts
    expect(companionAsrPathLabel('volc', 'auto')).toMatch(/可对着麦打断/)
```

云端行仍 `/打断用按钮/`。

- [ ] 在 `web/` 跑红测：

```powershell
npx vitest run src/session/companion/companionSettings.test.ts src/session/companion/companionLights.test.ts src/session/companion/asrPath.test.ts
```

- [ ] `companionSettings.ts`：

```ts
export function companionVoiceBargeInEnabled(settings: Pick<CompanionSettings, 'voicePath' | 'voiceBargeIn'>): boolean {
  return settings.voicePath === 'volc'
}
```

把函数上方注释里「火山 cascade (default): half-duplex, 打断 button only」改成：cascade 定稿仍说完再答；**仅 volc 说话中可对着麦打断**。`voiceBargeIn` 仍 inert。

- [ ] `companionLights.ts`：`listenLabel = '火山 seed-asr · 说完再答 · 可对着麦打断'`
- [ ] `asrPath.ts`：`return '火山听写 · seed-asr · 说完再答 · 可对着麦打断'`
- [ ] `voicePersonas.ts` 两段火山 `desc`：把「打断用按钮」改成「说话中可对着麦打断」，保留「通话核仍是设置里的全双工」含义。不要改云端/本机 desc。
- [ ] `web/src/settings/VoicePathPicker.tsx` 顶部说明里火山那句「打断用按钮」改成「说话中可对着麦打断」。云端/本机句保持。这是设置文案，不是 `CompanionStage.tsx` 视觉。`VoicePathPicker.test.tsx` 的 `/打断用按钮/` 仍会因云端/本机文案绿。
- [ ] 不要改 `CompanionStage.tsx`。
- [ ] 同一 vitest 命令再加 `src/settings/VoicePathPicker.test.tsx`，必须绿。`voicePersonas.test.ts` 云端卡仍匹配 `/打断用按钮/`。
- [ ] Commit only if asked: `Enable mic barge-in on Volc cascade only.`

---

## Task 7: 回归门禁

- [ ] 

```powershell
go test ./internal/app ./internal/voice/volcsauc ./internal/videounderstand -count=1
```

- [ ] 在 `web/`：

```powershell
npx vitest run src/session/companion src/settings/VoicePathPicker.test.tsx
```

- [ ] `git diff --name-only` 确认没有 `speech.ts`、`gui_fallback.go`、`CompanionStage.tsx`、`SessionPage.tsx`、`VERSION`。
- [ ] 对照规格 §5 勾 F-A1–A7、F-C1、F-D1–D2、F-E1–E4。F-C2 只确认现测仍绿。

---

## Task 8: 手工 15 分钟（下一构建，非 0.4.63）

现网 `Lunitide-Setup-0.4.63-x64.exe` **不含**本计划。装下一构建后：

三听写各一遍：

1. 「今天合肥的天气怎么样」→ 有搜索活动 → 口播结果或「无法执行」。禁止停在稍等。
2. 「帮我打开记事本」→ 打开成功口播。
3. 「今晚月色如何」→ 闲聊，无搜索条。

只火山：

4. 「帮我打开……桌面上的记事本」换气不足 1s → 整句字幕，不抢答。
5. 月汐说话时对着麦说「停」→ 打断；云端/本机同一操作不打断。

工作台打字 vs 月伴语音：「帮我打开桌面道具」都走 `desktop.open`；「打开记事本」在电脑控制开/关下点/填权限同档。

---

## Spec coverage

| Spec | Task |
|---|---|
| §3.1 执行收口 / R0 零工具 | 1, 2, 3 |
| §3.1.5–6 失败口播 / 满次无法执行 | 2, 3 |
| §3.2 火山 1200ms | 5 |
| §3.3 插话仅火山 + 三处文案 | 6 |
| §3.4 打字=语音 + 预批 | 1, 4 |
| §3.5 GUI 不新做 | 7 的「不改 gui_fallback」 |
| §5 测试锁 | 各 Task 红测 |
| §6 手工 | 8 |
| §9 勘误 | Task 1 禁止 R0；Task 5 新写 F-C1 |

## 4.9 口径（落地后）

综合 4.9：执行句必挂工具并收口；问候仍零工具；三次空等补无法执行；火山等 1.2s 再定稿；仅火山可插话；CC 开时语音点/填不卡批准。剩 0.1=真网延迟与火山服务端 VAD 不能与 sherpa 解码器逐帧对齐。
