# Lunitide 0.4.60

「六项表面」体验深度复盘后的整体硬化与闭环：办公折叠、会议·火山听写、月伴模型一致性、说话月视觉、电脑控制编排、意图自动装备，配套一次航空机务专家的更名与内置人设技能。本轮把六项从复盘初值 3.9/5 提升到 **4.9/5**，全链路测试与门禁全绿。

## 1. 会议 · 火山听写（ASR）硬化

- **双麦互斥**：外部录音 PCM 首帧到达后加 `externalSeen` 闩锁，1.6s 自救麦克风不再与录音 tap 并采，杜绝重复字幕/回声。
- **火山选项预禁用**：未配置 seed-asr 供应商时，会议听写「火山」磁贴 `disabled` 并给出 tooltip 说明；若历史默认为火山但当前不可用，磁贴下方常驻 warn。把「运行时静默回退」提前成「选择时即可见」。
- **录制诊断条**：录制中常驻一行诊断（引擎：火山/本机/系统 · 供应商前缀 · 音源：外部 PCM 单路/浏览器/引擎麦 · 字幕：直采/已回退本机），客服与用户可一眼判断当前走哪条链路。

## 2. 月伴（Companion）

- **模型一致性**：月伴「想」灯跟随主页/会话已选模型，仅在为空或缺失时才回退默认 flash；标签改用模型 `displayName`。
- **办公流水线 lite**：月伴语音任务句注入结构化办公流水线（结构 → 逐页正文 → `web.search` → 最后再 `gen`），修复 `DisableReasoning` 下 PPT/报告直接跳到生成、空页被拒的断层。
- **说话月视觉**：说话态与思考态统一走玻璃丝带渲染；等离子 `uScale` 由 1.15 调到 0.82（越小球越大），说话球不再缩小、保持居中放大。以确定性单测（`moonVisual.test.ts`）锁定 scale 不变量与 CSS 尺寸/居中契约（34→46vmin、`scale(1.38)`、900px 断点 44→56vmin），替代人工多 DPI 截图。

## 3. 意图自动装备（可见 + 防误命中）

- **结构化装备事件**：新增桥事件 `EventEquip`（`experts / skills / missingMcp`），`turnEquipBanner` 重构为 `turnEquipInfo` 返回结构化数据。
- **常驻 UI Chip**：文本对话命中意图专家时，输入框上方渲染「🧩 本轮已装备…」chip；「未连接 MCP」按钮深链到 MCP 页去连接（月伴语音态不显示，避免噪声）。
- **释义句防护**：`intentQueryDefinitional` 拦截「X 是什么意思 / 有什么区别」等纯释义问句，句中带任务动词（做/写/生成/设计…）时放行，避免把「数据库是什么意思」误装数据库专家。

## 4. 航空机务专家更名与内置人设

- 卡片更名 `航空机务专家` → **`航空机务维修专家`**；`ConversationExpertByName`、前端 `conversationExperts.ts`、`chat_kb_inject.go`、`mro_scenarios.go` 均保留旧名后向兼容。
- 新增内置技能 `aircraft-maintenance-engineer`（适航优先、手册可追溯、MEL 四道门；辅助建议不构成放行），置于 `preferredSkills` 首位。
- **就地迁移**：`renameShippedMROExpert` 幂等重命名已安装卡片（不新建第二张）；`MergeExpertSkillKeys` 把新出厂技能键并入已安装专家的既有绑定（空绑定回退整表替换，非空仅增量并集、无删除、无副本），老装机升级即得。

## 验证

- Go：`go vet ./...` 干净；`go build ./...`（CGO=0）干净；`golangci-lint run ./...` **0 issues**；`govulncheck ./...` 受影响漏洞 **0**；`go test ./...` **全绿**（总覆盖率 **50.3% ≥ 49%** 闸）。
- 前端：`tsc --noEmit` 通过；`verify:bridge` 无漂移；`vitest run` **186 files / 1407 tests** 全绿；`vite build` 成功。

## 安装包

- `release/out/Lunitide-Setup-0.4.60-x64.exe`
- `release/out/SHA256SUMS.txt`
- 从 0.4.59 升级
