# Lunitide：DeepSeek / GLM 深度融合与 Codex 对标审查

审查日期：2026-09-12。对象：`E:/Trae-Work-Projects/lunitide` 当前工作区，VERSION `0.4.77`，HEAD `afbcb2c733d4`，包括审查时尚未提交的 Agent Hub 代码。本文是代码与公开协议审查，不是已安装二进制的全量验收，也不是模型性能排行榜。

## 1. 决策结论

**项目已经具备继续深度融合 DeepSeek、GLM 的工程基础，但尚未达到“完整发挥模型能力、随版本升级稳定变强”的成熟状态。围绕本次目标，我给当前工程成熟度 62/100，定位在 60—70 分阶段。**

这个分数用于判断投资顺序，不代表任务成功率为 62%，也不代表只有 Codex 的 62% 能力。当前没有足够的同题、同预算、可重复实测，不能给出两者真实效果差距的百分比。

最关键的判断如下：

| 用户的问题 | 结论 |
| --- | --- |
| 现在能不能深度融合 DeepSeek、GLM？ | 能继续做，而且部分原生能力已经进入实际调用链；跨轮状态、版本参数和兼容边界仍需补齐。 |
| 能不能和它们一起成长？ | 可以通过适配、评测和发布机制跟进；现在主要能更新模型配置，还没有经过验证的持续升级闭环。 |
| 是否已经发挥最大能力？ | 没有证据支持。已发现会损害连续推理、长任务可靠性及参数控制的具体缺口。 |
| 与 Codex 相比怎么样？ | 已有自主执行底座；通用长任务与模型适配的现有证据不足以认为达到 Codex 同级。中文办公交付是值得集中发展的方向，是否领先需要同题验证。 |
| 能否提升到 100 分？ | 可以针对明确版本、任务集和验收标准做到全部通过；无法承诺所有开放任务永久满分，Codex 也不能作为无误的 100 分基准。 |
| 现在应当重写吗？ | 不建议推倒重来。应先补原生协议、完成判定和评测闭环，保留已有工具、存储、权限与办公能力。 |

**下一阶段目标应当是“每次模型升级都能证明交付更好”，并把这个证明用于是否上线的决策。**

## 2. 评分依据与证据边界

| 维度 | 权重 | 本次评分 | 判断依据 |
| --- | ---: | ---: | --- |
| 基础接入与工程防护 | 20 | 17 | 实际流式调用、工具分片组装、凭据租约、缓存用量、异常处理和离线测试已有基础。 |
| 模型原生适配 | 25 | 16 | 同轮 reasoning 与工具回传已接通；跨轮全历史保真、切模型隔离和型号参数策略不完整。 |
| 长任务执行、上下文与完成验证 | 25 | 17 | 有工具循环、检查点、压缩和预算；轮内输入增长、计划验证覆盖和子代理总预算存在缺口。 |
| 模型版本持续演进 | 15 | 5 | 有 codec 与资格表骨架；探测、采用、回归监测、回滚还未形成主路径。 |
| 可重复效果证据 | 15 | 7 | 有离线测试与历史局部 live 验收；缺少当前适配版本绑定的成体系模型对照评测。 |
| **合计** | **100** | **62** | **主观工程成熟度审查，不能代替效果测量。** |

没有把设计文档中的计划、结构体中预留但未赋值的字段、仅存在的数据库表算成已经完成的产品能力。没有因缺少系统评测就否认已经存在的真实交付记录。

本次未调用真实模型 API，也未用私人会话或凭据做测试。观察到的静态缺口与它们在最新官方协议下的预期后果分开陈述；具体上游是否拒绝、延迟增加多少，需要受控实测。

## 3. 已经落地的部分：值得保留的基础

实际主链为：

```text
Chat 请求与上下文装配
  → llmadapter 流式调用
  → reasoning / 正文 / tool_calls 分别收集
  → 工具执行与结果回传
  → 同轮继续推理
  → turn checkpoint + 协议消息组持久化
  → 后续恢复与正文交付
```

具体加分点：

1. **同轮原生推理连续性已有实现。**适配器收集 reasoning，写入 assistant message，同轮工具循环把消息和工具结果加入下一次请求。见 [openai.go](E:/Trae-Work-Projects/lunitide/internal/llmadapter/openai.go:318)、[chat_run_stream.go](E:/Trae-Work-Projects/lunitide/internal/app/chat_run_stream.go:730)。
2. **正常工具组确实保留了每条 assistant 的 reasoning。**SQLite 对组进行序列化，并不是把所有推理都丢掉。见 [protocol.go](E:/Trae-Work-Projects/lunitide/internal/storage/sqlite/protocol.go:12)。
3. **流中断与不完整工具调用有防护。**存在结束标记检查、工具参数组装、生成完成原因校验；不能把这些实现评价为单纯 UI 接口包装。见 [openai.go](E:/Trae-Work-Projects/lunitide/internal/llmadapter/openai.go:353)、[chat_generation_budget.go](E:/Trae-Work-Projects/lunitide/internal/app/chat_generation_budget.go:154)。
4. **用量处理有实际细节。**能读取 DeepSeek 缓存命中和兼容协议的 cached tokens，区分“未报告”和“零命中”。见 [usage.go](E:/Trae-Work-Projects/lunitide/internal/llmadapter/usage.go:14)。
5. **项目存在真实局部交付记录。**9 月 9 日新闻 DOCX 审计最终记录整轮成功、工具回执与文件校验。该记录早于当前 Model Fit 实现阶段，范围是文档转换验收，不能扩展成最新融合能力或视觉美观的证明。见 [历史验收记录](E:/Trae-Work-Projects/lunitide/docs/audits/2026-09-09-news-artifact-postdeploy.md:276)。
6. **有软件质量流程。**仓库配置了 Windows 测试、覆盖率门槛、静态检查、前端类型检查和契约漂移检查。这说明工程维护有基础；本次没有重跑整套 CI，也不能把 CI 配置存在等同于当前全部通过。见 [quality.yml](E:/Trae-Work-Projects/lunitide/.github/workflows/quality.yml:1)。

因此，继续投入是有依据的。最有价值的工作是修通现有链条中损失能力的环节。

## 4. 模型融合的三个首要缺口

### 4.1 跨轮恢复没有保留完整、原序的模型历史

**代码事实：**

- `ExtractMessageGroups` 只提取带工具调用的 assistant，普通回复不进入原生消息组。见 [messagegroup.go](E:/Trae-Work-Projects/lunitide/internal/modelfit/messagegroup.go:58)。
- 无工具终答结束后，最终保存的原生输入仍是 `req.Messages`；正文另行保存。见 [chat_run_stream.go](E:/Trae-Work-Projects/lunitide/internal/app/chat_run_stream.go:1502)。
- 重建时，native 工具组先放入，普通历史随后追加；后者仅恢复 role 和 content。见 [chat.go](E:/Trae-Work-Projects/lunitide/internal/app/chat.go:1218)。

**实际影响：**先问普通问题，再让模型用工具工作，或者重启后继续原任务，恢复出的协议上下文可能缺少历史 reasoning，工具组还可能偏离原来的因果顺序。现有 `native_complete` 只说明局部数据检查通过，不能证明整个会话原生连续。

DeepSeek 当前官方说明：请求携带 tools 时，后续调用须回传所有历史轮的完整 reasoning，包括没有工具调用的轮次。这使上述缺口具有明确的协议风险。[DeepSeek 思考模式](https://api-docs.deepseek.com/guides/thinking_mode/)

**应当怎样改：**建立按会话顺序保存的完整模型消息记录，覆盖普通回复、工具调用、工具结果和终答，逐条绑定原始 reasoning。用 message ID 与顺序索引恢复，不把“正文历史”和“原生工具历史”简单拼接。缺失的旧数据必须标记为只能恢复事实摘要，不能拿其他回合的一段推理补齐。现有 [codec.go](E:/Trae-Work-Projects/lunitide/internal/modelfit/codec.go:83) 的补字段逻辑尤其应收紧。

**验收：**至少覆盖普通回复→工具调用→普通回复→再次工具调用，以及重启恢复；比较实际发出的请求，确认原序与逐条字段一致。流式 UI 展示可以裁剪，协议存储必须遵守对应模型合同。

### 4.2 换模型或端点时，没有目标兼容闸门

**代码事实：**`nativeReplayMessages` 只接收 session ID，使用旧检查点的 codec 重放；没有传入当前目标模型、端点和生效配置用于比较。虽然定义了 `RejectCrossFamily`，生产重放路径没有调用。见 [chat_continuation.go](E:/Trae-Work-Projects/lunitide/internal/app/chat_continuation.go:286)、[codec.go](E:/Trae-Work-Projects/lunitide/internal/modelfit/codec.go:69)。

**触发场景：**同一会话从 DeepSeek 改为 GLM、切换到代理渠道、切换能力不同的新版本。旧模型的专属状态可能进入新目标。是否被拒绝依赖上游，但当前没有足够的边界保证。

**应当怎样改：**恢复前比较目标的 family、模型合同、规范化端点、账户用途、codec/profile revision。只有已验证兼容的组合才能继续原生状态；不兼容时保留事实、目标、工具回执与未完成事项，在新模型上下文中重新建立任务。对用户说明“已接续任务资料”，不要宣称无损继承原模型推理。

**验收：**DeepSeek→GLM、GLM→DeepSeek、同家族版本切换、标准 API→Coding Plan、部署别名五类场景。每类都要证明该保留的任务事实保留、该隔离的原生状态隔离。

### 4.3 请求参数依赖通用默认，没有按型号和端点调优

**代码事实：**当前 OpenAI-compatible 请求类型没有 `reasoning_effort` 与 `clear_thinking`；主要通过 `DisableReasoning` 同时发送两种禁用提示。见 [openai.go](E:/Trae-Work-Projects/lunitide/internal/llmadapter/openai.go:20)。

GLM-5.3 已不允许关闭思考，标准 API 的 effort 仅接受 low/high/max；Coding Plan 的映射规则又不同。[智谱深度思考说明](https://docs.bigmodel.cn/cn/guide/capabilities/thinking)

GLM 的 preserved thinking 还涉及端点默认和 `clear_thinking:false`，要求原始 reasoning 顺序完整。[Z.AI 思考模式](https://docs.z.ai/guides/capabilities/thinking-mode)

**准确影响：**不是 GLM 完全不能运行。端点自身默认仍可启用推理；而代码遇到 400 会剥离禁用提示重试。但产品缺少可控的型号策略，短任务可能增加失败请求和延迟，复杂任务也无法准确选择和记录生效模式。见 [兼容重试](E:/Trae-Work-Projects/lunitide/internal/llmadapter/openai.go:173)。

**应当怎样改：**由版本化 profile 将“快速回应／正常工作／复杂分析”等产品意图编译为合法参数；记录 requested 与 effective 的差别。缺少能力时显式降级。对 400 按错误原因处理，不把所有错误都归为 schema 不兼容。

strict 工具调用和结构化输出可作为后续能力，但必须按端点验证支持范围。DeepSeek 的 strict 文档说明了专用 beta 端点及 schema 条件，因此不能简单给所有工具统一加 strict。[DeepSeek Tool Calls](https://api-docs.deepseek.com/guides/tool_calls/)

## 5. 真正限制长期效果的执行问题

### 5.1 初始上下文检查之后，轮内输入仍在增长

当前有 pre-turn compaction，也有装配后的输入预算检查；主工具循环每次会继续追加消息并发起下一次模型调用。现有 `turnGenerationBudget` 主要限制生成量和调用时间，不是每一步输入上下文预算。见 [chat.go](E:/Trae-Work-Projects/lunitide/internal/app/chat.go:817)、[循环调用](E:/Trae-Work-Projects/lunitide/internal/app/chat_run_stream.go:359)、[生成预算](E:/Trae-Work-Projects/lunitide/internal/app/chat_generation_budget.go:45)。

所以“第一步装得下”不能保证后面几十次工具结果也装得下。原生 reasoning 回传越完整，这个问题越需要同步解决。

建议每一步发送前都检查输入、保留输出空间和任务剩余预算；工具大结果用可检索工件引用，减少重复注入。压缩只能采用对应协议允许的方式：对要求精确保留的原生历史，不得改写后假称原生续接。必要时以经过验证的任务摘要开启新的模型上下文。

更大的窗口是一项容量，不能直接等同于更高交付质量。固定输出上限也不必一律改到模型最大值；应依据任务的截断率、质量、延迟和成本做选择。

### 5.2 Chat 的 plan.run 验证缺少步骤覆盖与证据来源约束

`runPlanCycle` 只收集能够提取 L0 observation 的步骤，`decidePlanVerify` 在这些已收集记录全部通过时直接判 verified。若某一步失败但没有产生 L0，而另一步 L0 成功，缺失步骤不会参与这个判定。见 [chat_plan.go 收集逻辑](E:/Trae-Work-Projects/lunitide/internal/app/chat_plan.go:225)、[验证逻辑](E:/Trae-Work-Projects/lunitide/internal/app/chat_plan.go:139)。

还有更直接的风险：某一步没有调用工具时，模型正文仍进入结果；后续会从正文提取 L0。若正文自称 passed，就可能成为验证依据。见 [无工具返回分支](E:/Trae-Work-Projects/lunitide/internal/app/chat_plan.go:326)。应当对每个必须完成的步骤保存 outcome：通过、失败、不确定、未执行，并把证据绑定到真实工具回执。缺观察本身就是待验证状态；只有全部必要步骤和最终成果通过，任务才能标记完成。

这个问题限定于 Chat 的 `plan.run` helper。项目里的另一条 `PlanExecution` 路径已有 `plan.finish` 与成果校验，不能把两条路径混为一谈。应把已有的强约束复用到其他执行入口。见 [项目计划完成校验](E:/Trae-Work-Projects/lunitide/internal/app/plan_execution.go:215)。

另外，主循环在步数耗尽且已有过程文字时，可能跳过强制总结。`EventCompleted` 本身代表流结束，不能直接说它是虚假业务成功；但 UI 和任务系统必须区分“模型回合结束”“部分交付”“成果已验证”。见 [收尾分支](E:/Trae-Work-Projects/lunitide/internal/app/chat_run_stream.go:1337)。

### 5.3 子代理的总 token 预算没有在循环中完整执行

子代理把 budget 转成每次请求的 MaxTokens，累计 spent，却没有在每次继续调用前用剩余总预算约束下一步。工具循环和最后总结都可能继续消费。见 [chat_subagent.go](E:/Trae-Work-Projects/lunitide/internal/app/chat_subagent.go:216)。

建议让主代理和子代理共享明确定义的预算账户，区分输入、输出、累计调用和时间；每次调用前预留，调用后按实际 usage 结算。预算耗尽后返回已取得的证据和剩余工作。增加代理数量之前，应先证明这种委派确实提升单位成本下的成功率。

这些问题不会因为换一个更强模型自然消失。模型可能提出更长的计划、读取更多材料，反而更容易触发它们。

## 6. 如何做到与 DeepSeek、GLM 一起成长

目前 `CodecForModel` 按名字包含 deepseek/glm/zhipu 选择两个固定 codec。同家族版本、端点和部署别名没有独立合同。见 [codec.go](E:/Trae-Work-Projects/lunitide/internal/modelfit/codec.go:92)。

资格状态仅有 `untested / fixture_pass / blocked`，读取时 `Adopted=false`；本次检索没有发现资格表进入 app、bridge、UI 的采用流程。主路径 `codec.Qualify` 是数据形态检查，不是模型效果认证。见 [资格定义](E:/Trae-Work-Projects/lunitide/internal/modelfit/qualify.go:3)、[资格存储](E:/Trae-Work-Projects/lunitide/internal/storage/sqlite/qualify.go:11)。

已有的连接测试主要证明 HTTP 能通：普通 LLM 分支发送极小 ping，2xx 后不验证完整智能体行为。因此“连接成功”不能升级为“深度适配完成”。见 [连接测试](E:/Trae-Work-Projects/lunitide/internal/llmadapter/openai.go:94)。

建议形成以下闭环：

```text
发现候选模型或接口变化
  → 建立/更新版本化能力 profile
  → 离线协议回归
  → 受控真实协议探测
  → 固定产品任务评测
  → 保留证据并决定是否采用
  → 小范围上线与回归监测
  → 保持、撤回适配配置或切回可用的已验证组合
```

四种状态应明确分开：

| 状态 | 能证明什么 | 不能据此推断什么 |
| --- | --- | --- |
| 声明支持 declared | 官方或供应商宣称支持该能力 | 当前渠道真实可用 |
| 探测通过 observed | 当前端点在指定用例中可用 | 长任务或办公交付稳定 |
| 任务合格 qualified | 固定任务与门槛通过 | 用户已选择采用 |
| 当前采用 active | 当前会话/产品使用该配置 | 未来版本仍然有效 |

每份证据至少绑定：应用 commit、规范化端点与用途、请求模型 ID、上游提供的返回标识、profile digest、提示词版本、工具 schema 版本、用例版本、时间和结果。上游不提供可靠模型修订号时应记“未知”，不能编造指纹。返回 model 名字相同也不能证明底层权重未变化。

**回滚有实际限制：**本地可以回滚 profile 和路由；如果供应商替换了同名模型且不再提供旧版本，产品不能凭本地配置恢复旧权重。应提前保留可用的备用组合，并把这种不可锁定性纳入评测和用户说明。

从产品侧持续成长不要求参与模型训练。优先把新模型能力适配正确、以实际任务验证增益；只有积累了合法且高质量的任务数据、并能证明微调有净收益时，再考虑训练项目。

## 7. 与 Codex 的合理比较方式

Codex 是完整代理产品，包含模型调用、工具、执行环境和会话管理。拿模型 API 连通性与它比较，会遗漏最影响交付的部分。

官方 App Server 已公开 thread/turn/item 结构，支持恢复、运行中补充指令、取消、事件流及审批交互。这些可以作为运行协议的参照，但文档存在某功能不等于本文实测了其所有可靠性。[Codex App Server](https://learn.chatgpt.com/docs/app-server)

| 比较维度 | Lunitide 当前证据 | 应采用的判断 |
| --- | --- | --- |
| 模型选择 | BYOK、多供应商与部署配置已经存在 | 产品灵活性是优势；不自动代表更高任务成功率。 |
| 模型专属状态 | 同轮工具与 reasoning 已接通；跨轮及换模型边界不足 | 优先修正协议，不靠堆提示词补偿。 |
| 长任务 | 有检查点、工具循环、压缩与子代理；仍有输入增长和完成验证缺口 | 需要用长任务、重启、故障恢复证明可靠性。 |
| 中文办公工作流 | 有 Office 生成工具与局部交付记录 | 是适合形成产品优势的方向；需要原生编辑与视觉验收。 |
| 模型升级 | 更新配置可接入，资格采用闭环未完成 | 与模型共同成长的核心建设项。 |
| 可证实的优劣 | 尚未找到本项目对 Codex 的同题系统评测 | 不发布“达到 Codex 的某个百分比”或未经实测的胜率。 |

工作区的新 Agent Hub 已有 `codex exec --json` 命令适配，见 [adapter.go](E:/Trae-Work-Projects/lunitide/internal/agenthub/adapter.go:30)。这是调用外部代理的集成路径；它不会自动把 Codex 的内部执行能力转化为 DeepSeek/GLM 自有运行时能力。该部分包含未提交代码，不能当作已发布验收能力。

如果将来需要更完整的外部 Codex 交互，可以评估 App Server 的持久会话与事件协议；应锁定版本并验证所需方法，不能把实验性接口当长期稳定保证。外部代理适合成为一个可选择的执行器，自有模型融合仍须单独通过验收。

建议做两组对照，避免归因错误：

- **适配实验：**同一个模型、相同资料、相同任务、相同预算，对照现有运行时与修复后的运行时。回答“我们的改动有没有释放更多模型能力”。
- **产品实验：**Lunitide 与 Codex 处理相同用户目标，各自使用明确记录的模型、工具和预算。回答“用户最终得到的东西哪个好”。工具环境不同必须记录，不能把所有差异都归因于模型。

## 8. 办公平台最应该形成的优势

用户真正购买的是可交付结果：PPT 可演示且可编辑，Word 排版稳定，Excel 公式可信，PDF 阅读与打印一致。模型写得更长、推理更久，只能帮助其中一部分。

因此模型工程和文档工程必须一起验收：

| 格式 | 模型主要负责 | 确定性工具与验证主要负责 |
| --- | --- | --- |
| PPT | 叙事、页序、信息取舍、图表意图 | 网格、字体、对齐、品牌、原生元素、导出后逐页渲染 |
| Word | 结构、论证、摘要、引用说明 | 样式、目录、分页、表格跨页、页眉页脚和兼容性 |
| Excel | 分析计划、公式意图、异常解释 | 数据类型、公式计算与校验、单位、图表范围、输入输出边界 |
| PDF | 内容组织与说明 | 字体、分页、文字层、链接、页面尺寸和渲染检查 |

深度融合不意味着强迫同一个模型完成所有模态。例如 GLM-5.3 官方页面当前说明其处理文本；视觉验收应路由给经过验证的视觉模型或专用检查器，不能只把截图附给文本模型就认为完成了质检。[GLM-5.3 模型说明](https://docs.bigmodel.cn/cn/guide/models/text/glm-5.3)

Skills 适合沉淀叙事与工作流程，MCP 适合连接工具与数据。它们的数量不会自动修复 reasoning 丢失、导出失真或公式错误。关于办公视觉、导出和组件选型，可结合既有 [办公平台商业交付 PRD](E:/Trae-Work-Projects/lunitide/docs/design/PRD-office-quality-commercial-2026-09-11.md) 推进。

## 9. 改进顺序与验收门槛

以下是建议验收目标，不是已经达到的指标，也不是对未来成功率的保证。

| 阶段 | 工作范围 | 进入下一阶段的门槛 |
| --- | --- | --- |
| 第一阶段：修正关键语义 | 完整原生消息历史；目标兼容检查；GLM/DeepSeek 型号参数；计划验证覆盖；子代理总预算 | 原序重放、跨模型隔离、失败步骤不误判成功、预算耗尽正确停止等确定性用例全部通过。 |
| 第二阶段：形成增长闭环 | profile 与实际参数记录；资格进入主路径；协议探测；轮内预算；固定任务评测 | 两个目标家族各有可复现证据；每次采用能追溯配置和用例；失败候选不会自动成为当前配置。 |
| 第三阶段：提高交付效果 | 提示词与工具集合调优；办公渲染反馈；长任务故障恢复；对照实验 | 在约定任务集上获得可重复质量提升，同时报告成本、延迟和失败；通过后再扩大任务范围。 |

先用 24 个固定任务启动，建议构成为：12 个办公任务（四种格式各 3 个）、4 个协议与切换任务、4 个长任务恢复场景、4 个代码/数据处理任务。另留一批未参与调优的任务，避免只对题库优化。已知的关键协议缺口仍需单独确定性回归测试，不能靠平均分掩盖。

每个真实任务重复多次，记录：完整交付、部分交付、错误执行、人工介入、实际耗时、token 与可确认成本、工具失败、重试、成果可编辑性。办公视觉评分应尽量盲评，并配合导出后文件检查；模型自己说“很美观”不算通过。

建议关键门槛：所有必须完成的步骤有证据；未验证任务不能显示为已验证完成；协议测试中不跨模型误用专属状态；固定办公任务中无打不开文件、丢数据和公式失效等阻断性问题。对于真实任务成功率，先测基线，再设置合理提升目标与统计区间，避免凭空写 99.9%。

## 10. 本次验证记录与限制

已完成两个包的离线测试：

```powershell
$env:GOPROXY='off'
$env:GOSUMDB='off'
$env:GOTOOLCHAIN='local'
go test ./internal/llmadapter ./internal/modelfit -count=1 -timeout=45s
```

结果：`internal/llmadapter` PASS（1.768s），`internal/modelfit` PASS（0.385s），退出码 0。相关 HTTP 测试使用 fakeConnector。这证明现有离线测试通过，不证明缺失场景已经得到覆盖。

另外，10 项 `internal/app` 定向测试全部通过（包耗时 2.239s），覆盖续轮、UI 断开、journal、生成预算、计划、子代理和项目计划交付；6 项 `internal/contextapp` 定向测试通过（0.602s），覆盖窗口、附件边界、最新用户保护和工具分组。运行时测试第一次因默认 Go 工具链自动校验无法联网而未启动；改为本地 Go 1.26.5，并设置上述三个离线环境变量后通过。没有运行真实模型 API。

本次没有修改产品实现，没有部署、替换模型或执行付费模型对照。没有对实际安装包、所有 Office 导出、所有 GUI 流程作出通过结论。

**最终建议：继续发展这个项目，把下一阶段的核心目标定为“正确适配、可靠完成、可证明升级”。当前 62 分的主要提升空间在可执行的工程缺口中；要证明达到更高水平，靠的是修复后可重复的交付结果。**
