# M9 威胁模型 5/6：混合 Runner 信任、驻留路由与 UNKNOWN 恢复

> 阻断期产物（T-9.0.2）。概念威胁模型，不是生产合同；不含 DDL/API 决策。
> 范围：FR-08 混合 Runner（local/managed/cloud 按驻留、能力、信任路由）+ Execution Governance 只读投影。
> 映射用例：T-13（禁出域数据仅有云 Runner → 阻断）、T-14（有副作用运行 Runner 失联 → UNKNOWN 不重复派发）。

## 1. 资产
- RunnerRegistration（kind、workload_identity、attestation、region、capabilities、network/secret tier、status、lease/heartbeat）
- 能力票据（绑定组织、策略版本、包摘要、预算预留、有效期）
- ResidencyPolicySnapshot（org/data_class/purpose、允许 storage/processing/backup regions、runner kinds、key region 等）
- M6 CloudTask/Runner canonical 状态（M9 只读投影，禁第二状态机）

## 2. 威胁主体
- 自报驻留/能力虚假标签的 Runner
- 试图把禁出域数据降级发往云 Runner 的调用方（可用性压力下）
- 心跳伪造者（把已死 Runner 伪装在线）
- 利用 outcome_unknown 重复触发副作用的重放者

## 3. 攻击面（STRIDE）
| # | 攻击场景 | 类别 | 影响 |
|---|---|---|---|
| S1 | Runner 自报 region/capabilities 被采信（无证明） | Spoofing | 数据出域 |
| S2 | 数据分类/驻留禁止云 Runner，但无本地/托管可用时"降级"外发 | Elevation | 驻留违规 |
| S3 | 心跳在线但证明过期/失败仍路由任务 | Spoofing | 不可信执行 |
| S4 | 票据未绑定包摘要/预算/组织：任务跑非批准内容或他组织数据 | Tampering | 票据重放 |
| S5 | Runner 失联（有副作用步骤）→ 自动另跑一次 | Tampering | 副作用重复 |
| S6 | 回执丢失后重复派发/重复结算 | Tampering | 双重扣费/执行 |
| S7 | M9 侧自建 FSM 推进/重试/补偿（违反禁第二状态机） | Tampering | 状态分叉 |
| S8 | 网络出口/Secret tier 不匹配仍绑定（如出网 Runner 拿内网 Secret） | Elevation | 凭证外泄 |

## 4. 缓解措施
1. 心跳不是信任证明：注册+证明+短期票据+网络出口+驻留标签共同决定路由（02 市场、审计与 Legal Hold，S1/S3）。
2. 路由输入必须含数据分类、驻留、能力、信任、网络、Secret、预算、可用性；任一必需约束无匹配 Runner → BLOCKED 不降级外发（M9-021/022，S2/T-13）。
3. 票据短期有效且绑定组织、策略版本、包摘要、预算预留；任务组织取自票据而非请求（S4）。
4. 失联 → DISPATCHED→UNKNOWN：不自动另跑有副作用步骤；只允许诊断、终止或独立批准恢复（S5/T-14）。
5. 结算幂等对齐 M6 CloudTask 回执；重放不重复扣款（M9-025 协同，S6）。
6. M9 投影只读：DISPATCHED/CHECKPOINTED/SUCCEEDED/UNKNOWN/COMPENSATING 直接投影 M6 canonical 状态；冲突时以 M6 事实对账并 fail closed（S7）。
7. network/secret tier 匹配为硬约束（S8）；证明失败 → quarantined。

## 5. 可验证断言（解封后落入测试）
- T-13：禁出域数据仅有云 Runner → 阻断，不跨域降级（M9-021/022）。
- T-14：有副作用运行时 Runner 失联 → UNKNOWN，不重复派发。
- Runner 断网、回执丢失、重复派发注入 → UNKNOWN 安全恢复，副作用不重复。
- 复用证明：仅调用 M6 契约；不存在第二状态机或双写账本（M6 内核复用验收）。

## 6. 残余风险与移交
- 证明格式与远程证明根属 D-4；未决前 attestation 仅原型占位。
- UNKNOWN 对账窗口内的用户体验（长时间不可判定）→ 运营度量需含 Runner 健康与对账时延指标（FR-12）。
