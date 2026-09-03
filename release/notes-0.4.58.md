# Lunitide 0.4.58

七个真实运行期缺陷一次收口：供应商同步 404、火山重复音、会议纪要云端/火山录不到、项目管理工作台跳动与"表面化"、月伴说话时圆球缩小偏心、以及"电脑控制已开却打不开桌面应用/一直思考中"。全部安装即用，无需任何额外配置。

## 1. 配置供应商：点「从供应商同步模型」报错

- **根因**：不少 OpenAI 兼容端点（尤其火山方舟 Plan `/api/plan/v3`）**没有 `GET /models` 列表接口**，同步时直接 404。旧逻辑把 404 当成连接失败，整条同步硬失败并复用了通用报错文案。
- **修复**（`internal/app/provider_diagnostics.go` + `web/src/provider/ProviderApp.tsx`）：发现 discovery 返回 404 时**保留该供应商已有模型**并回一条 `MODEL_LIST_UNSUPPORTED` 友好提示（"该供应商未提供模型列表接口，请手动填写模型 ID 后保存"），不再把 `chat/completions` 明明可用的供应商判成坏的。

## 2. 月伴·火山：还是"前后两个声音"

- **根因**（`web/src/session/companion/ttsPlayer.ts`）：火山是唯一"始终流式"的级联引擎，`playStreamingSegment` 把每个流式分片按 `join=true`（140ms 交叉淡入）排布，等于把上一分片的尾巴又重放一遍——听感就是回声/两个声音。
- **修复**：流式分片改为 `join=false` 严格首尾相接（与 `enqueueTalkPcm` 一致）。新增回归测试 `ttsPlayer.volcstream.test.ts`：`join=false` 零重放、`join=true` 会重放。

## 3. 会议纪要：选「云端」/「火山」时录不到内容

- **根因**：云端（浏览器 Web Speech）与会议录音抢麦克风、可能静默无字幕；火山需要单独的 seed-asr 语音凭据，起了也可能不吐字。两者都会"起了但没内容"。
- **修复**（`web/src/meetings/MeetingPage.tsx` + `meetingAsr.ts`）：新增**实时字幕看门狗**。选中的云端/火山引擎在 8 秒窗口内没有任何字幕时，**透明回退到本机 sherpa**（吃同一路麦克风+系统声混音 PCM）并给出状态提示；本机 sherpa 未安装时明确告知"实时字幕不可用，停止后仍会补出完整逐字稿"。新增纯函数 `shouldFallbackLiveCaption` 单测。

## 4. 项目管理·工作台：对话框一直跳动 + 功能"表面化"

- **4a 聊天跳动根因修复**（`web/src/session/SessionPage.tsx`）：多个滚动 pin 触发源（主 effect、ResizeObserver）互相竞争。改为**用单个 `requestAnimationFrame` 合并所有 pin**，流式增量下稳定跟随不抖动。
- **4b 交付物证据闸**（`DeliverablePanel.tsx`）：交付物只有在**绑定了附件 / 引用了资产库模版 / 存了清单**时才能点"确认"批准，杜绝一键确认空交付物造成的"看着完成、其实没内容"。
- **4c 全阶段三关 + 软完成锁**（`deliverableTypes.ts` + `ProjectWorkbenchShell.tsx`）：三关晋级从旧的 `[1,2,6,7,8]`（跳过数据库/接口/开发）**扩展到全部有交付物的阶段**；跨到后续阶段而上一阶段尚未三关确认时，给一条**可忽略的软提示**（不硬拦，提示衔接风险）。
- **4d 阶段对话产出接入交付物**（`internal/app/chat_workflows.go`）：阶段工作流注入现在明确要求模型产出规范/设计/清单的**完整正文并提示保存到右侧「交付物」草稿**，需要文件时用 `structured.output`/`docx.gen`。
- **4e 切阶段不再整块重挂**（`ProjectWorkbenchShell.tsx`）：`SessionPage` 的 key 从 `项目:会话` 收敛为**仅项目**，切阶段时通过 `initialSession` 重播换会话而不整块卸载重建——消除切换时的闪动与"未定义会话"竞态。
- **4f 本阶段专家成为一等控件**（新增 `PhaseExpertsBar.tsx`）：工作台侧栏直接展示/增删本阶段挂载的专家（复用 `session.experts.get/set`），不再只能从 @ 菜单里改。

## 5. 月伴说话时：圆球缩小且偏心

- **根因**（`web/src/session/companion/visual/moonVisual.ts` + `styles.css`）：说话态 `uScale` 用 1.45（uScale 越大等离子越"缩"），比思考态 1.15 明显小；CSS 又叠了**非对称 `scaleX(1.22)`**，把等离子相对居中的光环拉偏。
- **修复**：说话态 `scale` 对齐思考态 1.15（不再缩），活力仍由 gain 驱动的 speed/amplitude/glow 承担；CSS 换成**对称 `scale(1.04)` + `transform-origin:center`**，居中不变形。

## 6/7. 电脑控制已开，却打不开桌面「汽水音乐」/ 一直思考中

- **6a 状态实时刷新**（`ComputerPanel.tsx` + `SessionPage.tsx` + `ensureCompanionCapabilities.ts`）：电脑控制开关变化时派发 `lunitide:cc-config` 事件，月伴舞台监听该事件 + 窗口 focus + 可见性变化**即时刷新横幅**，不再只在进月伴那一刻读一次导致"明明开了还提示未启用"。
- **6b 站立式授权去掉审批卡顿**（`approval_profile.go` + `companion_context.go` + `companion_capabilities.go`）：电脑控制开启即视为对**发射型桌面工具**（`desktop.open` / `media.play`）的站立授权——语音轮直接执行，不再卡在没人能点的审批弹窗上（"一直思考中"的根因）。`cc.*` / `computer.act` / `desktop.type` 这类能点任意像素/往窗口打字的工具**仍需确认**。
- **6c 汽水音乐解析加固 + 清晰失败原因**（`desktop_launch.go` + `desktop.go`）：补齐汽水音乐常见安装路径（含中文 `汽水音乐.exe`、`Programs\SodaMusic`、`ProgramFiles`）；解析失败时返回**中文明确原因**"桌面、安装目录和开始菜单里都没找到「X」，请确认已安装或把快捷方式放到桌面"，模型可直接转述。

## 验证

- Go：`go build ./...` 干净；`go test ./...` **0 失败**。更新 `approval_profile_test.go` 覆盖站立式授权（CC 关→需确认、CC 开→desktop.open/media.play 免确认、像素/键盘工具仍需确认）。
- 前端：`tsc --noEmit` 通过；`vitest run` **173 files / 1334 tests** 全绿。新增/更新 `ttsPlayer.volcstream`、`meetingAsr`（回退决策）、`ProjectWorkbenchShell.send`（合并 pin 跟随）单测。

## 安装包

- `release/out/Lunitide-Setup-0.4.58-x64.exe`（真签名，Authenticode 已验、时间戳）
- `release/out/SHA256SUMS.txt`
- 从 0.4.57 升级
