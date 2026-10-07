// 移动伴侣轮询通知（PRD §9/M4）：PWA 在后台时定期轮询会话列表，发现
// 某会话 updatedAt 前进则发浏览器通知。引擎离线/断线时静默跳过；回到
// 前台清空基线（界面直接呈现，不再补发通知）。poll 由调用方注入
// （main.tsx 用个人对话项目 ID 轮询 sessions.list），保持 bridge 层
// 不反向依赖 app 层。

export interface NotifiedSession { id: string; title: string; updatedAt: string }

type Timer = ReturnType<typeof setTimeout>

export interface NotifyFn { (title: string, body: string): void }

export function createNotifier(deliver?: NotifyFn): NotifyFn {
  return deliver ?? ((title, body) => {
    if (typeof Notification === 'undefined' || Notification.permission !== 'granted') return
    try { new Notification(title, { body }) } catch { /* 通知通道不可用则跳过 */ }
  })
}

// startRemoteNotify 启动轮询循环，返回停止函数。默认 60 秒一轮。
export function startRemoteNotify(poll: () => Promise<NotifiedSession[]>, notify: NotifyFn = createNotifier(), intervalMs = 60_000): () => void {
  let stopped = false
  let timer: Timer | undefined
  let baseline = new Map<string, string>()
  const tick = async () => {
    if (stopped) return
    if (typeof document === 'undefined' || document.visibilityState === 'visible') {
      baseline = new Map()
    } else {
      try {
        const items = await poll()
        if (stopped) return
        if (baseline.size > 0) {
          for (const item of items) {
            const known = baseline.get(item.id)
            if (known !== undefined && known !== item.updatedAt) {
              notify('Lunitide', `${item.title || '对话'}有新回复`)
            }
          }
        }
        baseline = new Map(items.map(item => [item.id, item.updatedAt]))
      } catch { /* 断线由 WsTransport 负责重连，这里静默 */ }
    }
    if (!stopped) timer = setTimeout(() => void tick(), intervalMs)
  }
  void tick()
  return () => {
    stopped = true
    if (timer !== undefined) clearTimeout(timer)
  }
}
