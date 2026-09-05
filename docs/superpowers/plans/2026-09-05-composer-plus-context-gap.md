# 工作台加号栏 · v3 缺口关闭 Implementation Plan

> **For agentic workers:** Execute task-by-task. Do not commit unless the user asks. Do not bump `VERSION`.

**Goal:** 把已落地的 v2 加号栏从「单测绿、真机仍可能翻车」补到规格综合 **4.9/5**：原生对话框兜底、文件夹过滤诚实、跳过点名、隐藏 input 取消静默、`attachment.get` 处理器测到 JPEG 头。

**Architecture:** 不改 Host/Engine 边界。`desktop.files.pick` 仍只写 Host allowlist；`readChunk` 仍只读 allowlist。本计划只改 Windows 对话框实现、pick 结果语义、前端对空结果的分支。

**Tech Stack:** Go `go test` 指定包；前端 `npx vitest run` 指定文件。PowerShell 用 `;` 不用 `&&`。

**Spec:** [docs/superpowers/specs/2026-09-05-composer-plus-context-prd.md](../specs/2026-09-05-composer-plus-context-prd.md)（v3 勘误 §11）。

**已完成（不要重做）：** schema、allowlist、图片 parse、`contentBase64` 预览、选择器三态、`CHAT_ASK_FOLLOWUP_FAILED`、工作台/启动页 Host pick 接线。对照旧计划 [2026-09-05-composer-plus-context.md](./2026-09-05-composer-plus-context.md)。

## Global Constraints

- 不新 daemon；不改 VERSION / 不打包
- 不改 poison / TTS / 玉盘 / 星尘 / 会议 / 月伴 / People 传文件
- 不重写 `SessionPage` 整页
- 不调用 `people.file.pick`，不 `import` `internal/people`
- 不把 Engine 改成按路径读文件
- 隐藏 `<input type=file>` 只作 jsdom / `DESKTOP_PICK_UNAVAILABLE` 降级

## File map

| File | Responsibility |
|---|---|
| `internal/desktopfiles/pick_windows.go` | WinForms 失败后走 `GetOpenFileNameW` / `SHBrowseForFolderW` |
| `internal/desktopfiles/handler.go` | 文件夹过滤后为空 + `skipped` |
| `internal/desktopfiles/handler_test.go` | 空文件夹 ≠ cancel；skipped 点名 |
| `api/bridge/v1/desktop.files.pick.schema.json` | 可选 `skipped` |
| `web/src/session/composerPlusPick.ts` | 文件夹空 / 文件空 分码 |
| `web/src/session/SessionPage.tsx` | `uploadBatch` 空列表不再误报 |
| `internal/app/attachment_handlers_test.go` | `attachment.get` 还原 JPEG 头 |

---

## Task 1: 文件夹过滤后为空不是对话框失败

**Files:**
- Modify: `internal/desktopfiles/handler.go`
- Modify: `internal/desktopfiles/handler_test.go`
- Modify: `api/bridge/v1/desktop.files.pick.schema.json`
- Modify: `web/src/session/composerPlusPick.ts`
- Modify: `web/src/session/composerPlusPick.test.ts`
- Modify: `web/src/session/SessionPage.tsx`

### Step 1: 红灯 — Host 文件夹只含 exe

在 `handler_test.go` 追加：

```go
func TestPickFolderWithOnlyExeIsNotDialogFailure(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "setup.exe"), []byte("MZ"), 0600); err != nil {
		t.Fatal(err)
	}
	h := New()
	h.Pick = func(folder, multiple bool) ([]Item, error) {
		if !folder {
			t.Fatal("expected folder pick")
		}
		return listFolder(dir)
	}
	r := h.HandleHost(context.Background(), testReq("desktop.files.pick", `{"folder":true}`))
	if !r.OK {
		t.Fatalf("filtered-empty folder must be ok: %#v", r)
	}
	raw, _ := json.Marshal(r.Payload)
	var out struct {
		Canceled bool     `json:"canceled"`
		Items    []any    `json:"items"`
		Skipped  []string `json:"skipped"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatal(err)
	}
	if out.Canceled || len(out.Items) != 0 {
		t.Fatalf("payload = %s", raw)
	}
	if len(out.Skipped) != 1 || out.Skipped[0] != "setup.exe" {
		t.Fatalf("skipped = %#v", out.Skipped)
	}
}
```

### Step 2: 跑红灯

```powershell
go test -count=1 ./internal/desktopfiles -run TestPickFolderWithOnlyExeIsNotDialogFailure
```

Expected: FAIL（现在 `listFolder` 丢掉 exe 且不回 `skipped`；前端会把它当成 `DESKTOP_PICK_FAILED`）。

### Step 3: 最小实现

- `listFolder` 对非白名单普通文件记下 `fileName`，随 pick 结果返回 `skipped`。
- schema `x-result.properties.skipped`：`{ "type":"array","items":{"type":"string","maxLength":256},"maxItems":20 }`。
- `composerPlusPick.ts`：`folder===true && !canceled && items.length===0` → error code `DESKTOP_FOLDER_EMPTY`，文案「这个文件夹里没有可导入的文件。」若有 skipped，拼进 message。
- 文件模式空 items 仍是 `DESKTOP_PICK_FAILED`。
- `SessionPage.uploadBatch`：`files.length===0` **不要** `setError`。空且非取消只由 `pickComposerFiles` 决定。

### Step 4: 前端红灯

```ts
test('folder pick with zero whitelist files is DESKTOP_FOLDER_EMPTY', async () => {
  const bridge: DesktopFilesBridge = {
    pick: vi.fn().mockResolvedValue({canceled: false, items: [], skipped: ['setup.exe']}),
    readChunk: vi.fn(),
  }
  const result = await pickComposerFiles(bridge, true)
  expect(result.kind).toBe('error')
  if (result.kind === 'error') {
    expect(result.error.code).toBe('DESKTOP_FOLDER_EMPTY')
    expect(result.error.message).toContain('没有可导入的文件')
    expect(result.error.message).toContain('setup.exe')
  }
})
```

```powershell
npx vitest run src/session/composerPlusPick.test.ts
```

### Step 5: 绿灯

```powershell
go test -count=1 ./internal/desktopfiles ./internal/contract
npm --prefix web run generate:bridge
npx vitest run src/session/composerPlusPick.test.ts src/session/SessionPage.runtime.test.tsx
```

Expected: PASS。`closes the attachment menu as soon as the file picker is opened` 仍绿。

---

## Task 2: Windows 原生对话框兜底

**Files:**
- Modify: `internal/desktopfiles/pick_windows.go`
- Create: `internal/desktopfiles/pick_native_windows.go`（COM 对话框，不 import people）
- Modify: `internal/desktopfiles/pick_windows.go` 的 `pickOS`

### Step 1: 红灯 — PowerShell 不可用时不得直接 Unavailable

在 `handler_test.go`（或 `pick_windows_test.go`，`//go:build windows`）测注入：

```go
func TestPickFallsBackWhenFormsUnavailable(t *testing.T) {
	h := New()
	var usedNative bool
	h.Pick = func(folder, multiple bool) ([]Item, error) {
		// 生产路径：forms 返回 ErrUnavailable 后必须再试 native。
		usedNative = true
		return nil, ErrCanceled
	}
	r := h.HandleHost(context.Background(), testReq("desktop.files.pick", `{"multiple":true}`))
	if !r.OK || !usedNative {
		t.Fatalf("native fallback not reached: %#v", r)
	}
}
```

现码 `pickOS` 在 PowerShell `err != nil` 时直接 `return nil, ErrUnavailable`，没有第二路。把可测缝挂在 `pickOS`：先 `pickForms`，`ErrUnavailable` 才 `pickNative`。

更稳的测法：抽

```go
var pickForms = pickFormsOS
var pickNative = pickNativeOS

func pickOS(folder, multiple bool) ([]Item, error) {
	items, err := pickForms(folder, multiple)
	if err == nil || errors.Is(err, ErrCanceled) {
		return items, err
	}
	return pickNative(folder, multiple)
}
```

单测替换 `pickForms` 返回 `ErrUnavailable`，`pickNative` 返回一个 temp 文件 item。

### Step 2: 跑红灯

```powershell
go test -count=1 ./internal/desktopfiles -run TestPickFallsBackWhenFormsUnavailable
```

Expected: FAIL（没有 fallback 钩子）。

### Step 3: 最小实现

从 `internal/people/pick_windows.go` **抄** `GetOpenFileNameW`（多选：`OFN_ALLOWMULTISELECT | OFN_EXPLORER`，缓冲解析连续 `\0` 路径）和 `SHBrowseForFolderW`。禁止 import people。

多选 native 结果再走现有 `itemFromPath`。文件夹 native 只返回目录路径，再 `listFolder`。

`pickForms` 保持现有 PowerShell 脚本。PowerShell `err != nil` 且不是空输出（空输出仍是取消）→ `ErrUnavailable` → native。

### Step 4: 绿灯

```powershell
go test -count=1 ./internal/desktopfiles ./internal/people
```

Expected: PASS。People 测试不得被改。

---

## Task 3: `attachment.get` 处理器必须能还原 JPEG 头

**Files:**
- Modify: `internal/app/attachment_handlers_test.go`（或该包里已有 get 测试的文件）

### Step 1: 红灯

入库 2KB 级 `image/jpeg`（头 `FF D8 FF`），经 **Engine `attachment.get` handler**（不是只调 `PreviewWorkspaceImage`）：

```go
// 断言 result["contentBase64"] 解码后 data[0:3] == []byte{0xff,0xd8,0xff}
// parseStatus == succeeded，parseErrorCode == ""
```

### Step 2: 跑红灯后补 handler 接线（若已接线则应已绿）

```powershell
go test -count=1 ./internal/app -run 'Get.*JPEG|AttachmentGet.*Vision|Preview'
```

若 handler 已写 `PreviewAttachmentImage`，此测试应一次绿。若红，只补 `handleAttachmentGet`，不要改视觉上限。

---

## Task 4: 回归 + 人工清单

### Step 1: 自动

```powershell
go test -count=1 ./internal/desktopfiles ./internal/attachmentapp ./internal/app ./internal/contract ./cmd/desktop
cd web
npx vitest run src/session/composerPlusPick.test.ts src/session/composerPickerState.test.ts src/session/SessionPage.runtime.test.tsx src/session/SessionPage.conversationExperts.test.tsx src/workspace/Workspace.test.tsx src/App.test.tsx
cd ..
npm run verify:bridge
```

Expected: 全绿。

### Step 2: 人工 3 分钟（Windows 安装包，缺一条不得自称 4.9）

1. 工作台加号 → 附件 / 文件 → **看见系统多选框（可被任务栏点到，不是毫无反应）** → 选小 JPEG → 作曲条缩略图 → 工作区能看见图，解析行是「图片 · 可供视觉分析」。
2. 加号 → 上传文件夹 → **看见系统文件夹框** → 含 `notes.txt` + `setup.exe` 的目录 → txt 入库，界面出现 `setup.exe`（不支持的类型），**不是**「系统没打开文件框」。
3. 只含 exe 的文件夹 → 「这个文件夹里没有可导入的文件。」
4. 留着「模型结果不完整」气泡，不点横幅重试 → 加号 → 选技能有列表；选专家能勾；@ 能插入已入库图片名。
5. 启动页加号 → Files → 同样是系统框。

不提版本号，除非用户另说。
