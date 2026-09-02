# Lunitide 0.4.56

会议纪要「收全电脑内部声音」的两项硬化，加上「月伴」语音进入同一条长青对话（全局单例）。这两条是上一版明确加入工作计划的新需求。

## 会议纪要：系统声音收全 + 去静默降级

- **火山 / 本机实时字幕都混入系统声**：`火山` 与 `本机` 两条 PCM 识别引擎现在都会把 WASAPI loopback 抓到的系统内部声音（腾讯会议 / 飞书会议 / 微信语音 / 音乐 / 视频等，凡是从扬声器出声的）混进**实时字幕**，此前只有 `本机 sherpa` 一条分支能听到对方。改在 `web/src/meetings/meetingAsr.ts`：PCM-capable 引擎统一走 `externalPcm`/`extraStreams`，`meterless` 仅对云端生效。
- **WASAPI 失败不再静默降级**：当会议要求「麦克风 + 系统声」但没有可用的系统回环轨道（或连续三帧静音），录制区顶部弹出**醒目红条告警**（`⚠ 没有收录到系统内部声音，当前只在录麦克风…`），明确提示对方的声音不会进这场，请检查系统「立体声混音」/ 音频回环后重开。改在 `web/src/meetings/MeetingPage.tsx` + `web/src/styles.css`。
- **平台事实（诚实说明）**：`云端` 走浏览器 Web Speech，引擎在内部自采麦克风，**无法注入系统声**——这是平台限制。但录制的 WAV 本就混了麦克风 + 系统声，**停止后的本机 sherpa 补转写会对整段混音重新转写**，因此系统声始终会进入最终逐字稿与纪要；云端只是「实时字幕」看不到对方。

## 月伴：全局单例长青对话

- **每次进入都指向同一条对话**：新增 `web/src/session/companion/companionSession.ts` 的 `ensureCompanionSession`——进月伴时先按本地记忆的会话 id 复用，id 丢失则按「月伴对话 / Companion talk」标题回找，都没有才新建并置顶。不再每次进入 `session.create` 新建一条。
- **标题稳定、退出不删**：月伴单例保持「月伴对话」标题，**不再被首句自动重命名**（`isRenameableChatTitle` 收窄为仅占位标题）；退出时不再丢弃这条单例（`SessionPage` 退出判定对月伴标题短路）。你从安装到卸载的全部语音对话都留在这一条里。
- **侧栏可切换查看**：单例置顶在左侧「对话」列表，随时点开即以文本模式查看全部历史；全局只有一条，不再散落一堆「月伴对话」。

## 已知延后（有明确技术阻断，未硬塞进本版）

- **月伴「按天 gzip 压缩存档」的落盘删除**：当前 `message_session_state` 存在 `CHECK (last_sequence = message_count)` 且消息序号严格连续（1..N），只允许删除连续尾段（rewind）。删除「旧的、居中的」消息做落盘归档会破坏该核心不变量并导致数据库启动校验失败。要真正落盘按天压缩，需要一次专门的、带迁移的核心 schema 改造（改不变量 + 字节计数 + FTS + token_ledger），不宜在本版内匆忙上线。单例长青对话 + 侧栏查看已满足「一条对话、可切换查看、历史留存」的核心诉求。
- **会议多渲染设备 loopback 聚合、说话人分离、长会边录边增量补转写**：均为较大的原生 / 模型工程，作为后续增强。

## 验证

- Go：`go build ./...` 干净；全量 `go test ./...` **0 失败**（本版无 Go 改动，树与 0.4.55 一致）
- 前端：`tsc --noEmit` 通过；`vitest run` **172 files / 1322 tests** 全绿；`generate-bridge --check` 无契约漂移
- 新增单测：`companionSession.test.ts`（单例复用 / 标题回找 / 置顶 / 新建 / 空 update 回退）、`sessionTitle.test.ts`（月伴标题不重命名）、`meetingAsr.test.ts`（火山/本机混 loopback、云端仅麦克风）
- 复核修复：全量回归抓到 `ensureCompanionSession` 在 `session.update`（置顶）解析为空时会把会话置空导致进入月伴崩溃，已加 `?? 原会话` 回退并补测

## 安装包

- `release/out/Lunitide-Setup-0.4.56-x64.exe`（真签名，Authenticode 已验、时间戳）
- `release/out/SHA256SUMS.txt`
- 从 0.4.55 升级
