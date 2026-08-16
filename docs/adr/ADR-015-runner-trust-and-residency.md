# ADR-015: Runner 信任与驻留（D-4）

## Status
Accepted（v1.0.0 · 2026-08-16 · 决策人：项目业主 · 接受方式：会话确认 2026-08-16）

## Context
M9 FR-08（混合 Runner）要求冻结 Runner 证明格式、驻留路由与 UNKNOWN 语义；
威胁模型 `docs/threat/m9-05-runner-residency.md`（T-13/14）已评审。
M6 已交付 CloudTask/Runner canonical 状态机与委派签名（ed25519 envelope）。

## Decision
1. **证明格式**：Runner 注册时提交签名证明（proof）：`{runner_kind, trust_tier,
   residency_regions, key_region, capabilities, network_egress, issued_at, nonce}`，
   以 ed25519（复用 `internal/delegation` envelope 机制）签名；证明可验证、可审计、
   带时效。本地 Runner 默认 trust_tier=trusted、residency=in-org；云 Runner 需有效证明。
2. **驻留路由**：路由输入必须包含数据分类、驻留地域、能力、信任证明、网络、Secret、
   预算与可用性八要素；任一必需约束无匹配 Runner 时**阻断派发，不降级外发**
   （「数据禁止出域；当仅云 Runner 可用；则阻断而不外发」）。
3. **UNKNOWN 语义**：Runner 失联转 UNKNOWN；UNKNOWN 期间**不自动另跑有副作用步骤**；
   恢复仅经治理对账（以 M6 事实与 M9 治理实体分别对账），冲突即 fail closed。
4. **M6 复用约束（批准条件之一）**：M9 不建第二状态机/第二内核——
   RECEIVED/POLICY_LOCKED/WAITING_APPROVAL/BUDGET_RESERVED/ROUTED 为只读投影，
   DISPATCHED/CHECKPOINTED/SUCCEEDED/UNKNOWN/COMPENSATING 直接投影 M6 canonical
   状态与事件；M9 不得独立推进/重试/补偿/写第二账本/建第二 checkpoint。

## Alternatives considered
- **无匹配 Runner 时降级到云 Runner**：被否决——违反数据不出域硬约束。
- **UNKNOWN 后自动重跑副作用步骤**：被否决——副作用重复不可接受，须人工/治理对账恢复。
- **自研派发状态机**：被否决——出现「新建第二内核」即违规退卡。

## Consequences
- 解锁 T-9.3.2（execution/broker.go，`go test ./internal/execution/ -run TestBroker`）。
- 无安全 Runner（证明无效/过期/驻留不符）一律阻断派发（M9-019/020/021/022 负测）。

## 评审记录
- 威胁模型评审：`docs/threat/m9-05-runner-residency.md`。
- 接受签字：项目业主（待签）。
