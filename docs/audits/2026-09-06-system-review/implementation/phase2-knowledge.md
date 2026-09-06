# E07 / E08 知识库来源与解析实施记录

本轮在现有本地文件导入链路上实施。实际支持本地普通文件的 Markdown、纯文本、DOCX、PPTX、XLSX、PDF 文本层；不增加 URL 抓取、网络共享文件、OCR 等尚未实现的入口。E08 解析隔离由主任务实现，E07 来源生命周期及界面由 conversation_voice_meetings 实现。

## E07 已实施

1. 新增迁移 `0138_kb_sources.sql`。来源由固定 collection 与规范本地路径标识，保存实际文件摘要、状态、索引版本、单调 revision、校验时间和失败原因；`kb_source_versions` 与 `kb_source_documents` 保留每版发布回执及分组文档关联。
2. `expert.knowledge.ingest` 复用原文件导入作为主动刷新入口。前端刷新携带观察到的 `expectedRevision`；过期修改明确冲突。无内容变化的成功重放复用来源与文档版本，不生成重复文档。
3. 文件先进入持久 `refreshing` 状态，随后在写事务外读取与解析；全部分组正文、来源版本映射、发布状态、审计在同一短事务提交。任一分组失败都不会发布部分文件，也不会重新暴露上一版内容。
4. 解析完成后再次核对实际文件摘要；读入字节、索引正文和发布摘要必须属于同一来源版本。原 `kb.upsertDocument` 的真实文件解析同样校验摘要；没有实现的 blob 等来源不会通过默认空投影假报 ready。
5. FTS、LIKE 回退、向量候选与统计统一限制为最新 ready 文档及当前 fresh 来源映射。对外返回检索引用前再次验证实际源文件，文件改变记 stale、删除记 missing、读取错误记 failed，过期正文不进入当前答案；`kb.cite` 和 `DocumentsReady` 同样复核来源与当前版本。
6. 一个文件分组数量减少时，被移除的组仍保留历史证据，但不会继续进入当前检索。来源级发布保证一份文件的各组版本一致。
7. 刷新失败有持久失败回执；数据库投影事务失败会尝试留下独立失败状态，且不返回未提交的 ready 文档。程序中断留下的 refreshing 明确显示未完成，可重试；后续请求以 CAS 取代它时记录该中断版本，迟到提交不能覆盖新版本。
8. 来源、文档与专家库的归属沿用实际 collection/subject。其他 subject 不得导入、查询该库。当前专家参考资料是本机全局目录；组织私有 MRO 手册由 MRO 自身的 org 记录与访问检查约束，不把全局参考资料伪装成组织私有账本。
9. 来源列表采用稳定 sourceId 游标分页，每页最多 4 个来源，单次文件校验最多 128 MiB，校验上下文预算 5 秒；仅校验本页文件。版本历史每页最多 50 版，携带明确的更早版本游标，可翻页及返回最新版本；历史查询同样绑定实际 expert collection，不能借 sourceId 跨库读取。
10. 知识面板显示来源状态、最近校验时间、失败原因、版本摘要与刷新按钮。刷新失败后重新读取持久状态，切换专家时丢弃旧专家的异步回包。前端调用实际文件导入，不再把整份文件读入 renderer 计算摘要；没有原生 File.path 时可直接填写完整本地路径。

## 遗留数据升级

升级将既有绝对本地文件来源按 collection 与路径归并，初始标记 stale，要求显式刷新验证后才能重新检索；不会自动信任升级前的 ready 标记。原有全部文档版本继续保留。

回填只把每个 documentId 的最高版本映射为一组，避免同一文档的多版历史被误当成多个文件分组。正式迁移测试覆盖同一个旧 documentId 已有两版、升级后文件需要两组的情形，检查两组不会复用同一个 documentId 导致一组被 latest 条件隐藏。

## E08 对接

所有真实 KB 本地解析入口使用 `doctext.ReadSource` 与 `doctext.ExtractContext`。主任务已实现 engine 私有子进程解析模式，输入 32 MiB、解压单段 16 MiB / 总量 64 MiB、最多 2048 ZIP 段、500 PDF 页、200 万字符，以及子进程 10 秒 / 512 MiB / 并发 2 的限额；超限明确失败，不截断成功。生产私有 worker 的实际验证和发行打包证据以主任务的 E08 / Q04 记录为准。

`KBService.UpsertDocument` 保留主任务完成的 prepare → 事务外解析 → 最终 CAS 发布结构；原 `capChunkBody` 静默截断与对应死函数已移除。

## 自动验证

- 前端定向：`ExpertKnowledgePanel.test.tsx` 与 `ExpertKnowledgePanel.sources.test.tsx`，2 文件 / 11 测试全部通过；包含状态刷新、版本 CAS、失败后读回状态、切换专家迟到回包隔离及完整路径导入、来源分页和历史版本分页。最终 typecheck 通过；分页之前主任务全量前端曾通过 214 文件 / 1623 测试，最终全局门禁由主任务统一重跑。
- 最终 Go 定向：`go test ./internal/m8app ./internal/app ./internal/storage/sqlite -run 'Test(KB|ExpertKB|ChatKB|UpgradeV26|ParseBodyIndexer|GoldenNonEmpty)' -count=1 -json`，3 包 / 41 个测试及子测试全部通过，0 失败、0 跳过；原始日志 `evidence/phase2-knowledge-go.jsonl`。
- 新增正式 source 回归：实际 SQLite、文件变更/删除/失败恢复/数据库重开、分组缩减、subject 边界与 CAS、伪造摘要、提交失败回执、未完成刷新重试。
- 新增迁移回归：旧版本完整保留、默认隐藏、刷新后重新检索，以及历史版本不会被误当分组。
- 来源及升级专项 race：`go test -race ./internal/m8app ./internal/storage/sqlite -run '^TestKBSource' -count=1`，2 包 / 8 测试通过，日志 `evidence/phase2-knowledge-race.log`。
- 前端原始证据 `evidence/phase2-knowledge-web.json`：11 通过 / 0 失败。
- 已修正旧正向文件测试的占位摘要为实际 SHA，错误摘要负向测试继续保留；空文件测试也使用真实空白内容摘要，确保失败原因确实是无正文。
- 编译中间态阻断过的探索运行没有计为测试通过。

历史审计证据保持原样；本文件记录整改后的真实行为，不将现场硬件、第三方付费服务或未实现来源计为已验证。
