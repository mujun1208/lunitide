# Lunitide 0.4.30

## 修复与改进

- **Word / 桌面类型**：识别「Word 之后=…」类指令；`desktop.type` 与月伴上下文对齐，避免误路由。
- **会议 ASR**：补录与摘要链路修复，减少长段音频导致的逐字稿丢失；前端 `meetingText` / `localSpeech` 同步。
- **会议录制**：关闭录制时的多余弹窗（`MeetingPage` `interactive: false`）。
- **人脉截图**：原生屏幕捕获 + 局域网分块上传；Bridge 大文件超时延长至 120s（`people.screen.capture` / `template.file.stage` 契约）。
- **PPT 产物**：会话卡片过滤 PPT 类 artifact；不再因 PPT 自动打开工作区。
- **项目工作台**：按阶段隔离会话（`projectPhaseSession`）；PM 聊天滚轮上滑时暂停自动跟随（`streamScroll` + 样式）。
- **失败轮次**：聊天失败/中断时持久化用户输入，便于重试（`chat.go` / `turnHistory`）。
- **小说 DOCX**：生成时自动填充作者字段。
- **Composer**：进行中轮次主按钮改为停止图标。
- **资产模板**：模板文件分阶段上传与登记（`assetStage` / `template_staging`）。
- **Session / UI**：多项 SessionPage、Workspace、Profile 与测试同步。

## 验证

- `go test ./... -count=1`
- `cd web && npm test -- --run`
- `release/Test-OmniExcluded.ps1`（Setup 不含 Omni 运行时）

## 安装包

- `release/out/Lunitide-Setup-0.4.30-x64.exe`
- `release/out/SHA256SUMS.txt`
