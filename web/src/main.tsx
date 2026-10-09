import React from 'react'
import { createRoot } from 'react-dom/client'
import { App } from './App'
import { RootErrorBoundary } from './RootErrorBoundary'
import { installVisibilityRestore } from './visibilityRestore'
import { activateRemoteTransport, loadRemoteCredentials } from './bridge/wsTransport'
import { startRemoteNotify } from './bridge/remoteNotify'
import { PERSONAL_CHAT_PROJECT_ID_KEY } from './app/appHelpers'
import { getSessionBridge } from './bridge/client'
import './styles.css'
import './managementLayout.css'
installVisibilityRestore()
// 移动伴侣 PWA：本地存在已配对凭据时，在任何 bridge 单例构建之前激活
// WSS 传输 override（桌面端 localStorage 无此键，路径不触发）。
const remoteCredentials = loadRemoteCredentials()
if (remoteCredentials) {
  // 连接状态实时通知安卓壳：壳据此判断远程模式是否已连上电脑。
  // 'connecting'/'reconnecting' 后 15s 内未 'open'，壳会弹连接错误页
  // （重新扫码/重试），避免 WsTransport 静默重连时界面白屏、用户无从
  // 得知是连不上还是卡死。仅在壳内（有 LunitideShell 桥）生效，浏览器
  // 端无此桥不影响。
  const shell = (window as unknown as { LunitideShell?: { notifyConnectionState?(state: string): void } }).LunitideShell
  activateRemoteTransport(remoteCredentials.wsUrl, remoteCredentials.token, remoteCredentials.candidates, state => {
    try { shell?.notifyConnectionState?.(state) } catch { /* 壳桥调用失败不阻塞传输 */ }
  })
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
