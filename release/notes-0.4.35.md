# Lunitide 0.4.35

## 修复与改进

- **会议纪要**：总结提示改为覆盖每条实质内容，禁止用「未说明背景 / 内容零散 / 尚未生成待办」搪塞；宁可多列要点，也不漏人名、数字和结论。
- **同事聊天截图**：📷「截取桌面」恢复为整屏拍照并发送；「框选截图」仍走区域选择。`people.screen.capture` 增加可选 `region`。
- **月伴打断**：打断后立刻清掉 TTS 在途合成，先同步聆听模式再开麦，避免后续语音识别被当成仍在播放。
- **桌面图标**：月亮源图去掉深蓝光晕圈；`gen-icon` 增加 halo knockout，重新生成 png / ico / `lunitide.syso`。
- **资产模版上传**：最后分片重试不再清空已写入字节；`template.create` 成功后才删除暂存，避免 UI 到 100% 后报「分片上传未完成或已过期」。
- **侧栏生成中**：普通对话列表用实时会话登记驱动转圈，正在生成的那一条会转；转圈对读屏隐藏，按钮名称仍是会话标题。
- **工作区**：`workspace.write` / 代码页不再自动弹出右侧工作区，PPT 和文档生成留在对话里。
- **产物与追问**：PPT / Word / Excel / PDF 以可点击卡片显示，点开会用系统默认程序打开；助手回复下给出下一步建议按钮（问句或按产物类型）。

## 验证

- Quality 等价门禁通过（`CGO_ENABLED=0`）：`verify:bridge`、`Test-OmniExcluded.ps1`、`go test -timeout 20m -count=1 ./...`、`go vet ./...`、`go build ./...`、`generate:bridge`、`typecheck`、`npm --prefix web test`（129 files / 959 tests）、`npm --prefix web run build`、生成契约与工作区一致
- 未跑 CI 第二岗 `go test -race -timeout 90m ./...`（本机耗时长，GitHub `windows-cgo-race` 仍会跑）
- `Verify-Release.ps1` 对 0.4.35 stage + installer 通过
- 本机 Authenticode：`CN=Yy.MJ`（仅当前 Windows 账户信任）

## 安装包

- `release/out/Lunitide-Setup-0.4.35-x64.exe`（Authenticode `Valid`：`CN=Yy.MJ`，DigiCert RFC3161 时间戳。仅当前 Windows 账户信任该证书；其他电脑仍可能提示未知发布者）
- SHA-256：`d4c2b3090af8a6e58919cb20c400d8b27adec7581568e141838449c2fde1f91a`
- `release/out/SHA256SUMS.txt`
- `release/out` 只保留 0.4.35 安装包与 stage
