# Office 可用度清单收口包

**日期：** 2026-09-12  
**口径：** 本包 100% = Studio 没有剩余死胡同。不是首发工程 / 整份 HTML PRD 100%。

## 必须 100% 完成

| ID | 条目 | 完成定义 |
|---|---|---|
| I1 | 检查状态词表 | `officeCheckStatusLabel` 覆盖 passed/failed/blocked/missing/unsupported/unavailable/pending；列表不空白。可选检查在快照里不把 missing/unsupported 压成 unavailable |
| I2 | 组件诊断对齐 | probe 含 pdfa/vision/typst/presenton/pptxgenjs；Presenton 写未进主链；桌面安装 ≠ 打开验证 |
| I3 | 目标软件标签 | 未验证写「仍可导出后自行打开」；missing 与 unsupported 可区分 |
| I4 | 表单失败可见 | 保存概要/风格/登记品牌失败可见；无效登记品牌按钮禁用 |
| I5 | 导入与生成预期 | 导入不能反推简报；生成在对话；阶段不是进度条 |

## 不做

Presenton 进 Generate、designerReviewed=36、目标软件假 passed、85 认证、FR17/FR18、从成稿反推 Brief、母版还原、假称 0.4.75 已修、手改 generated bridge。
