# 原始需求逐项覆盖、竞品借鉴与证据边界

日期：2026-09-15。项目代码审计基线 HEAD `b1d58b0d8e4867bd5c5efd7048ce82b5daf07c09`（有用户未提交修改）；白龙马证据冻结在官方 GitHub 提交 `b20f4ad5d16425be0520745d69048af11439d617`。源码能力不等于发行包实测效果，未运行两产品同场对比，不给虚构性能胜负。

## 1. 你的需求没有被缩成“加一个数据库”

| 原始问题/要求 | 本期决策 | 文档/执行落点 |
|---|---|---|
| 和白龙马全面比较、借鉴优点 | 区分功能证据/产品体验/实测，不盲复制架构 | 本文第2–5节、01报告 |
| 记忆提升一个台阶、参考先进系统 | 原生Go/SQLite演进；参考分层、时间、混合召回、版本整理 | 02第6节、03、06-C1/C2 |
| 不要每次问是否记忆 | 自动明确稳定事实；问候天气不存；冲突静默审阅 | 03 Task3/8；07 M-R11 |
| Claude是否先进、专家材料是否可借鉴 | 可借鉴机制，不复制托管实现，不采用未经对照的“最强/6倍” | 本文第3节 |
| TTS/ASR是什么、谁更流畅 | 源码配置核实；保留现有模型，本期补音频焦点与回归测量 | 本文第4节、06-C5、07第6节 |
| Windows.Media.Ocr vs HunyuanOCR-1.5，是否替换 | 不因名称替换；后续明确选择Paddle，保留Windows快速/兜底 | 02第7节、04 |
| PaddleOCR-VL-1.6自选安装 | 完整layout+VLM受管离线包；真正Windows门通过才可安装 | 04 Task0、06-C3/C4 |
| OCR更准确 | 200页分层盲测；手动识别与自动路由分门 | 07第4节 |
| 能不能内置放歌/放电影 | 内置用户授权本地/生成资产播放器；不附带第三方曲库 | 05、06-C5 |
| 自动工具操作能力借鉴 | 借鉴可见阶段/证据/恢复；原审批/锁/急停保留 | 05 Task0/2/7 |
| UI设计漂亮、工作效率高；融合现有记忆/OCR配置 | V2保留月汐纯黑+极光品牌；设置“智能能力”恰好两卡，记忆三态/双scope收进抽屉，OCR为只读自动状态+可选增强，高级配置按需展开；媒体独立中心+条件式MiniPlayer | 02 §6.8/7.7、06-C6、07第7节 |
| 省Token | 注入预算+累计后台预算+全链成本测量 | 06-C2、07第6节 |
| 多模型融合、办公、写代码、自生成 | 本期保持原能力并回归；不在记忆改造里重写模型调度/办公/代码Agent | 本文第2节、07端到端30例 |
| 完整可执行PRD，不假设/夸大 | 明确现码/新增合同/待实证；阻断风险、文件、任务、测试与发布门 | 00–07 |
| 文档存指定目录 | 本目录唯一现行开发包；旧superpowers版本为历史 | 00 README |

“不假设”不能意味着不允许设计新接口；正确做法是将新接口明确标成待实现合同，并为其定义失败行为和验收，不把它说成现码已有。

## 2. 最初15个比较维度

| 维度 | 可确认/不能确认 | 本次升级收益与边界 |
|---|---|---|
| 处理问题速度 | 无相同模型/机器/任务计时，不能判谁快 | 自动记忆减少用户操作；前台150ms召回预算、后台可降级；不保证总回答更快 |
| 记忆 | 两者都有SQLite记忆；Lunitide当前双平面、来源/召回有明确缺口 | 补canonical、source、scope、时间版本、自动筛选，不以star排名 |
| UI界面设计 | 白龙马源码有Brain UI与媒体卡片；本地现有纯黑、极光与工作台品牌语言可复用 | 借鉴状态与连续交互；V2减少常驻配置和卡片，不照搬密集页面或声明审美必胜 |
| 操作电脑能力 | Lunitide已有ToolRuntime/SMTC/审批；能力存在不等于任务成功率 | 修假成功、验证目标、失败恢复；不放松权限 |
| 视频 | 生成、播放器、第三方内容库是三种不同能力 | 本期用户资产本地播放，不新增视频生成模型或影视内容服务 |
| 音乐播放 | 播放器源码开源不等于歌曲授权 | 独立媒体中心承载完整体验，跨页按需显示MiniPlayer；本地队列/进度连续，云端搜索保留external路径 |
| 办公 | 已有Office/文档识别调用路径 | 复杂OCR+正确缓存+项目约束记忆可助工作流；最终文档仍需校验 |
| 写代码 | 没有同任务成功率/测试通过率对照 | 项目约束、决策、续接记忆有帮助；不承诺模型编码能力跃升 |
| 省Token | 少注入可能多花后台提取/embedding | 同口径总量账本与预算，质量不降才宣称节省 |
| 融合模型 | 白龙马兼容多provider，本地也有路由；“支持多个”不等于最优融合 | 复用本地现有路由；memory不创造新的主调度器 |
| 自生成能力 | 生成内容/生成工具/自改代码/权重学习不能混称 | 可复用procedure仍经证据和治理；不自动生成并执行未经审批代码，不训练权重 |
| 功能全面性 | 列表数量不代表每项可用 | 本次只交三子系统+集成，不宣称补全全部竞品功能 |
| 系统稳定性 | 无实机长稳对照，不能按技术栈断定 | 持久job/fencing/恢复/资源上限与失败隔离，仍需故障测试 |
| 工作效率性 | 自动化程度不等于正确产出 | 用人工纠正次数、任务成功率、总耗时测，不只看动效 |
| 产品美观性 | 源码阅读不能替代真实UI评审 | 纯黑主背景、首页/媒体限定极光、大留白和按需披露；以多视口截图、焦点/缩放和5人测试验收，不自封满分审美 |

白龙马README提供持续循环、记忆、工具、媒体和Brain UI功能线索；这些是上游描述，非本轮基准结论。[官方仓库](https://github.com/xiaoyuanda666-ship-it/BaiLongma/tree/b20f4ad5d16425be0520745d69048af11439d617)

## 3. 记忆参考方案与专家材料校正

继续选择原生演进：现有SQLite/版本/上下文预算/压缩/审计可用，接第三方记忆服务会新增部署与删除边界。不是说外部框架能力差，而是本项目不能同时维护第三个记忆真相源。

| 参考 | 采用 | 不采用/不能据此声称 |
|---|---|---|
| Letta/MemGPT | 小核心+外部按需召回 | 不让模型绕过policy任意写核心；不靠框架名称保证正确 |
| Hindsight | 事实/经历/观察区分、多路检索/来源 | 不复用厂商benchmark数字为本产品效果 |
| Graphiti | 事实有效时间与记录时间、历史版本 | 首发不加图数据库，关系与索引先SQLite |
| Claude Memory/Dreams | 独立输出版本、检查后激活/丢弃 | 是托管平台功能，非可直接嵌入的开源库；不依赖research preview |
| Mem0 | 提取/去重/检索思路 | hosted与OSS版本不能混讲；不把“5行接入”当完整隐私/恢复实现 |

依据：[Letta](https://docs.letta.com/v1-sdk/concepts/stateful-agents)、[Hindsight](https://github.com/vectorize-io/hindsight)、[Graphiti](https://github.com/getzep/graphiti)、[Claude Dreams](https://platform.claude.com/docs/en/managed-agents/dreams)、[Mem0](https://github.com/mem0ai/mem0)。

专家材料中的“6倍任务完成率”“最成熟/最强”“stars/融资”等不可用作本项目效果依据。其Mem0图实现描述也需区分版本与服务：当前平台文档描述内建实体关联图，历史外部图集成另有变化，不能笼统断言所有Mem0版本都不用Neo4j。[Mem0官方Graph Memory](https://docs.mem0.ai/platform/features/graph-memory)

“OpenAI Memory (Dreaming V3)”在用户材料里没有可靠官方证据支撑，本PRD不采用该名称/结论。自动补日期/关系不能以“梦中整理”名义制造来源没有的事实。借鉴的重点是可验证的工程机制，不是营销名词。

## 4. TTS、ASR与流畅度

白龙马本轮固定源码：

- TTS是内置服务商适配代码、需用户配置云端凭据，不是把这些云模型权重内置。默认provider为doubao；列表含doubao/minimax/openai/elevenlabs/volcano。该提交调用中有MiniMax speech-2.8-hd、OpenAI tts-1、ElevenLabs eleven_flash_v2_5；这是该源码默认值，不保证用户安装版本/个人配置相同。[默认配置](https://github.com/xiaoyuanda666-ship-it/BaiLongma/blob/b20f4ad5d16425be0520745d69048af11439d617/src/voice/tts-defaults.js)、[适配实现](https://github.com/xiaoyuanda666-ship-it/BaiLongma/blob/b20f4ad5d16425be0520745d69048af11439d617/src/voice/tts-providers.js)
- ASR有云适配，阿里路径paraformer-realtime-v2，另有腾讯/讯飞/火山连接；火山使用双向WebSocket。另有本地Whisper启动manager，默认small并尝试打包可执行或Python路径；源码含启动器不证明当前发行包已带全部权重。[云ASR](https://github.com/xiaoyuanda666-ship-it/BaiLongma/blob/b20f4ad5d16425be0520745d69048af11439d617/src/voice/cloud-asr.js)、[本地启动器](https://github.com/xiaoyuanda666-ship-it/BaiLongma/blob/b20f4ad5d16425be0520745d69048af11439d617/src/voice/manager.js)

本地Lunitide `internal/tts/router.go`已经路由SAPI/OneCore、Edge、Volc、reference/GPT-SoVITS、ONNX；不是只有一种本地声音。Volc/Edge支持ChunkStreamer分支，其余可走emitWhole，因此不能把统一SynthesizeStream方法名当所有模型都逐块推理。`internal/voice/sherpa.go`与`volcsauc`已有本地/云识别，`ttsPlayer.ts`已有播放排队与打断，`voiceTiming.ts`有时间记录。

结论：**当前无实测不能判谁更流畅，也没有证据必须替换你的TTS/ASR。** 本期保留语音模型与设置，只补播放音频焦点、资源竞争与回归计时。若以后实测瓶颈在emitWhole或endpointing，再单独立语音优化任务；不能未经评估把1.2秒静音判断缩短导致截断说话。

## 5. OCR与媒体许可

PaddleOCR-VL-1.6的官方pipeline/模型卡存在；完整layout+VLM与单独VLM不能混淆。Windows PaddlePaddle安装文档并不等于本项目完整离线包已验证；以W0锁定ABI、DLL、CPU指令集与包内所有许可证。[官方pipeline](https://github.com/PaddlePaddle/PaddleOCR/blob/main/docs/version3.x/pipeline_usage/PaddleOCR-VL.md)、[模型卡](https://huggingface.co/PaddlePaddle/PaddleOCR-VL-1.6)、[Windows安装文档](https://www.paddlepaddle.org.cn/documentation/docs/install/pip/windows-pip_en.html)

本轮没有从白龙马固定源码目录证明它存在可独立分发的专用OCR模型包；不能因此断言“白龙马完全没有OCR”或猜某个API就是其OCR。你后续已明确选择Paddle选装，研发不依赖这项竞品未知。微信视频链接不能作为本地模型准确率、体积与Windows可运行性的证据。

V2界面不把OCR路由复杂度交给用户：首屏只显示“文字识别 · 自动”的**只读状态**和一个复杂文档增强包动作；它不是总开关，也不能拿现有 `preferProvider` 冒充。Windows/Paddle选择、语言、pipeline、失败回退与版本信息由系统处理并在高级信息中按需展开。当前代码中的 PP-OCR 只是目录 marker 登记、识别链未接线，必须显示 `registered_unwired`，不得映射为 Paddle ready。Paddle未验证时安装入口禁用，不能把UI简化写成能力已经具备。

白龙马仓库标MIT，复用代码需保留许可和版权声明；仓库中的歌曲、图片、字体、模型等资产及第三方依赖必须分别检查，不能把顶层MIT推定为所有内容许可。本期采用交互思想、用现有WebView2 audio/video实现，不直接复制其音乐文件，不附带任何歌曲/电影曲库。[固定版本LICENSE](https://github.com/xiaoyuanda666-ship-it/BaiLongma/blob/b20f4ad5d16425be0520745d69048af11439d617/LICENSE)

产品可以内置播放器并播放用户合法持有的文件；V2用独立媒体中心呈现完整音乐/视频体验，离开后仅在存在活动会话时显示共享同一播放快照的MiniPlayer。pause保留会话，close结束并释放，而不是只隐藏界面。这不授权抓取、分发或绕过第三方平台保护。若未来接内容服务，另核服务协议/API授权与内容权利，本PRD不替代该合规审查。
