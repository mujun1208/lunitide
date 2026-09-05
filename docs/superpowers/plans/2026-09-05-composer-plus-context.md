# 工作台加号栏 Implementation Plan

> **For agentic workers:** Execute task-by-task. Do not commit unless the user asks.

**Goal:** 加号五条入口在真机可用：系统框选文件/夹并入库；图片工作区能看见；@ / 技能 / 专家选择器诚实；对话失败不再伪造引擎死。对照规格综合 **4.9/5**。

**Architecture:** Host `desktop.files.pick` + `desktop.files.readChunk`（进程内 allowlist）。渲染进程拼 `File[]` 后走现有 `ingestAttachments`。Engine **不**读用户路径。视觉图 parse=succeeded，`attachment.get` 带 `contentBase64`。选择器三态。`submitUserAsk` 改用 `CHAT_ASK_FOLLOWUP_FAILED`。

**Tech Stack:** Go `go test` 指定包；前端 `npx vitest run` 指定文件；改 schema 后 `npm run generate:bridge`。PowerShell 用 `;` 不用 `&&`。

**Spec:** [docs/superpowers/specs/2026-09-05-composer-plus-context-prd.md](../specs/2026-09-05-composer-plus-context-prd.md)（v2 勘误：禁止 ingestFromPath）。

## Global Constraints

- 不新 daemon；不改 VERSION / 不打包
- 不改 poison / TTS / 玉盘 / 星尘 / 会议 / 月伴 / People 传文件
- 不重写 `SessionPage` 整页
- 不调用 `people.file.pick`
- 隐藏 `<input type=file>` 只作 jsdom / Host 不可用时的降级

## File map

| File | Responsibility |
|---|---|
| `api/bridge/v1/desktop.files.pick.schema.json` | Host 选文件/夹 |
| `api/bridge/v1/desktop.files.readChunk.schema.json` | Host 分块读 allowlist 路径 |
| `api/bridge/v1/attachment.get.schema.json` | 可选 `contentBase64` |
| `internal/desktopfiles/*` | Host handler + allowlist |
| `cmd/desktop/main.go` | 注册两个 host 方法 |
| `internal/attachmentapp/service.go` | 视觉图 parse 成功 |
| `internal/app/attachment_handlers.go` | get 返回图片字节 |
| `web/src/session/composerPlusPick.ts` | pick → File[] |
| `web/src/session/composerPickerState.ts` | 选择器三态 |
| `web/src/session/SessionPage.tsx` | 接线 + 去伪造引擎死 |
| `web/src/App.tsx` | 启动页同一套 pick |
| `web/src/workspace/Workspace.tsx` | 图片 `<img>` |

---

## Task 1: 契约

- [x] 新增两个 host schema；envelope 枚举按字母插在 `deliverable.upsert` 与 `devTask.create` 之间
- [x] `attachment.get` x-result 增加可选 `contentBase64`（maxLength 240000）
- [x] `generate-bridge.mjs` enabled 列表 + host ownership 列表同步
- [x] `npm run generate:bridge`

## Task 2: Host pick / readChunk

- [x] `internal/desktopfiles` 单测：未 pick 的 path → `ATTACHMENT_PATH_DENIED`；过期拒绝；取消 `canceled:true`
- [x] Windows 多选 / 选文件夹一层白名单；非 Windows `DESKTOP_PICK_UNAVAILABLE`
- [x] `deadline.go`：`desktop.files.pick` = 120s
- [x] `cmd/desktop/main.go` 注册

## Task 3: 图片可读

- [x] 改 `TestService_IngestFile_UnsupportedMIME` 改用 `application/pdf`（先红再改 parse）
- [x] 2KB jpeg ingest → succeeded + get 含 JPEG 头
- [x] `Workspace.test`：有 `contentBase64` 则 `<img>`，无「尚未取得原图字节」

## Task 4: 选择器 + 伪造引擎死 + 接线

- [x] `composerPickerState` 单测：失败 ≠ 空
- [x] `submitUserAsk` 不再构造 `ENGINE_UNAVAILABLE`
- [x] 加号文件/文件夹走 `composerPlusPick`；无 host 时回退 hidden input
- [x] 启动页同样接线
- [x] 指定 vitest + `go test` 绿
