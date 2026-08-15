import React from 'react'

export interface RecoveryEvent {
  seq: number
  type: string
  summary: string
  at: string
}

export interface RecoveryCenterProps {
  events: RecoveryEvent[]
  onReplay?: () => void
  recovering: boolean
}

/** 找出 seq 不连续的位置：下一事件 seq !== 上一事件 seq + 1 即为缺口 */
function findGaps(events: RecoveryEvent[]): Array<{ from: number; to: number; beforeIndex: number }> {
  const gaps: Array<{ from: number; to: number; beforeIndex: number }> = []
  for (let i = 1; i < events.length; i++) {
    const from = events[i - 1].seq
    const to = events[i].seq
    if (to !== from + 1) gaps.push({ from, to, beforeIndex: i })
  }
  return gaps
}

export function RecoveryCenter({ events, onReplay, recovering }: RecoveryCenterProps): React.JSX.Element {
  const gaps = findGaps(events)
  const gapBefore = new Map<number, { from: number; to: number }>()
  for (const g of gaps) gapBefore.set(g.beforeIndex, { from: g.from, to: g.to })
  return (
    <section className="m5-recovery" aria-label="恢复中心">
      <header className="m5-recovery-head">
        <h3>恢复中心</h3>
        <button
          type="button"
          className="m5-btn m5-btn-primary"
          disabled={recovering || !onReplay}
          onClick={() => onReplay?.()}
          data-testid="m5-replay-btn"
        >
          {recovering ? '恢复中…' : '恢复执行'}
        </button>
      </header>
      {events.length === 0 ? (
        <p className="m5-empty">暂无可重放事件。</p>
      ) : (
        <ol className="m5-event-list">
          {events.map((e, i) => (
            <React.Fragment key={`${e.seq}-${i}`}>
              {gapBefore.has(i) && (
                <li className="m5-gap-warning" role="alert">
                  <span className="m5-gap-arrow">⚠</span>
                  序列缺口 {gapBefore.get(i)!.from}→{gapBefore.get(i)!.to}
                </li>
              )}
              <li className="m5-event-item">
                <span className="m5-event-seq">#{e.seq}</span>
                <span className="m5-event-type">{e.type}</span>
                <span className="m5-event-summary">{e.summary}</span>
              </li>
            </React.Fragment>
          ))}
        </ol>
      )}
    </section>
  )
}

export default RecoveryCenter
