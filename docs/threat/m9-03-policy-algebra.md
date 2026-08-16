# M9 威胁模型 3/6：策略层级代数与"只可收紧"证明

> 阻断期产物（T-9.0.2）。概念威胁模型，不是生产合同；不含 DDL/API 决策。
> 范围：FR-04 PolicyCenter（platform→organization→teamspace→project→run 只可收紧）。
> 映射用例：T-05（子级放宽全部拒绝）、T-06（父发布与子保存竞态）。

## 1. 资产
- PolicyVersion（scope、parent_version、canonical rules/digest、status、author/reviewer、effective_at；发布后不可变）
- 约束分量：allowlist、denylist、上限（预算/超时等）、必需项（强制 Review 等）
- 运行期钉住的策略版本凭证

## 2. 威胁主体
- 试图放宽父级约束的空间/项目管理员（有意或误配）
- 利用父版本发布竞态窗口的子草稿提交者
- 通过非规范化表达（等价改写）绕过字面比较的攻击者
- 令策略求值不可用来降级为"默认允许"的攻击者

## 3. 攻击面（STRIDE）
| # | 攻击场景 | 类别 | 影响 |
|---|---|---|---|
| S1 | 子级 allowlist 并入父级未允许项（放宽） | Elevation | 越权能力 |
| S2 | 子级抬高上限/预算额度 | Elevation | 超支 |
| S3 | 子级移除父级 deny/required 项 | Elevation | 强制控制丢失 |
| S4 | 等价改写绕过（新增更宽的并行通道而非修改原分量） | Tampering | 字面"收紧"实为放宽 |
| S5 | 父版本发布与子保存竞态：子基于旧父证明，提交时父已变严 | Tampering | 证明失效仍发布 |
| S6 | 求值器故障/超时被当作"通过" | DoS 降级 | fail-open |
| S7 | 已发布版本被原地改写（非 supersedes） | Tampering | 历史失真、审计断链 |
| S8 | 运行期使用非钉住版本（运行中途策略漂移） | Tampering | 执行与批准口径不一 |

## 4. 缓解措施
1. 规范化后合并：允许集合取交集、上限取最小、禁止项与必需项取并集（02 PolicyCenter）。
2. 发布前机器证明"相等或更严格"：逐分量比较规范化 AST，不可证明即拒绝（M9-009，S1~S4）。
3. 子保存携带 parent_version；服务端检测父已发布新版本 → 子草稿重新证明（M9-008/010，S5/T-06）。
4. 求值不可用一律 fail closed（M9-011），禁止默认通过（S6）。
5. PolicyVersion 发布后不可变；新版 supersedes 旧版不改写历史（S7）。
6. 运行钉住策略版本；策略变严时运行重新求值（POLICY_LOCKED 投影恢复语义，S8）。
7. 拒绝应答定位冲突来源（父/子值与来源逐条展示，UI 状态"策略冲突"）。

## 5. 可验证断言（解封后落入测试）
- T-05：子级放宽 allowlist/上限/必需项 → 全部拒绝并定位父规则（M9-008/009）。
- T-06：父发布与子保存竞态 → 子草稿重新证明（M9-010）。
- 策略包随机组合属性测试：allow 只取交集、deny/required 只取并集、上限只减（04 验收）。
- PolicyCenter 超时/不可用 → 失败关闭（M9-011），无降级外发。

## 6. 残余风险与移交
- "等价改写"全集不可判定 → 属性测试+规范化 AST 收敛；无法证明即拒绝是最终防线。
- 策略代数形式化（D-3）accepted 前，任何合并实现都可能被推翻 → 原型仅验证概念（tools/proto-m9/policy；可丢弃原型已于切片 1 生产实现合入后按计划整体删除，生产实现见 internal/policy/algebra.go）。
