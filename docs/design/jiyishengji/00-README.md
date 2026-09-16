# 记忆、OCR、媒体与UI升级：R3开发交付包

日期：2026-09-15。代码审计基线为 `b1d58b0d8e4867bd5c5efd7048ce82b5daf07c09`，审计时工作树含用户未提交改动。**本目录为现行版本；完成的是复核、交互原型和开发文档，不是产品代码改造。** 开发开工时必须按 11 的 P00/X0 重新记录 `HEAD + trackedDiffDigest + untrackedManifestDigest`；基线变化后，本页日期不能被当作当前测试证明。

## 阅读顺序

| 文档 | 读者/用途 |
|---|---|
| [01 三角复核报告](01-review-findings.md) | 产品/技术负责人：24项缺陷、代码证据、旧版3.4/5与修正方向 |
| [02 完整升级PRD](02-upgrade-prd.md) | 全员：范围、架构、Memory/OCR/媒体、48项原需求验收、迁移与回滚 |
| [03 记忆实施计划](03-memory-implementation-plan.md) | Go/前端/测试：Task 0～9，共10个开发任务 |
| [04 OCR实施计划](04-ocr-implementation-plan.md) | Go/Windows/Python/前端：8个开发任务 |
| [05 媒体与UI实施计划](05-media-ui-implementation-plan.md) | Host/Go/React：9个开发任务 |
| [06 跨模块合同与实施计划](06-integration-contracts-plan.md) | 所有开发必须读：6组合同、X0–X3共4个集成任务 |
| [07 验收与五分标准](07-acceptance-and-scorecard.md) | QA/负责人：新增回归、基准、发布门和本轮实际验证 |
| [08 原始需求与竞品证据](08-original-requirements-and-evidence.md) | 产品：此前全部问题、15维覆盖、语音/OCR/许可与先进记忆借鉴 |
| [09 UI交互演示说明](09-ui-demo-guide.md) / [打开独立演示](ui-demo/index.html) | 产品/设计/开发：V2简约品牌版；首页、设置、记忆、OCR、独立媒体中心与按需迷你播放器；不接真实服务 |
| [10 最终复核与五分整改](10-final-audit-and-five-point-remediation.md) | 负责人必读：当前评分、阻断缺陷、修正结论与5/5条件 |
| [11 总实施计划](11-master-implementation-plan.md) | 开发负责人必读：唯一依赖顺序、责任边界、红绿测试、提交与发布门 |
| [12 需求追踪矩阵](12-requirements-traceability.md) | 产品/研发/QA：原需求→PRD→合同→任务→测试→Demo→证据的唯一映射 |

共13份编号文档（00–12）与1套独立HTML原型。规范优先级逐字按 11：02 固定产品行为，06 固定跨模块合同，03/04/05 细化模块实现，11 只冻结执行顺序，07 定义验收，12 仅维护状态与证据映射；任一层仍冲突就停止该项并同步修正文档，开发者不得临场选择。未来生成的 runbook 与实测报告放本目录 `runbooks/`、`evidence/`，未生成前不标 `VERIFIED`；发布身份固定为被测源码 `testedSourceHead` 加 P14/P15 两级仅含白名单证据的 attestation 提交，不能把提交证据造成的 HEAD 变化误判为源码漂移。

## 最终方案

1. 原生Memory Fabric v2：自动明确稳定信息；问候/天气/一次性命令不进长期记忆；可追溯、可纠正、可撤销、可忘记，后台与注入总成本受控。
2. Windows.Media.Ocr不替换；PaddleOCR-VL-1.6完整模型包用户选装，真实Windows离线验证先行；未达门只保留原OCR，不假报安装能力。
3. 借鉴白龙马媒体连续体验与可见工具状态；App级播放器、队列、真实核验、音频焦点与活动历史；保留电脑权限/审批/急停，不附带第三方影视曲库。

## 开发顺序（规范入口为11，不要按文档顺序逐本做完）

```text
P00基线/备份/迁移分配 → P01 OCR/媒体真实性修复
  ├→ Memory schema/来源/scope/删除保护 → 基础读/纠正/导入导出 → 自动捕获 → hybrid/generation
  ├→ OCR scoped数据面 + Windows六态probe + disabled Paddle UI → P09真实pack门 → 仅通过后P10安装/worker
  └→ 媒体Windows垂直切片 → App生命周期/音频焦点 → 活动分页/视觉
  → 全链质量、恢复、总成本、语音和用户体验验收
```

三条子线按依赖可并行；共享 migration/Bridge/bootstrap 由集成人维护。`0160–0164` 是本审计时的候选分配，不是永久硬编码：X0 先扫描实际 manifest 并生成 `evidence/migration-allocation.json`，在同一次合同提交中冻结最终编号；之后任何冲突均阻断，不得由各子线自行顺延或覆盖已发布 SQL。

## 评分与未完成边界

旧版 R2 曾自评 4.4/5；按当前代码、PRD、Demo 和追踪闭环重新审计后，**整改前实施就绪度为 3.6/5，当前文档实施就绪度暂评 4.2/5**，详见 10。剩余分数只能由 P00～P15 的真实实现与同版本证据取得；产品只有在 07 全部门禁和 12 全部 P0/P1 行达到 `VERIFIED` 后，才可验收为本期 5/5。这个分数不是行业排名，也不承诺 Paddle 在所有机器必然可发布。

此前旧基线记录仍为 `STALE`。本轮文档收口在当次 HEAD `62711f3d` 的 dirty 工作树执行了 10 个相关 Go 包、4 个现有前端文件共 30 项测试、Bridge 一致性、Demo 13 项回归及 Edge 5 页面×6宽度检查，均通过；开始/结束 HEAD 一致。因为尚未执行 P00、没有绑定 worktree digest/环境 manifest，这些只算非发布 sanity check，仍不能把任何 R3 requirement 标为 `VERIFIED`。未安装任何模型、未修改产品源码，不把现有 dirty 改动计成本任务成果。

旧docs/superpowers/specs与plans中的2026-09-14四份文件保留作历史，不再作为开发执行入口。遇到外部依赖版本变化或新代码合入，先更新本目录受影响合同和验收，再开发；不能悄悄跳过阻断门。
