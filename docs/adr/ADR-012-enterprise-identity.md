# ADR-012: 企业身份模型（D-2）

## Status
Accepted（v1.0.0 · 2026-08-16 · 决策人：项目业主 · 接受方式：会话确认 2026-08-16）

## Context
M9 FR-03（企业身份）要求 IdP 桥接、外部身份、限时角色与令牌/批准票失效语义；
威胁模型 `docs/threat/m9-02-identity-approval.md`（T-03/04 身份生命周期）已评审。

## Decision
1. **Principal 本地目录为权威**：身份以本地 `principal` 表为权威根；外部 IdP（OIDC 桥接）
   仅作断言来源，映射为 Principal 绑定，不可旁路本地目录授权判定。
2. **限时角色**：`role_binding` 携带 `expires_at`；每次授权检查读取时钟判定过期，
   过期即时失效（M9-005），不存在宽限期缓存。
3. **令牌失效语义**：会话令牌与签名能力票据支持显式 revoke，revoke 即时生效；
   已发能力票据在 revoke 后按吊销水位（revocation watermark）拒绝。
4. **批准票失效语义**：审批票（approval vote）撤票即重算审批状态（2-of-3 撤一票回 WAITING，
   对应 T-9.2.2 / M9-012~014）；主体身份过期/吊销时其未决票作废。
5. **生命周期演练方案**（批准条件之一）：身份过期、令牌吊销、限时角色到期三类负测
   纳入 `go test ./internal/org/ -run TestIdentity` 全负测矩阵（M9-001/002/004/005/006）。

## Alternatives considered
- **直接信任外部 IdP 断言（无本地权威）**：被否决——IdP 不可达/断言漂移时无法 fail closed。
- **过期带宽限期缓存**：被否决——限时角色的安全语义要求即时失效。

## Consequences
- 解锁 T-9.1.2（identity.go）与 T-9.1.3（组织管理 UI）身份部分。
- 令牌/票据吊销水位须在备份恢复时先于读取开放加载（对齐威胁模型 m9-06 S7/T-19）。

## 评审记录
- 威胁模型评审：`docs/threat/m9-02-identity-approval.md`。
- 接受签字：项目业主（待签）。
