# Runbook: Legal Hold 发起 / 解除 / 演练手册（T-9.4.4）

> **法律声明**：本手册是工程操作文档，**不替代法律意见**。Hold 的发起权威、
> 保全范围与解除依据以法务（D-5 决策所载法务角色流程会签）为准；工程侧只
> 负责忠实执行与留痕。任何与法律要求冲突的操作，以法律要求为准并立即冻结
> 相关对象。

- 实现：`internal/legal/hold.go`（T-9.4.3）
- 自动化验证：`go test ./internal/legal/ -run TestHold -v -count=1`
- 错误码：M9-028 LEGAL_HOLD_ACTIVE（保全阻断清除）/ M9-029
  LEGAL_HOLD_AUTHORITY_REQUIRED（缺权威依据）
- 关联威胁模型：`docs/threat/m9-06-budget-audit-hold-privacy.md`（S6/S7，T-18/T-19）

## 1. 角色与职责

| 角色 | 职责 |
|---|---|
| 法务（发起权威） | 出具保全指令（案件号 authority_ref）、界定 scope、批准解除 |
| 运维/管理员 | 执行 Activate/Release API 调用，不得自创权威依据 |
| 审计员 | 复核 hold 生命周期 journal（激活/阻断/访问/导出/解除全留痕） |

## 2. 发起（Activate）

前置：法务书面保全指令，含 **案件号（authority_ref）** 与 **有效期限**。

1. 收到指令后由运维执行 Activate，字段必须齐全：
   - `OrgID`：组织 ID（保全按组织隔离，跨组织需各自发起）
   - `Scope`：保全范围选择器（`user:alice`、`project:p1`、`message:01JDX…`
     或层级组合 `project:p1/message:01JDX…`；`*` 为全组织，慎用）
   - `AuthorityRef`：案件/文书号 —— **缺失直接拒绝 M9-029**
   - `ExpiresAt`：有界有效期，**必须晚于当前时刻**（无期限保全拒绝）
2. 激活成功后系统在 journal 写入 `hold.activate`（含案件号）。
3. 生效即时：此后任何命中 scope 的删除请求走 `ScreenDelete` 门禁。

## 3. 删除阻断行为（运维须知）

命中 active hold 的删除请求**永不物理清除**，系统自动：

1. 用户侧隐藏该对象，替换为**墓碑**（提示"依法保全，已移出您的视图"）；
2. 对象转入**受限证据库**（`evidence://<org>/<hold-id>`）；
3. journal 写入 `hold.screen.blocked`；
4. 门禁二次校验：保全投影与权威登记簿不一致时 **fail-closed（M9-028）**，
   拒绝放行并上报——此时按 §7 排查投影同步。

注意：hold 到期后自动失去阻断力（无需仪式性操作）；解除前如需继续保全，
必须由法务出具新的保全指令续期。

## 4. 证据访问与导出（Access / Export）

- 均需**权威依据**（案件号），缺失拒绝 M9-029；
- 每次访问/导出均写 journal（`evidence.access` / `evidence.export`）；
- hold 解除后证据封存：访问拒绝，不再增长。

## 5. 解除（Release）

前置：法务出具**独立的解除依据**（新文书号）。**不得空白复用发起案件号**——
API 强制要求 `authority_ref` 非空（缺失 M9-029）。

1. 运维执行 Release，携带解除文书号；
2. journal 写入 `hold.release`（含解除依据）；
3. 解除后删除门禁恢复放行；已入证据库的对象保持封存。

## 6. 备份恢复顺序（T-19，强制）

恢复部署必须按以下顺序，禁止跳步：

1. `ReplaySnapshot(holds)` —— **先**重放 Legal Hold（连同策略、身份吊销水位）；
2. 重放完成前 registry 处于 fail-closed：ScreenDelete 与 Activate 一律拒绝
   （消除"恢复窗口泄漏"：备份早于保全激活时，恢复后立即恢复阻断）；
3. 重放完成后才开放读取。

## 7. 故障处置

| 症状 | 处置 |
|---|---|
| ScreenDelete 报 M9-028（投影不一致） | 立即停止该组织批量删除；核对 hold 登记 vs 投影；修复前保持 fail-closed |
| 误想删除已保全对象 | 不存在该路径（Redirected 设计）；若发现墓碑缺失立即上报法务与审计 |
| 到期误续 | 到期解除阻断是设计行为；需继续保全走新指令，勿改 ExpiresAt |

## 8. 演练（半年度 + 重大变更后）

自动化部分（每次演练必跑）：

```
go test ./internal/legal/ -run TestHold -v -count=1
```

手工部分（按下表逐项确认并会签）：

| 步骤 | 期望 | 结果 |
|---|---|---|
| 1. 无案件号发起 Activate | 拒绝 M9-029 | ☐ |
| 2. 持案件号发起 Activate | 成功且 journal 有 hold.activate | ☐ |
| 3. 删除命中对象 | 用户视图变墓碑，证据库出现该对象 | ☐ |
| 4. 无依据访问/导出证据 | 拒绝 M9-029 | ☐ |
| 5. 持依据导出 | 成功且 journal 有 evidence.export | ☐ |
| 6. 无解除依据 Release | 拒绝 M9-029 | ☐ |
| 7. 持解除依据 Release | 成功，后续删除放行 | ☐ |
| 8. 恢复演练：重放快照前删删看 | 拒绝（fail-closed） | ☐ |
| 9. 重放后删删看 | 命中阻断（备份早于激活也阻断） | ☐ |

## 9. 演练记录

### 2026-08-16 首次演练（T-9.4.4 完成时）

- 自动化：`go test ./internal/legal/ -run TestHold -v -count=1` ——
  9/9 通过（M9-028/029 负测、T-18 零误清除、T-19 恢复顺序、并发唯一证据行）。
  原始输出归档：`docs/evidence/m9/T-9.4.4.txt`
- 手工步骤：§8 表 1-9 由自动化用例等价覆盖（对应关系见测试名），真机
  UI 部分待运营界面接入后补录。
- 法务会签：________________（法务角色签署，见 D-5 流程）
- 运维复核：________________

> 备注：本记录中的"自动化等价覆盖"指工程门禁行为验证；法律流程有效性
> （文书格式、审批链）以法务会签为准，本手册不替代法律意见。
