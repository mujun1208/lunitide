# P0 真机验收清单（主尺 4.8）

日期：2026-08-31  
只在 **Windows 本机、已登录的月汐** 勾。单测绿 ≠ 勾。  
合同定义：`2026-08-31-engine-gateway-48.md` §6。对标整改：`2026-08-31-peer-alignment-and-remediation.md`。

| # | 合同 | 操作 | 通过（日期 / 操作人） |
|---|---|---|---|
| G1 | 关窗留引擎 | 开工作台 → 关窗口 → 任务管理器 | 2026-08-31 mu：安装包 0.4.43；关窗后引擎 PID 29236 未换，`--rpc-health` ready |
| G2 | 关窗 cron | 建一条短期自动化 → 关工作台 → 等触发 | 2026-08-31 mu：关窗后 cron 跑出 run（`p0-g2-live` / trigger=cron / PID 29236 未换）。当时 chat.start `DeadlineMS=600000` 被拒，已在现码改成 `ChatStartDeadlineMS` |
| G3 | 再开工作台 | 再开窗口 | 2026-08-31 mu：同一引擎 29236，窗口再可见，health ready |
| D6a | 记事本 | `LUNITIDE_CC_ACCURACY=1 go test ./internal/ccapp/accuracy -count=1` | 2026-08-31 mu：`TestLiveNotepadTypesVisibleText` PASS |
| D6b | 计算器 | 同上 | 2026-08-31 mu：整包先绿；`-v` 复跑一次 15s 未等到窗口，单测重跑 PASS |
| D6c | 资源管理器 | 同上（`TestLiveExplorerNamedObserve`） | 2026-08-31 mu：PASS |
| D9 | 月伴电脑控制 | 新画像、开关关 | 2026-08-31 mu：旁路桌面 `Lunitide-next.exe` + 现码引擎（安装包 0.4.43 引擎因库已有 0108/0109 拒启）。`cc.updateConfig enabled=false` 后点首页「月伴对话」：舞台横幅「电脑控制未启用。第一次控桌面请到设置里打开，月伴不会自己打开。」；关开关时 `computer.act` 拒 `ccapp: computer control disabled` / M10-CC-012。验收后 `UpdateConfig enabled=true` 已恢复，blocklist/档位未改 |
| D11 | 杀引擎 | 只杀引擎进程，不要杀托盘 | 2026-08-31 mu：安装包 0.4.43 复现空场；旁路 `Lunitide-next.exe`（含 `--takeover`）杀引擎 PID 21116 后 2s 内新桌面 `40348 --takeover` 拉起引擎 35784，`--rpc-health` ready。host 日志：`relaunching desktop with --takeover` |
| D12 | UAC | 点 UAC 确认 | |
| I1 | 入站 | 飞书或钉钉白名单，关窗仍写入 | 未勾：飞书/企微/钉钉 inboundEnabled 均为 false |
| I4 | 回程 | 自动跑完后原 IM 会话看得到回复（先验收私聊） | 未勾：通道全关 |
| I3 | 微信入站 | 设置 | 2026-08-31 mu：`im.channels.get` 微信 inboundEnabled=false；Normalize 强制关掉；设置文案「不能收消息」 |
| B1 | 浏览器未就绪 | 卸 Playwright MCP 后 `browser.act` click | 错误，不是空成功： |
| R0 | 包装 | 干净树 + 批准身份签名 | 陌生机安装能连引擎： |

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
