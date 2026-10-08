# Lunitide 移动伴侣 · 可落地设计 PRD

- 日期：2026-10-07
- 状态：可实施（本文冻结契约；M0–M4 分里程碑交付）
- 参考：TRAE 移动端配对页（扫码 + 授权 180 天 + 防休眠）
- 红线：不引入云账号体系；不把任何密钥/凭证下发到手机；不动现有桌面 UI 布局与交互

---

## 0. 结论先行：能不能实现？

**能，且比想象中便宜。** 决定性因素是现有架构：

> 引擎（lunitide-engine.exe）已经集中拥有**全部能力和全部数据**——chat.start 的流式对话、MCP6 注册表、技能/专家/插件、产品中枢、DPAPI 密钥、会话与消息存储、工作区文件产物。桌面 WebView2 只是一个"本地渲染窗口"，通过 bridge 协议（JSON 帧：request/response/event，MaxFrameSize 4MB，DeadlineMS 上限体系）与引擎通信。

因此手机端**不需要移植任何能力**，只需要：

1. 引擎侧新增一个 **远程网关**（HTTPS + WebSocket 监听），把现有 bridge 帧协议原样承载在 WSS 上；
2. 前端 web/ React 应用（现有聊天 UI、BridgeClient 抽象）增加 **WebSocket 传输通道**，打包成 PWA 从引擎直接下载；
3. 桌面端新增「移动伴侣」设置页：开关、二维码配对、设备管理、防休眠。

同步问题随之消失：**引擎存储是唯一真源**。手机上发起的对话、调用的工具、生成的产物全部落在引擎侧的既有存储里；电脑打开同一会话即见同一内容——"手机和电脑同步"不是功能，是架构结果。

---

## 1. 目标与非目标

### 目标

1. 手机扫码配对电脑端产品，授权后通过 **蜂窝流量/任意网络** 远程使用
2. 手机上打字对话（流式输出，与桌面同体验）
3. 手机可用桌面产品的**全部工具面**：MCP、技能、插件、专家
4. 手机产出的产物（文档/报告/文件）落盘在工作区，电脑端可见
5. 电脑端可管理：配对设备列表、吊销、有效期、防休眠开关

### 非目标（本期不做）

- 不做云账号、不做官方中继服务器（零持续运营成本）
- 不做原生 iOS/Android 双端开发（PWA 先行，见 §9 包装策略）
- 不做手机端离线自治（引擎离线时手机只提示"电脑不在线"，不本地跑模型）
- 不做手机反向控制电脑屏幕（是"用手机操作引擎"，不是远程桌面）
- 不改现有桌面 UI 布局与交互习惯

---

## 2. 总体架构

```
┌────────────────────────── 同一进程：lunitide-engine.exe ──────────────────────────┐
│                                                                                    │
│  [现有] 命名管道会话 ←──── hostbridge ←──── cmd/desktop (WebView2 本地渲染窗口)      │
│                                       │                                            │
│  [新增] remotegateway 包：                                                            │
│    ├─ HTTPS :47651 (自签证书, DPAPI 封存私钥)                                        │
│    │    ├─ GET /              → 提供 web/ 构建产物(PWA)                              │
│    │    ├─ GET /pair          → 配对落地页                                           │
│    │    ├─ POST /api/pair     → 一次性配对码 → 设备令牌                              │
│    │    └─ WS  /bridge        → 复用同一 bridge 帧协议 + 同一 handler 派发           │
│    └─ 设备注册表 / 审计日志（sqlite 既有存储）                                        │
│                                                                                    │
└────────────────────────────────────────────────────────────────────────────────────┘
          ▲ WSS(证书指纹锁定)                ▲ WSS
          │                                  │
   ┌──────┴──────┐                    ┌──────┴──────┐
   │ 手机 PWA     │                    │ 平板/其他设备 │（后续，协议相同）
   │ (蜂窝/IPv6)  │
   └─────────────┘
```

关键决策与理由：

| 决策 | 理由 |
|---|---|
| 网关放在**引擎**进程而非桌面 GUI 进程 | 引擎常驻后台（G1：桌面退出引擎存活），手机随时可达；引擎已持有 handler 注册表、会话认证、DPAPI、slot 并发闸门——全部直接复用 |
| 帧协议**原样复用**，不造新协议 | 4MB 帧上限、DeadlineMS 上限（deadline.go）、流式 event 序列、错误码体系全部继承；手机端与桌面端行为一致性由同一份 `web/src/bridge` 客户端代码保证 |
| 手机端是 **PWA**，从引擎直接下载 | 单人开发者维护一份前端；无应用商店审核与年费；iOS/Android 均可"添加到主屏幕" |
| 自签证书 + 二维码携带指纹 | 无需 CA/域名/续期；二维码同时完成"地址发现 + 证书锁定"，公网流量下防中间人 |

---

## 3. 配对流程（对照 TRAE 的扫码授权）

```
电脑                                手机
────                                ────
「移动伴侣」页开启远程访问
 ├─ 引擎首次生成自签证书（私钥 DPAPI 封存）
 ├─ 生成一次性配对码（8 位，5 分钟有效，单次）
 └─ 展示二维码:
    https://<ip-or-ipv6>:47651/pair#<code>&fp=<cert-sha256-[:16]>
                                    扫码 → 打开配对页 → 浏览器 TLS 握手
                                    校验证书指纹 == fp（不符即中止并警示）
                                    POST /api/pair {code, deviceName, platform}
 ├─ 引擎校验码（TTL/单次/限速）
 ├─ 登记 paired_devices 行（见 §6）
 └─ 返回 {deviceToken, product, version, expiresAt}
                                    令牌存入 PWA localStorage
                                    后续 WS 升级请求 Authorization: Bearer <token>
```

- **有效期 180 天**（与 TRAE 一致），到期前 14 天手机端提示重新配对
- 配对码错误 5 次 → 该来源 IP 锁 15 分钟
- 重新配对不撤销旧令牌；撤销只走桌面端设备管理页

---

## 4. 协议契约

### 4.1 HTTPS 端点

| 方法/路径 | 鉴权 | 请求 | 响应 |
|---|---|---|---|
| `GET /` | 无（静态资源） | - | web/ 构建产物 + manifest + service worker |
| `GET /pair` | 无 | - | 配对落地页（读 URL fragment 中的 code/fp） |
| `GET /api/info` | 无 | - | `{product:"Lunitide", version}` 供配对前展示 |
| `POST /api/pair` | 无（靠一次性码） | `{code, deviceName, platform}` | `{deviceToken, expiresAt, product, version}` |
| `WS /bridge` | Bearer deviceToken | bridge 帧 | bridge 帧 |

### 4.2 WebSocket 承载 bridge 协议

- **文本帧**承载现有 JSON 帧格式，逐字段不变：

```jsonc
// 请求（手机 → 引擎），与桌面侧完全一致
{"version":1,"kind":"request","id":"01J8...","traceId":"01J8...","method":"chat.start",
 "sentAt":"2026-10-07T14:00:00Z","payload":{...},"deadlineMs":180000}

// 流式事件（引擎 → 手机），Sequence 单调递增，按 StreamID 关联
{"version":1,"kind":"event","id":"01J8...","streamId":"01J8...","sequence":1,
 "type":"chat.delta","payload":{...}}
```

- 帧上限 4MB（复用 `ipc.MaxFrameSize`）；超限即断连并记审计
- **并发闸门复用**：远程请求与本地请求共享同一 slot 池（DefaultGeneralSlots/ControlSlots），手机不会挤爆电脑端
- 每连接限速：120 req/min 滑动窗口，超出返回 `REMOTE_RATE_LIMITED`（retryable）。初版 30 req/min 被手机实测证伪——一次冷启动的并发预取（providers/sessions/experts 等）就 15-25 个请求，进会话发一轮对话即触顶；120/min 维持「持续 >2 req/s 才拦截」的防滥用语义（2026-10-08 修订）

### 4.3 新增 bridge 方法（engine 侧）

| 方法 | 权限 | 语义 |
|---|---|---|
| `remote.access.status` | 本地/远程 | 远程网关是否开启、监听地址、证书指纹 |
| `remote.access.enable` / `disable` | 仅本地 | 开关远程访问（disable 即断开全部远程连接） |
| `remote.devices.list` | 仅本地 | 配对设备列表（名称/平台/授权范围/到期/最近在线/IP） |
| `remote.devices.revoke` | 仅本地 | 吊销指定设备令牌 |
| `remote.sessions.list` | 仅本地 | 当前在线远程会话与审计摘要 |
| `power.keepAwake.set` | 本地/远程 | 防休眠开关（引擎进程 `SetThreadExecutionState(ES_CONTINUOUS\|ES_SYSTEM_REQUIRED)`，见 §8） |

其余全部现有方法（`chat.*`、`internal.mcp6.*`、技能/专家/插件/产品中枢/工作区文件）**对远程会话默认放行**，受 §5 权限范围约束。

---

## 5. 权限模型（对标截图中的 TRAE 授权语义）

### 5.1 主开关

「允许移动端控制本机」——关闭时网关停止接受新连接，既有连接 30 秒内优雅断开。

### 5.2 设备级 scopes

| scope | 覆盖 | 默认 |
|---|---|---|
| `chat` | 对话（含流式、附件上传） | ✅ |
| `tools` | MCP/技能/插件/专家调用 | ✅ |
| `files` | 工作区文件读写（受既有 workspace 根白名单约束） | ✅ |
| `hub` | 产品总览只读 | ✅ |
| `settings` | 读写系统设置 | ❌（需单独勾选） |

超出 scope 的方法返回 `REMOTE_SCOPE_DENIED`（不可重试），手机 UI 相应入口置灰。

---

## 6. 数据模型（sqlite，挂在既有 storage 上）

```sql
CREATE TABLE paired_devices (
  device_id     TEXT PRIMARY KEY,      -- ULID
  name          TEXT NOT NULL,         -- "穆军的 iPhone"
  platform      TEXT NOT NULL,         -- ios-pwa / android-pwa
  token_hash    TEXT NOT NULL,         -- deviceToken 的 SHA-256（明文不落盘）
  scopes        TEXT NOT NULL,         -- JSON 数组
  granted_at    INTEGER NOT NULL,      -- unix 秒
  expires_at    INTEGER NOT NULL,      -- granted_at + 180d
  last_seen_at  INTEGER,
  last_ip       TEXT,
  revoked_at    INTEGER                -- 非空即吊销
);

CREATE TABLE pairing_codes (
  code_hash     TEXT PRIMARY KEY,      -- 8 位码的 SHA-256
  created_at    INTEGER NOT NULL,
  expires_at    INTEGER NOT NULL,      -- created_at + 300s
  used_at       INTEGER
);

CREATE TABLE remote_audit (
  id            INTEGER PRIMARY KEY,
  device_id     TEXT NOT NULL,
  at            INTEGER NOT NULL,
  kind          TEXT NOT NULL,         -- pair / auth-fail / method / file-write / revoke
  method        TEXT,                  -- 远程调用的方法名
  detail        TEXT                   -- 摘要（不含 payload 全文，防止审计表膨胀）
);
CREATE INDEX idx_remote_audit_device ON remote_audit(device_id, at);
```

---

## 7. 网络模式（"在外面用流量也能关联"的落地路径）

| 模式 | 何时可用 | 说明 |
|---|---|---|
| **M1 局域网** | 同一 WiFi | 二维码携带 LAN IPv4，零配置。首个里程碑即通 |
| **M3a IPv6 直连**（主路径） | 手机蜂窝 + 家宽均有 IPv6（国内三大运营商现已普及） | 二维码同时携带 AAAA 地址；引擎启用远程访问时自动加 Windows 防火墙入站规则（仅 47651/tcp）。**零成本、零中继、点对点加密** |
| **M3b 端口映射 + DDNS** | 家宽有公网 IPv4 | 引导页说明路由器配置；二维码携带 DDNS 域名 |
| **M3c 覆盖网络** | 上述都不可达（如公司 NAT） | 指引手机与电脑各自安装 Tailscale/ZeroTier（免费档），手机访问引擎的虚拟 IP——网关代码零改动 |
| **M4 自托管中继**（可选） | 用户有 VPS | 预留 `remote.relay.url` 设置项，SOCKS/反向代理转发到引擎；本期只冻结设置项不实现 |

断线重连：指数退避（1s→2s→…→60s 封顶）；PWA 端 outbound 消息在断线时入 IndexedDB 队列，重连后按序补发。

---

## 8. 关键实现点（引擎/前端改动清单）

| 位置 | 改动 | 备注 |
|---|---|---|
| `internal/remotegateway/`（新包） | HTTPS+WSS 监听、路由、设备鉴权中间件、限速、审计 | 不碰 handler 本体 |
| `internal/ipc/` 或新 `transportws` | 帧编解码在 WS 上的适配（复用 decodeStrict 语义） | 文本帧 JSON |
| 引擎 composition root | 注册 remote.* 方法；启用时挂载网关、注入静态资源目录（与 WebView2 rendererDir 同源同产物） | `bootstrap.WireEngine` 加一个可选依赖 |
| 证书管理 | 首次启用生成 RSA-2048/ECC 自签证书（CN=Lunitide Remote，10 年），私钥经 DPAPIService 封存 | 指纹入二维码 |
| `web/src/bridge/client.ts` | 抽象传输层：`PipeTransport`（现有）/ `WsTransport`（新增）；`createRemoteBridge(url, token)` | 上层调用方零改动 |
| `web/` PWA 化 | manifest + service worker（应用壳离线缓存）；移动端响应式适配聊天页 | 桌面端行为不变 |
| 桌面「移动伴侣」设置页 | 开关/二维码/设备列表/吊销/防休眠/审计查看 | 新增设置分区，不动现有布局 |
| 防休眠 | 引擎进程 `SetThreadExecutionState`，`power.keepAwake.set` 控制 | syswindows 系统调用，无需 host 参与 |
| 防火墙 | 启用远程访问时 `netsh advfirewall` 添加入站规则（需用户在桌面端确认 UAC 一次性授权） | 关闭时移除规则 |

---

## 9. 手机端形态策略

**PWA 先行**：`GET /` 直接下载与桌面同源的 React 应用（同一份构建产物，多一个移动端入口路由）。iOS Safari / Android Chrome 均"添加到主屏幕"获得全屏体验。

**后续可选原生包装**（M4 之后，非承诺）：Capacitor 壳获得真推送（FCM/APNs）与后台刷新。推送的本地化替代：M4 先做**轮询通知**（PWA 激活时查 `chat.turn.completed`），成本为零。

---

## 10. 里程碑与验收标准

### M0 · 传输打通（可行性尖刺）

- [ ] WsTransport + 网关 echo；另一台设备浏览器以锁定证书访问 `system.health`
- **验收**：浏览器与引擎完成 WSS 握手（指纹校验通过），一去一回收到 `{"ok":true,...}`；错误指纹被拒并留审计记录

### M1 · 局域网配对 + 打字对话

- [ ] 配对全流程（§3）、`chat.start` 流式、会话历史拉取
- **验收**：手机与电脑同 WiFi；扫码 → 打字提问 → 看到逐字流式回复；电脑端打开同一会话看到完整往返记录；拔线重连后消息不丢不重

### M2 · 全工具面 + 产物同步

- [ ] MCP/技能/专家/插件经手机调用；附件上传、产物落盘
- **验收**：手机发起一个专家任务（如生成诊断报告），流式看进度，产物文件出现在电脑工作区且桌面文件区可见；scope=hub 的只读设备尝试写操作收到 `REMOTE_SCOPE_DENIED`

### M3 · 广域网可达

- [ ] IPv6 直连 + 防火墙规则 + 断线重连 + 离线队列
- **验收**：手机关 WiFi 走蜂窝流量完成 M1 全部验收项；电脑休眠被防休眠开关阻止；连续 24 小时挂机后手机仍可唤醒使用

### M4 · 管理与安全闭环

- [ ] 设备列表/吊销/审计查看/配对码限速锁定/轮询通知
- **验收**：桌面吊销设备后手机 30 秒内被断开且令牌作废；审计表能查到该设备全部敏感操作；错误配对码连试 5 次触发 15 分钟锁定

---

## 11. 风险与对策

| 风险 | 影响 | 对策 |
|---|---|---|
| 运营商/路由器禁入站（无 IPv6 也无公网 IPv4） | M3 直连不可达 | M3c 覆盖网络指引兜底；M4 自托管中继为最终兜底 |
| iOS PWA 后台限制 | 推送不及时 | 轮询通知先行；真推送留给 Capacitor 阶段 |
| 暴露端口的攻击面 | 安全 | 自签指纹锁定 + 令牌哈希落盘 + 限速 + scope + 审计 + 主开关一键断（五层叠加） |
| 4MB 帧上限 vs 大产物 | 传输失败 | 产物一律落盘后传 `workspace.file.read` 分块句柄（M2 验收项） |
| 手机与桌面同时流式 | 事件路由 | v1 每连接独立 StreamID（互不串流）；v2 再做跨设备实时镜像（本期不承诺） |
| 引擎关机/掉电 | 手机不可用 | 明确产品语义"电脑在线才可用"；M4 轮询通知附最近在线时间 |

---

## 12. 明确不做 / 不承诺

- 不承诺"100% 可达"（广域网可达性受网络环境制约，§7 提供三级路径）
- 不做远程桌面式屏幕控制
- 不把任何供应商密钥、DPAPI 凭证传输到手机——**密钥永不离开引擎**
- 不改现有桌面 UI 布局、对话交互、月伴音色、星尘配方、poison 自愈机制
