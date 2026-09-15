import React, { useEffect, useState } from 'react'
import { createMutationAttempt, getAppUpdateBridge, getSystemHealthBridge } from '../bridge/client'

const DISMISS_KEY = 'lunitide:update-dismissed'

type Offer = { updateId: string; version: string; digest: string }

export function AppUpdateBanner(): React.JSX.Element | null {
  const [offer, setOffer] = useState<Offer>()
  const [status, setStatus] = useState('')
  const [busy, setBusy] = useState(false)

  useEffect(() => {
    let alive = true
    const run = async () => {
      try {
        const health = await getSystemHealthBridge().health().catch(() => undefined)
        const current = health?.version || '0.0.0'
        const result = await getAppUpdateBridge().check({ channel: 'stable', currentVersion: current })
        if (!alive || !result.updateId) return
        if (localStorage.getItem(DISMISS_KEY) === result.version) return
        setOffer({ updateId: result.updateId, version: result.version, digest: result.digest })
      } catch {
        /* stay hidden when the engine or feed is unavailable */
      }
    }
    void run()
    const id = window.setInterval(() => { void run() }, 6 * 60 * 60 * 1000)
    return () => {
      alive = false
      window.clearInterval(id)
    }
  }, [])

  const install = async () => {
    if (!offer || busy) return
    setBusy(true)
    setStatus('正在下载并安装更新…关闭应用后会自动覆盖安装，不必先卸载。')
    try {
      const payload = { updateId: offer.updateId, expectedDigest: offer.digest }
      await getAppUpdateBridge().install(payload, { attempt: createMutationAttempt('appUpdate.install', payload) })
    } catch (error) {
      const detail = error instanceof Error ? error.message.trim() : ''
      setStatus(/[\u4e00-\u9fff]/.test(detail) ? detail : '安装失败，请稍后在设置 → 诊断与更新中重试。')
      setBusy(false)
    }
  }

  if (!offer && !status) return null
  return (
    <div className="app-update-banner" role="status">
      <span>{status || `发现新版本 ${offer?.version}，可以覆盖安装，不必先卸载。`}</span>
      {offer && !status.startsWith('正在下载') && !status.startsWith('正在安装') && (
        <>
          <button type="button" disabled={busy} onClick={() => void install()}>立即升级</button>
          <button
            type="button"
            disabled={busy}
            onClick={() => {
              localStorage.setItem(DISMISS_KEY, offer.version)
              setOffer(undefined)
              setStatus('')
            }}
          >
            稍后
          </button>
        </>
      )}
    </div>
  )
}
