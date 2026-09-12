# Office 代码可 100% 收口（刨除真人试验）

**日期：** 2026-09-12  
**对照：** `docs/design/PRD-office-quality-commercial-2026-09-11.md`

## 边界

- **不做：** 真人试验、设计师已检 36、PowerPoint/WPS 实机、视觉模型、校准 85、竞品盲评、试点、FR17/FR18、把 unsupported 标成 passed。
- **本规格只收** 现有主链上还半截、但测试能一次做完的工程缺口。
- 做完后试验包仍 100%；整份 PRD 产品验收仍约 60%。

## 必须 100% 完成的条目

| ID | 条目 | 完成定义 |
|---|---|---|
| C1 | 四格式密级 | 已填密级写入 PPT 备注、Excel「说明」页、Word 页眉、独立 PDF；未填不虚构「机密」 |
| C2 | 聊天证据不虚构 Brief | 未填受众/用途/页数写 unset，不写成「管理层/经营汇报/12」；已填密级带上 |
| C3 | 许可清单加载即校验 | `LoadLicensePackChecklist` 调用 `Validate`；reviewed+空条目加载失败 |
| C4 | §7.3 质量承诺只据实 | 检查区显示「内容完整 / 排版已检查 / 关键数字有来源 / 可继续编辑 / 存在需处理的问题」；无证据不点亮 |
| C5 | Brief 合同含密级/大纲 | `OfficeBriefDTO` 接受 `confidentiality` 与 `outline`；保存概要不再依赖未声明字段 |

## 明确仍不是 PRD 100%

设计师验收、实机矩阵、视觉模型、校准分、盲评、出版内核、Presenton 主链、装机 0.4.75。
