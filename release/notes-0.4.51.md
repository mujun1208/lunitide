# Lunitide 0.4.51

在 0.4.50 的完整性/防重放收口之上，本包落地 road-to-4.9 的**执行深度（阶段二）**与一项 token 优化（阶段四 S1），并合入前序语音/生命周期硬化。全部改动 test-first，默认行为对既有用户不破坏：新增能力要么走既有审批闸门，要么只在“已授予写权限的回合 / 纯闲聊回合”生效。

## 新增（阶段二 · 执行深度，对标 Codex/OpenClaw）

- **E1 `run_terminal_cmd` 终端工具**：模型可用一条命令行直接跑 build/test/lint/git 循环。零新增执行面——运行时在进入 hooks / 变更闸门 / switch 之前把 `{command}` 规范化成 `command.run` 的 `{argv}`，完全复用同一套内置+用户白名单、full-disk 解锁、hardline 底线、审批闸门与有界流式输出。argv 直执行（无 shell），`| & ; < > \` $` 等 shell 操作符明确拒绝而非静默误跑。与 `command.run` 同为 mutating（approval/auto-edit 下逐次审批）、串行、月伴禁用、危险名单、且不进只读子代理工具集。
- **E2 git 写入白名单**：内置白名单新增可逆写 `git add / commit / stash`（仍受 mutating 审批，approval 模式逐次确认）。破坏性动词 `checkout/reset/clean/restore/push` 仍需 `command-policy.json` 显式开启，避免静默毁掉未提交改动。
- **E3 可写“实现者”子代理**：新增 governance-gated `implementer` profile，可 `workspace.write/edit` 并跑白名单命令。**只有当派生回合本身已持写权限（auto-edit / full-access）时**才授予写工具；approval/plan 模式下自动降为只读。委派永不越权——与 `subagentToolMode` 同一不变量。
- **E4 自适应工具步数**：把硬性 24 步截断改为自适应上限。非月伴回合若在接近上限时仍在产出真实工具调用，步数上探至 48 硬顶；停滞/空转回合永不触发扩容；月伴（语音）回合保持固定预算。每步落检查点，扩容即断点续跑。

## 新增（阶段四 · S1 token 优化，对标 PI/DeepSeek）

- **S1 按任务自动选工具面**：客户端未显式指定 profile 且非月伴回合时，从回合意图推断。短且高置信的纯闲聊回合（问候/致谢/寒暄，无任务信号）收窄到 `minimal`，丢掉它永不调用的完整工具+MCP+技能+专家+插件 schema。任何可执行意图或超 40 字的回合都保留完整面；minimal 仍保留 web/memory/user.ask，误判也能优雅降级。

## 合入的前序硬化（语音 / 生命周期）

- 火山模式重声修复（talk PCM 连续拼接不再叠 140ms 交叉淡入）、cascade seed-tts 与 talk 并发消音、卸载即停 talk/mic/轮询、语音换模型竞态收口、本地 GPT-SoVITS 启动路径去硬编码 + 按需下载脚手架、语音延迟诊断面板、侧栏“同事聊天”移位与横线清理。

## 不要指望本包做的

- **阶段三 M1/M2（向量召回+信任分层自动接受、专家共享记忆总线）与阶段四 S2（collabgate 写协作）未落地**：它们与产品既有的强制人工确认记忆、collabgate 默认关闭、“不启动 P2 独立 Agent 运行时”等**书面架构冻结**直接冲突，且 M1 需要 embedding/向量库这类新子系统。以默认开启形态硬上会破坏产品自身的安全/治理契约，须走冻结要求的产品决策，或以默认关闭旗标另行分支落地。
- **阶段一剩余 V2 乐观终字**需真麦风验证（会改变送入 LLM 的转写准确度），V4/V6/V7 现有降级/恢复/持久化已相当完备——均按上一轮结论保持默认关闭旗标或暂缓。
- 不重写 chat.go / engine 上帝对象；不改默认执行模式；语音主尺不因本包变化。

## 验证

- `go vet ./...`、全量 `go test ./...`（0 失败）、`internal/app` + `internal/toolruntime` 定向复跑绿
- `golangci-lint run`（app + toolruntime）0 issues
- 前端 `tsc --noEmit` 通过、`vitest run` **171 files / 1307 tests** 全绿
- 新增单测：`terminalCommandToArgv`（解析/引号/拒绝 shell 操作符）、`extendToolLoopLimit`（边界+硬顶）、`implementer` 写闸（跨全部执行模式）、`autoToolProfile`（闲聊→minimal / 任务→default / 空+超长→default）、E2 白名单 allow/deny、前端 `approvalProfile`（run_terminal_cmd 危险分类）
- 未做桌面 WebView 真机点选

## 安装包

- `release/out/Lunitide-Setup-0.4.51-x64.exe`
- `release/out/SHA256SUMS.txt`
- 从 0.4.50 升级
