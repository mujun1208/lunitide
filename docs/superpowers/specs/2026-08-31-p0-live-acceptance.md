# P0 真机验收清单（主尺 4.8）

日期：2026-08-31  
只在 **Windows 本机、已登录的月汐** 勾。单测绿 ≠ 勾。  
合同定义：`2026-08-31-engine-gateway-48.md` §6。对标整改：`2026-08-31-peer-alignment-and-remediation.md`。

| # | 合同 | 操作 | 通过（日期 / 操作人） |
|---|---|---|---|
| G1 | 关窗留引擎 | 开工作台 → 关窗口 → 任务管理器 | 2026-09-01 mu：官方包 0.4.44（含 W2/W3）。关窗后桌面 39696、引擎 27088 未换，`--rpc-health` `engine=ready` `version=0.4.44`。2026-08-31 曾在 0.4.43 勾过 PID 29236 |
| G2 | 关窗 cron | 建一条短期自动化 → 关工作台 → 等触发 | 2026-08-31 mu：关窗后 cron 跑出 run（`p0-g2-live` / trigger=cron / PID 29236 未换）。当时 chat.start `DeadlineMS=600000` 被拒，已在现码改成 `ChatStartDeadlineMS` |
| G3 | 再开工作台 | 再开窗口 | 2026-09-01 mu：官方 0.4.44 再开仍是引擎 27088 / 桌面 39696，health ready。2026-08-31 曾在 0.4.43 勾过 PID 29236 |
| D6a | 记事本 | `LUNITIDE_CC_ACCURACY=1 go test ./internal/ccapp/accuracy -count=1` | 2026-08-31 mu：`TestLiveNotepadTypesVisibleText` PASS |
| D6b | 计算器 | 同上 | 2026-08-31 mu：整包先绿；`-v` 复跑一次 15s 未等到窗口，单测重跑 PASS |
| D6c | 资源管理器 | 同上（`TestLiveExplorerNamedObserve`） | 2026-08-31 mu：PASS |
| D9 | 月伴电脑控制 | 新画像、开关关 | 2026-09-01 mu：官方 0.4.45 关电脑控制后开月伴，横幅「电脑控制未启用。第一次控桌面请到设置里打开，月伴不会自己打开。」**横幅过**。打开文档未执行（预期）。打断后灯闪 / 「上一句还在发送」是残差，改在 0.4.46 源码，未装新包前不能勾「打断后灯稳」。2026-08-31 曾在 0.4.43+next 旁路勾过 M10-CC-012 |
| D11 | 杀引擎 | 只杀引擎进程，不要杀托盘 | 2026-09-01 mu：官方 0.4.44。只杀引擎 27088，数秒内新桌面 51688 拉起引擎 50708，`--rpc-health` ready。2026-08-31 旁路 `Lunitide-next` 也曾勾过 |
| D12 | UAC | 点 UAC 确认 | |
| I1 | 入站 | 飞书或钉钉白名单，关窗仍写入 | 未勾：飞书/企微/钉钉 inboundEnabled 均为 false |
| I4 | 回程 | 自动跑完后原 IM 会话看得到回复（先验收私聊） | 未勾：通道全关 |
| I3 | 微信入站 | 设置 | 2026-08-31 mu：`im.channels.get` 微信 inboundEnabled=false；Normalize 强制关掉；设置文案「不能收消息」 |
| B1 | 浏览器未就绪 | 卸 Playwright MCP 后 `browser.act` click | 错误，不是空成功： |
| R0 | 包装 | 干净树 + 批准身份签名 | 陌生机安装能连引擎：本机 2026-09-01 已从 0.4.44 升到 0.4.45（`DisplayVersion` / `--rpc-health` version=0.4.45 engine=ready）。**不是**干净 Win10/Win11 双机 R0 |

---

## 语音三卡（须 0.4.46，旧 W1-VOICE / 仅 0.4.45 作废）

| # | 合同 | 操作 | 通过（日期 / 操作人） |
|---|---|---|---|
| V-TURN | 一轮不连发 | 月伴说一句完整中文；退回打字等 10s | |
| V-VOLC-ASR | 火山出字 | 灯为火山听；说「打开记事本」 | |
| V-VOLC-TTS | 火山有声 | 等助手答完 | |
| V-VOLC-KEY | 密钥坏停在火山 | 可选 | |
| V-LOC-HOST | 本地诚实失败 | 保持坏 venv；设置页须见 dict/jieba；试听可点 | |
| V-LOC-OK | 补 dict 后出声 | 环境未复则记未复 | |
| V-CLOUD | 云端一轮 | 选云端说一句，晓晓出声 | |
| V-LIGHT | 三卡灯 | 听/说/想与所选卡一致 | |

发布检查命令（本机）：

```text
set LUNITIDE_CC_ACCURACY=1
go test ./internal/ccapp/accuracy -count=1
go test ./internal/app ./internal/imapp ./cmd/engine ./cmd/desktop -count=1
```

I 不过仍可报主尺 4.8，对照 OpenClaw 停在 4.6。R0 不过不得称正式发布。

---

## 收尾（本机一次走完）

代码路径已齐。下面只能在 **已登录的月汐** 勾，单测绿不能代勾。

1. **D12** 诱发系统提权（装驱动/改系统设置一类），确认出现 `user.ask` /「我不能代点是」，自己点是或取消。
2. **B1** 设置 → MCP 卸掉 Playwright 后，让月伴或对话 `browser.act` click：必须报 `BROWSER_MCP_NOT_READY`，不能空成功。验完可再装回。
3. **跨面 e2e** 文字说「以后回答默认用中文」→ 会话/月伴/同事条点「确认沉淀」→ 首页开月伴说「继续刚才的」→ 同事 @专家「继续刚才的」。三面都要接得上刚才的偏好。
4. **做完抽检（可选）** 一条真机桌面任务（记事本写入可见字），中途截图/点一下不能说「完成了」。
5. **R0** 干净树 + 批准身份签名包装。不过不得称正式发布。

13 个独立 Agent 不是本波收尾，要产品立项。加权 4.8 拆分上限约 4.77，真机勾完也不能把算术乘到 4.80。
