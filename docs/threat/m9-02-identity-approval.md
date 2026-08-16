# M9 威胁模型 2/6：身份生命周期、SoD 与 N-of-M 审批

> 阻断期产物（T-9.0.2）。概念威胁模型，不是生产合同；不含 DDL/API 决策。
> 范围：FR-03 身份角色 / FR-05 职责分离 / FR-06 N-of-M。
> 映射用例：T-03（外部身份过期）、T-04（服务身份冒充人类门槛）、T-07（自批）、T-08（撤票）、T-09（多角色重复计票）。

## 1. 资产
- Principal（human/service/external；issuer/subject、auth_strength、status、expires、revoked_version）
- RoleBinding（scope、role、valid_from/to；组织不可改绑）
- ApprovalRequest（operation_snapshot_digest、candidate_set_digest、n/m、status、expiry）
- Vote（approver、decision、auth_context、created/revoked）

## 2. 威胁主体
- 请求者 attempting 自批；共享审批身份的小团体
- 持过期/被吊销身份或绑定的调用者；被攻陷的服务身份
- 通过重复投票/身份轮换稀释阈值的攻击者
- 利用时钟偏移延长批准票有效期的攻击者

## 3. 攻击面（STRIDE）
| # | 攻击场景 | 类别 | 影响 |
|---|---|---|---|
| S1 | 请求者用第二角色给自己投票（同主体多角色） | Elevation | SoD 失效、假 quorum |
| S2 | 多人共享一个审批身份（凭证借用） | Spoofing | 一人凑齐 N 票 |
| S3 | 外部身份已过期仍批准/执行；binding 版本变化后旧批准仍有效 | Repudiation | 失效批准执行 |
| S4 | 服务身份计入"人类批准"门槛 | Elevation | 自动化绕过人类控制 |
| S5 | 候选集在请求创建后被替换/扩充（未冻结） | Tampering | 攻击者自选审批人 |
| S6 | 批准绑定旧 operation_snapshot_digest，请求内容已被篡改 | Tampering | 批准与执行不一致 |
| S7 | 2-of-3 达标后撤票，但已派发执行不回退（时序竞态） | Tampering | 未授权执行 |
| S8 | 时钟偏移使过期投票/请求复活 | Tampering | 有效期绕过 |
| S9 | 已执行事实被撤票"反转"（历史改写） | Repudiation | 审计失真 |

## 4. 缓解措施
1. SoD：请求/审批/执行/审计角色互斥按动作计算；自批拒绝并审计（M9-007，S1/T-07）。
2. 审批人不共享身份：Vote 绑定 auth_context，同主体多角色只计一票（T-09，S1/S2）。
3. 身份/binding 版本变化使未执行批准立即失效；过期外部身份的令牌与批准票同失效（M9-005，S3/T-03）。
4. 服务身份不能满足人类批准门槛（principal_type 强校验，S4/T-04）。
5. M 候选在请求创建时冻结为 candidate_set_digest；非候选投票拒绝（M9-013，S5）。
6. 每票绑定 operation_snapshot_digest + 策略版本；摘要变化触发重算回 WAITING_APPROVAL（S6）。
7. 撤票/身份失效/策略收紧 → 阈值重算；执行前守卫强制复查 quorum（S7/T-08）。
8. 时间判定用单调时钟+服务端权威时间窗；临界偏移 fail closed（S8）。
9. approved→waiting_approval 可回退，但已执行事实不可反转，只可补偿（M6 语义，S9）。

## 5. 可验证断言（解封后落入测试）
- T-03：外部身份过期 → 令牌、批准票和运行失效（M9-005）。
- T-04：服务身份参与人类门槛 → 不能满足该批准要求。
- T-07：请求者自批 → SoD 拒绝并审计（M9-007）。
- T-08：2-of-3 达标后撤票 → 执行前回 WAITING_APPROVAL（M9-014 参与语义）。
- T-09：同一主体多角色投票 → 只计一票。
- 撤票/重复投票/时钟偏移故障注入 → 阈值重算且审计完整（M9-012 未达标路径）。

## 6. 残余风险与移交
- 凭证借用（真实多人共享）无法纯技术根除 → 需认证强度（auth_strength）策略与访问复核运营控制。
- SoD 矩阵复杂度随角色数膨胀 → 解封后策略代数（见模型 3）需含互斥可判定性证明。
