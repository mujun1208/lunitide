// useAutomationBroadcast.ts links finished automation runs to companion
// TTS broadcasts (P3-4): polls automation.run.list, detects terminal
// runs that were not seen before, and hands a short broadcast text to
// the stage, which speaks it through the normal TTS pipeline. The
// linkage is conservative by design: broadcasts only fire when the
// stage is idle (never over the open microphone or an ongoing reply)
// and auto-speak is enabled; the scheduler's Windows toast still
// covers plain notification duty, and runs that finish while the stage
// is busy are dropped rather than queued.
import { useEffect, useRef } from 'react'
import { automationBridge, type AutomationBridge } from '../../bridge/client'
import type { AutomationRunListResult } from '../../generated/bridge'

type AutomationRun = AutomationRunListResult['runs'][number]

export interface UseAutomationBroadcastOptions {
  bridge?: Pick<AutomationBridge, 'listRuns'>
  /** Poll cadence; 30s mirrors the AutomationPanel reload. */
  intervalMs?: number
  /** Companion gate: settings enabled + auto-speak + TTS voices ready. */
  enabled: boolean
  /** Stage-idle probe (ref-backed) — busy stages never broadcast. */
  idle: () => boolean
  onBroadcast: (text: string) => void
}

const POLL_LIMIT = 10
const MAX_MENTION = 3
const MAX_DETAIL_CHARS = 120

const isTerminal = (run: AutomationRun): boolean => run.state === 'succeeded' || run.state === 'failed'

const firstLine = (value?: string): string =>
  (value ?? '')
    .split(/\r?\n/)
    .map(line => line.trim())
    .find(line => line.length > 0) ?? ''

/** Cap the spoken detail mid-rune-safely; keep the broadcast brief. */
const clip = (value: string): string => {
  const chars = Array.from(value)
  if (chars.length <= MAX_DETAIL_CHARS) return value
  return `${chars.slice(0, MAX_DETAIL_CHARS).join('')}。`
}

/** Build the spoken text for newly finished runs (1–N combined). */
export function buildBroadcastText(runs: readonly AutomationRun[]): string {
  const parts = runs.slice(0, MAX_MENTION).map(run => {
    const head =
      run.state === 'failed'
        ? `自动化任务「${run.jobName}」执行失败。`
        : `自动化任务「${run.jobName}」已完成。`
    const detail = clip(firstLine(run.state === 'failed' ? run.error : run.summary))
    return detail ? `${head}${detail}` : head
  })
  if (runs.length > MAX_MENTION) parts.push(`其余 ${runs.length - MAX_MENTION} 个结果请查看运行历史。`)
  return parts.join('\n')
}

export function useAutomationBroadcast({
  bridge = automationBridge,
  intervalMs = 30_000,
  enabled,
  idle,
  onBroadcast,
}: UseAutomationBroadcastOptions): void {
  /** null until the first poll has marked the baseline of terminal runs. */
  const seen = useRef<Set<string> | null>(null)
  const enabledRef = useRef(enabled)
  enabledRef.current = enabled
  const idleRef = useRef(idle)
  idleRef.current = idle
  const broadcastRef = useRef(onBroadcast)
  broadcastRef.current = onBroadcast

  useEffect(() => {
    let alive = true
    const poll = async () => {
      let runs: AutomationRun[]
      try {
        runs = (await bridge.listRuns({ limit: POLL_LIMIT })).runs
      } catch {
        return // Bridge unavailable (non-WebView2/tests): retry next tick.
      }
      if (!alive) return
      const known = seen.current
      if (known === null) {
        // Baseline: historical results are never replayed as broadcasts.
        seen.current = new Set(runs.filter(isTerminal).map(run => run.id))
        return
      }
      const fresh = runs.filter(run => isTerminal(run) && !known.has(run.id))
      for (const run of runs) if (isTerminal(run)) known.add(run.id)
      if (!fresh.length) return
      if (!enabledRef.current || !idleRef.current()) return
      broadcastRef.current(buildBroadcastText(fresh))
    }
    void poll()
    const timer = window.setInterval(() => void poll(), intervalMs)
    return () => {
      alive = false
      window.clearInterval(timer)
    }
  }, [bridge, intervalMs])
}
