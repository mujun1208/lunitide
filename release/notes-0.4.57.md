# Lunitide 0.4.57

三个"进语音就报错 / 会议误报"的真实运行期缺陷收口：月伴「无法执行」的根因修复、失败轮不再被读成两个声音、会议纪要在明明收着系统声时不再弹「只在录麦克风」红条。

## 月伴：一直「无法执行」的根因修复（安装即用，进语音就能用）

- **根因**：0.4.56 把月伴改成**一条长青单例对话**后，这条会话会持续累积 `role:"tool"` 的历史行（工具执行结果被持久化）。但消息库只存 `角色 + 文本`，**不存 `tool_call_id`**。下一轮把历史装配回请求时（`combineDurableProviderMessages`），这些 tool 行被原样重放成 `role:"tool"` 且 `tool_call_id` 为空——glm / 智谱这类严格的 OpenAI 兼容服务商会**直接 400 拒绝整个请求**，报 `missing messages.tool_call_id parameter`。系统检测到 400 后去掉 `tools` 重试，但**毒在 messages、不在 tools**，重试仍是同样的 400，于是永远「无法执行 / 模型结果不完整」。
- **修复**（`internal/app/chat.go`）：装配历史时，凡持久化的 tool 结果一律**折叠成普通 user 上下文**（剥掉内部 `[tool-result …]` 记账头），**永不向任何服务商发送"孤儿 tool 消息"**。这是读取路径修复，**会自动追溯修好你现有那条月伴会话**——无需迁移数据、无需任何配置，进语音就能用。
- 覆盖面：所有 OpenAI 兼容服务商都受益（该错误对任何严格实现都成立，不止 glm）。

## 语音：不再「前后音 / 两个声音同时」

- 上面那个失败轮正是重复音的来源——一轮里先念出"（系统提示：…）"降级通知、又念重试的回答，听起来就是"前后两个声音"。根因修好后一轮干净跑完、只念一次。
- 额外加固（`web/src/session/companion/companionText.ts`）：语音字幕现在会**剥掉"（系统提示：…）"系统降级通知**，即便未来任何模型触发降级，月伴也**绝不会把这段系统提示读出来**。

## 会议纪要：明明在收系统声，却误报「只在录麦克风」

- **根因**：0.4.56 新增的红条告警把"有没有系统声"判定成"浏览器里有没有活的系统音轨"。但 Windows 上系统声是由 **Go 引擎的 WASAPI loopback** 直接以 PCM 喂给识别器 / WAV 的，**根本没有浏览器音轨**，于是 `planHasLiveSystemAudio` 恒为 false → 只要在录就弹红条，哪怕歌词正一句句被转写出来（正是你截图那一幕）。
- **修复**（`web/src/meetings/meetingAsr.ts` + `MeetingPage.tsx`）：改用**结构性缺失**判定，绝不因"一时安静"误报：
  - 引擎自采（WASAPI loopback）：以引擎**是否真的开出了 loopback 会话**（`active`）为准——`active===false` 才是真缺失；开着但一时没声（没人说话 / 暂停播放）不算缺失。
  - 浏览器兜底（getDisplayMedia）：无任何活的系统音轨才算缺失。
  - 新增纯函数 `meetingSystemAudioMissing` 单测覆盖：活跃但静音=不报、从未开启=报、浏览器无轨=报、非录制/纯麦不报。
- **质量/速度/准确度说明**：实时字幕由 seed-asr / 本机 sherpa 承担；**停止后仍会对整段麦克风 + 系统声混音 WAV 做本机 sherpa 补转写**，最终逐字稿与纪要以补转写为准，系统声始终进入最终结果。这条链路本版未改动，红条修复只影响"是否误报"。

## 验证

- Go：`go build ./...` 干净；`go test ./internal/app/... ./internal/gateway/... ./internal/contextapp/...` **0 失败**。新增单测 `TestCombineDurableProviderMessagesFoldsHistoricalToolResults`、`TestFoldHistoricalToolResultStripsHeaderAndNeverEmpty`。
- 前端：`tsc --noEmit` 通过；`vitest run` **172 files / 1327 tests** 全绿。新增 `companionText`（剥系统提示）、`meetingAsr`（`meetingSystemAudioMissing` 四类场景）单测。

## 安装包

- `release/out/Lunitide-Setup-0.4.57-x64.exe`（真签名，Authenticode 已验、时间戳）
- `release/out/SHA256SUMS.txt`
- 从 0.4.56 升级
