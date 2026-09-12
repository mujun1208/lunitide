> **已由 V1.3.3 替代。** 不要按本文施工。按原文会把 `inbox`/`changed` 直接写入 SQLite（0153 CHECK 只允许 `event|scan|outside`），任务结束 Upsert 失败。最新计划：`lunitide-agent-hub-三场景-落地规格-v1.3.3.md`。

# Agent 调度台 V1.3.2 实施计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans 或 subagent-driven-development，按任务勾选执行。每步先红后绿。

**Goal:** 四张卡（做 PPT / 写新项目 / 改现有代码 / 其它任务）+ 工作台把本机文件/资料夹复制进 `.agenthub-inbox` 交给三家 CLI；捷径锁 Agent；自由模式仍可任选三家做任意事。

**Architecture:** 场景前缀只在前端拼。后端适配器不读 scene。传文件走新方法 `agentHub.inbox`（选/列/删），不改 `task.start`，不加迁移。扫描标 `inbox`。顺手把 `dir.pick`/`inbox` 的 Go 截止天花板改成 600s。

**Tech Stack:** 现网 `internal/agenthub` + `web/src/agentHub` + generate-bridge。

**Spec:** `lunitide-agent-hub-design-v2/lunitide-agent-hub-三场景-prd.md` **V1.3.2**

## Global Constraints

- 禁止 `commandworker.Run` / `command.run` / `fsnotify` / 手改生成物。
- 禁止改 `OfficeStudioPage.tsx` / `SessionPage.tsx` / `office.generate` / `token_ledger`。
- 禁止去掉 Codex `--ignore-user-config`。
- 禁止删掉自由选 Agent。
- 禁止把传文件做成「请自己去资源管理器拷」而不做按钮。
- 禁止复用 `desktop.files.pick` / `people.file.*` / `attachment.*`。
- 禁止移动或删除用户原文件。
- 字段 `taskId` 不是 `runId`；所有 `agentHub.*` 不进 `dataScopedMethod`。
- 新 schema 必须 `npm --prefix web run generate:bridge`，禁止手改 `web/src/generated/bridge.ts` / `internal/bridge/schema_generated.go`。
- 用户没要求不要 commit、不要改 VERSION、不要打包、不要推送。
- `go test` / vitest 通常需要完整权限；vitest 必须在 `web/` 下跑。
- PowerShell 用 `;` 不连接 `&&`。
- 不跑生产 `%LocalAppData%\Lunitide` 引擎。

---

## 文件地图

**新建**

- `api/bridge/v1/agentHub.inbox.schema.json`
- `internal/agenthub/inbox.go`
- `internal/agenthub/inbox_test.go`
- `internal/agenthub/pick_files_windows.go`
- `internal/agenthub/pick_files_other.go`
- `internal/agenthub/skills.go`
- `internal/agenthub/skills_test.go`

**改**

- `api/bridge/v1/envelope.schema.json`
- `web/scripts/generate-bridge.mjs`
- 生成物（只通过 generate:bridge）
- `internal/bridge/deadline.go`、`deadline_test.go`
- `web/src/bridge/client.ts`、`web/src/bridge/agentHub.deadline.test.ts`
- `internal/app/handlers_registry.go`、`agenthub_handlers.go`、`agenthub_handlers_test.go`
- `internal/agenthub/scan.go`、`scan_test.go`、`service.go`、`adapter.go`
- `internal/agenthub/kimi_test.go`、`cursor_test.go`、`codex_test.go`
- `web/src/agentHub/agentHubApi.ts`、`agentHubCopy.ts`、`AgentHubWorkbench.tsx`、`agentHub.css`、`AgentHubPage.test.tsx`
- `web/src/agentHub/AgentHubDetail.tsx`、`AgentHubArtifacts.tsx`

**不改**

- `0153` / 不加 `0154`、Office/Session 大页、token_ledger。

---

## Task 1: 扫描跳过依赖 + changed + inbox

**Files:** `internal/agenthub/scan.go`、`scan_test.go`、`service.go`

**接口：**

```go
func ScanWorkDir(workDir string, eventPaths []string, startedAt time.Time) []Artifact
```

`startedAt` 零值：不标 `changed`。路径落在 `.agenthub-inbox` 下则 `Source=inbox`，优先于 changed/scan。

常量（可放 `scan.go` 或与 inbox 共用，名称必须是 `.agenthub-inbox`）：

```go
const inboxDirName = ".agenthub-inbox"
```

### 步骤

- [ ] 把现有 `TestScanWorkDirCollectsInsideAndOutside` 的调用改成 `ScanWorkDir(root, []string{"hello.txt", outside}, time.Time{})`。
- [ ] 在 `scan_test.go` **追加**：

```go
func TestScanSkipsNodeModules(t *testing.T) {
	root := t.TempDir()
	hidden := filepath.Join(root, "node_modules")
	if err := os.MkdirAll(hidden, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(hidden, "x.js"), []byte("1"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "app.js"), []byte("2"), 0o644); err != nil {
		t.Fatal(err)
	}
	arts := ScanWorkDir(root, nil, time.Time{})
	for _, art := range arts {
		if strings.Contains(filepath.ToSlash(art.Path), "node_modules") {
			t.Fatalf("leaked %s", art.Path)
		}
	}
}

func TestScanMarksRecentAsChanged(t *testing.T) {
	root := t.TempDir()
	old := filepath.Join(root, "old.txt")
	if err := os.WriteFile(old, []byte("o"), 0o644); err != nil {
		t.Fatal(err)
	}
	past := time.Now().Add(-time.Hour)
	if err := os.Chtimes(old, past, past); err != nil {
		t.Fatal(err)
	}
	started := time.Now()
	if err := os.WriteFile(filepath.Join(root, "hello.txt"), []byte("n"), 0o644); err != nil {
		t.Fatal(err)
	}
	arts := ScanWorkDir(root, nil, started)
	got := map[string]string{}
	for _, art := range arts {
		got[art.Name] = art.Source
	}
	if got["hello.txt"] != "changed" {
		t.Fatalf("%v", got)
	}
	if got["old.txt"] != "scan" {
		t.Fatalf("old should stay scan: %v", got)
	}
}

func TestScanMarksInboxSource(t *testing.T) {
	root := t.TempDir()
	inbox := filepath.Join(root, inboxDirName)
	if err := os.MkdirAll(inbox, 0o755); err != nil {
		t.Fatal(err)
	}
	past := time.Now().Add(-time.Hour)
	target := filepath.Join(inbox, "note.pdf")
	if err := os.WriteFile(target, []byte("p"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(target, past, past); err != nil {
		t.Fatal(err)
	}
	arts := ScanWorkDir(root, nil, time.Now())
	for _, art := range arts {
		if art.Name == "note.pdf" && art.Source == "inbox" {
			return
		}
	}
	t.Fatalf("%+v", arts)
}
```

- [ ] `go test ./internal/agenthub/ -count=1 -run "TestScanSkipsNodeModules|TestScanMarksRecentAsChanged|TestScanMarksInboxSource"` — 必须红。
- [ ] 改 `scan.go`：`WalkDir` 若 `d.IsDir()` 且名称（`strings.EqualFold`）属于 `node_modules` `.git` `.hg` `.svn` `dist` `build` `out` `coverage` `.venv` `venv` `__pycache__` `.cursor` `.kimi-code` `.codex` `vendor` `.idea` `.vs` 则 `fs.SkipDir`。**不要**跳过 `.agenthub-inbox`。
- [ ] 非事件文件：相对 `workDir` 的第一段是 `inboxDirName` → `Source=inbox`；否则若 `!startedAt.IsZero()` 且 `info.ModTime().After(startedAt.Add(-2*time.Second))` → `changed`；否则 `scan`。
- [ ] `service.go` 两处 `ScanWorkDir` 改为传入 `parseRFC3339(task.StartedAt)`；空则零值。本地小函数，不要新包。
- [ ] `go test ./internal/agenthub/ -count=1` 全绿。

---

## Task 2: Inbox 复制 / 列出 / 删除（无对话框）

**Files:** `inbox.go`、`inbox_test.go`；`service.go` 增加 `PickFiles`/`PickFolder` 字段。

```go
type InboxFile struct {
	Name string
	Path string
	Size int64
}

func (s *Service) Inbox(action, workDir, name string) (canceled bool, dir string, files []InboxFile, skipped []string, err error)
```

### 步骤

- [ ] 写 `inbox_test.go`（先红）：

```go
func TestInboxCopiesRegularFileLeavesSource(t *testing.T) {
	s := New(NewMemoryStore(), t.TempDir(), nil)
	srcDir := t.TempDir()
	src := filepath.Join(srcDir, "note.txt")
	if err := os.WriteFile(src, []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	s.PickFiles = func() ([]string, error) { return []string{src}, nil }
	canceled, dir, files, skipped, err := s.Inbox("files", "", "")
	if err != nil || canceled || len(skipped) != 0 || len(files) != 1 {
		t.Fatalf("%v %v %v %v %v", canceled, dir, files, skipped, err)
	}
	body, err := os.ReadFile(src)
	if err != nil || string(body) != "hello" {
		t.Fatalf("source mutated: %q %v", body, err)
	}
	copyPath := filepath.Join(dir, inboxDirName, "note.txt")
	got, err := os.ReadFile(copyPath)
	if err != nil || string(got) != "hello" {
		t.Fatalf("copy: %q %v", got, err)
	}
}

func TestInboxRenamesCollision(t *testing.T) {
	s := New(NewMemoryStore(), t.TempDir(), nil)
	src := filepath.Join(t.TempDir(), "a.txt")
	if err := os.WriteFile(src, []byte("1"), 0o644); err != nil {
		t.Fatal(err)
	}
	s.PickFiles = func() ([]string, error) { return []string{src}, nil }
	_, dir, _, _, err := s.Inbox("files", "", "")
	if err != nil {
		t.Fatal(err)
	}
	_, _, files, _, err := s.Inbox("files", dir, "")
	if err != nil || len(files) != 2 {
		t.Fatalf("%v %v", files, err)
	}
	names := files[0].Path + "," + files[1].Path
	if !strings.Contains(names, "a.txt") || !strings.Contains(names, "a (2).txt") {
		t.Fatal(names)
	}
}

func TestInboxFolderSkipsNodeModules(t *testing.T) {
	s := New(NewMemoryStore(), t.TempDir(), nil)
	src := t.TempDir()
	if err := os.MkdirAll(filepath.Join(src, "node_modules"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "node_modules", "x.js"), []byte("1"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "keep.md"), []byte("k"), 0o644); err != nil {
		t.Fatal(err)
	}
	s.PickFolder = func() (string, error) { return src, nil }
	_, _, files, _, err := s.Inbox("folder", "", "")
	if err != nil || len(files) != 1 || files[0].Name != "keep.md" {
		t.Fatalf("%v %v", files, err)
	}
}

func TestInboxDropOnlyInsideInbox(t *testing.T) {
	s := New(NewMemoryStore(), t.TempDir(), nil)
	src := filepath.Join(t.TempDir(), "note.txt")
	if err := os.WriteFile(src, []byte("h"), 0o644); err != nil {
		t.Fatal(err)
	}
	s.PickFiles = func() ([]string, error) { return []string{src}, nil }
	_, dir, _, _, err := s.Inbox("files", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, _, _, err := s.Inbox("drop", dir, "../note.txt"); err == nil {
		t.Fatal("expected escape to fail")
	}
	_, _, files, _, err := s.Inbox("drop", dir, "note.txt")
	if err != nil || len(files) != 0 {
		t.Fatalf("%v %v", files, err)
	}
	if _, err := os.Stat(src); err != nil {
		t.Fatal("source must remain")
	}
}
```

- [ ] `go test ./internal/agenthub/ -count=1 -run TestInbox` — 必须红。
- [ ] 实现 `Inbox`：`files`/`folder` 走 `resolveWorkDir`；取消（`errors.Is(err, ErrPickCanceled)` 或空选择）返回 `canceled=true`。复制用 `io.Copy`，不 `Rename`。重名：`base (n).ext`。单文件 > 104857600 或累计 > 209715200 或超过 20 个 → skipped 中文。folder 遍历跳过 Task 1 同一组目录名。`drop` 用 `insideDir(filepath.Join(workDir, inboxDirName), abs)`。
- [ ] `go test ./internal/agenthub/ -count=1 -run TestInbox` 绿。

---

## Task 3: `agentHub.inbox` 契约 + handler + 10 分钟截止

**Files:** schema、envelope、generate-bridge.mjs、handlers、client、deadline。

### Schema 原文（新建 `api/bridge/v1/agentHub.inbox.schema.json`，LF）

```json
{
  "$schema": "https://json-schema.org/draft/2020-12/schema",
  "$id": "https://lunitide.local/schema/bridge/v1/agentHub.inbox.schema.json",
  "title": "agentHub.inbox payload",
  "x-method": "agentHub.inbox",
  "x-owner": "engine",
  "x-enabled": true,
  "type": "object",
  "additionalProperties": false,
  "properties": {
    "action": { "type": "string", "enum": ["files", "folder", "list", "drop"] },
    "workDir": { "type": "string", "maxLength": 1024 },
    "name": { "type": "string", "maxLength": 512 }
  },
  "required": ["action"],
  "x-result": {
    "type": "object",
    "additionalProperties": false,
    "properties": {
      "canceled": { "type": "boolean" },
      "workDir": { "type": "string", "maxLength": 1024 },
      "files": {
        "type": "array",
        "maxItems": 200,
        "items": {
          "type": "object",
          "additionalProperties": false,
          "properties": {
            "name": { "type": "string", "minLength": 1, "maxLength": 256 },
            "path": { "type": "string", "minLength": 1, "maxLength": 1024 },
            "size": { "type": "integer", "minimum": 0 }
          },
          "required": ["name", "path", "size"]
        }
      },
      "skipped": {
        "type": "array",
        "maxItems": 20,
        "items": { "type": "string", "minLength": 1, "maxLength": 256 }
      }
    },
    "required": ["canceled", "workDir", "files"]
  },
  "x-examples": {
    "positive": [
      { "action": "list", "workDir": "D:/work" },
      { "action": "files" },
      { "action": "drop", "workDir": "D:/work", "name": "note.txt" }
    ],
    "negative": [{}, { "action": "upload" }, { "extra": true }]
  }
}
```

### 步骤

- [ ] 写入上述 schema。
- [ ] `envelope.schema.json` 方法枚举在 `agentHub.file.preview` 后插入 `"agentHub.inbox",`。
- [ ] `generate-bridge.mjs` enabled 断言数组在 `'agentHub.file.preview',` 后插入 `'agentHub.inbox',`。
- [ ] `npm --prefix web run generate:bridge`（需要完整权限）。
- [ ] `internal/bridge/deadline.go` 增加常量并纳入 `MaxDeadlineMS`：

```go
AgentHubPickDeadlineMS = 600_000
```

```go
case MethodAgentHubDirPick, MethodAgentHubInbox:
    return AgentHubPickDeadlineMS
```

（生成后确认常量名；若生成器尚未跑完，先生成再改 deadline。）

- [ ] `deadline_test.go` 追加：

```go
if MaxDeadlineMS("agentHub.dir.pick") != 600_000 {
	t.Fatalf("dir.pick cap = %d", MaxDeadlineMS("agentHub.dir.pick"))
}
if MaxDeadlineMS("agentHub.inbox") != 600_000 {
	t.Fatalf("inbox cap = %d", MaxDeadlineMS("agentHub.inbox"))
}
```

- [ ] `client.ts` `capBridgeDeadlineMs`：`agentHub.dir.pick` **或** `agentHub.inbox` 都用 `AGENT_HUB_DIR_PICK_MS`。`createAgentHubBridge` 同样判断这两个方法。
- [ ] `agentHub.deadline.test.ts` 增加 inbox 断言。
- [ ] `handlers_registry.go` 增加 `bridge.Method("agentHub.inbox"): handleAgentHub`。
- [ ] `handleAgentHub` 增加 `case "agentHub.inbox"`：解码 `{Action, WorkDir, Name}`；`action` 不在枚举则 schema invalid；调用 `e.agentHub.Inbox`；`ErrPickCanceled` → `canceled=true` 且 `files=[]`。
- [ ] `agenthub_handlers_test.go`：用 `PickFiles` 注入后 `validRequest("agentHub.inbox", ...)` 应 Ok；并把 `"agentHub.inbox"` 加入 `dataScopedMethod` 禁止列表。
- [ ] `go test ./internal/bridge/ ./internal/app/ ./internal/agenthub/ -count=1` 与 `npx vitest run src/bridge/agentHub.deadline.test.ts`（在 `web/`）绿。

---

## Task 4: Kimi 短 argv + prompt 文件 + skills-dir

**Files:** `skills.go`、`skills_test.go`、`adapter.go`、`kimi_test.go`

短指令固定：

`请阅读并执行本目录 .agenthub-prompt.txt。只在本目录创建或修改文件。`

### 步骤

- [ ] 改 `TestKimiBuildCommand`：

```go
func TestKimiBuildCommand(t *testing.T) {
	dir := t.TempDir()
	exe, args, stdin, err := (kimiAdapter{}).BuildCommand(TaskRequest{Prompt: "写 hello.txt", WorkDir: dir})
	if err != nil || exe != "kimi" || len(stdin) != 0 {
		t.Fatalf("%s %v %q %v", exe, args, stdin, err)
	}
	joined := strings.Join(args, " ")
	if strings.Contains(joined, "写 hello.txt") {
		t.Fatalf("long prompt must not sit on argv: %v", args)
	}
	if !strings.Contains(joined, "-p") || !strings.Contains(joined, "stream-json") {
		t.Fatalf("%v", args)
	}
	body, err := os.ReadFile(filepath.Join(dir, promptFileName))
	if err != nil || !strings.Contains(string(body), "写 hello.txt") {
		t.Fatalf("%q %v", body, err)
	}
}
```

- [ ] `skills_test.go`：

```go
func TestKimiSkillDirsFindsSlides(t *testing.T) {
	root := t.TempDir()
	slides := filepath.Join(root, "kimi-slides")
	if err := os.MkdirAll(slides, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(slides, "SKILL.md"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	got := filterKimiSkillRoots([]string{root, filepath.Join(root, "missing")})
	if len(got) != 1 || got[0] != root {
		t.Fatalf("%v", got)
	}
}
```

- [ ] `go test ./internal/agenthub/ -count=1 -run "TestKimiBuildCommand|TestKimiSkillDirsFindsSlides"` 先红。
- [ ] `filterKimiSkillRoots`：候选是 skills 根，存在 `kimi-slides/SKILL.md` 则收录该根。`kimiSkillDirs()` 组装 `%APPDATA%\kimi-desktop\daimon-share\daimon\skills`、`%USERPROFILE%\.kimi-code\skills`、`KIMI_SKILLS_DIR`。
- [ ] `BuildCommand`：`WorkDir` 空则错误「工作目录无效」；写入 `promptFileName`；argv `-p` + 短指令 + `--output-format` + `stream-json` + 可选 `--skills-dir`。
- [ ] `go test ./internal/agenthub/ -count=1 -run "TestKimi|TestCursorBuildCommand|TestCodexBuildCommand"` 绿。

---

## Task 5: Cursor `--workspace`

**Files:** `adapter.go`、`cursor_test.go`

```go
func hasPair(args []string, flag, value string) bool {
	for i := 0; i+1 < len(args); i++ {
		if args[i] == flag && args[i+1] == value {
			return true
		}
	}
	return false
}
```

### 步骤

- [ ] `TestCursorBuildCommand` 增加 `hasPair(args, "--workspace", `D:\work`)`，先红。
- [ ] `BuildCommand`：

```go
return "cursor-agent", []string{"-p", "--force", "--trust", "--workspace", req.WorkDir, "--output-format", "stream-json"}, []byte(req.Prompt), nil
```

- [ ] 绿。`TestCodexBuildCommand` 必须仍是 `string(stdin)=="hi"` 且 argv 含 `--ignore-user-config`。

---

## Task 6: 工作台四卡 + 传文件 chips

**Files:** `agentHubCopy.ts`、`agentHubApi.ts`、`AgentHubWorkbench.tsx`、`agentHub.css`、`AgentHubPage.test.tsx`

```ts
export type HubScene = 'ppt' | 'write' | 'fix' | 'free'
export type InboxFile = { name: string; path: string; size: number }

export function scenePrefix(scene: HubScene, workDir: string, userText: string): string {
  if (scene === 'free') return userText
  if (scene === 'ppt') {
    return `【场景：做 PPT】\n工作目录：${workDir}\n只在本目录写文件。优先使用 kimi-slides。产出 pptx。参考文件在本目录或 .agenthub-inbox 时先读再做。\n\n用户任务：\n${userText}`
  }
  if (scene === 'write') {
    return `【场景：写新项目】\n项目根：${workDir}\n把该路径当作唯一项目根。只在该根下创建文件夹和文件。\n先读 AGENTS.md、.cursor/rules、README（没有则跳过）。\n用户传入的需求/设计在 .agenthub-inbox（没有则跳过）。\n用户写的规则优先。做完即结束。\n\n用户任务：\n${userText}`
  }
  return `【场景：改现有代码】\n项目根：${workDir}\n只阅读和修改这个根内的文件。先看 README、AGENTS.md 和相关源码。\n用户传入的说明/截图在 .agenthub-inbox（没有则跳过）。\n改动保持最小。不要 git 提交或推远程。做完即结束。\n\n用户任务：\n${userText}`
}

export function inboxPrefix(files: InboxFile[]): string {
  if (files.length === 0) return ''
  const lines = files.map(item => `- ${item.path}`)
  return `\n\n用户传入的文件已复制到 .agenthub-inbox/（原路径未改）。请先阅读。可以修改这些副本，或在本工作目录写出新文件。不要去改用户原路径上的文件。完成后把结果留在本目录。\n${lines.join('\n')}`
}

export const FREE_TEMPLATES = [
  { zh: '写文档', prompt: '根据本工作目录已有材料写一份 Markdown 说明，只在本目录保存。' },
  { zh: '总结本目录', prompt: '阅读本工作目录里的资料（含 .agenthub-inbox 与 pdf/ppt/md/txt），写一份结构化总结 Markdown，只在本目录保存。' },
  { zh: '做小游戏', prompt: '在本工作目录做一个可运行的小游戏（说明怎么运行），只在本目录写文件。' },
  { zh: '写周报 Markdown', prompt: '根据本工作目录材料写一份周报 Markdown，不要生成 Office 文档。' },
] as const
```

`agentHubApi.inbox`：

```ts
inbox: (payload: { action: 'files' | 'folder' | 'list' | 'drop'; workDir?: string; name?: string }) =>
  request<{ canceled: boolean; workDir: string; files: InboxFile[]; skipped?: string[] }>('agentHub.inbox', payload),
```

`start` payload：

- 捷径：`agent` 锁死，`workDir` 必有，`prompt=scenePrefix(...)+inboxPrefix(files)`，`timeoutMin: 60`，fix 时 `sandbox: 'workspace-write'`
- 自由：`agent` 为选中胶囊；`workDir` 有则传（含因添加文件分配的）；空且 files 空则 `undefined`；`prompt=原文+inboxPrefix`；`sandbox` 仅 codex

### 步骤

- [ ] 在 `AgentHubPage.test.tsx` **先写**（沿用 `stubLists`）：

1. `其它任务` + available Codex + 不选目录 + 「写说明」+ 执行 → `start` 且 `agent==='codex'` 且 prompt 不含 `【场景：`。
2. 「做 PPT」不选目录 + 填字 + 执行 → `start` 不被调用。
3. `localStorage` 预置 `scene=ppt` 与 `workdir:ppt` 后执行 → `agent==='kimi'` 且含 `【场景：做 PPT】`。
4. 自由点 Kimi 后执行 → `agent==='kimi'`。
5. stub `agentHub.inbox` `action=files` 返回 `{canceled:false, workDir:'E:/hub', files:[{name:'纪要.pdf', path:'纪要.pdf', size:12}]}`；自由模式点「添加文件」后执行 → `start.workDir` 为 `E:/hub` 且 prompt 含 `纪要.pdf` 与 `.agenthub-inbox`，不含 `【场景：`。
6. 捷径 ppt 无 workDir 时点「添加文件」→ 先出现 `dir.pick`（stub cancel）且 **不**调用 `inbox`。

现有「starts a kimi/cursor task」改走「其它任务」，**不要删自由路径测试**。

- [ ] `npx vitest run src/agentHub/AgentHubPage.test.tsx`（`web/`）先红。
- [ ] 实现四卡、chips、添加文件/资料夹、× drop、`list` on workDir 变化。CSS：`.agent-hub-scene` 与 `.agent-hub-file` 选中/chip 用 `var(--tide1)`，不要新颜色。
- [ ] vitest 该文件全绿。

---

## Task 7: 详情默认看见 inbox

**Files:** `AgentHubDetail.tsx`、`AgentHubArtifacts.tsx`、对应 test。

默认：`['event','changed','inbox'].includes(item.source)`。开关「显示目录内其它文件」再含 `scan`。永不展示 `outside`。

### 步骤

- [ ] 测试：默认看得见 `source:'inbox'` 的 `纪要.pdf` 和 `source:'changed'` 的 `hello.txt`；看不见 `source:'scan'` 的 `lib.js`。
- [ ] 实现过滤。
- [ ] `npx vitest run src/agentHub`（`web/`）绿。

---

## Task 8: Windows 选文件对话框 + 回归真机

**Files:** `pick_files_windows.go`、`pick_files_other.go`、`live_windows_test.go`

Windows 对话框（与 `pick_windows.go` 同类：TopMost 隐藏窗体）：

- files：`OpenFileDialog`，`Multiselect=true`，`Filter='所有文件|*.*'`，标题「选择要交给 Agent 的文件」
- folder：`FolderBrowserDialog`，说明「选择要交给 Agent 的资料夹」
- 取消或空 → `ErrPickCanceled`
- 非 Windows：`pick_files_other.go` 返回 `ErrPickCanceled`

### 步骤

- [ ] 实现 pick 脚本；`Service.Inbox` 在 `PickFiles`/`PickFolder` 为空时调用它们。
- [ ] `go test ./internal/agenthub/ ./internal/app/ ./internal/bridge/ -count=1`
- [ ] `npx vitest run src/agentHub src/bridge/agentHub.deadline.test.ts`（`web/`）
- [ ] livehub hello 三条必须仍过。
- [ ] 可选：`TestLiveKimiInboxSummary` — `PickFiles` 指向临时 `note.txt`，`Inbox("files")` 后 `StartTask` prompt 含 inbox 段 +「写 summary.md」；断言源文件仍在；产物有 `summary.md` 或时间线有中文。

---

## 施工顺序

1. Task 1 扫描  
2. Task 2 Inbox 服务  
3. Task 3 Bridge 契约（含 dir.pick 截止修复）  
4. Task 4 Kimi  
5. Task 5 Cursor  
6. Task 6 工作台四卡 + 传文件  
7. Task 7 产物过滤  
8. Task 8 对话框 + 回归  

每 Task：红 → 最小实现 → 绿。不要先做四卡再补测试。

---

## Spec 覆盖自检

| PRD 条款 | 任务 |
|---|---|
| 四卡 / 自由不丢 | Task 6 |
| 捷径必选目录 | Task 6 |
| 自由可选目录 | Task 6 |
| 添加文件 / 资料夹 / 移除 | Task 2 + 3 + 6 |
| 原件不被改 | Task 2 |
| 自由无目录先分配再拷 | Task 2 + 6 |
| 捷径无目录先 dir.pick | Task 6 |
| inbox 入参段 | Task 6 |
| 扫描 inbox / skip / changed | Task 1 |
| Kimi 文件+skills | Task 4 |
| Cursor workspace | Task 5 |
| Codex 无强制改代码前缀 | Task 5 + 6 |
| 10 分钟选文件/选目录 | Task 3 |
| UI 默认 inbox+变更 | Task 7 |
| 不拆自由真机 | Task 8 |
| 不改 Office / 不手改生成物 | 全局约束 |
