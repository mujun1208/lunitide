# M0-M6 补全收口证据（gap-0 ~ gap-11）

生成时间：2026-08-15T09:16:48.971799+00:00（UTC）· 平台：windows/amd64 · 结论：**PASS**

## 验证矩阵

| gap | 范围 | 验证命令 | 结果 |
|-----|------|----------|------|
| 1 | 0053 迁移 + store schema 精确匹配 | `go test ./internal/storage/sqlite/...` | PASS |
| 2 | M5-H3 revert 残留 / H1 convert 审计 / H2 publish 崩溃回滚 / M1 committed CAS / M4 changeset 审计 | `go test ./internal/...` | PASS |
| 3 | endpoint/barrier 审计 / cancel 级联 effect_journal / M0-M3 审计事务化 / emitFinal marshal | `go test ./internal/...` | PASS |
| 4 | OpenAPI 3.0 consumer 解析器（≤5MB、$ref≤10、paths≤500、六认证枚举、压缩炸弹防护） | `go test ./internal/openapi/` | PASS |
| 5 | Integration/ApiOperation/FieldMapping 治理 + CredentialRef（M6-CRD-001） + 乐观锁 | `go test ./internal/app/ -run TestM6S5` | PASS |
| 6 | HealthSample/CallLog append-only + 五态健康聚合（paused 优先，M6-HLT-001） | `go test ./internal/app/ -run TestHealthFiveStateAggregation` | PASS |
| 7 | Skill Manifest 校验（M6-SKL-001）+ skill 五表 + ImportCandidate 状态机 | `go test ./internal/app/ -run TestSkillImportPipeline` | PASS |
| 8 | ComplexityDecision 路由 + Manifest/Bundle/Synthesis 冻结 + delegation 集成 | `go test ./internal/app/ -run TestComplexityRoutingAndSynthesis` | PASS |
| 9 | CloudRunner registry/attestation/mTLS + region+egress 调度 + 回执对账 | `go test ./internal/app/ -run TestCloudRunnerLeaseAndReconcile` | PASS |
| 10 | 8 个 bridge schema + envelope 枚举 + 白名单 + handler + engine 路由 + 路由级测试 | `node scripts/generate-bridge.mjs --check` + 路由级测试 | PASS |
| 11 | 全量回归 | `go vet ./...`（0 警告）· `go test ./...`（ok=74 fail=0）· `npm run build`（tsc + vite 319 模块） | PASS |

结构化证据：[bundle.json](./bundle.json)（bundleDigest: `4cc25e26a4fc9630f9c34e2ba41e43f09799eb982448bf1ea5cda5e4b8a21f4e`）

## gap-10 接线明细

- 新增 schema：`openapi.parse` / `complexity.decide` / `skill.import.{discover,inspect,submit,approve,reject,revoke}`（api/bridge/v1/，均含 x-examples 正反例）。
- `envelope.schema.json` method 枚举与排序后的 x-method 精确一致（生成脚本强校验）。
- `generate-bridge.mjs` enabled 白名单同步扩展；契约再生成（web/src/generated/bridge.ts、internal/bridge/schema_generated.go、internal/contract/schema_generated_test.go）。
- engine.go：`m6skills`/`m6routing` 服务字段、7 条路由、`SetM6GovernanceServices` 装配器。
- 路由级测试（m6_s5c_handlers_test.go）：openapi 解析含 M6-OAS-002 ref 环上送、复杂度同信号摘要重放复用决策、skill 流水线 7 方法全链（幂等重放语义验证）。

## 未实现披露（诚实边界）

1. **DatabaseContract 三实体与 ETL/OData**：legacy deferred，按设计不实现。
2. **stdio 传输 Gate 保持关闭**（M6-MCP-004）：M6 不宣称 stdio 完整交付；红队证据见 `docs/evidence/stdio-5c/`。
3. **M6 服务未在 cmd/engine/main.go 生产装配**（切片 1-4 与 S5C 同此状态）：装配前相关 bridge 方法返回可重试的 `STORAGE_UNAVAILABLE`，不 panic、不静默；装配属后续装配/UI 切片，本收口不冒然接线（避免在缺少 extension 目录/stdio 沙箱等基础设施决策时引入半装配状态）。
4. **错误码形态**：新增码按域内定义（`M6-OAS-001` 等，见 internal/openapi 与 internal/domain/m6supply），无集中注册表——遵循项目既有惯例。
