# P01–P04 项目与计划实施进度

日期：2026-09-06。下述是已落地的小批修复；未完成能力和限制单列，不据此宣称达到 4.9 分。

## P01 已修复

- 项目变更审计 ID 纳入幂等键与完整请求摘要，消除同一项目第二次独立更新的主键冲突。
- `ProjectService.Mutate` 必须携带完整解码 payload；摘要覆盖 actor/action/id/version/payload。同键同请求重放，同键改业务字段明确冲突。
- 重放 DTO 补齐 statusBeforeClose、reopenReason、orgId、spaceId，关闭/重开重放与首次响应一致。
- 真实 Bridge+SQLite 连续 100 次更新与各次重放、改 payload 拒绝、两轮关闭/重开测试通过。
- 旧版本保存的 mutation 摘要没有完整载荷，升级后旧幂等键会安全地返回冲突，不按弱摘要重放。客户端应刷新并创建新的操作。

## P02 已落地的原子文档阶段完成

- `project.advanceStatus` 委托事务内 `CompleteProjectPhase`；检查项目版本/当前业务状态、当前阶段、全部前序阶段完成、每个必需交付物已批准及其项目/阶段/证据版本，再一次冻结交付物、完成阶段、推进项目并写审计/幂等。
- `stage.update` 的 completed 分支也进入该用例，不能绕过门禁直接完成阶段；完成阶段不允许任意改回未完成。
- 前端取消“逐个冻结 → 项目推进 → 阶段更新”的分离写入；提交后重新读取阶段列表。
- 修复确认交付物时省略 attachmentId/templateId 会把已有证据引用清空的问题；重新变更内容会清零旧 gate 计数。
- 三种项目类型全部文档阶段按顺序推进测试、无发布直接跳上线拒绝、跨阶段/空证据拒绝，以及 stages/projects/audit_events 三类故障注入零部分推进测试通过。
- 剩余：发布阶段没有文档，应接真实 release 证据专用用例；当前文档端点拒绝无文档阶段的直接上线。附件校验覆盖数据库归属、阶段、SHA-256 元数据及版本，模板校验启用状态/文件引用/版本；实际文件内容读取与散列复核、审批内容摘要绑定仍需后续完成。不能将本批元数据门禁描述为已验证实际文件字节。

## P03 已落地的计划状态与阶段绑定

- 统一 node.create 正整数 sequence；UI 不再创建 sequence=0 根节点。
- plan.create 增加既有后端可接受的可选 stageId 契约。阶段计划通过 projectId+stageId 派生稳定 ID，在同一事务创建 plan、sequence=1 根节点及 active 状态；并发重复创建返回同一计划，根节点失败不留下半份计划，无新增迁移。
- 前端按 stageId 查找计划，不再按名称猜测或退回第一个计划；只读页不创建阶段/计划/根节点；阶段切换和同步的迟到响应不覆盖新页面。
- StartNode 校验 plan active、父节点同计划且完成、必要的治理审批；pending 节点在通过准备检查后可进入 running。
- SQLite 在同一事务复核 plan/node 转换、父依赖和当前状态条件；node 终态与父计划聚合同时提交，计划版本递增。完成聚合读取全部节点，失败/取消不会被 IsTerminal 误算为全部成功，failed 父计划不被后续完成覆盖。
- 剩余：暂停是阻止后续派发的状态门禁；已有外部执行器取消确认、审批记录与变更版本绑定、失败重试的显式 UI/策略及节点分页属于后续工作，未在本批宣称完成。

## P04 仅完成如实反馈止血

- 计划页明确展示任务协调记录，不再把协调成功数称为实际完成数，删除按协调状态回写开发完成/测试通过的功能。
- 既有 backend `executionStarted:false` 保留。真实 agent run、执行产物验证和恢复执行尚未实现；P04 不得标记完成。

## 定向验证

- `go test ./internal/app ./internal/storage/sqlite -run 'TestProject|TestStage' -count=1`：通过。
- `go test ./internal/storage/sqlite ./internal/planningapp -run 'TestPlanningUpgrade|TestStartNode|TestActivate|TestComplete|TestFail' -count=1`：通过（8 个新增真实存储计划场景）。
- `npm --prefix web run generate:bridge`：通过；未手工修改生成代码。
- `npm --prefix web run typecheck`：通过。
- `npm --prefix web run test -- --run src/project/ProjectPlanPanel.test.tsx`：3 个测试通过，覆盖只读零写、稳定阶段绑定和迟到响应。
- `go test -race ./internal/storage/sqlite -run 'TestPlanningUpgrade|TestProjectPhase' -count=1`：通过（33.292 秒）。
- `go test ./internal/planningapp -count=1`：整包通过。
- 根任务负责全项目回归。所有新增数据库验证使用临时数据库。

## 全量前端回归中的滚动测试收敛

- `evidence/web-test-final.json` 唯一失败指向 `ProjectWorkbenchShell.send.test.tsx:236`：实际是 `/主要分歧/` 表头节点的 `toBeInTheDocument` 失败，并非查不到消息超时；`findByText` 已取得节点，但外层断言时该引用已脱离 DOM。修前单文件 3 个测试通过，未发现消息持久丢失。
- 源码分析：该测试已 mock `DeliverablePanel`，没有挂载 `ProjectPlanPanel`。`MarkdownMessage.tsx` 每次渲染创建新的 `components.table` 函数，正常会话/provider 异步状态刷新可替换表格子树；Testing Library 的异步 wrapper 在取得节点后还排一次微任务/计时器，形成旧节点引用与新渲染之间的等待竞态。
- 仅调整测试：将用户消息及表头的查询、存在断言放进同一个 `waitFor`；表头改为 `columnheader` 精确名称查询。发送、连续跟随、滚轮暂停、conversation/window/html/body 均不滚动的原断言完整保留，末尾额外检查用户消息和表头仍在。没有修改产品或 Markdown 渲染代码。
- 修后整文件连续执行 8 次，每次 3 个测试全部通过；日志为 `evidence/project-scroll-repeat.txt`。这次重复仅针对全量回归暴露的等待时序问题，不替代主任务最终全量验收。
