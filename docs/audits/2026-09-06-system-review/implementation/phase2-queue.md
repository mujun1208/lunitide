# C01 / S02：排队补充输入的持久交付与响应丢失恢复

实施日期：2026-09-06。范围是已授权的打字对话队列链路及其跨页面回包边界；此记录不改变人工验收范围，也不以自动化结果提高评分。

## 问题与实现结果

此前 `run.queueConsume` 把排队记录改成 injected 后立即返回正文。消费响应丢失、前端发送失败，或中途注入后 checkpoint 保存失败，都会失去可重放交付依据；前端错误提示仍可能称内容留在队列。此次保留既有原子额度、完整正文幂等和审计事务，增加独立持久批次，真实模型调用前必须取得已保存批次的唯一启动权。

`0140_queue_delivery_receipts.sql` 新增 `queue_deliveries` 与 `queue_delivery_items`，已登记迁移清单、指纹和历史升级夹具。旧队列状态表未重写。批次状态含义如下：

| 状态 | 持久事实 | 允许的恢复 |
| --- | --- | --- |
| claimed | 已保存该批次成员，尚未完成用户消息准备 | 重放同一批次，或用户取消整批 |
| prepared | 用户消息均已保存，消息 ID 与内容核验通过 | 使用同一消息 ID 启动一次，或用户取消整批 |
| started | 启动权和实际 stream ID 已提交 | 活跃流只观察；失去实际 owner 后转 unknown |
| unknown | 进程恢复或失败后，不能证明模型处理完成 | 显式核对后重试，或核对后不再执行 |
| confirmed | 成功终态已保存，或用户明确结束该批次 | 不允许旧启动请求重新执行 |

`confirmed` 不一概代表模型成功：用户选择取消/不再执行时同样关闭批次，审计 phase 保留 `dismiss`。不会把取消包装成模型完成。

## 生产链路

1. `queueapp.Enqueue → sqlite.EnqueueQueuedMessage` 继续在同一 writer 事务校验请求内容、5 条容量、每分钟额度和审计；UTF-8/NUL 与 8000 字符预算在服务层验证。
2. `run.queueConsume → ClaimQueueDelivery` 持久保存不可变队列成员，同一会话的未完成批次重复读取返回实际状态，不另造批次。升级前因旧并发额度缺陷留下的超额记录，按 5 条分批读取/认领，余量保留，不越过 Bridge 响应预算。
3. `Engine.prepareQueueDelivery → messageapp.Append` 使用现有消息服务及永久 `queue:<queuedId>:<分片序号>` 键。8000 个四字节字符按普通消息的 2048 字符上限保存为 4 条消息，正文不截断。部分消息提交后响应丢失，重新准备仍返回同一消息 ID；不在前端重复 `message.append`。
4. `chat.start(queueDeliveryId) → validateQueueChatStart → StartQueueDelivery` 校验实际会话、不可变消息收据、项目可编辑性及 prepared 状态，事务提交唯一启动权之后才启动模型协程。并发和丢 ACK 后的旧请求不能再次调用模型。网络推理不占用 SQL 事务。
5. 流中补充在 `pullQueuedSupplements` 保存用户消息和 checkpoint 中的批次/正文后才注入。保存失败不继续生成；认领前后新出现的独立任务按实际认领正文再次核对，交回 renderer 为后续任务，不误并入当前任务。
6. `runStream` 仅在有队列批次时保存交付终态；用户消息/assistant/checkpoint 的持久化失败不能产生成功收据。重启由 `run.queueList` 对照实际 stream registry 标记 unknown，正常 prepared 可直接恢复，已启动且结果不明的批次必须由用户核对。
7. 会话回退保留永久队列消息键：原消息被删除后，旧批次不能重新造回正文，可取消该批次；同批其他文本仍完整显示。真正删除会话/项目按作用域清理队列、批次及消息幂等键，避免可重放的孤立元数据。

生产 SQLite 实现统一使用上述 `DeliveryStore`。旧 `Consume` 接口仅保留给没有挂载持久交付接口的嵌入式兼容适配器和既有测试，不作为生产丢失恢复能力的证据。

## 页面行为

`inputQueue.tsx` 保存原请求身份直到得到实际结果；发送回调被 await，失败后读取持久批次，提示“已保存/处理中/结果待核对”，不再假称仍留在未消费队列。`SessionPage` 携带批次 ID 启动，避免再次写入正文；超过普通编辑器长度的合法排队正文也保留完整。

页面同时展示待处理条目和当前批次全部正文。claimed/prepared 可以继续发送或明确取消整批；unknown 提供核对后重试/不再执行；started 轮询实际状态，不自动重发。实际流已结束但启动响应丢失时，轮询仍可收敛。取消一批不会取消下一批尚未认领的条目。

异步处理使用 mounted 与 session generation，而非仅比较 session ID。A→B→A 后旧 A 的读取、消费、撤回结果不覆盖新 A；卸载后不触发发送；旧请求的 finally 不能解锁新一轮 flush。

## 正式回归与证据

使用真实隔离 SQLite、真实消息幂等与实际 chat.start/流注册链路，模型适配器为可控内存假实现，没有外部推理或真实 OS 动作。

| 正式用例 | 验证事实 |
| --- | --- |
| `TestQueueDeliveryLostAckLongTextAndRestartReuseMessages` | 首分片提交后响应丢失、新 Engine 恢复、8000 emoji 完整保留、跨会话收据拒绝 |
| `TestQueueDeliveryConcurrentStartUnknownResumeAndRewind` | 12 路并发启动仅一方成功、失主转 unknown、显式恢复、回退后不重建旧用户消息 |
| `TestQueueDeliveryMidTurnCheckpointFailureRetainsDelivery` | checkpoint 保存失败无补充注入；恢复不重复批次/正文，成功结束后无未完成批次 |
| `TestQueueDeliveryActualChatStartCannotRepeatAfterLostAck` | 真实 chat.start 调用一次模型，活跃及完成后旧请求不能双发 |
| `TestQueueDeliveryConcurrentPivotUsesActualClaimedPayload` | 读取后、认领前到达的新任务按实际批次重新分类，保留全部说明 |
| `TestQueueDeliveryAuditRollbackPermanentReplayAndScopeDeletion` | 审计失败回滚启动，过期日期不使永久键失效，真实会话和项目删除清理作用域 |
| `TestQueueDeliveryLegacyOverCapacityBacklogPreservesNextBatch` | 旧库 7 条分为 5+2；取消第一批后另两条仍可认领 |

已通过的定向记录：

- [Go 定向验证](../evidence/phase2-queue-go.log)：queueapp/sqlite/app/messageapp 受影响用例筛选；SQLite 5.937 秒、app 2.782 秒。messageapp 包该筛选没有直接测试，由 app/SQLite 的实际调用覆盖，不代表该包全套测试。
- [短 race](../evidence/phase2-queue-race.log)：`go test -race ./internal/storage/sqlite ./internal/app -run 'TestQueueDelivery|TestQueuedInput' -count=1 -timeout=120s`；SQLite 54.371 秒、app 57.611 秒，包含永久键和真实 chat.start 的新接线。最后新增的遗留超额用例另有最终定向日志，不混称在先前 race 内。
- [前端回归](../evidence/phase2-queue-ui.log)：`inputQueue.test.tsx`、`SessionPage.queueDelivery.test.tsx`、`SessionPage.followup.test.tsx`、`SessionPage.turnControl.test.tsx` 共 25/25 通过，含页面 generation、卸载及新旧 flush 交错。
- [TypeScript](../evidence/phase2-queue-typecheck.log) 与 [源契约/生成](../evidence/phase2-queue-contract-generate.log) 通过；schema 的正式正反例校验保留。
- [最终定向补跑](../evidence/phase2-queue-final-go.log) 包含历史超额批次回归；见最终运行输出，不以其他代理编译中间态的失败冒充本链路通过。
- 全量前端预检暴露两个旧测试文件没有模拟 `run.queueList`（该响应包含交付批次），于是页面显示了正确的队列读取错误，导致原先单一 alert/list 定位不再成立。已仅修复测试的空队列 list/consume 接口与消息历史定位；未隐藏生产错误。[旧夹具最终专项](evidence/phase2-queue-legacy-fixtures-final.json) 两文件 79/79 通过，14.89 秒。最初调整使用过宽的全局 mock 清理破坏了文件级 fixture，该失败保留在 `phase2-queue-legacy-fixtures.*`，最终只还原两个队列 spy；只以带 `-final` 的进程终态认定本次通过。

文件范围：`internal/queueapp/{service,delivery}.go`、`internal/storage/sqlite/{m10_queue,queue_delivery,uow}.go`、`internal/messageapp/service.go`、`internal/app/{queue_delivery,m10_queue_handlers,chat,chat_turn,chat_run_stream}.go`、`web/src/session/{inputQueue,SessionPage}.tsx`，以及相应用例、三份 Bridge 源 schema/生成物和迁移登记。

## 验收边界

这些用例验证持久状态、新 Engine 恢复、丢回包和并发边界；不冒称完成真实 engine.exe 崩溃注入或长稳真机验收。升级前已经标成 injected 且无交付证据的旧记录，不凭空回填“模型成功”或自动重发。模型已产生外部副作用但结果不明时，系统保存 unknown 并要求用户核对；代码不能据此证明外部副作用恰好一次。全仓普通/race/发布构建的最终候选验证由根任务统一执行。
