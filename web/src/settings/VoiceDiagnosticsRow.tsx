// Read-only latency diagnostics for the voice pipeline. Reads the in-memory
// voiceTiming ring populated by the companion stage during live turns and
// shows p50/p95/max against each SLA budget, so V1 (duplex) / V3 (prewarm)
// can be validated on the real machine with numbers instead of feel.
import React, { useState } from 'react'
import { peekVoiceTimings, voiceStallCount, voiceTimingExportJSON, voiceTimingRows, type VoiceTimingDisplayRow } from '../session/companion/voiceTiming'

function fmt(ms?: number): string {
  return typeof ms === 'number' ? `${ms}ms` : '—'
}

export function VoiceDiagnosticsRow(): React.JSX.Element {
  const [snapshot, setSnapshot] = useState(() => ({ rows: voiceTimingRows(), stalls: voiceStallCount(), turns: peekVoiceTimings().length }))
  const [exportNote, setExportNote] = useState('')
  const refresh = (): void => setSnapshot({ rows: voiceTimingRows(), stalls: voiceStallCount(), turns: peekVoiceTimings().length })
  const exportTiming = async (): Promise<void> => {
    const json = voiceTimingExportJSON()
    try {
      await navigator.clipboard.writeText(json)
      setExportNote('已复制到剪贴板')
    } catch {
      // Clipboard blocked (no gesture / permission): fall back to a download.
      try {
        const url = URL.createObjectURL(new Blob([json], { type: 'application/json' }))
        const a = document.createElement('a')
        a.href = url
        a.download = `voice-timing-${Date.now()}.json`
        a.click()
        URL.revokeObjectURL(url)
        setExportNote('已导出为文件')
      } catch {
        setExportNote('导出失败')
      }
    }
    window.setTimeout(() => setExportNote(''), 4000)
  }
  const { rows, stalls, turns } = snapshot
  return (
    <div className="setting-row" style={{ flexDirection: 'column', alignItems: 'stretch', gap: 8 }}>
      <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center' }}>
        <div>
          <div className="setting-label">语音延迟诊断</div>
          <div className="setting-desc">
            本机最近 {turns} 轮语音的实测延迟；p95 超过目标即判为未达标{stalls > 0 ? `；卡壳 ${stalls} 次` : ''}。开启全双工/预热后跑几轮再刷新。
          </div>
        </div>
        <div style={{ display: 'flex', gap: 6, alignItems: 'center' }}>
          {exportNote && <span className="setting-desc" role="status">{exportNote}</span>}
          <button onClick={() => void exportTiming()} disabled={turns === 0} aria-label="导出本轮语音延迟数据">导出</button>
          <button onClick={refresh} aria-label="刷新语音延迟诊断">刷新</button>
        </div>
      </div>
      <table className="voice-diagnostics" style={{ width: '100%', fontSize: 12, fontFamily: 'var(--mono)', borderCollapse: 'collapse' }}>
        <thead>
          <tr style={{ textAlign: 'right', opacity: 0.7 }}>
            <th style={{ textAlign: 'left' }}>阶段</th>
            <th>p50</th>
            <th>p95</th>
            <th>max</th>
            <th>目标</th>
            <th style={{ textAlign: 'center' }}>状态</th>
          </tr>
        </thead>
        <tbody>
          {rows.map((row: VoiceTimingDisplayRow) => (
            <tr key={row.field} style={{ textAlign: 'right' }}>
              <td style={{ textAlign: 'left' }}>{row.label}</td>
              <td>{fmt(row.p50)}</td>
              <td>{fmt(row.p95)}</td>
              <td>{fmt(row.max)}</td>
              <td>≤{row.budgetMs}ms</td>
              <td style={{ textAlign: 'center', color: row.count === 0 ? 'var(--muted)' : row.healthy ? 'var(--tide1)' : '#e06c6c' }}>
                {row.count === 0 ? '—' : row.healthy ? '达标' : '超标'}
              </td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  )
}
