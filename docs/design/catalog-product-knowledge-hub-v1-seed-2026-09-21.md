# 产品知识中枢 · 初版种子清单 seed.v1

- 日期：2026-09-21
- 角色：**第一版详细说明书**，不是以后每版都手改的全集
- 实现落点：`internal/producthub/manifest/seed.v1.json`（实施时从本文结构化抽出）
- 迭代：每次重建走 [生成原则](./PRD-product-knowledge-hub-generation-principles-2026-09-21.md) §6 Merge  
  活源多出来的功能自动补卡；本文有而活源没有的标弃用；本文有深度 chain 的优先保留讲解

对照仓库锚点（2026-09-21 `trae/docs/Design`）：

- `Page`：`home|projects|providers|settings|skill|expert|mcp|plugins|assets|office|automation|media|meetings|people|mro|agentHub`
- 设置 18 分类：`web/src/settings/settingsNav.ts`
- 办公组现为 6 项（办公 / 自动化 / **媒体中心** / 同事聊天 / 机务 / 会议），入口挂在会议之后
- 媒体动作：`play|pause|toggle|stop|previous|next|seek|set_volume|mute|unmute|create|move|remove|clear|jump` + `media.asset.open`
- 插件名册：`HarnessPlugins()` 23 项
- 迁移号必须用 **0167**（0166 已被 capability_pack_skip 占用）

---

## 0. 怎么用这份初版

1. **人读**：下面每张卡就是说明书——简介、描述、属性、方法、主干、成功/失败/重试/降级。
2. **机读**：实施时变成 JSON；`stable_key` 是唯一身份。
3. **下次改版**：不要重写本文。让采集器扫新产品，merge 出 snapshot N。若某条讲解要写得更细，只改对应 `stable_key` 的 overlay，或出 seed.v2 补丁（只含变更卡）。

---

## 1. 本体：5 域 / 模块（随 Page + 设置自动对齐）

| 域 | 模块 | 活源 |
|---|---|---|
| 对话体验 dialog | 月伴语音、打字聊天、会话管理 | companion / SessionPage / voice 设置 |
| 业务工作台 office | 办公工作台、自动化、媒体中心、同事聊天、机务、会议、Office 交付 | 对应 Page + officeMenu |
| 资产与智能 assets | 专家、技能、插件、MCP、资产、记忆、OCR | 注册表采集 |
| 执行与控制 execution | 命令、文件工作区、浏览器、电脑控制、消息通道、子智能体、Agent Hub | command / desktopfiles / browser / computer / channels / agentHub |
| 底座与治理 foundation | 供应商、路由、协作门禁、诊断更新、Token、设置 18 分类 | providers / routing / collab / diagnostics / settingsNav |

Product 根节点：`product.lunitide`。

---

## 2. 原子功能总表（初版必须细到动词）

实施时每行 = 一张 Feature。有「详」的在 §3 展开全文；其余用对应 `chain_class` 模板生成，种子只给 summary。

### 2.1 对话体验

| stable_key | 中文 | EN | chain_class | 详 |
|---|---|---|---|---|
| feature.dialog.companion.voice-talk | 月伴语音对话 | Companion voice talk | intent-control |  |
| feature.dialog.companion.wake | 唤醒月伴 | Wake companion | intent-control |  |
| feature.dialog.companion.barge-in | 语音插话 | Barge-in | intent-control |  |
| feature.dialog.chat.type | 打字聊天 | Type a message | crud-bridge |  |
| feature.dialog.chat.mention-skill | 对话里 @技能 | Mention a skill | asset-invoke |  |
| feature.dialog.chat.mention-expert | 对话里 @专家 | Mention an expert | asset-invoke |  |
| feature.dialog.session.new | 新建对话 | New chat | page-enter |  |
| feature.dialog.session.search | 搜索对话 | Search chats | crud-bridge |  |
| feature.dialog.session.delete | 删除对话 | Delete chat | crud-bridge |  |
| feature.dialog.session.rename | 重命名对话 | Rename chat | crud-bridge |  |
| feature.dialog.app.launch | 打开电脑应用 | Launch desktop app | intent-control | 详 |
| feature.dialog.music.open-player | 打开音乐播放软件 | Open music player | intent-control | 详 |
| feature.dialog.music.play | 播放歌曲 | Play track | media-transport | 详 |
| feature.dialog.music.pause | 暂停播放 | Pause | media-transport | 详 |
| feature.dialog.music.toggle | 播放/暂停切换 | Play/pause toggle | media-transport |  |
| feature.dialog.music.next | 下一曲 | Next track | media-transport | 详 |
| feature.dialog.music.prev | 上一曲 | Previous track | media-transport |  |
| feature.dialog.music.stop | 停止播放 | Stop | media-transport |  |
| feature.dialog.music.seek | 跳转到进度 | Seek | media-transport |  |
| feature.dialog.music.volume | 调节音量 | Set volume | media-transport |  |
| feature.dialog.music.mute | 静音 | Mute | media-transport |  |
| feature.dialog.music.search | 搜索并播放歌曲 | Search and play song | intent-control | 详 |
| feature.dialog.file.open | 打开文件 | Open a file | file-open | 详 |
| feature.dialog.file.pick | 选择本地文件 | Pick local file | file-open |  |
| feature.dialog.computer.screenshot | 截一张屏 | Screenshot | intent-control |  |
| feature.dialog.computer.click | 点击屏幕位置 | Click on screen | intent-control |  |

### 2.2 业务工作台

| stable_key | 中文 | EN | chain_class | 详 |
|---|---|---|---|---|
| feature.office.page.office | 进入办公工作台 | Open Office Studio | page-enter |  |
| feature.office.studio.task-create | 创建办公任务 | Create office task | crud-bridge |  |
| feature.office.studio.artifact-export | 导出办公产物 | Export office artifact | crud-bridge |  |
| feature.office.page.automation | 进入自动化 | Open automation | page-enter |  |
| feature.office.automation.job-set | 保存自动化任务 | Save automation job | crud-bridge |  |
| feature.office.automation.job-trigger | 立刻跑自动化 | Trigger automation | asset-invoke |  |
| feature.office.page.media | 进入媒体中心 | Open media center | page-enter |  |
| feature.office.media.asset-open | 打开媒体资产 | Open media asset | media-transport | 详 |
| feature.office.media.play | 媒体中心播放 | Media play | media-transport |  |
| feature.office.media.pause | 媒体中心暂停 | Media pause | media-transport |  |
| feature.office.media.next | 媒体中心下一首 | Media next | media-transport |  |
| feature.office.media.previous | 媒体中心上一首 | Media previous | media-transport |  |
| feature.office.media.stop | 媒体中心停止 | Media stop | media-transport |  |
| feature.office.media.seek | 媒体中心跳转 | Media seek | media-transport |  |
| feature.office.media.set_volume | 媒体中心音量 | Media volume | media-transport |  |
| feature.office.media.mute | 媒体中心静音 | Media mute | media-transport |  |
| feature.office.media.unmute | 媒体中心取消静音 | Media unmute | media-transport |  |
| feature.office.media.toggle | 媒体中心切换播放 | Media toggle | media-transport |  |
| feature.office.media.create | 新建媒体会话 | Create media session | crud-bridge |  |
| feature.office.media.jump | 跳到队列项 | Jump in queue | media-transport |  |
| feature.office.media.move | 调整队列顺序 | Move queue item | crud-bridge |  |
| feature.office.media.remove | 移出队列 | Remove queue item | crud-bridge |  |
| feature.office.media.clear | 清空队列 | Clear queue | crud-bridge |  |
| feature.office.page.people | 进入同事聊天 | Open colleague chat | page-enter |  |
| feature.office.people.send-file | 给同事发文件 | Send file to colleague | file-open |  |
| feature.office.people.open-file | 打开同事发来的文件 | Open received file | file-open | 详 |
| feature.office.page.mro | 进入机务工作台 | Open MRO | page-enter |  |
| feature.office.mro.search-manual | 检索机务手册 | Search MRO manual | asset-invoke | 详 |
| feature.office.mro.plan | 生成机务计划 | Build MRO plan | crud-bridge |  |
| feature.office.page.meetings | 进入会议记录 | Open meetings | page-enter |  |
| feature.office.meetings.start | 开始会议听写 | Start meeting capture | meeting-pipeline | 详 |
| feature.office.meetings.transcribe | 转写会议音频 | Transcribe meeting | meeting-pipeline |  |
| feature.office.meetings.summary | 生成会议纪要 | Summarize meeting | meeting-pipeline | 详 |
| feature.office.meetings.todo | 抽出会议待办 | Extract meeting todos | meeting-pipeline |  |

### 2.3 资产与智能

| stable_key | 中文 | EN | chain_class | 详 |
|---|---|---|---|---|
| feature.assets.page.expert | 进入专家 | Open experts | page-enter |  |
| feature.assets.expert.try | 试用专家 | Try expert | asset-invoke |  |
| feature.assets.page.skill | 进入技能 | Open skills | page-enter |  |
| feature.assets.skill.install | 安装技能 | Install skill | asset-invoke |  |
| feature.assets.skill.invoke | 调用技能 | Invoke skill | asset-invoke |  |
| feature.assets.page.plugins | 进入插件 | Open plugins | page-enter |  |
| feature.assets.plugin.enable | 启用插件 | Enable plugin | settings-toggle |  |
| feature.assets.page.mcp | 进入 MCP | Open MCP | page-enter |  |
| feature.assets.mcp.connect | 连接 MCP | Connect MCP | asset-invoke |  |
| feature.assets.mcp.invoke | 调用 MCP 工具 | Invoke MCP tool | asset-invoke |  |
| feature.assets.page.assets | 进入资产管理 | Open assets | page-enter |  |
| feature.assets.memory.recall | 召回记忆 | Recall memory | asset-invoke | 详 |
| feature.assets.memory.capture | 写入一条记忆 | Capture memory | crud-bridge |  |
| feature.assets.ocr.screenshot | OCR 识别截图 | OCR a screenshot | asset-invoke | 详 |
| feature.assets.ocr.document | OCR 识别文档 | OCR a document | asset-invoke |  |

插件名册 23 项各自再生成 `feature.assets.plugin.<id>`（llm/session/jobs-local/web-search-deepseek/tool-bash/tool-pwsh/tool-cmd/tool-python/web-search/web-fetch/workspace/filesystem/git/browser/agent-loop/thinking/memory/skills/cron/clipboard/notification/tts/stt）。初版不手写 23 段说明书，用 `asset-invoke` 模板；有需要再 overlay。

### 2.4 执行与控制

| stable_key | 中文 | EN | chain_class | 详 |
|---|---|---|---|---|
| feature.execution.command.run | 执行一条白名单命令 | Run allowlisted command | asset-invoke | 详 |
| feature.execution.workspace.open-file | 在工作区打开文件 | Open workspace file | file-open | 详 |
| feature.execution.workspace.save | 保存工作区文件 | Save workspace file | crud-bridge |  |
| feature.execution.browser.navigate | 浏览器打开网址 | Browser navigate | asset-invoke | 详 |
| feature.execution.browser.snapshot | 浏览器快照 | Browser snapshot | asset-invoke |  |
| feature.execution.computer.launch | 电脑控制启动应用 | Computer launch app | intent-control |  |
| feature.execution.computer.mouse | 电脑控制键鼠 | Computer input | intent-control |  |
| feature.execution.channels.send | 发到消息通道 | Send to IM channel | crud-bridge |  |
| feature.execution.subagent.spawn | 拉起子智能体 | Spawn subagent | asset-invoke |  |
| feature.execution.page.agentHub | 进入 Agent Hub | Open Agent Hub | page-enter |  |
| feature.execution.agenthub.file-open | Agent Hub 打开文件 | Agent Hub open file | file-open |  |
| feature.execution.agenthub.task-start | Agent Hub 开工 | Start Agent Hub task | asset-invoke |  |

### 2.5 底座与治理（设置 18 分类各一张 + 关键动作）

| stable_key | 中文 | chain_class |
|---|---|---|
| feature.foundation.settings.general | 常规设置 | settings-toggle |
| feature.foundation.settings.appearance | 外观设置 | settings-toggle |
| feature.foundation.settings.office-menu | 办公菜单显隐 | settings-toggle |
| feature.foundation.settings.profile | 个人资料 | settings-toggle |
| feature.foundation.settings.providers | 模型与供应商 | settings-toggle |
| feature.foundation.settings.routing | 能力路由 | settings-toggle |
| feature.foundation.settings.voice | 语音与麦克风 | settings-toggle |
| feature.foundation.settings.meetings | 会议纪要设置 | settings-toggle |
| feature.foundation.settings.personal | 智能能力（记忆/OCR） | settings-toggle |
| feature.foundation.settings.security | 安全与治理 | settings-toggle |
| feature.foundation.settings.datasources | 数据源 | settings-toggle |
| feature.foundation.settings.browser | 浏览器设置 | settings-toggle |
| feature.foundation.settings.computer | 电脑控制设置 | settings-toggle |
| feature.foundation.settings.channels | 消息通道设置 | settings-toggle |
| feature.foundation.settings.subagents | 子智能体设置 | settings-toggle |
| feature.foundation.settings.collab | 协作门禁 | settings-toggle |
| feature.foundation.settings.diagnostics | 诊断与更新 | settings-toggle |
| feature.foundation.settings.about | 关于 | page-enter |
| feature.foundation.provider.add | 添加模型供应商 | crud-bridge |
| feature.foundation.provider.test | 测试供应商连通 | crud-bridge |
| feature.foundation.page.providers | 进入供应商页 | page-enter |
| feature.foundation.page.projects | 进入项目 | page-enter |
| feature.foundation.page.home | 进入主页 | page-enter |
| feature.foundation.diagnostics.health | 查看系统健康 | diagnose-only |
| feature.foundation.update.check | 检查更新 | crud-bridge |
| feature.foundation.token.compact | Token 精简 | crud-bridge |

活源 Bridge 方法（约数百个 `x-method`）**不要抄进本文**。catalog 生成器会为每个方法出 `bridge.<method>` 节点；种子只给上表这种「人能听懂的动词」。merge 时 `bridge.media.play` 与 `feature.office.media.play` 用 `calls` 边连上。

---

## 3. 详细功能卡（初版示范，后续自动卡按此结构对齐）

字段约束与 V3.1 相同：`summary` ≤128；`description` ≤1024；主干 3–8 步；success+failure 必有；failure 必须闭合。

---

### 3.1 打开音乐播放软件 · `feature.dialog.music.open-player`

**A 简介**  
用语音或打字打开本机已安装的汽水音乐、网易云等播放器。

**B 描述**  
月伴或对话框理解「打开网易云 / 打开汽水音乐」后，走电脑控制启动进程，用窗口句柄或 SMTC 会话确认真的起来了。没装、超时、权限拒绝会换注册表 App Paths 重试，再失败则播报原因并推荐已安装的替代播放器。连续语音不必重新唤醒。

**C 属性**

| 类 | 值 |
|---|---|
| operations | 电脑操作 |
| tools | computer.control, process.launch, window.find |
| mcps | （若已连 windows sysmon 则挂上，活源解析） |
| skills | 电脑操作类技能（活源有则挂） |
| capabilities | capability.stt.asr, capability.llm.intent, capability.tts.voice |

**D 方法**

- 语音：唤醒后说「打开网易云音乐」；可接着说播放/暂停/下一曲。
- 打字：输入「帮我打开汽水音乐」；可 @电脑操作技能。

**链路**

```
1 用户输入     语音/文字     月伴说或打字；语音经 VAD 断句
2 语音识别     ASR 转写      本地 sherpa；在线不可用切本地
3 意图理解     LLM           computer.open_app + 实体「网易云」；低置信反问
4 方案生成     工具绑定      FullAccess 免批，否则弹授权
5 执行控制     启动进程      开始菜单/快捷方式 → 启进程 → 8s 内等窗口
```

| 分支 | 挂点 | 说明 | 下一步 |
|---|---|---|---|
| success | 5 | 窗口句柄或 SMTC 会话到了 | TTS「已打开」+ 记 change_log |
| failure | 5 | 未安装 / 超时 / 权限拒绝 | → retry |
| retry | 5 | 注册表 App Paths 再搜 | 成功并入 success；再失败 → fallback |
| fallback | retry | 播报原因 + 推荐已装播放器 | 结束，原因入库 |

**核验锚点**：`internal/winexec/media_windows.go`、`internal/winexec/media_session.go`、`MediaSnapshotDTO.verificationSource = smtc|owned_runtime`。只发媒体键不算已打开。

---

### 3.2 播放歌曲 · `feature.dialog.music.play`

**A 简介**  
对当前播放器或媒体中心会话发出「播放」，并用 SMTC / 自有运行时核验。

**B 描述**  
用户说「播放 / 放这首 / 继续放」或点媒体中心播放。引擎建 `media` operation `action=play`。外部播放器最多记 `command_dispatched`；只有匹配目标的 SMTC 或本产品 audio/video 事件才能 `verified_playing`。命名搜歌走外部 origin，不进自有队列。

**C 属性** operations=媒体控制；tools=media.play / SendMediaKey；capabilities=smtc, owned_runtime  

**D 方法** 语音「播放」；打字「播放」；媒体中心 ▶。

**链路**

```
1 选定会话     当前媒体会话或前台播放器
2 发 operation play + idempotencyKey
3 派发         自有 runtime 或媒体键/AppCommand
4 核验         SMTC / owned_runtime 回读 phase=playing
```

| 分支 | 说明 |
|---|---|
| success | verification=verified_playing，UI 切播放中 |
| failure | 无会话 / 审批拒绝 / 核验失败 |
| retry | 用户点重试或 recoveryAction=retry |
| fallback | 仅 command_dispatched：界面标「已发送、未核验」，不说「已播放」 |

---

### 3.3 暂停 · `feature.dialog.music.pause`

**A 简介** 暂停当前曲目，核验 paused。  
**B 描述** 语音「暂停」、打字、媒体中心暂停。`action=pause`。媒体键路径与 `play_pause` 共用 VK。  
**链路** 同 media-transport：选会话 → pause → 核验 paused。失败：无可暂停会话；fallback：键已发送未核验。

---

### 3.4 下一曲 · `feature.dialog.music.next`

**A 简介** 切到下一首。  
**B 描述** 外部播放器用 VK_MEDIA_NEXT_TRACK；自有队列用 queue next + auto_advance。禁止一次派发两套路径导致连跳两首（`SendMediaKey` 注释约束）。  
**成功** 新曲名/队列指针变化且核验通过。  
**失败** 队列空 / 播放器不响应。  
**重试** 只再发一次 next。  
**降级** 提示「没有下一首」或打开媒体中心让用户挑。

---

### 3.5 搜索并播放歌曲 · `feature.dialog.music.search`

**A 简介** 按歌名搜索并播放（外部播放器或本机资产）。  
**B 描述** 「放周杰伦的晴天」→ 意图 media.play + query。外部 origin 不把文件导入 owned queue、不抓版权资源。本机资产则走 `media.asset.open` + play。  
**链路** 输入 → ASR → 意图（歌名实体）→ 解析资产或外部查询 → play → 核验。  
**失败** 无匹配 / 无许可。**降级** 列出近似名或打开媒体中心。

---

### 3.6 打开文件 · `feature.dialog.file.open`

**A 简介** 用对话或文件对话框打开本机/工作区/同事传来的文件。

**B 描述**  
三条入口合成同一核验：对话「打开桌面上的周报.docx」；工作区点文件；同事聊天点附件。Renderer **不得**拿绝对路径当 payload；走 assetId / destPath 由引擎解析。Windows 上选文件走 `GetOpenFileNameW` 或托管对话框。

**C 属性** tools=desktop.files / people.OpenFile / agentHub.file.open；operations=文件  

**D 方法** 语音「打开某某文件」；打字；工作区单击；同事聊天附件。

**链路**

```
1 入口        对话 / 工作区 / 同事聊天 / Agent Hub
2 解析目标    文件名、assetId、会话附件，禁止渲染进程直接拼盘符路径
3 权限        工作区范围或用户点选；越权拒绝
4 打开        关联应用或产品内预览
5 核验        预览可见或外部进程窗口出现
```

| 分支 | 说明 |
|---|---|
| success | 预览或外部应用已开 |
| failure | 找不到 / 无权限 / 对话框取消 |
| retry | 再弹出选择框 |
| fallback | 只揭示所在文件夹，不擅自打开 |

**锚点**：`internal/desktopfiles/pick_windows.go`、`internal/people/service.go OpenFile`、`internal/agenthub/pick_files_windows.go`、`bridge agentHub.file.open`。

---

### 3.7 打开同事发来的文件 · `feature.office.people.open-file`

同 file-open，入口固定同事聊天。成功：本地 staging 打开。失败：文件未下完。重试：再下再开。降级：只预览元数据。

---

### 3.8 打开电脑应用 · `feature.dialog.app.launch`

**A 简介** 打开计算器、浏览器、网易云等已装应用。  
**B 描述** 与开播放器同族，目标不限播放器。开始菜单 / App Paths / 快捷方式。  
**链路** 同 intent-control 五步。失败：未安装。重试：App Paths。降级：报应用名并请用户从开始菜单开。

---

### 3.9 打开媒体资产 · `feature.office.media.asset-open`

**A 简介** 在媒体中心打开已授权的音视频资产。  
**B 描述** `media.asset.open` 要 assetId + mediaSessionId，回 playbackUrl（`https://media.lunitide.local/v1/assets/...`）和过期时间。禁止 payload 带本机 path。  
**链路** 选资产 → 校验会话 → 发 open → 领 URL → 自有播放器 loadedmetadata。  
**失败** 资产 missing/revoked。**降级** 提示重新选择。

---

### 3.10 开始会议听写 · `feature.office.meetings.start`

**A 简介** 开始会议拾音并进入转写管线。  
**B 描述** 会议页点开始或语音「开始会议记录」。音频进 meetings 管线，ASR 可本机 sherpa / 系统 / 火山（设置决定）。  
**链路** 进入会议页 → 检查麦克风 → start → 拾音 → 实时字幕。  
**失败** 无麦 / 权限。**降级** 只录文件稍后转写。

---

### 3.11 生成会议纪要 · `feature.office.meetings.summary`

**A 简介** 把转写做成纪要。  
**B 描述** 转写完成后生成摘要；来源记在 meeting_summary_source。  
**链路** 取转写 → 调模型 → 落库 → 展示。失败：无转写。重试：换路由。降级：只给原文。

---

### 3.12 检索机务手册 · `feature.office.mro.search-manual`

**A 简介** 在机务工作台按关键词查手册。  
**B 描述** 已注册手册上检索，带 scope receipt。  
**链路** 进入 MRO → 输入 → 检索 → 展示片段。失败：未注册手册。降级：提示去注册。

---

### 3.13 召回记忆 · `feature.assets.memory.recall`

**A 简介** 按对话上下文取出相关记忆。  
**B 描述** memory fabric / retrieval，不把用户密钥当记忆展示。  
**链路** 查询 → 检索 → 排序 → 注入上下文。失败：库空。降级：不注入，对话照走。

---

### 3.14 OCR 识别截图 · `feature.assets.ocr.screenshot`

**A 简介** 把截图打成字。  
**B 描述** 本机 OCR 兜底（Windows OCR / RapidOCR / 已验证的 PP-OCR 包）。未验证 runtime 不得当可用。  
**链路** 截图 → 选引擎 → 识别 → 回文本。失败：无引擎。降级：提示安装 OCR 包。

---

### 3.15 执行白名单命令 · `feature.execution.command.run`

**A 简介** 跑一条产品清单里签过名的命令，不走裸 shell 字符串。  
**B 描述** `CommandSpec` + argv 模板 + Job Object。未在清单或被撤销的拒绝。  
**链路** 选 spec → 填参 → 校验 cwd/env → 启动作业 → 回执。失败：签名/超时/不在清单。无「随便拼一条 cmd」的降级。

---

### 3.16 工作区打开文件 · `feature.execution.workspace.open-file`

同 3.6，入口固定工作区 CodePanel。读 `desktop.files.readChunk` 一类方法，内容进编辑器。

---

### 3.17 浏览器打开网址 · `feature.execution.browser.navigate`

**A 简介** 让产品浏览器打开一个 URL。  
**B 描述** `br.navigate`，Playwright/探测到的 Chrome。  
**链路** 给 URL → 校验 → navigate → snapshot 可选。失败：浏览器未就绪。降级：提示去浏览器设置探测。

---

## 4. 初版标签（rule 层，写进 tags.json）

| vocabulary | 值 | 打给 |
|---|---|---|
| scenario | 娱乐 | music.* |
| scenario | 办公 | office.* / meetings.* |
| scenario | 机务 | mro.* |
| scenario | 开发 | command.* / workspace.* / agenthub.* |
| capability | 语音 | companion.* / voice / meetings.start |
| capability | 控制 | computer.* / app.launch / music.open-player |
| capability | 媒体 | media.* / music.* |
| capability | 检索 | memory.recall / mro.search-manual |
| entry | 语音 | dialog.companion.* / dialog.music.* / dialog.file.open |
| entry | 打字 | dialog.chat.* |
| entry | 菜单 | page.* / settings.* |
| status | 核心 | 上表「详」卡 + Page 入口 |
| status | 增强 | 媒体 queue 编辑类 |
| status | 实验 | landscape.* |

---

## 5. 竞品 / 前沿种子（不进健康度）

初版只放槽位，人可改 `landscape.json`：

| stable_key | 类型 | 说明 |
|---|---|---|
| landscape.competitor.placeholder | competitor | 对比维：语音本机控制 / 媒体核验 / 技能/MCP / 知识自描述。来源与日期必填，禁止无出处结论 |
| landscape.frontier.smtc-verification | frontier | 观察：以 SMTC/owned runtime 为播放真相，而不是「键发了就算成功」 |

---

## 6. 初版之后怎么自动变新

1. 产品加「收藏歌曲」且加了 `media.favorite` 的 schema → catalog 多一个 method → 下次快照多 `bridge.media.favorite` + 模板卡。  
2. 若要说明书写到 §3 这种细度：给该 key 加 overlay 或 seed.v2 补丁（只含这一张）。  
3. 产品删了某个 Page → 对应 `page.*` / `feature.*.page.*` 退役，种子讲解仍留在 git 供考古，图上标弃用。  
4. 诊断器盯：模块零卡、枚举未出卡、种子引用活源不存在、链路悬空。

这份初版的职责到此结束。下一版清单由引擎生成，不再手抄总表。
