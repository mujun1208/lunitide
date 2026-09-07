# 真实桌面旧测试夹具隔离记录：2026-09-07

## 已确认事实

在本轮文档/文件系统复核中，执行原有 `go test ./internal/toolruntime ./internal/officetools -count=1` 后发现两组既有测试直接调用真实 `userDesktopDir()`，以固定名称写入产物，并在测试清理中调用 `os.Remove`。测试未检查目标是否预先存在，也未备份原内容。

发生时间约为 **2026-09-07 10:54:37（Asia/Shanghai）**。只读查询 Windows 桌面注册路径解析为 `C:/Users/mujun/Desktop`；该目录当时的最后修改时间为 `10:54:37.276964`。相关路径：

| 原有测试 | 写入并清理的真实目标 |
| --- | --- |
| `TestHTMLGenDesktopArtifactPath` | `C:/Users/mujun/Desktop/点球大战.html` |
| `TestOfficeGenDesktopArtifactPath/excel.gen` | `C:/Users/mujun/Desktop/半年财报.xlsx` |
| `TestOfficeGenDesktopArtifactPath/docx.gen` | `C:/Users/mujun/Desktop/半年报告.docx` |
| `TestOfficeGenDesktopArtifactPath/pptx.gen` | `C:/Users/mujun/Desktop/结构.pptx` |

只读核对时四个路径均不存在。没有执行前的目录快照，无法确认这些同名文件在测试前是否存在，因而不能确认或排除覆盖了原有内容。原测试没有备份或可回滚日志。对 `C:/Users/mujun/AppData/Local/Lunitide` 的一次只读同名文件查找未发现这四个名称的副本；这不等于已经全面搜索附件数据库、回收站或磁盘恢复信息。

发现后已立即报告主任务并暂停可能涉及此类夹具的测试。没有尝试恢复、覆盖现存用户文件或修改用户数据库。

## 已修复

- `Runtime` 新增实例级可注入桌面目录解析器；生产默认仍使用原有真实桌面解析，不改变产品功能或用户权限。
- 上述 HTML 和 Office 测试明确注入 `t.TempDir()`，清理仅由临时目录生命周期负责，删除原固定目标的 `os.Remove` 清理代码。
- 公共测试辅助 `enableFullDisk` 默认注入独立临时桌面，防止后续相似全盘夹具遗漏隔离。
- 桌面写入及产物读取统一使用同一实例解析器，保证测试同时覆盖生成与回读。
- 全仓库测试文件对 `userDesktopDir()` 的调用检查已清除上述两处真实调用；其余已检查的桌面路径相关单测使用临时目录或模拟调用。

## 验证与剩余不确定性

隔离后 HTML 和 Excel/Word/PPT 桌面产物定向测试通过，耗时 0.173 s。测试后只读核对真实桌面目录修改时间仍为 `10:54:37.276964`，未因重跑这些测试改变。随后 `toolruntime` / `officetools` / `doctext` race 回归通过。

该修复阻止这些测试继续触及真实桌面，不能反向证明四个路径在首次执行前没有原文件。本记录保留此不确定性，不虚构备份或恢复结果。
