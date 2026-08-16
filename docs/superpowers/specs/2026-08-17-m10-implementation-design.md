# M10 实施设计（Implementation Design）

日期：2026-08-17
依据：`E:\Trae-Work-Projects\lunitide详细设计文档V2.0\M10\01~05`（产品功能设计已冻结）
范围裁决：全量 7 域分 4 梯队推进（用户确认 2026-08-17）

## 1. 总体架构

零骨架改造。所有新能力走已验证的三条标准扩展链路：

### Bridge 链路
1. 新增 `api/bridge/v1/<method>.schema.json`（含 `x-method` 元数据 + `x-examples` 正/反例）
2. `envelope.schema.json` method enum 与全部 x-method 排序后严格一致
3. `npm run generate:bridge` → `web/src/generated/bridge.ts` + `internal/bridge/schema_generated.go`
4. `internal/app/m10_*_handlers.go`（参数校验 → 服务 nil 检查 → 错误映射，参照 m8_expert_handlers.go 模式）
5. `internal/app/engine.go` 方法映射表注册
6. `web/src/bridge/client.ts`：查询方法加接口；变更方法追加 `MutationMethod` 联合类型
7. `web/scripts/generate-bridge.mjs` 的 `x-enabled` 断言同步更新

### 数据链路
- 迁移从 `0071_m10_<domain>.sql` 起连续编号
- `internal/storage/sqlite/store.go` manifest 登记 SHA-256（不可变清单）
- 已发布表只加列带 DEFAULT（append-only），不改旧约束

### UI 链路
- `web/src/App.tsx` Page 联合类型追加；侧边栏"项目管理"分组追加导航（沿用技能中心/专家中心/资产管理先例）
- 复用 Dialog/ConfirmDialog 与 styles.css 设计变量；不改现有 UI 风格（硬约束）
- 中英文双语（zh 变量模式，与现有页面一致）

## 2. 梯队划分

每梯队独立走 计划→实现→测试 循环；梯队内域可并行。

### T1 增强域
| 域 | 内容 | 关键决策 |
|---|---|---|
| 记忆增强 | 提名工作流（nominate/approve/reject + nominee 列表 API）+ 记忆中枢 3-tab 页 | 基于现有 MemoryPage（m8core 候选/确认/版本链已有）；MemoryPage 从设置页提升为侧边栏独立页 |
| 专家库 | 场景卡实体（scenario_cards 表 + 专家↔场景多对多）+ 专家中心卡片区 | 六段式已有；场景卡为新增聚合，不动 expert 版本链 |
| 技能中心 | skills 加 `category` 列（DEFAULT '未分类'）+ 12 分类枚举 + 第三添加路径（自定义创建向导已有 chat 路径，补 manifest 导入校验）+ 分类筛选 UI | catalog 模板补 category 字段 |

### T2 MCP 市场
- 8 条校验规则补齐（现有：签名链/法务/撤销 → 补：manifest schema 完整性、依赖声明、权限边界、版本兼容、来源溯源）
- `market.search` / `market.detail` bridge 方法 + 市场页（4 列响应式网格）
- 复用 M9 `internal/market/registry.go` org 骨架；个人/org 双轨（org 缺省 personal scope）

### T3 浏览器多模式
- `PageDriver` 接口抽象（internal/browserapp），WebView2 为 driver-1
- 外部 Chrome CDP driver-2（连接状态机：disconnected/connecting/connected/degraded）
- 复刻同套 URL 策略（HTTPS-only、禁 credentials、阻断私网/loopback/file/UNC）
- 设置区 5 个配置节；连接模式切换 UI

### T4 从零域
| 域 | 方案 |
|---|---|
| 排队输入 | 外挂队列层：`queued_user_messages` 表（4 状态机 queued/absorbed/superseded/cancelled）+ `handleRunSend/RunCancel` 替换 FEATURE_DISABLED + drain 钩在 finishTerminal；**不改现有 streamState** |
| 电脑控制 | Go 宿主 Windows API（UI Automation/SendInput）+ 三层拦截（意图识别/输入过滤/进程监控）+ 4 级风险（low/medium/high/critical，critical 强制人工确认）+ SessionPage 状态栏 5 态 |

## 3. 错误处理

- 61 错误码按域前缀（M10-CC/BR/EX/SK/MC/QI/ME）映射 bridge.Failure
- 文档缺失的 M10-BR-010 实现时补定义并记录
- 服务 nil → STORAGE_UNAVAILABLE(retryable)；参数 → BRIDGE_SCHEMA_INVALID

## 4. 测试与验收

每梯队交付标准：
- Go 单测（handler + service + storage）全绿
- 前端 vitest 全绿；现有 294 前端测试零回归
- `npm run verify:bridge` 通过
- `go test ./...` 90+ 包零回归
- T4 追加：24 项安全演练 + 33 项故障注入抽查

## 5. 风险与约束

- 硬约束遵守：Renderer 不接触 SQL/文件系统/密钥/Shell；密钥不进日志/DB/Renderer
- 电脑控制涉及宿主进程新能力 → 威胁模型文档先行（T4 内）
- 0071+ 编号立即锁定，避免与并行分支冲突
- 排队输入与流状态机解耦（外挂层），保护 panic guard/400 回退/cancel 收据等已固化行为
