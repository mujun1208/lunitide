import { useEffect, useState } from 'react'
import { prefersCompanionStillVisual } from './webglSupport'

export const COMPANION_ENTER_MS = 2800

export function easeCompanionEnter(elapsedMs: number, durationMs = COMPANION_ENTER_MS): number {
  const p = Math.min(1, Math.max(0, elapsedMs / Math.max(1, durationMs)))
  // Ease-in-out: hold the jade disc, then bloom into aurora + Orb face.
  return p < 0.5 ? 4 * p * p * p : 1 - ((-2 * p + 2) ** 3) / 2
}

export function useCompanionEnter(replay = 0, durationMs = COMPANION_ENTER_MS): number {
  const still = prefersCompanionStillVisual()
  const [enter, setEnter] = useState(still ? 1 : 0)

  useEffect(() => {
    if (prefersCompanionStillVisual()) {
      setEnter(1)
      return
    }
    setEnter(0)
    const started = performance.now()
    let frame = 0
    const tick = (now: number) => {
      const next = easeCompanionEnter(now - started, durationMs)
      setEnter(prev => (Math.abs(prev - next) < 0.012 ? prev : next))
      if (next < 1) frame = requestAnimationFrame(tick)
    }
    frame = requestAnimationFrame(tick)
    return () => cancelAnimationFrame(frame)
  }, [replay, durationMs])

  return still ? 1 : enter
}
