# 工作台加号栏（附件 / 文件夹 / @ / 技能 / 专家）— 可落地 PRD

日期：2026-09-05  
版本：复核勘误（v3）。对照安装后工作台实拍（图1 附件预览失败、图2 加号五条菜单）+ 2026-09-05 现码。  
状态：v2 根因面成立；v3 锁死「原生对话框兜底 / 文件夹过滤空 ≠ 对话框失败 / 跳过要点名」。不改 `VERSION`。  
产品：月汐（Lunitide）工作台 `SessionPage` + 启动页 `App` launch composer + WebView2 + Host + Engine  
对照：用户在 **0.4.65** 安装包上的工作台对话（不是同事 People，不是月伴，不是会议）。

**本文是本波唯一事实源。** 与语音 / 会议 / 专家 People 波次冲突处，以本文为准，且只解锁下文点名的锁。

不改：`VERSION`（落地后另开版本）。不新 daemon。不改 poison / 月伴 TTS / 玉盘像素 / 星尘配方 / `AnnotateCapture` / `assignNodeIDs` / 专家六段身份表。不改 `speech.ts` 窗。不重写 `SessionPage` 整页。不把工作台文件选择接到 `people.file.pick`。

---

## 0. 打分

| 口径 | 分 | 含义 |
|---|---|---|
| 当前产品（0.4.65 工作台实拍 + 现码） | **1.7 / 5** | 加号菜单能展开，五条入口在真机上都不能算完成 |
| 只修文案 / 只加重试 | **2.2 / 5** | 图1 是 API 缺字节；文件选择是 WebView2 宿主洞，文案碰不到 |
| 只当 ENGINE_UNAVAILABLE 修引擎 | **2.6 / 5** | 图1 横幅常是**对话轮次失败被涂成引擎死**；加号栏被连坐 |
| **按本文落地并过验收** | **4.9 / 5** | 契约可测；剩 0.1 是系统文件框被策略软件拦截、以及超大图只预览缩略 |

分项（当前 → 目标）：

| 轨 | 当前 | 目标 | 为什么扣到 4.9 而不是 5.0 |
|---|---|---|---|
| A 本机选文件 / 文件夹 | 1.4 | 4.9 | 用户取消对话框必须静默；企业策略拦系统框时只能诚实说「系统没打开文件框」 |
| B 图片入库后工作区能看见 | 1.2 | 4.9 | 超视觉上限的图只保证「已入库 + 可交给模型」，不保证原图像素墙 |
| C @ / 技能 / 专家选择器 | 2.0 | 4.9 | 目录为空是合法态，必须和「桥失败」分开写 |
| D 加号栏与对话失败解耦 | 1.8 | 4.9 | 引擎真死时入库仍会失败，但必须是这条动作的错，不是把整个加号栏判死刑 |
| E 启动页同一套入口 | 1.6 | 4.9 | 启动页没有工作区预览，只保证选到文件并带进下一会话 |

4.9 不是「感觉菜单活了」。每轨有可跑测试 + 一条人工 3 分钟清单。任一轨验收失败，整波不得自称 4.9。

### 0.1 对「当前分析」本身打分

未对照现码时，把五条入口当成五个独立前端 bug、或把图1 横幅当成加号栏根因，分析质量约 **2.4 / 5**。

v1 分析（已写入本文初稿）**4.2 / 5**：根因面找对了，但 A3 写成 `attachment.ingestFromPath` +「本进程 allowlist」。Pick 在 **Host**，Ingest 在 **Engine**，两个进程，Engine **看不见** Host 的 map。按 v1 落地会二选一翻车：要么 Engine 能读任意路径（安全洞），要么 allowlist 永远空（选了也入库失败）。

v2 分析 **4.9 / 5**，必须同时成立：

1. 五条入口是**同一加号栏产品面**，共享宿主选文件、选择器错误处理和「对话失败 ≠ 引擎死」。
2. 图1 `BIRETURN.jpg` 的「解析状态 failed + 尚未取得原图字节」是**附件读模型缺口 + 文案撒谎**，不是用户没上传成功。
3. `ENGINE_UNAVAILABLE` 横幅与「无法执行。模型结果不完整」是**对话轮次**问题；加号栏被它吓死或连坐，是本波要拆开的。
4. **路径字节不出 Host。** 选文件与读字节都在 Host；Engine 只吃现有 `attachment.upload.*` / `ingest` 的 base64。禁止 Engine 打开用户磁盘路径。
5. 每条锁能指出文件、现码行为、改后契约、测试名。

---

## 1. 用户原话锁成产品句

图2 加号菜单五条，用户原话：「全部都不能正常使用」。图1：「上传也不行」。

锁成五句，禁止用「引擎忙一下就好了」打发：

1. **附件 / 文件。** 点加号 → 附件 / 文件 → 系统多选框必须出现。选完支持的文本或图片后，作曲条出现进度，成功后待发送附件芯片可点掉。工作区「文件」里能打开它。图片必须能看见图，不许停在「尚未取得原图字节」。
2. **上传文件夹。** 点加号 → 上传文件夹 → 系统文件夹框必须出现。只入库白名单扩展名（现码 `ALLOWED_EXTENSIONS`）。跳过的文件要点名，不能整批消失。
3. **@ 上下文。** 点加号 → @ 上下文 → 输入框出现 `@`，并弹出可引用列表（本会话已成功入库的附件 / 图片、已挂专家、可信同事）。选一项后插入对应 token。列表失败要说失败原因，不许空列表装没事。
4. **选技能。** 点加号 → 选技能 → 弹出已发布技能列表。选中后挂到本条消息（现码 `referencedSkills`），随发送走。不是跳去技能中心改全局。
5. **选专家。** 点加号 → 选专家 → 弹出已启用专家。可多选，最多 8 个，挂到**本会话**直到移除（现码 `session.experts.set`）。芯片在不代表「选专家坏了」；芯片在但列表再也打不开、或挂上立刻被静默撤回，才算坏。

并列锁：

6. **上一轮对话失败，加号栏仍可用。** 图1 的「模型结果不完整」和「ENGINE_UNAVAILABLE 重试」不得关掉加号、不得让五条入口变成空操作。
7. **启动页加号同一套。** `App` launch composer 的 Files / Upload folder / @ Context / Choose skill / Choose expert 走同一条本机选择与同一套选择器错误处理。不得只修工作台、启动页继续点隐藏 `<input type="file">`。

---

## 2. 现码根因（对照 0.4.65，不是猜）

表面：加号**能打开**（图2）。失败发生在打开之后的宿主、桥和读模型。

### A. 附件 / 文件夹：WebView2 隐藏 file input + 空列表静默

工作台（`SessionPage`）和启动页（`App`）都是：

- 隐藏 `<input type="file" multiple>`，文件夹再叠 `webkitdirectory`
- 菜单按钮里 `fileRef.current?.click()` / `folderRef.current?.click()`
- `uploadBatch`：**`files.length === 0` 直接 `return null`，不报错**

同事页已经不用这条。`people.file.pick` 走 Windows `GetOpenFileNameW` / `SHBrowseForFolderW`（`internal/people/pick_windows.go`）。工作台没有对等的 host 方法。

真机常见结果：系统框根本不出现，或 `onChange` 得到空 `FileList`。用户看见「点了没反应」，记成「不能用」。单元测试只 spy 了 `input.click()`，**测不到 WebView2 是否真弹出系统框、是否真有字节**。

拖拽 / 粘贴仍走 `uploadBatch`，所以偶发能进一条附件。图1 的 `BIRETURN.jpg`（2.3 KB）就是「元数据进了、预览没做完」，不是「完全没上传」。

### B. 图1：图片已经入库，读模型和工作区在撒谎

`attachmentapp.parse` 只提取 `text/*` 等文本。图片走 `ErrUnsupportedMIME` → `parseStatus=failed`，`parseErrorCode=UNSUPPORTED_MIME`。**创建附件的调用仍返回成功**（`return att, nil`）。

`attachment.get` 的结果契约只有 `parsedText`，**没有原图字节**（`api/bridge/v1/attachment.get.schema.json`）。工作区写死：

> 此附件是图片。当前版本可将它交给视觉模型分析，但工作区尚未取得原图字节，暂不能在这里显示。

这是产品文案，不是 ingest 现场错误。引擎侧视觉路径 `ReadVisionImage` **已经能从 `fileStorage` 读出原图**。工作区说「尚未取得」是 API 没把字节交给渲染进程。

前端 `rememberAttachmentPreview` 只活在本次页面内存 / `sessionStorage`。刷新或从列表点开附件就没了。图1 右栏「1 个附件 / 解析状态 failed」是这条缺口的屏幕证据。

### C. @ / 技能 / 专家：失败被 `.catch(() => [])` 吃掉

`SessionPage` 在 `trigger` 为 `@` / `/` / `expert` 时拉：

- `attachments.list`（只要 `succeeded` 或 `image/*`）
- `people.list`（同事）
- `skills.list({status:'published'})`
- `experts.list({state:'enabled'})`

任一失败都 `setXxx([])`。选择器要么空白，要么只有 `composer-candidates-empty`。用户记成「选技能 / @ / 选专家报错」。

`选专家` 现码是能工作的（图1 已挂「航空港机计划专家」；`SessionPage.conversationExperts.test.tsx` 覆盖打开列表）。本波不是重做挂载模型，是：**列表失败要诚实；挂载失败要回滚并 `setError`；对话轮次失败不得拆掉这个选择器。**

`@ 上下文` 现码只保证输入框出现 `@`（`SessionPage.runtime.test.tsx`）。不保证列表有数据、失败可见。

### D. 图1 横幅：对话失败被涂成引擎死，加号栏连坐

「无法执行。模型结果不完整，请重试。」来自轮次收口 `TURN_ERROR_INCOMPLETE_CAUSE` / `internal/app/chat_turn_notice.go`。这是**模型没给完结果**，不是加号栏处理器崩了。

`SessionPage.submitUserAsk` 在 `user.ask` / 发送重试失败后会：

```text
new BridgeClientError('核心引擎暂时不可用。…', 'ENGINE_UNAVAILABLE', true, 'renderer')
```

`BridgeClientError` 只要 `code === 'ENGINE_UNAVAILABLE'` 就派发 `lunitide:engine-unavailable`。于是：

- 会话内错误条：`核心引擎暂时不可用 代码 ENGINE_UNAVAILABLE 重试`（图1 黑条，正是这个，不是 `EngineHealthBanner` 的「已断开，正在自动重连」）
- 全局健康横幅也被点亮

引擎可能仍活着。`skill.list` / `expert.list` / `attachment.ingest` 本可以继续。用户看见「核心引擎暂时不可用」，再点加号五条，会把所有失败归因到引擎，并停止重试选择器。

真引擎死（gateway `Call` / poison → `ENGINE_UNAVAILABLE`）时，列表和入库**应当**失败，但必须是**该动作**的错误文案，且文件框仍应由 **Host** 弹出（host handler 不经过 Engine Caller）。

### E. 启动页复制了同一套隐藏 input

`App.test.tsx`：`Add context` → Files / Upload folder / @ Context / Choose skill / Choose expert，并断言存在 `input[webkitdirectory]`。工作台修了、启动页不改，用户从「今天想聊什么」进来会复现图2。

---

## 3. 三条路径与推荐

**推荐：路径 1 — 保持一个加号菜单；Host 选文件；补图片字节；选择器诚实；禁止把对话失败涂成引擎死。**

| 路径 | 做什么 | 代价 | 4.9？ |
|---|---|---|---|
| **1 按面修（推荐）** | 新 host 方法选文件/夹；工作台+启动页改走它；`attachment.get` 给视觉图字节；工作区真预览；选择器 loading/空/失败三态；发送失败不得伪造 `ENGINE_UNAVAILABLE` | 契约面清楚，可单测，不重写作曲条 | 能 |
| 2 重写作曲条 / 新加号子系统 | 抽全新 Composer、新附件服务 | `SessionPage` 已是高危巨石，和本波五条入口无关的回归面过大 | 晚，且容易带回发送 |
| 3 工作台复用 `people.file.pick` + 只改工作区文案 | 单路径、单文件、走 Engine；图片仍无 `contentBase64`；@ / 技能仍吞错 | 域错了，多选/文件夹过滤不够，引擎一抖选择也死 | 不能 |

否决路径 2、3。路径 1 的共享面只许这些：

1. `desktop.files.pick` + `desktop.files.readChunk`（**都是 host**，不是 engine，不是 `people.*`）
2. 渲染进程把 Host 字节拼成 `File[]`，再走**现有** `ingestAttachments` / `attachment.upload.*`
3. `attachment.get` 对视觉 MIME 增加可选 `contentBase64`
4. 选择器三态：`loading` / `ready` / `failed`（failed 带 `code` + 人话）
5. 渲染进程禁止再用 `ENGINE_UNAVAILABLE` 包装「发送 / user.ask / 模型不完整」

**禁止** `attachment.ingestFromPath`。那会逼 Engine 读用户路径。

---

## 4. 五轨产品锁

### 轨 A — 本机选文件 / 选文件夹

**A1. 系统框由 Host 打开，不依赖 WebView2 `<input type=file>`。**  
新桥方法 `desktop.files.pick`：

| 字段 | 值 |
|---|---|
| owner | `host` |
| payload | `{ folder?: boolean, multiple?: boolean }` |
| 文件模式 | `folder !== true`：Windows 多选文件框（`multiple` 默认 true） |
| 文件夹模式 | `folder === true`：选一个目录，Host 只枚举**一层**普通文件（不跟符号链接、不递归Junction） |
| 结果 | `{ items: [{ path, fileName, mime, size }], canceled: boolean }` |
| 取消 | `canceled: true`，`items: []`，**不是错误**，UI 不 toast |
| 上限 | 最多返回 20 个条目；单个 size 可大于 10 MiB（交给现码校验跳过并点名） |
| 过滤 | 文件夹模式只返回扩展名 ∈ 现码 `ALLOWED_EXTENSIONS` 的文件；文件模式不先按扩展名拒收，让现码 `validateAttachmentBatch` 跳过并点名 |
| 失败 | 系统框调不起来：`DESKTOP_PICK_UNAVAILABLE`（retryable false）。权限/IO：`DESKTOP_PICK_FAILED` |
| 对话框实现 | **必须**与 People 同级：先试 WinForms，失败再 `GetOpenFileNameW` / `SHBrowseForFolderW`。只跑 HiddenPowerShell、没有 COM 兜底，不算 A1 完成。禁止 `import` People 包，只允许复制对话框能力到 `internal/desktopfiles`。 |
| 文件夹跳过 | 结果可带 `skipped: string[]`（被扩展名过滤掉的文件名）。UI 必须点名，不能整批消失。 |

禁止工作台调用 `people.file.pick`。

**A2. 工作台与启动页的加号「附件 / 文件」「上传文件夹」必须调用 A1。**  
隐藏 `<input type=file>` 只允许作为 **jsdom / 无 host 的测试后备**，或 Host 返回 `DESKTOP_PICK_UNAVAILABLE` 时的一次性降级。真机 Windows 安装包验收不得把「弹出系统框」建立在 `input.click()` 上。

点菜单项后：先关加号菜单，再 `await desktop.files.pick`。用户取消：菜单保持关、无错误条。

**A3. 选中之后仍走现有入库管道；字节只在 Host 读。**

| 方法 | owner | 作用 |
|---|---|---|
| `desktop.files.pick` | host | 系统框；把选中的绝对路径写入 **Host 进程内** allowlist（10 分钟过期）；返回 `{ items, canceled }`，items 含 path/fileName/mime/size |
| `desktop.files.readChunk` | host | `{ path, offset, limit }` → `{ contentBase64, nextOffset, eof }`。path 不在 allowlist、已过期、不是普通文件、或是符号链接 → `ATTACHMENT_PATH_DENIED`。`limit` 最大 32768 |

渲染进程对每个 item 循环 `readChunk`，在内存拼成 `File`，再调用现有 `uploadBatch` → `ingestAttachments`（begin/chunk/commit）。**Engine 从不看见文件系统路径。** 一条 bridge 消息不得塞整文件；20×10MiB 只允许分块过 Host。

进度 UI 沿用 `uploadProgress`。三种空结果必须分开，禁止再用同一句「系统没打开文件框」：

| 结果 | 用户看见 |
|---|---|
| `canceled===true` | 什么都不说 |
| 文件模式 `canceled!==true` 且 `items.length===0` | `DESKTOP_PICK_FAILED`：「系统没打开文件框，请再试一次。」 |
| 文件夹模式对话框成功，但一层白名单后 0 个文件 | **不是**对话框失败。文案：「这个文件夹里没有可导入的文件。」可带被跳过的文件名。码：`DESKTOP_FOLDER_EMPTY` |

隐藏 `<input>` 的 `onChange` 得到空 `FileList`（用户取消原生框）必须静默，禁止走 `DESKTOP_PICK_FAILED`。空且非取消只适用于 **Host pick 文件模式**。

**A4. 拖拽、粘贴、启动页已有 `File` 对象的路径保持。**  
不删 `ingestAttachments(File[])`。本机选择是第三条进管线的入口，不是唯一入口。

### 轨 B — 图片入库后工作区必须能看见

**B1. 视觉图片不是解析失败。**  
MIME ∈ `image/png|image/jpeg|image/webp` 且文件已写入 `fileStorage`：

- `parseStatus` = `succeeded`
- `parseErrorCode` = `""`
- `parsedText` 省略或 `""`
- `parsedTextBytes` = 0

禁止再把「图片没有 UTF-8 正文」写成用户可理解的失败。`UNSUPPORTED_MIME` 留给 PDF / DOCX 等真正不支持的二进制。

**B2. `attachment.get` 对视觉图返回字节。**  
当 `isVisionMIME && size ≤ MaxVisionImageBytes`（现码 180 KiB）且完整性校验通过：结果增加 `contentBase64`（标准 base64，无 data URL 前缀）。非图片或不通过完整性校验：字段省略，不得假装有图。

schema + generated client + `handleAttachmentGet` 必须一起改。

**B3. 工作区文件页对图片渲染 `<img>`。**  
优先级：`contentBase64` → 前端 `attachmentPreview(id)`。有图时：

- 标题仍是文件名
- 「解析状态」对图片显示「图片 · 可供视觉分析」，不显示 `failed`
- **删除**「尚未取得原图字节，暂不能在这里显示」这条死文案

没有字节且 MIME 是图片：写「图片已记录，但预览字节不可用（code）」+ code（`ATTACHMENT_IMAGE_BYTES_MISSING`）。禁止再用那句永久借口。

**B4. 图1 同款 JPEG 验收。**  
`BIRETURN.jpg` 这类 < 10 KiB 的 `image/jpeg`：加号选中 → 作曲条预览缩略图 → 工作区点开看见图。视觉模型路径保持现有 `ReadVisionImage`，本波不改提示词、不改 4 张 / 180 KiB 上限。

### 轨 C — @ / 技能 / 专家选择器诚实可用

**C1. 三态，禁止吞错。**  
`trigger` 为 `@` / `/` / `expert` 时，对应 list 调用：

| 态 | UI |
|---|---|
| loading | listbox 在，文案「正在载入…」，不可把空当成最终 |
| ready + 有项 | 现有 option 列表 |
| ready + 空 | `composer-candidates-empty`：**区分**「还没有可引用的附件」「没有已发布技能」「没有已启用专家」 |
| failed | listbox 在，`role=alert`，人话 + 短码，按钮「再试一次」。**禁止**只留下空列表 |

`people.list` 失败不得抹掉已挂专家（现码 catch 已保留专家；本波锁死这条，并补 alert）。

**C2. @ 上下文。**  
插入 `@` 并打开 `@` 列表。选项 = 本会话附件（`succeeded` 或图片）+ 已挂专家 + 可信同事。选中走现有 `insertComposerAtPick`。本波不新做「全项目知识库搜索」。

**C3. 选技能。**  
只列出 `status==='published'`。选中写入 `referencedSkills`，从输入框去掉 `/…`。不因为挂了 PPT 专家就自动钉技能（现有测试锁保持）。

**C4. 选专家。**  
列出 `state==='enabled'`。多选、上限 8、乐观更新 + `session.experts.set` 失败回滚并 `setError`（现码 `persistMounted`）。选择器被点开时，即使会话错误条还在，列表也必须请求 `experts.list`。

**C5. 加号菜单与选择器叠层。**  
`@` / 选技能走 `insertToken`（已关菜单）。选专家已关菜单。锁：选择器打开时，window click-outside 不得在**打开那一次点击**里立刻把 listbox 拆掉。若现码偶发自关，修监听（`pointerdown` 与打开同一 event 忽略），不要删整个 overlay 逻辑。

### 轨 D — 加号栏与对话失败解耦

**D1. 渲染进程禁止伪造引擎死。**  
下列情况**不得** `new BridgeClientError(..., 'ENGINE_UNAVAILABLE', ...)`：

- `chat.start` / 发送失败
- `user.ask` 批准后重试发送失败
- 「模型结果不完整」轮次收口
- 附件校验失败、用户取消选文件

应使用真实码：`CHAT_START_FAILED`、`CHAT_ASK_FOLLOWUP_FAILED`、`TURN_INCOMPLETE`（仅文案，不派发 engine-unavailable）、或桥返回的原码。

**D2. 只有 Host/Gateway 原码是 `ENGINE_UNAVAILABLE` 才允许点亮全局横幅。**  
`BridgeClientError` 可继续在该码上派发事件。禁止 renderer 为了「看起来像引擎坏了」而借用这个码。

**D3. 加号栏在会话错误条可见时仍可点。**  
`error` 为发送/轮次失败时：加号五条照常。点「附件 / 文件」仍开系统框。点「选技能 / 选专家 / @」仍拉列表。入库成功不清除「模型结果不完整」那条历史气泡（那是上一轮事实），但**可以**清除误报的引擎错误条（`code` 属于 D1 伪造族时，下一次加号动作成功则 `setError(undefined)`）。

**D4. 引擎真死时的诚实态。**  
`desktop.files.pick` 仍应弹出（host）。随后 `readChunk` 成功、但 `attachment.upload.*` / `skills.list` / `experts.list` 失败：该动作 alert「核心引擎暂时不可用，这份选择还没写进去」，带桥返回的原码（真死才是 `ENGINE_UNAVAILABLE`）。用户取消文件框仍不算错误。

### 轨 E — 启动页同一套

Launch composer 的五条入口：

- 文件 / 文件夹 → A1，选中的 `File` 或 path 仍进现有 `files` 状态，创建会话后走现有 `initialUploadFiles`
- @ / 技能 / 专家 → 与工作台相同的 trigger 与三态；创建会话时把已选技能 / 专家带进 `initialComposerTrigger` / `initialReferencedSkills` / 首屏 mount（现有启动路径能带的都要带；带不了的在进入会话后立刻打开对应选择器，不得丢）

启动页没有工作区。轨 B 的预览验收在工作台做。

---

## 5. 错误文案锁（中文，短，禁止调试腔）

| 场景 | 用户看见 |
|---|---|
| 用户取消系统框 | 什么都不说 |
| 系统框没能打开 | 系统没打开文件框，请再试一次。 |
| 类型/大小被跳过 | 现码 skipped 点名（不支持的类型 / 超过 10 MiB / 每次最多 4 张图） |
| 入库失败且原码是引擎死 | 核心引擎暂时不可用，文件还没入库。 |
| 图片预览无字节 | 图片已记录，但预览字节不可用（ATTACHMENT_IMAGE_BYTES_MISSING）。 |
| 文件夹已选但没有可导入文件 | 这个文件夹里没有可导入的文件。 |
| 技能列表失败 | 技能列表暂时读不到，请再试一次。 |
| 专家列表失败 | 专家列表暂时读不到，请再试一次。 |
| @ 列表失败 | 可引用的上下文暂时读不到，请再试一次。 |
| 技能 / 专家目录合法空 | 还没有已发布技能。 / 还没有已启用专家。 |
| @ 合法空 | 这一会话还没有可引用的附件、专家或同事。 |
| 模型不完整（历史气泡，保持） | 无法执行。模型结果不完整，请重试。 |
| 发送失败（不再叫引擎死） | 这次没发出去，请再试一次。 |

---

## 6. 数据流（路径 1）

```text
加号「附件/文件」
  → desktop.files.pick { multiple:true }     [host，写入 allowlist]
  → canceled? stop
  → for item:
        loop desktop.files.readChunk         [host]
        拼 File → ingestAttachments          [engine 只吃 base64]
  → uploadProgress + pendingAttachmentIds
  → Workspace list/get
  → image? attachment.get.contentBase64 → <img>

加号「上传文件夹」
  → desktop.files.pick { folder:true }       [host]
  → 一层白名单文件 → 同上入库

加号「@ / 选技能 / 选专家」
  → 关菜单 + trigger
  → list（失败则 failed 三态，不吞）
  → 选中：token / referencedSkills / persistMounted
```

---

## 7. 测试锁（没有这些测试不得自称 4.9）

### 自动

1. **Host pick 契约**：`desktop.files.pick` schema 正反例；owner=host；取消 → `canceled:true` 且 ok。
2. **allowlist**：`desktop.files.readChunk` 拒绝从未 pick 过的路径（`ATTACHMENT_PATH_DENIED`）；pick 后 11 分钟过期同样拒绝。Engine 侧**没有**按路径读文件的方法。
3. **图片 parse**：入库 2KB jpeg → `parseStatus=succeeded`，`parseErrorCode=""`，`get` 含 `contentBase64` 且能还原 JPEG 头 `FF D8 FF`。
4. **工作区**：`Workspace.test` 对 image/jpeg + contentBase64 断言有 `<img>`；断言页面**没有**「尚未取得原图字节」。
5. **uploadBatch 空且非取消**：必须 setError / alert，禁止静默。
6. **选择器失败**：`skills.list` / `experts.list` / `attachments.list` reject 时，出现带「再试一次」的 alert，listbox 仍在。
7. **选择器合法空**：空目录显示 C 轨空文案，不出现失败 alert。
8. **伪造引擎死**：`submitUserAsk` 发送失败不得再构造 `ENGINE_UNAVAILABLE`；不得派发 `lunitide:engine-unavailable`。可用 `CHAT_ASK_FOLLOWUP_FAILED`。
9. **加号在错误条下仍可打开**：会话 `error` 可见时，点「添加上下文」仍出现五条菜单；点「选专家」仍调用 `experts.list`。
10. **启动页**：Files / Upload folder 的 click 不再是「只 spy hidden input」这一种真理；有 host mock 时必须调用 `desktop.files.pick`。
11. **回归保持**：`closes the attachment menu as soon as the file picker is opened`；对话专家 8 人挂载；选 PPT 专家不钉技能。

### 人工 3 分钟（Windows 安装包）

1. 工作台点加号 → 附件 / 文件 → **看见系统多选框** → 选一张小 JPEG → 作曲条有缩略图 → 打开工作区看见图，解析行不是 failed。
2. 加号 → 上传文件夹 → **看见系统文件夹框** → 选含 txt + exe 的目录 → txt 入库，exe 被点名跳过。
3. 先故意发一条会「模型不完整」的长任务（或等现成气泡）。**不要点横幅重试。** 立刻加号 → 选技能，列表有已发布技能；选专家能勾；@ 能插入已入库图片名。
4. 启动页加号 → Files → 同样必须是系统框，不是毫无反应。

---

## 8. 明确不做

- 不修「模型结果不完整」的模型侧根因（工具历史 / max tokens / 供应商 400）。那是另一波。本波只禁止它冒充加号栏死和引擎死。
- 不支持 PDF / DOCX 正文预览。
- 不提高 20 文件 / 10 MiB / 4 图 / 180 KiB。
- 不重做技能中心、专家中心。
- 不把工作区改成通用资源管理器。
- 不改 People 传文件（已有 `people.file.pick`）。
- 不改会议 / 月伴语音。

---

## 9. 落地顺序

v2 契约 / Host allowlist / 图片字节 / 选择器三态 / 去伪造引擎死 **已在现码落地**。v3 只补缺口，计划见 [2026-09-05-composer-plus-context-gap.md](../plans/2026-09-05-composer-plus-context-gap.md)。

---

## 10. 规格自检

- 取消、真引擎死、合法空目录、**文件夹过滤后为空** 都有确定行为。
- 五轨不互相矛盾：Host 选文件与 Engine 入库分离；对话失败与引擎死分离。
- 「报错」在本波的意思锁死为：系统框不出现、空操作、选择器空白、工作区把已入库图片写成失败。不是「模型没写完文章」。

---

## 11. v3 复核勘误（2026-09-05 对照现码）

对照用户原话、本文 v2、以及仓库现码。v2 根因面仍成立（五条入口同一面、图片已入库缺字节、禁止 Engine 读路径、禁止伪造引擎死）。下列是 v2 写成 4.9 但现码 / 规格自己会翻车的点：

1. **A1 对话框实现没锁死。** v2 写「复用 people 对话框能力」，现码 `internal/desktopfiles/pick_windows.go` **只**跑 HiddenPowerShell + WinForms。People 自己在 PowerShell 失败后会落到 `GetOpenFileNameW` / `SHBrowseForFolderW`。企业策略拦 PowerShell 时，工作台会 `DESKTOP_PICK_UNAVAILABLE` → 再点隐藏 `<input>`，真机又回到 0.4.65 的「点了没反应」。
2. **A3 把「文件夹过滤后为空」判成对话框失败。** 选只含 `.exe` 的目录时 Host 返回 `canceled:false, items:[]`，前端写成「系统没打开文件框」。用户看见的是撒谎。v3 改为 `DESKTOP_FOLDER_EMPTY`。
3. **跳过文件没有点名。** Host `listFolder` 静默丢掉非白名单文件。产品句 2 要求「exe 被点名跳过」。
4. **`uploadBatch` 对空 `FileList` 一律报错。** 隐藏 input 取消若触发 `onChange([])`，会误报「系统没打开文件框」。空且非取消只适用于 Host **文件模式** pick。
5. **测试锁 3 没打到 `attachment.get` 处理器。** `TestService_IngestFile_VisionJPEGSucceeded` 只测 `PreviewWorkspaceImage`，没有经 `handleAttachmentGet` 还原 `FF D8 FF`。
6. **轨 E 启动页三态被写过了。** 启动页 `@` / 技能 / 专家本就会 `session.create` 后把 `composerTrigger` 交给 `SessionPage`。v3 接受这条，不再要求启动页自己画 listbox。启动页必须自担的只有 Files / Upload folder 的 Host pick。
7. **计划文档粒度过粗。** v2 计划是四条勾选，不是可交给生手的 TDD 步骤。缺口关闭计划必须带测试名和断言。

hwnd 宿主窗口（对话框跑到 WebView 后面）与 People 现码一样是 `hwndOwner=0`，本波不新开找窗。算进 4.9 扣的 0.1。
