# 产品知识中枢 · 自净化落地 PRD（方案 2）

- 日期：2026-09-21
- 状态：可实施（本里程碑做诊断报告；白名单 apply 按本文二期接线）
- 依赖：V3.1 诊断表 11/12、生成原则 §12、初版种子
- 红线：不自动改 Go/TS 业务源码；`fix_code` 只指路

---

## 1. 目标

产品每次把自己扫成知识快照后，必须再扫一遍「这张图和说明书是否还跟代码一致」。输出不是口号，是一份**可执行的诊断报告**：每条问题有证据、根因、改哪里、怎么验证。升级后再扫，已修/未修/新引入三态闭环。

本里程碑交付：

1. 引擎在 `verified` 后自动出报告并写入 `product_diagnostic_*`
2. UI 第四标签展示报告 + 导出 HTML
3. `wont_fix` 需管理员确认
4. 本文规定的 apply 协议（二期才接线，契约先冻结）

不交付：无人值守改 `internal/**/*.go`、`web/src/**` 业务文件。

---

## 2. 用户能看到什么

管理员解锁后打开「诊断报告」：

- 健康分 0–100、error/warn/info、较上份 `resolved_count`
- 问题列表按严重级
- 点开一条：证据 JSON（探针原文 + 时间）、根因、改进步骤、验证方式、首次出现版本、当前状态
- 闭环条：新增 n / 已解决 n / 未解决 n
- 导出与页同源的离线 HTML

---

## 3. 信号从哪来（只读）

| 类 | 来源 | 例 |
|---|---|---|
| 构建校验 | cards/manifest 校验 | PH-009 字段越界、PH-010 悬空分支、PH-004 孤儿引用 |
| 7 探针 | probes.go | Bridge 缺方法、设置分类漂移、入口 Page 没了、前置 TTS/ASR/MCP 超时 |
| 一致性规则 | diagnose_rules.go | 链路引用的工具不在图里；Feature 无 methods；模块零卡；种子写了活源已删的锚点；枚举 action 未出卡 |
| 生成原则 | merge 结果 | 仅 seed 且非 landscape → 弃用候选；catalog 有、图无 → 漏生成 |

诊断失败（PH-011）保留上一份报告，不影响 snapshot verified。

---

## 4. 一条 finding 的契约

```json
{
  "finding_id": "ULID",
  "severity": "error|warn|info",
  "error_code": "PH-004",
  "stable_key": "feature.dialog.music.play",
  "title": "属性引用了图中不存在的技能",
  "evidence_json": {
    "probe": "usesAssetsResolve",
    "missing": ["skill.computer-ops"],
    "at": "2026-09-21T12:00:00Z"
  },
  "root_cause": "种子 attributes.skills 仍写旧 id，技能注册表已无此包",
  "fix_json": [
    { "action": "edit_manifest", "target": "seed.v1.json#features[feature.dialog.music.play].attributes.skills", "detail": "改为当前 skillapp 中的稳定 id，或删掉该引用" },
    { "action": "rebuild", "target": "", "detail": "手动刷新快照" }
  ],
  "verify_method": "productHub.featureCard(feature.dialog.music.play) 的 attributes 全部可解析；本 finding 转 fixed",
  "status": "open",
  "first_seen_report": "ULID",
  "resolved_in_version": null
}
```

四要素缺一不可，构建期校验。

---

## 5. 改进动作枚举（冻结）

| action | 本里程碑 | 二期 apply | 含义 |
|---|---|---|---|
| `edit_manifest` | 只显示步骤 | 可执行：JSON patch，路径必须落在 `internal/producthub/manifest/**` | 改种子/landscape/规则 |
| `rebuild` | 用户点刷新 | 可执行：触发 refresh（30s 限流） | 重建快照 |
| `check_service` | 只显示 | 不自动重启进程 | 查 TTS/ASR/MCP |
| `install_asset` | 只显示 | 可执行：调用已有 skill.install / mcp 启用，目标必须已在名册 | 启用已有资产 |
| `fix_code` | 只显示 `file:line` | **永不可执行** | 给开发指路 |
| `wont_fix` | UI 确认 | 可执行：留痕 | 豁免 |

二期 `productHub.diagnostics.apply`：

```
{ "findingId": ULID, "action": "edit_manifest"|"rebuild"|"install_asset"|"wont_fix", "patch": optional }
```

引擎校验：管理员已解锁、action 在允许集、target 前缀白名单、写审计表、apply 后强制 rebuild 再跑诊断。  
拒绝：任何 `internal/**/*.go`、`web/src/**`（`productHub/` 与 `manifest` 除外）、任意 Shell。

---

## 6. 闭环

新报告与上一报告按 `(error_code, stable_key)` 对齐（比只靠 title 稳）：

- 仍在 → 保持 `open`，`first_seen_report` 不变
- 规则再跑通过 → `fixed` + `resolved_in_version=app_version`
- 新组合 → 新 finding
- `wont_fix` 跨快照保留，除非人取消豁免

报告头 `resolved_count` = 本轮新转 fixed 的条数。

---

## 7. 健康分

```
score = round(100 * probe_passed / max(probe_total,1))
        - 8 * error_count
        - 3 * warn_count
夹在 0..100
```

探针全超时可以 verified，但分数会很低；卡片仍可读（数据来自种子/注册表）。

---

## 8. 安全

- 报告和导出不含密钥、用户会话、本机绝对路径（路径只到仓库相对文件）
- 只有管理员会话能看 diagnostics / export report
- apply 审计：谁、何时、finding、action、patch digest、结果

---

## 9. 验收

1. 种子里写一个不存在的 skill id → 重建 → 报告出现 PH-004，四要素齐全。  
2. 改回合法 id 再重建 → 该条 `fixed`，闭环「已解决 +1」。  
3. `wont_fix` 不点确认点不了；确认后跨刷新仍在。  
4. 诊断器 panic → PH-011，旧报告还在，快照仍 verified。  
5. 未解锁调用 `productHub.diagnostics` → PH-012。

---

## 10. 二期实施顺序（契约已定，代码后做）

1. 表 `product_hub_apply_audit`  
2. schema `productHub.diagnostics.apply`  
3. allowlist path 单测（越权路径必须拒绝）  
4. 修种子 → apply edit_manifest → 自动 rebuild → finding fixed 的集成测  

本里程碑不写 apply handler，避免半套自愈。
