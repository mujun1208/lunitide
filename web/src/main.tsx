import React from 'react'
import { createRoot } from 'react-dom/client'
import { App } from './App'
import { RootErrorBoundary } from './RootErrorBoundary'
import { installVisibilityRestore } from './visibilityRestore'
import { activateRemoteTransport } from './bridge/fetchTransport'
import { loadRemoteCredentials } from './bridge/wsTransport'
import { probeViaTransport } from './bridge/gate'
import { startRemoteNotify } from './bridge/remoteNotify'
import { PERSONAL_CHAT_PROJECT_ID_KEY } from './app/appHelpers'
import { getSessionBridge } from './bridge/client'
import './styles.css'
import './managementLayout.css'
installVisibilityRestore()
// 移动伴侣 PWA：本地存在已配对凭据时，在任何 bridge 单例构建之前激活
// 远程传输 override（HTTPS NDJSON 流式桥；桌面端 localStorage 无此键，
// 路径不触发）。
const remoteCredentials = loadRemoteCredentials()
if (remoteCredentials) {
  // 连接状态实时通知安卓壳：壳据此判断远程模式是否已连上电脑。
  // 'connecting'/'reconnecting' 后 15s 内未 'open'，壳会弹连接错误页
  // （重新扫码/重试），避免传输静默重连时界面白屏、用户无从
  // 得知是连不上还是卡死。仅在壳内（有 LunitideShell 桥）生效，浏览器
  // 端无此桥不影响。
  const shell = (window as unknown as { LunitideShell?: { notifyConnectionState?(state: string): void } }).LunitideShell
  const transport = activateRemoteTransport(remoteCredentials.wsUrl, remoteCredentials.token, remoteCredentials.candidates, state => {
    try { shell?.notifyConnectionState?.(state) } catch { /* 壳桥调用失败不阻塞传输 */ }
  })
  // 可用性闸门（浏览器 PWA）：桥探活失败直接退回配对页重新扫码，不许
  // 用户进入一个全屏报错的系统（用户标准：进入正式系统就必须可用）。
  // 壳内不抢跑——壳层 15s 连接失败计时 + 原生错误页（带重新扫码出口）
  // 已覆盖同一场景，页面级跳转反而会和壳错误页互相打架。以
  // notifyConnectionState 的存在判定壳（壳桥两个方法总是成对注册）。
  if (!shell?.notifyConnectionState) {
    void probeViaTransport(transport).then(outcome => {
      if (!outcome.ok) location.replace('/pair')
    })
  }
  // Service Worker 仅在移动伴侣模式注册：桌面 WebView2 走命名管道，
  // SW 缓存只会干扰本地资源加载。
  void navigator.serviceWorker?.register('/sw.js').catch(() => {})
  // M4 轮询通知：后台时轮询个人对话会话，有新回复发浏览器通知。
  // 通知权限由配对成功页请求（iOS 16.4+ / Android Chrome PWA 支持）。
  startRemoteNotify(async () => {
    const projectId = localStorage.getItem(PERSONAL_CHAT_PROJECT_ID_KEY)
    if (!projectId) return []
    const listed = await getSessionBridge().list({ projectId })
    return listed.items.map(session => ({ id: session.id, title: session.title, updatedAt: session.updatedAt }))
  })
}
createRoot(document.getElementById('root')!).render(
  <React.StrictMode>
    <RootErrorBoundary>
      <App />
    </RootErrorBoundary>
  </React.StrictMode>,
)
