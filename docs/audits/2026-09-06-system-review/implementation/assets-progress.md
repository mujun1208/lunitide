# P05 资产上传与创建实施进度

日期：2026-09-06。仅记录本批已落地和已验证的能力。

## 已落地

- 暂存初始化使用 sync.Once；上传有逐文件锁，编号必须连续，重复编号必须同时匹配 SHA-256 与 last 标记。成功重试返回原 bytes/ready ACK，不再重复拼接非末分片。
- 上传结束与文件关闭有明确 ready 状态；创建只能消费当前服务已知、ready、未过期的上传，验证总长度及完整文件 SHA-256。不再猜测路径读取未完成/其他进程留下的文件。
- 写分片使用 WriteAt、Sync，失败回退至已确认长度；临时文件使用独立随机名称。最多 64 个活动上传；当前服务持有的暂存记录 1 小时后按后续请求清理；完成后关闭文件并删除暂存。
- 模板创建强制幂等键；完整解码 payload 的摘要参与冲突检测。先查询持久重放，再消费上传/写文件，因此清理上传及重启数据库后同一请求仍能得到原结果。
- 新增经主任务分配的 `0125_asset_template_creations.sql`。模板、幂等结果、审计同事务；同步 manifest 校验和、expectedSchemaSQL 及 v26 升级测试夹具。
- 幂等结果可重放 24 小时；过期键保留，返回明确冲突提示并要求刷新核对，不会把过期键当成新请求自动复制文件。
- 文件先写、数据库后提交；若提交响应丢失，使用独立短上下文查询幂等结果。确定未提交才删除本次文件，已提交保留；无法核对时保留文件并返回结果待核对错误。并发竞胜时删除未被引用的本次重复文件。

## 验证

- `TestAssetUpgradeNonLastRetryPreservesContentAndReceipt`：非末分片重试内容/ACK 一致。
- `TestAssetUpgradeRejectsIncompleteOutOfOrderAndChangedChunks`：未完成消费、乱序、同编号不同内容/结束标记均拒绝。
- `TestAssetUpgradeDetectsStagedFileTampering`：暂存文件变化被检测。
- `TestAssetUpgradeSameCreateReplaysOneFile`：同请求一份文件，修改载荷同键冲突。
- `TestAssetUpgradeReplayAfterDatabaseReopenAndStageCleanup`：数据库关闭重开、暂存清理后仍重放原结果。
- `TestAssetUpgradeDatabaseFailureCleanupAndLostAcknowledgement`：未提交清理；已提交但响应丢失保留文件并恢复原响应。
- SQLite 额外覆盖过期键不重复创建、幂等表/审计落盘失败全回滚、8 并发同键同一模板身份。
- 原有 template handler 测试通过，成功创建测试已补应有幂等键。
- `TestTemplatedOpenMatchesReplay` 通过。
- 资产上传/创建、原模板 handler 与 v26 升级夹具的 `-race` 定向回归通过：app 26.169 秒，sqlite 71.686 秒。

## 剩余事项

- 尚未建立跨进程崩溃上传恢复清单、孤儿文件定时对账或管理页面。本批拒绝消费未知遗留文件，无法核对提交结果的成品文件保留待对账，不盲删。
- TTL 清理限于本服务已知暂存；程序退出遗留文件的独立清理策略需后续接入，不应称为完整 GC 已完成。
- 模板列表仍有既有 100 条上限；分页/总数协议和资产作用域授权是后续任务，不由本批消除。
- 并发、故障与重启验证均使用临时数据库和测试文件存储；没有修改用户数据库，没有调用真实桌面能力。
