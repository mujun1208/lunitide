import {useEffect, useState, type JSX} from 'react'
import type {WidgetSpec, WidgetState} from './registry'

const defaultSeconds = 300

function clock(seconds: number): string {
  const m = Math.floor(seconds / 60)
  const s = seconds % 60
  return `${m < 10 ? '0' : ''}${m}:${s < 10 ? '0' : ''}${s}`
}

export function remainingSeconds(state?: WidgetState, fallback = defaultSeconds): number {
  const base = typeof state?.seconds === 'number' ? state.seconds : fallback
  if (!state?.running || !state.updatedAt) return Math.max(0, base)
  const started = Date.parse(state.updatedAt)
  if (!Number.isFinite(started)) return Math.max(0, base)
  return Math.max(0, base - Math.floor((Date.now() - started) / 1000))
}

export function TimerWidget({spec, title, onState}: {spec: WidgetSpec; title?: string; onState?: (state: WidgetState) => void}): JSX.Element {
  const label = title || spec.title || '计时器'
  const [left, setLeft] = useState(() => remainingSeconds(spec.state))
  const [running, setRunning] = useState(() => !!spec.state?.running && remainingSeconds(spec.state) > 0)
  const persist = (nextLeft: number, nextRunning: boolean) => {
    onState?.({...spec.state, seconds: nextLeft, running: nextRunning, updatedAt: new Date().toISOString()})
  }
  useEffect(() => {
    if (!running) return
    const tick = window.setInterval(() => {
      setLeft(cur => {
        if (cur <= 1) {
          setRunning(false)
          persist(0, false)
          return 0
        }
        return cur - 1
      })
    }, 1000)
    return () => window.clearInterval(tick)
  }, [running])
  return (
    <section aria-label={label} data-widget={spec.id}>
      <p>{label}</p>
      <p aria-label="countdown">{clock(left)}</p>
      <button type="button" onClick={() => { setRunning(true); persist(left, true) }}>开始</button>
      <button type="button" onClick={() => { setRunning(false); persist(left, false) }}>暂停</button>
      <button type="button" onClick={() => { setRunning(false); setLeft(defaultSeconds); persist(defaultSeconds, false) }}>复位</button>
    </section>
  )
}
