# Office 能力包 1（字形 / 视觉模型接线 / 诚实 PDF/A）

**日期：** 2026-09-12  
**对照：** `docs/design/PRD-office-quality-commercial-2026-09-11.md`  
**路线：** 方案 2 第一包。本包 100% ≠ 首发工程 100%，更不是整份 PRD 100%。

## 边界

- **做：** 本机字体能量宽则标 `measure=glyph`；已配置看图模型才允许视觉检查跑完；PDF/A 只有验证器对文件跑过才标 passed。
- **不做：** `designerReviewed=36`、Presenton/PptxGenJS 进 `office.generate`、PPT 母版还原、从成稿反推 Brief、FR17/FR18、把 unsupported 空改成 passed、校准视觉 85 对外认证。
- 缺字体 / 缺模型 / 缺验证器时保持诚实缺口。Formal 仍不把单纯 unsupported 的 `visual-model` / `pdfa` 当硬门槛。

## 必须 100% 完成的条目

| ID | 条目 | 完成定义 |
|---|---|---|
| G1 | 字形测量 | `GlyphMeasure` 在 extent 成功时 FitEvidence 含 `measure=glyph`，不含 `not glyph`；extent 失败退回估算且仍写 `not glyph`。禁止无测量写 `glyph-verified`。 |
| G2 | 视觉模型接线 | 未配置 → `visual-model=unsupported`。配置了但无渲染图或无 Review → 不标 passed。Review 跑完无问题 → passed，并写明规则分未校准。有问题 → failed。 |
| G3 | PDF/A 验证 | 未配置 `LUNITIDE_PDFA_VALIDATOR` → `unsupported`。已配置无 PDF → `missing`。验证失败 → `failed`。验证成功 → `passed`。Typst 输出本身不表示 PDF/A。 |

## 明确仍不是首发 100%

真实字形引擎未覆盖全部品牌字体、自动换版、母版还原、Presenton 主链、出版级 Typst 产品化、FR17/FR18。人测全过后首发工程仍约 91–94%。
