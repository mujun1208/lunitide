# R3 最终审计与五分整改路线

> 审计日期：2026-09-15
>
> 审计范围：`00`～`09`、`11-master-implementation-plan.md`、[12-requirements-traceability.md](12-requirements-traceability.md)、`ui-demo/index.html`、Demo 回归脚本，以及文档直接引用的当前代码入口。
>
> 审计方法：用户意图、PRD、跨模块合同、模块计划、总排程、当前代码、Demo、自动测试与实机证据八向复核。
>
> 修改边界：本轮校正 `00`～`12` 和 `ui-demo`；没有实现产品代码、安装模型或改动用户数据。

## 1. 审计结论

R3 已把本轮用户要求完整写入可执行合同：自动筛选有价值记忆且问候/天气等不记、`auto/manual/off` 三模式、个人/项目范围、统一简约 UI、Windows OCR 真实探测、PaddleOCR-VL-1.6 条件选装，以及已确认的媒体中心与 MiniPlayer 设计。迁移分配、设置恢复字段、OCR DTO、六态 probe、Bridge method、overview 零请求和 W0 依赖顺序也已在规范文档中收敛。

`00` 和 `07` 记录的 **3.6/5** 是 R3 整改前的透明基线，不是当前产品体验分，也不是已经交付的分数。按同一五维量表复核当前文档，整改后的**文档实施就绪度暂评 4.2/5**；缺少的 0.8 分来自真实代码、冻结基线和运行证据，而不是继续增加设计文字。产品仍为 **0 项 `VERIFIED`**，因此不能把 4.2/5 写成产品完成度，更不能宣称产品已经 5/5。

当前可以开始 11 的 P00；当前不能宣称 R3 已实现、Paddle 可安装或发布门已通过。

## 2. 必须分开的四类对象

**当前产品 ≠ R3 文档 ≠ UI Demo ≠ 目标产品。** 下表给出每一层能证明和不能证明的边界。

| 对象 | 当前事实 | 不得越界的表述 |
|---|---|---|
| 当前产品 | 保留既有 Memory/M8、Windows OCR 调用、OCR routing、legacy PP-OCR 目录登记和媒体外部控制基线。 | 不能把目标 migration、worker、播放器、融合 UI 或计划新增测试写成现状。 |
| R3 文档 | 8 项顶层需求、48 项 PRD requirement、C1～C6、模块任务、P00～P15 和发布门已建立追踪。 | 文档完整不等于代码存在，也不等于测试通过。 |
| UI Demo | 当前 13 项 DOM 回归覆盖核心设计行为；它是页面内存模拟。 | 不能用 Demo 证明 Bridge、WinRT、Paddle、WebView2、持久化、授权或恢复。 |
| 目标产品 | 以 11 的依赖图实施，并由 07/12 的同版本证据验收。 | 未达到 `VERIFIED` 前，不得用截图、mock、旧测试或厂商 benchmark 替代交付证据。 |

`00` 中的 `b1d58b0d` 只代表形成 R3 文档时的审计快照。它不是永久当前 HEAD，也不证明本轮功能；实际开工基线只能由 P00/X0 重新生成的 HEAD、tracked diff digest、untracked manifest digest 和 migration allocation 确定。

## 3. 已关闭且不得重新报告的问题

以下项目仅表示“R3 文档与 Demo 已对齐”，不表示正式产品已经实现：

| 已关闭项 | 当前唯一结论 | 规范位置 |
|---|---|---|
| 记忆三模式与范围 | 用户面仅 `auto/manual/off`；个人/项目开关门控对应范围。关闭范围阻断该范围写入、搜索、召回和注入但不删数据。 | `02 §6.3.0/§6.8`；`03` 冻结合同与 Task 8；`06 C1/C6`；`11 §0.2/P02/P04/P06`。 |
| 设置迁移 | `0160～0164` 只是候选；P00/X0 扫描后一次冻结。重开旧总开关使用 `last_non_off_capture_mode`。 | `00` 开发顺序；`02 §6.3.0/§11.1`；`03` 全局约束；`06` 全局约束/C1/X0；`11 P00/P02`。 |
| 记忆 UI | “智能能力”overview 恰好两卡且零数据请求；记忆详情为只读状态头、单列列表和一个复用抽屉。 | `02 §6.8`；`03 Task 8`；`06 C6`；`07 U-R15`；`11 P06`。 |
| OCR 合同 | `OCRScope/ResolvedOCRRequest`、六态 probe、`ocr.routing.get`、`ocr.pack.get`、`ocr.pack.uninstall` 已统一。 | `04 Task 3～5`；`06 C3/C4/C6`；`07 O-R09～O-R12`；`11 P07/P08/P10`。 |
| Paddle 条件门 | P07/P08 先交付 Windows OCR 与 disabled Paddle 基线；P09 执行 W0；只有 W0 通过才进入 P10。W0 失败不阻断 Windows 基线。 | `04 Task 0～7` 完成顺序；`06 C4/C6`；`11 P07～P10`。 |
| OCR 首屏 | 首屏显示只读路由摘要、真实 probe 摘要、检查时间和重试；语言、provider、legacy 与 pipeline 细节进入高级区。 | `02 §7.7`；`04 Task 6`；`06 C6`；`11 P08`。 |
| 媒体与活动 | 无会话及 load-only 离页均无 MiniPlayer；首次播放后离页才出现，pause 保留，close 在模拟终态前保持、终态后隐藏；视频离页不冒充音乐；“已发送待核验”与“已确认”分离。 | `02 §8.6`；`05 Task 0/6/7`；`06 C5/C6`；`11 P11～P13`；`09`。 |

因此，上表所列旧问题均已关闭，不得继续作为当前缺陷或 `BLOCKED` 理由。

## 4. 多角度复核

### 4.1 用户需求与 PRD

8 项顶层用户需求均能下钻到 `02 §10` 的 requirement ID；12 逐行覆盖 Memory 18、OCR 12、Tool 4、Media 10、UI 4，共 48 项。没有遗漏 requirement，也没有把“你好/天气不记”“Paddle 不得假实现”弱化成可选建议。

### 4.2 合同与 schema

`02` 负责产品行为，`06` 负责 C1～C6 技术合同，`03`～`05` 负责模块步骤，`11` 只负责任务依赖，`07` 负责验收，12 负责状态。当前复核未发现会让迁移号、设置字段、OCR DTO、Bridge method、probe enum 或 W0 顺序产生第二种实现答案的开放 P0/P1 文档冲突。

### 4.3 总计划可执行性

11 已把共同基线、真实性修复、Memory、Windows OCR、Paddle W0/P10、Media、集成测试和发布证据分为 P00～P15。P07 包含 Windows 基线、候选 0163、默认 disabled gate、Bridge 和 probe；P10 只包含 Paddle 专属实现，消除了 W0 失败时无法交付诚实 OCR 基线的死锁。发布证据使用 `testedSourceHead → p14AttestationHead → releaseAttestationHead` 两级白名单提交链，避免“提交证据后 HEAD 改变、证据立即失效”的循环。

### 4.4 当前代码与迁移

现码不是 R3 成果。migration allocation、升级前一致备份、目标 schema/worker/UI 和新验收 evidence 尚未生成。候选编号是已定义的 P00 输入，不是当前冲突；只有 allocation artifact 合入后，实际编号冲突才应转为 `BLOCKED`。

### 4.5 Demo 与正式产品

Demo 当前核心行为与已确认设计一致，13 项 DOM 回归可作为设计回归。它覆盖同一 `MemoryDrawer` 的设置/单条/高级视图、load-only 不出现 MiniPlayer、首次播放后离页出现、pause 保留以及 close 等待模拟终态；仍未覆盖真实项目 scope 捕获链，160ms stop 只是页面计时而非后端回执。这些是 Demo 保真度限制，不是产品合同冲突。`09` 已把 DOM、浏览器与截图证据边界分开，截图不能替代真实 WebView2 和辅助技术验收。

### 4.6 验收与证据

07 已定义 M-R01～15、O-R01～12、A-R01～12、V-R01～04、U-R01～15 和实机门；12 为每一行给出自动测试、人工/实机门和固定证据路径。因为目标实现和同版本 artifact 尚不存在，所有 48 项保持 `TEST_DEFINED`，`IMPLEMENTED=0`、`VERIFIED=0`。

## 5. 透明评分

评分对象是“需求到交付的实施就绪度”。每维最高 1 分，沿用 `07 §1` 的五项检查口径：

| 维度 | R3 整改前 | 当前文档暂评 | 依据 | 达到 1 分的条件 |
|---|---:|---:|---|---|
| 需求闭环 | 0.8 | 1.0 | 8 项顶层需求和 48 项 PRD requirement 均已追踪。 | 后续变更保持唯一 ID 与逐行映射。 |
| 现码与可行性 | 0.6 | 0.6 | 真实入口已审计；R3 代码、W0 和三条垂直切片未完成。 | P00/P01 与 Memory、OCR、Media 真实切片落地。 |
| 合同确定性 | 0.6 | 1.0 | 迁移/设置、DTO/API、六态 probe、UI 与依赖顺序已收敛。 | 实施期间 schema 生成与合同测试持续无漂移。 |
| 安全与恢复 | 0.8 | 0.8 | scope、来源、遗忘、签名、fencing 和回滚已定义，尚未实演。 | 备份、迁移、遗忘、导入、worker/player 故障恢复实演通过。 |
| 效果与可验证性 | 0.8 | 0.8 | 固定集、测试 ID、证据 schema 与实机门已定义，均未运行。 | 07 全部门在同一冻结版本执行并保存 artifact。 |
| **合计** | **3.6/5** | **4.2/5** | 4.2 仅表示当前文档更可执行。 | 第 8 节全部满足后才能验收产品 5/5。 |

## 6. P0 / P1 / P2 台账

### 6.1 P0：当前交付门，不是已解决功能

| ID | 当前证据 | 风险 | 关闭位置 |
|---|---|---|---|
| R3-P0-01 基线未冻结 | P00/X0 计划的 baseline manifest、migration allocation 与升级前一致备份尚未生成。 | 在移动 HEAD 或脏工作树上直接编码，会让迁移和测试证据不可复现。 | `11 P00`；`06 X0`；12 `BASE-E`。 |
| R3-P0-02 目标未实现 | 12 的 48 项全部为 `TEST_DEFINED`，`IMPLEMENTED=0`、`VERIFIED=0`。 | 计划、Demo 或计划新增测试可能被误写成已交付。 | 按 `11 P01～P15` 实施；只有 12 可升级状态。 |
| R3-P0-03 Paddle 条件门未运行 | 尚无真实 Windows 离线完整 layout+VLM、签名、许可、资源与故障报告，也无可验的 `verifiedRuntimeProfileDigest`。 | fake worker、legacy marker 或开发机缓存可能被错误升级为 ready。 | `11 P09`；通过后才进入 P10。未通过时保持 disabled、mutation/operation/download=0。 |

P00 尚未执行和 W0 尚未运行都是预期的交付门，不是新的文档矛盾。若 P00 发现占号且未能一次重分配，或 P09 发现 W0 客观失败，则把受影响 requirement 行转为 `BLOCKED`、对应发布证据标 `INCOMPLETE`；W0 失败仍不得阻断 P07/P08 的 Windows OCR 基线。

### 6.2 P1：开放合同缺陷

当前复核没有开放的 P1 合同缺陷。实现中若 Bridge/schema 生成报告、迁移 allocation 或同版本验收发现规范分叉，必须先把对应行改为 `BLOCKED` 并同步修正 `02/03/04/05/06/07/11/12`，不得在代码中选第三套语义。

### 6.3 P2：非阻断维护项

| ID | 当前问题 | 风险 | 应改位置 |
|---|---|---|---|
| R3-P2-01 | Demo 有个人 scope 的可执行捕获回归，但没有项目 scope 捕获场景；MiniPlayer 已模拟等待 stop 终态，却不具备真实后端超时、失败与恢复回执。 | 设计演示无法证明跨项目隔离和真实异步恢复；这不改变产品合同。 | `09` 已明确 Demo 边界；如后续需要提升原型保真度，可补项目 scope 与 stop 失败两个设计用例，正式验收仍只能由 M-R03、A-R12、U-R09 完成。 |

## 7. 文档逐份评分

下表评的是当前文档质量与可执行性，不是对应产品功能的完成度：

| 文档 | 评分 | 当前判断与应改重点 |
|---|---:|---|
| `00-README.md` | 4.8 | 范围、阅读顺序、快照边界与 P00/X0 原则清楚。 |
| `01-review-findings.md` | 4.2 | 作为历史审计有价值；引用时必须注明历史，不能覆盖 R3 当前结论。 |
| `02-upgrade-prd.md` | 4.8 | 用户行为、48 项 requirement 和未来/现状边界完整。 |
| `03-memory-implementation-plan.md` | 4.9 | 三模式、双 scope、迁移、manual create、单抽屉和新增 M-R 门已收敛。 |
| `04-ocr-implementation-plan.md` | 4.9 | Windows probe、legacy 边界、完整 Paddle pipeline、DTO/API 与 W0 顺序明确。 |
| `05-media-ui-implementation-plan.md` | 4.8 | 真实性、生命周期、私有资源和 canonical UI 组件已统一。 |
| `06-integration-contracts-plan.md` | 4.9 | C1～C6 成为唯一跨模块合同，并与子计划对齐。 |
| `07-acceptance-and-scorecard.md` | 4.9 | 自动/人工/实机门完整；U-R11 已与 O-R09 六态机器合同逐一对齐。 |
| `08-original-requirements-and-evidence.md` | 4.7 | 原始意图与证据边界稳定，能防止范围漂移。 |
| `09-ui-demo-guide.md` | 4.8 | 13 项 DOM、浏览器与截图证据已更新；仍应明确项目 scope 与异步 stop 只是未模拟。 |
| `10-final-audit-and-five-point-remediation.md` | 4.8 | 已删除旧 Demo/schema/W0 冲突，保留交付门与评分边界。 |
| `11-master-implementation-plan.md` | 4.9 | P00～P15、Bridge method、overview 零请求和 W0/P10 拆分可执行。 |
| `12-requirements-traceability.md` | 4.8 | 顶层需求与 48 项逐行追踪，0 `VERIFIED`，条件门不冒充交付。 |

## 8. 产品达到 5/5 的硬条件

只有同时满足以下条件，才能在冻结范围内签署产品 5/5：

1. P00 以实际集成基线生成 HEAD、tracked diff digest、untracked manifest digest、migration allocation 和升级前一致备份；候选编号只有此后才冻结。
2. 12 中全部本期 P0/P1 requirement 达到 `VERIFIED`；每行自动测试、人工/实机门和 artifact 绑定同一 testedSourceHead，P14/P15 仅产生 11 冻结的白名单 attestation 提交，没有 `NOT_RUN/INCOMPLETE/STALE/BLOCKED` 被计作通过。
3. 记忆固定集证明问候、天气、即时价格/时间和一次性命令误存为 0；稳定信息、三模式、个人/项目全路径门、纠正、遗忘、导入导出和恢复达到 07 门槛。
4. Windows OCR 真机覆盖 ready、缺语言、初始化失败、固定图失败、超时与非 Windows；UI 状态、检查时间和重试都来自 probe，不由平台名或 legacy marker 推断。
5. Paddle W0 在干净 Windows x64 CPU、无系统 Python/模型缓存、断网环境验证完整 layout+VLM、签名/许可/资源/篡改/取消/崩溃；W0 不通过时保持 disabled 和零 mutation。若本期承诺可安装，则 W0/P10 未完成时不能签 5/5。
6. 真实 WebView2 完成 Host 选择、私有登记、Range、audio/video、seek、pause/close、刷新恢复、SMTC、epoch 和音频焦点；未核验动作不显示成功。
7. 正式 UI 在目标视口、缩放、键盘、读屏、high contrast、reduced motion 和至少 5 名目标用户任务中通过；Demo 13/13 和截图不能替代产品证据。
8. Token、准确率、时延、资源和用户评价只报告冻结集实测，不用厂商数字或开发机演示冒充 Lunitide 结论。

## 9. 开发与状态入口

- 产品行为：`02-upgrade-prd.md`。
- 跨模块技术合同：`06-integration-contracts-plan.md`。
- 模块实现步骤：`03-memory-implementation-plan.md`、`04-ocr-implementation-plan.md`、`05-media-ui-implementation-plan.md`。
- 唯一总排程：`11-master-implementation-plan.md`。
- 发布门：`07-acceptance-and-scorecard.md`。
- 唯一需求状态与证据索引：[12-requirements-traceability.md](12-requirements-traceability.md)。
- `09/ui-demo`：只作设计证据。

当前状态应表述为：“R3 文档已具备从 P00 开始实施的条件；产品代码尚未实现，0 项 `VERIFIED`；Paddle 仍受 W0 条件门约束。”
