# Lunitide 0.4.69

把同事 9 月 7 日兼容性整改打进正式版本：个人对话与项目分离、月伴周归档、会议环回、同事文件预览、MCP/能力包修复、桌面操作串行。不改 poison / 月伴 TTS 音色 / 玉盘像素 / 星尘配方。

## 1. 对话、月伴、产物

- 个人容器兼容旧创建载荷；组织切换隔离；正式项目校验保留。
- 月伴单一长期会话，每周压缩归档，关闭期间缺失的周下次启动补做。
- 历史工具消息转为有效上下文；选择结果持久化；产物右侧详情与原件打开。
- 图片点击预览走共用模块；文件/目录/音视频打开按路径权限。

## 2. 会议、同事、语音

- 会议环回队列精确取帧并保留尾帧；设备失效恢复保持当前会议。
- 同事截图读取/分片/回执有期限；重试幂等；专家后台任务有生命周期。
- 三语音入口共用执行链；工具结果完整播报；旧连接迟到回调失效。

## 3. MCP、桌面、资产

- 撤销端点不再复用；修复错误包和凭据参数；标准远程 MCP / Streamable HTTP。
- 桌面多个操作串行，输入目标归属于当前任务。
- 资产大文件读取前检查大小；超时恢复；防同一时刻重复提交。
- 迁移未新增；0136 保持 LF checksum。

## 验证

- Go：`go vet ./...` 干净；`go build ./...`（CGO=0）干净；`golangci-lint run ./...` **0 issues**；`govulncheck ./...` 受影响漏洞 **0**；`go test ./...` **全绿**（总覆盖率 **54.0% ≥ 51%** 闸）。
- 前端：`npm audit` **0**；`tsc --noEmit` 通过；`verify:bridge` 无漂移；`vitest run` **224 files / 1706 tests** 全绿。
- `./release/Test-OmniExcluded.ps1` 通过。
- 本机未跑 CI 的 90 分钟 `go test -race`（`windows-cgo-race`）；推送后由 Quality 工作流补跑。
- 未跑 `Test-Install.ps1`（本机已有官方安装，脚本会拒绝）。

## 安装包

- `release/out/Lunitide-Setup-0.4.69-x64.exe`
- `release/out/SHA256SUMS.txt`
- 从 0.4.68 升级。同事未签名的 `compatibility-20260907-r1`（已撤回）和 `r2` 不作为正式包。本机有 `LUNITIDE_SIGN_COMMAND` 与 publisher thumbprint，按生产签名打包装包。
