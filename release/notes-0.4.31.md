# Lunitide 0.4.31

## 修复与改进

- **聊天图片气泡**：发送时把附件写入消息正文，历史里显示缩略图，不再只剩文字。
- **视觉路由**：聊天模型不支持识图时，先走已配置的视觉模型；失败则去掉图片并说明原因，不再误报「无法执行」。
- **OpenAI 兼容识图**：`image_url.url` 在 400 后自动改试 raw base64，适配 DeepSeek / 部分网关。
- **会议系统声音**：引擎用 WASAPI loopback 收录本机扬声器，不再弹出共享选择器；前端只把 poll PCM 混进 ASR，不二次混进录音文件。
- **会议纪要**：优先较快的 LLM，关闭推理模式，最多尝试 3 个已启用模型。
- **人脉截图**：继续走原生屏幕捕获与分块上传，避免浏览器共享选择器。

## 验证

- `go test ./... -count=1`（Quality 20m 预算）
- `go vet ./...` / `go build ./...`
- `npm --prefix web run verify:bridge` / `typecheck` / `test` / `build`
- `release/Test-OmniExcluded.ps1`

## 安装包

- `release/out/Lunitide-Setup-0.4.31-x64.exe`
- `release/out/SHA256SUMS.txt`
