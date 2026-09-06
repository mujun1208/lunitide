# S07 / S02：MRO 公开列表的通信预算与完整分页

2026-09-06。此补充来自最后一轮独立容量复核，保留此前 MRO 组织、事务、来源与发布逻辑；不改变历史基线评分，不代表现场机务验收。

## 实证缺陷

内部聚合恢复为完整 10000 条且超额明确失败后，公开列表仍把所有记录放入一个成功响应。受控 SQLite 保存 10000 条符合输入字段限制的记录，使用真实 `Engine.Handle` 和实际 `ipc.WriteFrame`：

| 入口 | 成功响应的实际 JSON 字节 | IPC 上限 | 实际写帧 |
| --- | ---: | ---: | --- |
| `mro.tool.list` | 12,500,161 | 4,194,304 | 拒绝，invalid frame size |
| `mro.parts.stock.list` | 5,810,177 | 4,194,304 | 拒绝，invalid frame size |

正文包含合法的 `&` 字符，Go JSON 的 HTML 转义计入实际字节。测试先通过真实 upsert handler 验证长字段可接受，再在隔离 SQLite 事务内准备容量夹具。探针 [源码](../evidence/mro-frame-review/probe.go.txt)、[overlay](../evidence/mro-frame-review/overlay.json)、[原始缺陷日志](../evidence/mro-frame-review/probe.log) 保留为历史复现；该探针的 PASS 表示缺陷存在，不是修复通过。

## 实际修复协议

`internal/app/mro_list_page.go` 在公开投影统一分页；原业务服务和数据库仍完整聚合到 10000 条，超过支持预算明确失败，未恢复截断前 200 条的旧行为。

- 16 个公开读取入口接受可选 `cursor`，返回可选 `nextCursor`。每个顶层集合最多 100 条；包括 response envelope 的实际 JSON 不超过 1 MiB。按序列化字节自动缩小单页，不以字符数推算通信大小。
- cursor 绑定真实方法、可信组织、固定过滤参数和完整业务内容摘要。只读动作不制造新的 `checkedAt` 参与摘要；内容、范围、方法或过滤变化时返回 `MRO_PAGE_CHANGED`，要求从第一页刷新，不混合两版资料。
- 多集合响应保留原字段，例如库存的 `items` 和 `alternates` 分别记录位置、共用一个 cursor；不等长集合仍能全部读取。某页一个集合为空时，不表示已经加载的该集合应被删除。
- `events/tails/missing/sources` 子数组也分片，每次最多 100 个子项。单个部件的长履历不会因父项分页仍超帧；所有正文保留。续页中某集合首项续接上页末项时，`continuedFields` 指出该集合，parent ID 和其他字段保持相同。客户端核对 parent 后合并数组。
- `mro.util.record` 和 `mro.due.recompute` 的写回执只包含有界首屏；其 cursor 属于 `mro.due.list`，后页必须使用只读方法。原写入不会因为翻页重复执行。重算改变排序字段后，在原事务内重新读取实际列表，避免首屏游标立即失效。
- 缺件待办只由页面传 `kitId`；服务在同一事务内读取完整套件，确认实际缺件，保存完整缺件计数和套件明细 SHA256，忽略 renderer 的局部缺件文本。已有待办重放返回原 ID，不重写登记事实；未知套件及无缺件套件明确拒绝。该回执仅含状态和 ID；排期发布只产生两个固定短待办，也不把 10000 条聚合放进写回执。
- 原本已经限为 100 条的 `mro.audit.list` 不在此次超帧缺陷修复范围。其审计历史范围不因此被声称为新增完整分页。

已接公开读取：aircraft.list、manual.list、due.list、tool.list、lot.trace、kit.staging、parts.stock.list、plan.list、ops.todo.list、component.list、pirep.list、aog.list、po.list、trigger.list、interval.list、plan.constraint.check。18 份响应源 schema 与 16 份读取请求源 schema 同步；生成物由根任务统一生成。

页面已由根任务接线：16 个列表均有后页入口，保留已加载内容与失败状态；对续片核对同一 parent；数据变化要求刷新。页请求具有刷新世代和卸载守卫，A→B→A 不接受旧请求回包。约束检查尚有后页时不能标记全部检查通过；质量通报完整读取同批次全部相关页面。利用率写入后只读刷新和续页；新增机尾、手册即使位于首 100 条之外，也保留刚登记记录供选择。

根任务运行 [页面和分页 41 项正式回归](evidence/phase2-mro-pages-web.json)，2 个文件、41 项通过，类型检查通过。独立只读复核确认续片、多集合、首次错误、晚回包、利用率及局部套件入口接线。当前生产组织切换经过设置页并卸载工作台；`scopeKey` 的原位切换守卫另有 hook 回归，不把它写成页面已订阅新的组织事件。

## 正式回归

新增 `internal/app/mro_list_page_test.go`：

1. `TestMROPublicToolPagesPreserve10000LegalLongRows`：真实 10000 条长字段工具，完整 100 页，每页实际写帧成功，所有 ID 唯一且字段不截断。
2. `TestMROPartsPagesBindScopeFiltersAndChangedContent`：223 库存与 257 替代关系全部可达；跨方法、组织、过滤及内容变更的旧 cursor 被拒。
3. `TestMROComponentPagesPreserveLongParentHistory`：同一部件 1500 条 512 字符履历分 15 页；续片标记准确，逐条正文完整、不重不漏。
4. `TestMRODueMutationReceiptContinuesThroughReadOnlyList`：205 条到期项跨页可达，利用量仅保存一次；重算后的排序与只读 cursor 一致。
5. `TestMROPirepByteBudgetPreservesAllLegalNotes`：200 条合法 4000 字符故障报告触发字节限制，页大小低于 100 条，全部原文仍可重建。
6. `TestMROPartsTodoUsesCompleteKitInsteadOfRendererFragment`：205 个实际缺件通过真实待办入口登记；保存完整计数及摘要，忽略伪造局部文本，重复登记仍为同一 ID，未知套件拒绝。

[最终全部 MRO 入口与 SQLite 定向](../evidence/mro-frame-review/mro-entry-final.log) 通过，app 10.475 秒、SQLite 2.155 秒，包含以上六项和已有业务回归；[mroapp 服务全包](../evidence/mro-frame-review/mro-service-final.log) 通过，0.417 秒。此前 [首批四项](../evidence/mro-frame-review/pagination-go.log) 和 [既有入口集成](../evidence/mro-frame-review/pagination-integration.log) 日志保留。

[首批 race 日志](../evidence/mro-frame-review/pagination-race.log) 记录 10000 工具单项通过（234.68 秒），随后组件用例遇到整批 240 秒时限，进程失败；这不是整批 race 通过证据，也未报告竞争问题。按剩余范围重跑的 [五项分页与既有缺件 SQLite 短 race](../evidence/mro-frame-review/pagination-rest-race.log) 通过，app 68.521 秒、SQLite 45.238 秒。全库门禁仍由根任务独立运行并记录，不由专项结果推定。

此分页约束公开响应，不把内部 10000 条聚合包装成无限容量，也不承诺读取 10000 条资料的内存为单页大小。现场资料语义、法规适用性和真实维修操作仍由用户验收；本轮没有外部服务调用或实际放行动作。
