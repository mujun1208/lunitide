import React, { useEffect, useRef, useState } from 'react'
import { emitEngineRecovered, ENGINE_RECOVERED_EVENT, ENGINE_UNAVAILABLE_EVENT } from './engineHealth'

type Props = {
  probe: () => Promise<unknown>
  onRecovered?: () => void
}

type Phase = 'ok' | 'down' | 'recovered'

export function EngineHealthBanner({ probe, onRecovered }: Props): React.JSX.Element | null {
  const [phase, setPhase] = useState<Phase>('ok')
  const [busy, setBusy] = useState(false)
  const probeRef = useRef(probe)
  probeRef.current = probe
  const onRecoveredRef = useRef(onRecovered)
  onRecoveredRef.current = onRecovered

  const finishRecover = () => {
    setPhase('recovered')
    onRecoveredRef.current?.()
    emitEngineRecovered()
  }

  useEffect(() => {
    const show = () => setPhase('down')
    const recovered = () => setPhase('recovered')
    window.addEventListener(ENGINE_UNAVAILABLE_EVENT, show)
    window.addEventListener(ENGINE_RECOVERED_EVENT, recovered)
    return () => {
      window.removeEventListener(ENGINE_UNAVAILABLE_EVENT, show)
      window.removeEventListener(ENGINE_RECOVERED_EVENT, recovered)
    }
  }, [])

  useEffect(() => {
    if (phase !== 'recovered') return
    const timer = window.setTimeout(() => setPhase('ok'), 2000)
    return () => window.clearTimeout(timer)
  }, [phase])

  useEffect(() => {
    if (phase !== 'down') return
    let cancelled = false
    const tick = () => {
      void probeRef.current()
        .then(() => {
          if (!cancelled) finishRecover()
        })
        .catch(() => {})
    }
    const id = window.setInterval(tick, 2000)
    return () => {
      cancelled = true
      window.clearInterval(id)
    }
  }, [phase])

  if (phase === 'recovered') {
    return (
      <div className="engine-health-banner is-recovered" role="status">
        <span>核心引擎已恢复</span>
      </div>
    )
  }
  if (phase !== 'down') return null
  return (
    <div className="engine-health-banner" role="alert">
      <span>核心引擎已断开，正在自动重连…</span>
      <button
        type="button"
        disabled={busy}
        onClick={() => {
          setBusy(true)
          void probe()
            .then(() => finishRecover())
            .catch(() => {})
            .finally(() => setBusy(false))
        }}
      >
        重试连接
      </button>
    </div>
  )
}
