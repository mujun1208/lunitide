# 六项表面可落地 PRD（办公栏 / 会议火山 / 月伴模型 / 月伴视觉 / 电脑控制 / 自动装备）

日期：2026-09-03  
范围：侧栏信息架构、会议听写、月伴模型一致性、月伴说话视觉、电脑控制对标、对话自动装备。

## 当前方案评分

| 项 | 现状 | 分 /5 | 卡点 |
|---|---|---|---|
| 1 办公折叠 | 自动化 / 同事聊天 / 会议记录平铺在「新对话」下 | 2.8 | 三项占主栏，对话/项目被挤 |
| 2 会议·火山 | 路径已接 seed-asr + 混音 PCM，但 8s 无字就切 sherpa；externalPcm 无帧则全聋 | 3.1 | 「开始了但不出字」被当成安静房间 |
| 3 月伴模型 | 首页会传 provider/model，但「想」灯走 `pickDefaultLLM`；同供应商还会被 `pickCompanionFlashModel` 偷换成 flash/GLM | 2.6 | 选了 DeepSeek 仍显示/调用 GLM5.3 |
| 4 说话视觉 | speaking 切成横向 wave，比圆形小且不像居中 | 2.4 | 用户看到的就是偏、小 |
| 5 电脑控制 | `computer.act` 已是 OpenClaw 形（截图/键鼠/窗口/剪贴板/观察），另有 `desktop.*` / `browser.act` / `command.run` / `workspace.edit` | 3.6 | 参考图能力表方向正确；缺系统音量/亮度等；准确度靠 see→act→verify，不是再装一套 OpenClaw |
| 6 自动装备 | 只有点名/@专家才组技能+MCP；月伴直接 `turnEquipmentFor` 空返回，且不跑 `expertComposeForTurn` | 2.9 | 不会按「做个PPT」自动批专家/技能 |

**综合：3.0 / 5。** 整改后目标 **4.9 / 5**（剩 0.1：系统设置类桌面动作与第三方 MCP 热装仍是增量，不在本轮假装一次做完）。

## 参考图是否正确（第 5 点）

方向正确，可以按「工具循环」落地，**不要**再嵌一套 OpenClaw 进程。

| 参考图能力 | 产品已有 | 本轮 |
|---|---|---|
| 屏幕感知 | `computer.act screenshot` + 视觉回传 `frameId` | 保持 |
| 鼠标/键盘 | click/drag/type/key/press/hold_key | 保持 |
| 应用/窗口 | `desktop.open` + `window_action` / focus / list | 保持 |
| 文件 | `workspace.*`（`str_replace_editor` ≡ `workspace.edit`） | 保持 |
| 网页 | `browser.act` / Playwright MCP | 保持 |
| 剪贴板 | `computer.act clipboard/paste` | 保持 |
| 终端 | `command.run`（≡ bash，带白名单/审批） | 保持 |
| 系统音量/亮度/网络 | **无独立动作** | 记为 D5-2，本轮不新开 cc 子系统 |
| Action loop | 工作流已写 see→act→verify + stale frame 失败闭环 | 月伴任务句打开工具 |

集成方式：继续把动作收口到已治理的 `computer.act`（审计、急停、坐标绑定），禁止模型直接调 `cc.*`。

## 第 6 点：对话自动批

其他产品的「自动批」= 按本轮意图选出专家 + 技能 + MCP，并立刻 `skill.invoke` / 用已连接 MCP，而不是等人点「选专家」。

本轮落地：

- 意图匹配：除点名外，用场景/名称/技能关键词给对话专家打分，命中则装备。
- 月伴：任务句不再空装备；问候句仍保持瘦身。
- 注入：`[专家装备]` + 技能目录置顶 preferred；未连接的 preferred MCP 只提示去 MCP 页打开，不偷偷装。

不自动持久挂载专家卡（避免对话列表被静默改身份）。本轮装备，下一轮无意图则卸下。

## 本轮必做

1. 侧栏「办公」折叠三项，默认开；进入其中一页自动展开。
2. 火山会议：无 PCM 1.6s 自救开麦；无字幕窗口 8s→20s；连接中给提示。
3. 月伴「想」灯和 `chat.start` 用首页/会话已选模型；有合法 current 不再改 flash。
4. 说话态保持圆形 glass，放大并居中，不再切成偏的小 wave。
5. 文档对齐 OpenClaw 映射；任务句打开电脑工具。
6. 意图自动装备专家/技能/MCP。
