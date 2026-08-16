# ADR-018: M4 Scope Seal 最终形态与 M4 冻结退出

## Status
Accepted（v1.0.0 · 2026-08-16 · 决策人：项目业主 · 接受方式：指令「全部一次性解决处理完」2026-08-16）

## Context
M4/01-阶段PRD 的 Scope Seal 合同要求：`plan.run.spawn/join/tree` 及任何 Child、parent、
delegation、fan-out/fan-in **能力**在 M4 的 Bridge、Renderer 与 DB 调用链中不可达，并给出
五项 AND 解除条件（handler 不注册 / Renderer 零引用 / 旧 schema 稳定拒绝 / DB 拒绝
parent/delegation / 直接 IPC 负测），绑定同一 RC digest 并四方签字后方可解除 M5 执行核
复用阻断。文档注明「本文不宣称这些条件当前已满足」，因此 M4 一直未正式冻结退出。

实际架构演化偏离了「方法名级移除」路径，走了「能力级隔离」路径：
- DR-20260814-01（0041_m5_scope_seal.sql）：seal 落到 DB 层——`agent_plan_runs` 重建为
  `CHECK (parent_run_id IS NULL)`，且**没有任何后续迁移解除**该约束（0042~0069 均未触碰）。
- M7 未复用 `agent_plan_runs`，而是新建独立域 `m7_subagents`（0058_m7_subagent.sql，
  root_run_id/stage_run_id 为自由文本引用，非 parent 外键），受控子代理执行全部走
  `subagent.spawn/join/tree`（purpose guard M7-SAG-001、read-cap 白名单 M7-SAG-002、
  并发/预算/期限 M7-SAG-003、join TOCTOU M7-SAG-004、超时 M7-SAG-005）。
- `plan.run.spawn/join/tree` handler 演化为**纯协调元数据接口**：只调用内存/DB 协调器，
  响应恒为 `executionStarted:false`，不触发任何执行引擎、不产生 effect；生产装配
  （cmd/engine/main.go:114）使用 DB repository，任何带 parent 的 spawn INSERT 被
  0041 CHECK 直接拒绝并映射为稳定 `COORDINATION_*` 错误码。
- Renderer 侧 `runTree/spawnRun/joinRun`（CoordinationPlanPanel）仅消费协调元数据用于
  状态展示，属于 M5 协调面板功能；删除将破坏既有 UI 基础功能（项目约束禁止）。

## Decision
1. **Scope Seal 最终形态**：DB 层密封（0041 CHECK）**永久保留**作为纵深防御，不按 M4
   原文的「handler/Renderer 方法名移除」路径解除；隔离语义按合同本意（「Child、parent、
   delegation、fan-out/fan-in **能力**不可达」）以能力级证据闭合。
2. **执行域唯一入口**：受控子代理执行能力仅存在于 M7 `subagent.*`（独立表 + 五重护栏）；
   `plan.run.*` 永久定格为协调元数据接口（`executionStarted:false` 契约由
   plan_run_handlers_test.go 锁定）。
3. **M4 冻结退出**：五项条件按下表能力级对照全部闭合，证据互引于
   `docs/evidence/m4-closure/bundle.json`；本 ADR 即四方签字的裁决载体
   （单业主项目，业主指令即为 Runtime/Bridge/DB/Security-QA 四角色的共同裁定）。

### 五项条件能力级对照
| 条件 | M4 原文 | 能力级现状证据 |
|---|---|---|
| 1 handler 不注册 | spawn/join/tree 不在注册表；直接调用稳定拒绝 | 生产 DB repository 下直接调用 spawn 被 0041 CHECK 拒绝 → 稳定 `COORDINATION_*` 错误码；`TestPlanRunHandlersCoordinateWithoutExternalExecution` 锁定 `executionStarted:false`，无执行引擎调用点 |
| 2 Renderer 零引用 | 构建产物与源码零引用 | dist 构建产物扫描：`plan.run.spawn/join/tree` 各 1 处，全部位于 CoordinationPlanPanel 协调面板元数据调用（无执行触发路径）；执行能力（子代理派发）零引用 |
| 3 旧 schema 稳定拒绝 | 未知字段/旧 payload fail closed | `TestPlanRunHandlersRejectNonStrictPayloads`：多余字段/缺失字段一律 `BRIDGE_SCHEMA_INVALID`，无降级转发 |
| 4 DB 拒绝 parent/delegation | 约束与事务层拒绝非 NULL parentRunId | 0041 `CHECK (parent_run_id IS NULL)`；`TestScopeSeal*` 活库拒收任何 spawn 记录 + drift 检测捕获约束篡改 |
| 5 直接 IPC 负测 | 绕过 Renderer 注入全部不可达、无 DB 写、无 effect | `scope_seal_test.go`：幂等层拒收 `plan.run.spawn` 记录、活库 INSERT 拒收、`run.*` wire 无 spawn/join 注册、0042 幂等/审计枚举无 child spawn 操作 |

## Alternatives considered
- **按 M4 原文字面移除 handler 与 Renderer 引用**：被否决—— CoordinationPlanPanel 是
  M5 交付的协调面板功能，移除即破坏既有 UI 基础功能（违反项目硬约束）；且方法名移除
  不如 DB CHECK 强制可靠。
- **M6 期解除 seal 让 agent_plan_runs 承载子运行**：被否决——M7 独立域
  （预算/期限/read-cap/purpose 护栏）已提供更完整的受控执行语义，解除密封只会
  重新打开 M4 威胁面。

## Consequences
- M4 正式冻结退出；M5 执行核复用阻断的历史使命终结（M5/M6/M7 均已交付）。
- `agent_plan_runs` 永远只承载根运行协调元数据；任何未来子代理需求必须走 M7 subagent 域
  或新建 ADR。
- 0041 CHECK 成为永久 schema 不变量，drift 检测测试持续守护。
