# 办公平台闭环验收脚本（2026-09-12）

对照合同：`docs/design/PRD-office-platform-closed-loop-2026-09-12.md` 1.1  
对照计划：`docs/superpowers/plans/2026-09-12-office-closed-loop.md`

这是本合同 D2 清单。旧试验协议 `office-trial-protocol-2026-09-12.md` 仍挂 trial-ready 规格，**不得当本脚本**。

装机若仍显示 0.4.75，必须用本工作区新编译桌面端并重启后再走。未重启不能签关闭。

范围内失败才算本 PRD 失败。设计师已检 36、校准 85、Presenton 主链、目标软件打开验证、PDF/A/UA 认证，不在范围。

## 开始前

1. `go build ./cmd/engine ./cmd/desktop` 后启动**这份**二进制。
2. 打开办公工作台，确认试验范围 / 可用范围各一句，无「已超过」。
3. 准备真实数字（例如订单 1280 单）。禁止编节约率。

## S1 对话生成四格式

| 步 | 操作 | 通过 | 失败 |
|---|---|---|---|
| 1 | 对话 `office.generate` PPTX | 当前任务出现不可变版本 | 无文件且无中文错误 |
| 2 | 同样生成 DOCX、XLSX、独立 PDF | 四个 Artifact 都在当前任务 | PDF 只在会话产物、未进任务 |
| 3 | 不要把 `pptx.gen` 说成已自动进任务 | 需「在办公工作台查看」才纳入 | 文案声称已自动导入 |

## S2 检查词诚实

| 步 | 操作 | 通过 | 失败 |
|---|---|---|---|
| 1 | 打开检查 | 每条有中文状态，无空白 | `unsupported`/`missing` 空白 |
| 2 | 无 LibreOffice 的 Word/PPT/Excel | `native_render` 写「缺组件，不能正式交付」 | 写成目标软件那句「未验证、仍可正式」 |
| 3 | 独立 PDF 无 Typst | 独立 PDF 为稳定稿 passed；可正式 | 因没装 Typst 不能正式 |
| 4 | 质量承诺 | 「内容完整」看 package/pdf_structure；「排版已检查」仅真实渲染 | layout/structure 假 id 点灯 |

## S3 草稿与正式

| 步 | 操作 | 通过 | 失败 |
|---|---|---|---|
| 1 | 未通过版本点「作为正式交付」 | 按钮禁用；若强发 API 则 `OFFICE_DRAFT_REQUIRED` | 服务端接受为正式 |
| 2 | 「接受为草稿 / 使用此版」 | 未通过也可接受，导出仍要草稿标记 | 接受后当正式导出 |
| 3 | 导出文案 | `passed` 不是「已接受为正式版」 | 检查通过写成已正式接受 |

## S4 定位与取消

| 步 | 操作 | 通过 | 失败 |
|---|---|---|---|
| 1 | 定位不在当前预览页的节点 | 翻页找到或「不在此版本」 | 静默无反应 |
| 2 | 检查中点停止 | 检查区「检查未完成」；生成不被取消 | 整任务/生成被杀掉 |

## S5 导入与同源 PDF

| 步 | 操作 | 通过 | 失败 |
|---|---|---|---|
| 1 | 导入 Office | 只能局部改；不能反推 Brief | 声称从成稿重建规格 |
| 2 | 改源后再看同源 PDF | 失效可见；有「按新版本重建」 | 旧 PDF 仍当当前正式阅读稿 |

## S6 组件与不做项

| 步 | 操作 | 通过 | 失败 |
|---|---|---|---|
| 1 | 组件探测 | Typst 未配置写「仍可用 gofpdf」；Presenton 未进主链 | 「独立 PDF 不可用」 |
| 2 | 目标软件 | 未做打开验证，仍可自行打开 | 探测到本机 Word 就放宽 Formal |

## 记录

### D2 引擎（2026-09-12，工作区 `go test`）

- 命令：`go test ./internal/app -count=1 -run TestOfficeClosedLoopProtocol`
- 结果：通过。覆盖 S1 四格式落入当前任务、S2 无 Typst 仍可用 gofpdf / 独立 PDF `quality=passed`、S3 未通过 Word 正式接受/导出失败且草稿成功、passed 导出通知含「检查通过不是已接受为正式版」、独立 PDF 通知含「不保证分页」且不是 PDF/A、无渲染绑定时 Word 不得当同源 PDF 导出、S6 Presenton 未进主链、单节点 Patch 出新版本。
- 新编译二进制（未启动 GUI）：`C:\Users\mujun\AppData\Local\Temp\lunitide-closed-loop-d2\`  
  `lunitide-desktop.exe -version` = `0.0.0-dev`（2026-09-12 11:14）。装机包仍可能显示 0.4.75，二者不是同一份。
- 范围内失败：无
- **不是**真人新桌面端走查。装机 0.4.75 不得当结论。

### D2 真人新桌面端（待签）

- 二进制来源：工作区 `go build ./cmd/engine ./cmd/desktop` 后须**新启动**这份二进制  
- 任务 ID：  
- 四个文件版本 ID：  
- 范围内失败（若有）：  

不得用未重启的 0.4.75 当结论。
