# Lunitide 0.4.42

## 修复与改进

- **对话专家**：13 个岗位改成独立智能体配方（身份/使命/规则/流程/交付/成败）。技能挂在专家身上，不再钉到对话输入框。启动时按目录刷新六段正文，并写入岗位底线技能。
- **专家技能契约**：新增 `expert.skills.get` / `expert.skills.set`。专家中心可改挂载技能；岗位底线不能卸完。
- **同事聊天**：被 @ 的智能体会用办公/网页/技能工具把事做完，回复里给本机路径，不走局域网传文件。拒绝 `user.ask`、`computer.act`、`desktop.*`、`im.send`。同群「认领」互斥。输入框可上下拖高度。
- **专家记忆**：一轮对话结束后把要点写到 `expert:{id}:last`，下次同专家能接上。
- **技能刷新**：目录重写清单不再把 TEXT 版本号 `+1` 弄坏。`EnsureComposeSkills` 可刷新已装配方而不升 semver。
- **PPT**：`pptx.gen` 支持逐页讲者备注（OOXML notes）。
- **会议纪要**：工作台只留「＋ 新纪要 / 历史纪要」。听写引擎和纪要模型改到「设置 → 会议纪要」。听写只管实时字幕，纪要模型只整理已有逐字稿；停止后补转写仍走本机 sherpa。从会议点进设置会回到会议；侧栏「设置」不再卡在会议分类。
- **消息通道**：粘贴 webhook 时识别飞书 / 企微 / 钉钉；支持测试发送与桌面探测。
- **月伴唤醒**：同听写路径连续听，「你好月汐」及常见误听可进月伴；火山听写失败不再改走系统识别。
- **桌面工具**：`desktop=true` 的办公文件落到真实桌面，聊天里给可预览路径。键入与窗口定位更稳。

## 验证

- Quality 等价门禁连续三轮通过（`CGO_ENABLED=0`）：`verify:bridge`、`Test-OmniExcluded.ps1`、`go test -timeout 20m -count=1 ./...`、`go vet ./...`、`go build ./...`、`generate:bridge`、`typecheck`、`npm --prefix web test`（149 files / 1106 tests）、`npm --prefix web run build`、生成契约与 `verify:bridge --check` 一致
- 未跑 CI 第二岗 `go test -race -timeout 90m ./...`（本机耗时长，GitHub `windows-cgo-race` 仍会跑）

## 安装包

- `release/out/Lunitide-Setup-0.4.42-x64.exe`
- `release/out/SHA256SUMS.txt`
- `release/out` 只保留 0.4.42 安装包与 stage
