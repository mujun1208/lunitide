# 扩展与专家专项：持久化复现证据

基线：0997859 / v0.4.67；记录日期：2026-09-06。

这里是缺陷存在性探针，不是产品成功路径验收。`PASS` 表示探针观察到了审计指出的错误行为；修复后应将探针转为相反的安全断言，不能要求本探针继续 PASS。

所有新增源码使用 `.go.txt` 后缀，普通 `go test ./...` 不会纳入。显式传入 overlay 后才把它们视为各包中的虚拟审计测试文件。没有把虚拟文件写入产品代码目录。

## 文件

| 文件 | 内容 |
|---|---|
| `app_audit_test.go.txt` | MCP 删除后调用、禁用 workspace 门闸后写文件、技能向导原样提交载荷 |
| `m8_audit_test.go.txt` | 专家正文存储失败、KB 旧版本召回、KB failed 状态回滚、伪引用接受 |
| `mcp_audit_test.go.txt` | MCP 实际 schema 变更后调用仍被下发 |
| `pool_audit_test.go.txt` | 连接池所有条目忙时突破硬容量 |
| `overlay.json` | 当前仓库绝对路径映射 |
| `reproduction.log` | 对以上持久源码运行 4 个包、9 项断言的完整输出，exit code=0 |
| `sha256.json` | 4 个源码、overlay、运行日志的 SHA-256 摘要 |

## 复跑

工作目录为 `E:\Trae-Work-Projects\lunitide`。

```powershell
go test -overlay E:/Trae-Work-Projects/lunitide/docs/audits/2026-09-06-system-review/evidence/extensions/overlay.json ./internal/m8app ./internal/mcp6 ./internal/app ./internal/mcp -run TestAuditReview -count=1 -v -timeout 120s
```

迁移仓库位置时，同时调整 overlay 的虚拟源路径和替代源码路径；不需改 `.go.txt` 文件内容。正式基线、Go 依赖和既有测试助手应保持一致。

真实执行部分包括 SQLite 迁移/事务、各服务/handler、MCP 注册表/连接池与 workspace 文件分发。外部 MCP 传输、磁盘满和解析失败用可控假端口注入；没有访问外部 MCP、用户数据库、真实凭据或付费模型。文件副作用仅在 `t.TempDir()` 下，测试结束自动清理。

此处最终日志使用合法 ULID session。首次临时夹具曾因非法 session 被底层正确拒绝，后修正夹具才成功复现门闸缺陷；该初始夹具错误未作为产品问题计分。
