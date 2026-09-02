# Lunitide 0.4.52

落地此前明确"未做到位"的**阶段三 M1/M2 与阶段四 S2**——三项都以**默认关闭的治理开关**（`internal/config/governance.go`，仅由环境变量武装）落地为可选、可审计、不破坏冻结的 opt-in 通道。**开关全关（出厂默认）时，每一条书面架构冻结都与之前完全一致**：记忆仍需人工显式确认、专家仍在单引擎上隔离、协作闸仍关闭。这不是"解冻"，而是在冻结之上叠加一条受治理的通道。

## 新增（默认关闭，需显式武装）

- **M1 记忆信任分层 + 低风险自动接受**（`LUNITIDE_MEMORY_AUTOACCEPT`）：新增纯函数 `m8core.ClassifyMemoryRisk`（保守判定：含密码/密钥/身份证/银行卡等标记、`sensitive` 及以上敏感级、超 280 字、空内容一律判 high）与 `MemoryService.AutoAcceptCandidate`。仅当开关武装**且**载荷判为 low-risk 时，才走与 `ConfirmCandidate` 完全相同的 fact/leaf/transition 写入自动落库，审计为 `memory.candidate.auto_confirm`；high-risk 一律保持 pending、审计 `memory.candidate.auto_hold`，回退到人工令牌确认路径。**通用 `AutoPromote`（频率/置信/压缩）的硬拒绝（FR-11）原样不动**——M1 是独立、显式武装、按风险分级的旁路，不是被冻结禁止的自动晋升。
- **M2 专家共享工作记忆总线**（`LUNITIDE_EXPERT_SHARED_BUS`）：回合内、进程内的临时便签。武装后专家理事会由并行独立发言改为**顺序发言**，每位专家能看到前序专家的要点摘要（有界渲染，单条 400 字、整体 1600 字封顶）并在其上补充/提异议。**不新增任何进程/运行时/调度器/跨代理消息**——"不启动 P2 独立 Agent 运行时"冻结原样成立；总线不落库、不跨会话、不跨回合。
- **S2 协作闸写协作共享会话交接**（`LUNITIDE_COLLABGATE_HANDOFF`）：`CollabGateService.PrepareWriteCollabHandoff` 复用与 `CheckGate` 相同的 `capabilityLocked` 预检。**在没有已确认的启用决策（出厂默认）时，任何交接请求一律以 M8-028 拒绝、且仅产生一条审计**（`collabGate.handoff.refused`）；只有闸被显式启用后才铸造一次性交接票据（`collabGate.handoff.prepared`）。引擎入口再叠加一层默认关闭的 `CollabHandoff` 开关。

## 工程说明

- 三项审计动作均写入 `m7_audit_events` 的自由文本 `action` 列（长度约束、非枚举），**无需迁移、不触碰 0112 审计链的 schema 漂移守卫**。
- 开关为进程级（启动读环境变量或测试 setter），非按回合的隐式启发；零 bridge/前端契约改动。

## 验证

- `go vet ./...`、全量 `go test ./...`（0 失败）绿
- `golangci-lint run`（app / m8app / m8core / config / cmd/engine）**0 issues**
- 前端 `tsc --noEmit` 通过、`vitest run` **171 files / 1307 tests** 全绿、`generate-bridge --check` 无契约漂移
- 新增单测：`ClassifyMemoryRisk` 判定矩阵、auto-accept low/high/sensitive/unknown（含高风险仍可人工确认）、共享总线 render/封顶、交接 disabled/enabled/校验
- 未做桌面 WebView 真机点选

## 不在本包

- 未解除任何冻结的默认行为；三开关默认全关，出厂表现与 0.4.51 一致
- M1 未引入 embedding/向量库新子系统：召回沿用既有关键词 + FTS5 通道，M1 只加"信任分层 + 低风险自动接受"，不新增向量子系统

## 安装包

- `release/out/Lunitide-Setup-0.4.52-x64.exe`（真签名，Authenticode 已验、DigiCert 时间戳）
- `release/out/SHA256SUMS.txt`
- 从 0.4.51 升级
