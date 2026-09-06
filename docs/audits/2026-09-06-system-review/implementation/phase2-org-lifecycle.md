# S06 追加：组织生命周期与旧入口

正式回归先复现旧缺陷：真实临时 SQLite 组织从 active 改为 suspended 后，Engine.Handle(mro.tool.upsert) 仍成功落盘，违背 ADR-011 和既有 org.liveOrg 的停用拒写约定。

现统一入口每次读取可信绑定及实际状态，组织业务新写在 suspended/closed 下返回 M9-002；读取、取消、既有执行结果和审计收尾继续允许且复查原始资源归属。重新启用使用原有 org.activate；closed 禁止重新启用。没有增加关闭接口或新租户认证需求。

org.switch/activate/suspend 共用栅栏：先取消流和计划 worker，再等待在途请求退出，取得写锁后再取消一次，关闭启动注册竞态。space/member 操作持同一读锁。身份及角色撤权改用 verified binding 的真实 owner 查询，停用/封闭仍可降低已有授权。共享本机设置、专家和知识参考目录保留其原有本机范围。

旧 workflow、devTask、项目 memory、ontology、trace 纳入统一解析器。工作流版本/实例/阶段/快照/任务/测试/扫描/制品/评审/失效记录沿持久父记录追溯组织，多父关联全部验权；不明 opaque 编号不能跳过检查。递归证据引用最多 128 个节点，历史环拒绝。ontology 边检查两个端点；trace 同事务核查每条遍历边，历史跨组织边不能进入结果、游标或下一层。subagent/delegation/barrier 收尾复查原始 run。

实际新增回归：

- `internal/app/org_lifecycle_test.go`：真实 active→suspended 拒写/仍读→activate 恢复→closed 拒写/不可恢复；停用及 closed 撤权实际持久化；取消先于阻塞请求栅栏。已收到语音 final 在停用后实际保存，切换组织后的旧 final 被拒，数据库只保留正确的 1 条。
- `internal/app/data_scope_legacy_test.go`：真实工作流/任务/记忆/图谱已知外部 ID 读写拒绝，本组织读写成功；历史混合边整页拒绝，不明任务编号不产生证据，外部记忆未被修改。
- `internal/storage/sqlite/data_scope_evidence_test.go`：真实历史证据环、工作流实例和版本父组织不一致、制品父组织不符均拒绝，合法证据正常。
- 普通定向集合通过：app 2.993s / SQLite 5.752s / org 0.141s / m9app 0.530s，含以上场景和 MRO、workflow、身份、记忆的既有回归。
- race：app 69.411s、org 1.173s 通过。SQLite 新证据测试首次把 5 秒查询期限错误地包含模板库初始化，并在并行全库 race 下超时；仅把期限移至真正的环查询阶段后，SQLite 定向 race 47.414s 通过。两轮原始记录：`evidence/org-lifecycle-race.txt`、`evidence/org-lifecycle-sqlite-race-rerun.txt`。未修改产品超时策略或放宽业务断言。

窗口/缓存范围依据：Windows 桌面通过 cmd/desktop/singleton_windows.go 的 Local\\lunitide-gateway 实例互斥量约束同一 Windows 会话的主窗口；第二次启动激活既有窗口，显式接管等待旧实例退出。当前 React 切到组织设置时卸载业务页，返回重新读取；后台每次查真实绑定/状态，不把页面缓存当授权。普通产品边界是单实例主窗口，不据此宣称支持多窗口多用户同步。

用于主 test-plan.csv 的 T51 更新：可信绑定、个人/A/B已知 ID、binding损坏拒绝、旧入口父归属、历史混合边、停用/closed拒新写、activate恢复、停用撤权、取消栅栏、切换后迟到 final 拒绝均有真实 SQLite/Engine 回归。其余 P/B03/S04/MRO 场景继续引用 phase2-projects.md；现场验收不在本轮自动化结论内。
