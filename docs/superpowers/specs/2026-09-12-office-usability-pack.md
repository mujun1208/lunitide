# Office 可用度收口包

**日期：** 2026-09-12  
**路线：** 方案 2 下一包。本包 100% = 可用度 100%，不是首发工程产品验收 100%。

## 可用度怎么到 100%

做不到的项不硬做第二套内核，改成每条都有可用路径：

- 未配置视觉模型 / PDF/A：不挡检查、改稿、草稿导出。
- 已配置：按当前预览或源 PDF 实跑，passed/failed/missing 只来自实跑。
- Presenton、企业审批、协同：写明不在可用范围，不当故障。

## 必须 100% 完成

| ID | 条目 | 完成定义 |
|---|---|---|
| U1 | Check 实跑 PDF/A | 有预览/源 PDF 时 `IndependentPDFACheck(bytes)`；`missing` 不改写成 `unsupported` |
| U2 | Check 实跑视觉模型 | 已配置且有渲染 PDF 才带 Review；无图不标 passed |
| U3 | Studio 可用范围 | 写明未配置仍可用、外部生成器未进主链、FR17/FR18 不做 |
| U4 | 检查区据实标签 | PDF/A / 视觉模型按检查状态显示；视觉 passed 不写成 85 认证 |

## 不做

designerReviewed=36、Presenton 进 Generate、母版还原、FR17/FR18、假 passed。
