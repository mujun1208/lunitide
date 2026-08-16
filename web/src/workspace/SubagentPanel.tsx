import React, { useCallback, useEffect, useRef, useState } from 'react'
import { subagentBridge, createMutationAttempt, type SubagentBridge, type BridgeClientError } from '../bridge/client'
import type { SubagentTreeResult, SubagentJoinResult } from '../generated/bridge'

// M7 只读子代理调研面板（subagent-ui / T-7.6.5）：派生采集、join 观察、
// 预算/状态徽标；≤4 并存（M7-SAG-003）、超时 partial（M7-SAG-005）。
const READ_CAPS = ['fs.tree', 'fs.stat', 'fs.read', 'fs.readMany', 'fs.glob', 'fs.grep', 'web.fetch', 'web.search', 'evidence.list', 'browser.act:navigate', 'browser.act:read', 'browser.act:snapshot'] as const
type ReadCap = typeof READ_CAPS[number]
const STATUS_LABELS: Record<string, string> = { queued: '排队中', running: '运行中', completed: '已完成', failed: '已失败', cancelled: '已取消', orphaned: '已失联' }
const statusColor = (s: string): string => s === 'completed' ? '#34d399' : s === 'running' ? '#22d3ee' : s === 'failed' ? '#f87171' : '#8fa3bf'
const fmtDeadline = (ms: number): string => ms < 60_000 ? `${Math.round(ms / 1000)} 秒` : `${Math.round(ms / 60_000)} 分钟`

export function SubagentPanel({ sessionId, bridge = subagentBridge }: { sessionId: string; bridge?: SubagentBridge }): React.JSX.Element {
  const [rootRunId, setRootRunId] = useState(sessionId)
  const [queryRunId, setQueryRunId] = useState('')
  const [rows, setRows] = useState<SubagentTreeResult['subagents']>([])
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState<string>()
  const [busy, setBusy] = useState(false)
  const [purpose, setPurpose] = useState('')
  const [caps, setCaps] = useState<ReadCap[]>(['fs.read', 'fs.grep', 'evidence.list'])
  const [budget, setBudget] = useState(8000)
  const [deadlineMs, setDeadlineMs] = useState(300_000)
  const [joined, setJoined] = useState<{ id: string; result: SubagentJoinResult }>()
  const mounted = useRef(true)
  useEffect(() => () => { mounted.current = false }, [])

  const load = useCallback(async (runId: string) => {
    if (!runId) return
    setLoading(true); setError(undefined)
    try { const r = await bridge.tree({ rootRunId: runId }); if (mounted.current) setRows(r.subagents) }
    catch (e) { if (mounted.current) setError(e instanceof Error ? e.message : '查询失败') }
    finally { if (mounted.current) setLoading(false) }
  }, [bridge])

  useEffect(() => { if (queryRunId) void load(queryRunId) }, [queryRunId, load])

  const activeCount = rows.filter(x => x.status === 'queued' || x.status === 'running').length
  const spawnBlocked = activeCount >= 4

  const toggleCap = (cap: ReadCap) => setCaps(v => v.includes(cap) ? v.filter(x => x !== cap) : [...v, cap])

  const spawn = async () => {
    const text = purpose.trim()
    if (!text || text.length > 2000 || caps.length === 0 || !queryRunId) return
    setBusy(true); setError(undefined)
    try {
      const payload = { rootRunId: queryRunId, purpose: text, readCaps: caps, budgetTokens: budget, deadlineMs, requestId: `ui-${Date.now()}-${Math.random().toString(36).slice(2, 10)}` }
      const attempt = createMutationAttempt('subagent.spawn', payload)
      await bridge.spawn(attempt.payload as typeof payload, { attempt })
      setPurpose('')
      await load(queryRunId)
    } catch (e) {
      const issue = e as BridgeClientError
      setError(`${issue.message}${issue.code ? `（${issue.code}）` : ''}`)
    } finally { if (mounted.current) setBusy(false) }
  }

  const join = async (subagentId: string) => {
    setBusy(true); setError(undefined)
    try { const r = await bridge.join({ subagentId, waitMs: 5000 }); if (mounted.current) setJoined({ id: subagentId, result: r }) }
    catch (e) { const issue = e as BridgeClientError; setError(`${issue.message}${issue.code ? `（${issue.code}）` : ''}`) }
    finally { if (mounted.current) setBusy(false) }
  }

  const panel: React.CSSProperties = { border: '1px solid #1f2937', borderRadius: '16px', background: '#0e1c30', padding: '20px' }
  const card: React.CSSProperties = { padding: '14px', border: '1px solid #1f2937', borderRadius: '12px', background: '#111827' }

  return (
    <div className="shell">
      <header className="brand"><div><p className="eyebrow">STAGE 2 RESEARCH</p><h1>调研与证据</h1><p>只读并行采集：派生子代理收集证据摘要（不展开全文，防上下文污染）。</p></div></header>
      {error && <div className="error" role="alert"><b>{error}</b></div>}
      <section style={panel}>
        <div style={{ display: 'flex', gap: '10px', alignItems: 'flex-end', marginBottom: '18px' }}>
          <label style={{ display: 'grid', gap: '4px', fontSize: '13px', color: '#e5e7eb', flex: 1 }}>
            Root Run 标识
            <input value={rootRunId} onChange={e => setRootRunId(e.target.value)} aria-label="Root Run 标识" />
          </label>
          <button onClick={() => setQueryRunId(rootRunId.trim())} disabled={loading || !rootRunId.trim()}>{loading ? '查询中…' : '查询子代理'}</button>
        </div>
        {!queryRunId ? (
          <div className="empty"><b>并行采集说明</b><span>输入 Root Run 标识查询其派生子代理。示例 purpose：「收集 CR-42 相关的全部扫描证据并给出摘要」。</span></div>
        ) : rows.length === 0 ? (
          <div className="empty"><b>暂无子代理</b><span>该 Run 还没有派生记录，可在下方发起第一次采集。</span></div>
        ) : (
          <div style={{ display: 'grid', gap: '10px' }} aria-live="polite">
            {rows.map(row => (
              <div key={row.id} style={card}>
                <div style={{ display: 'flex', justifyContent: 'space-between', gap: '8px', alignItems: 'flex-start' }}>
                  <div style={{ minWidth: 0 }}>
                    <strong style={{ fontSize: '13px', wordBreak: 'break-all' }}>{row.purpose}</strong>
                    <p style={{ margin: '4px 0 0', color: '#8fa3bf', fontSize: '11px', fontFamily: 'monospace' }}>{row.id}</p>
                  </div>
                  <div style={{ display: 'flex', flexDirection: 'column', alignItems: 'flex-end', gap: '4px', flexShrink: 0 }}>
                    <span style={{ color: statusColor(row.status), fontSize: '12px' }}>{STATUS_LABELS[row.status] ?? row.status}</span>
                    {row.status === 'orphaned' && <span style={{ color: '#fbbf24', fontSize: '11px' }}>partial · 已收 {row.observationCount} 条</span>}
                  </div>
                </div>
                <div style={{ marginTop: '8px', display: 'flex', gap: '12px', alignItems: 'center', fontSize: '11px', color: '#8fa3bf', flexWrap: 'wrap' }}>
                  <span>已耗 {row.spentTokens} tokens</span>
                  <span>观察 {row.observationCount} 条</span>
                  <button type="button" disabled={busy} onClick={() => void join(row.id)} style={{ marginLeft: 'auto' }}>查看观察</button>
                </div>
              </div>
            ))}
          </div>
        )}
      </section>
      <section style={{ ...panel, marginTop: '14px' }}>
        <h2 style={{ margin: '0 0 6px', fontSize: '15px' }}>派生子代理{spawnBlocked && <span style={{ color: '#fbbf24', fontSize: '12px', marginLeft: '8px' }}>已达 4 个并存上限（M7-SAG-003）</span>}</h2>
        <p style={{ margin: '0 0 14px', color: '#8fa3bf', fontSize: '12px' }}>活跃 {activeCount}/4 · 子代理仅持有下方勾选的只读能力，不含任何写入或凭据。</p>
        <fieldset disabled={busy || spawnBlocked || !queryRunId} style={{ border: 'none', padding: 0, display: 'grid', gap: '12px' }}>
          <label style={{ display: 'grid', gap: '4px', fontSize: '13px' }}>采集目的（{purpose.length}/2000）
            <textarea value={purpose} onChange={e => setPurpose(e.target.value)} rows={3} aria-label="采集目的" placeholder="例：收集 CR-42 相关证据并逐条给出摘要" />
          </label>
          <div>
            <p style={{ margin: '0 0 6px', fontSize: '13px' }}>只读能力集（固定枚举，不可自定义）</p>
            <div style={{ display: 'flex', flexWrap: 'wrap', gap: '6px' }}>
              {READ_CAPS.map(cap => (
                <label key={cap} style={{ display: 'inline-flex', alignItems: 'center', gap: '4px', fontSize: '11px', fontFamily: 'monospace', border: '1px solid #1f2937', borderRadius: '999px', padding: '3px 10px', cursor: 'pointer', color: caps.includes(cap) ? '#22d3ee' : '#8fa3bf' }}>
                  <input type="checkbox" checked={caps.includes(cap)} onChange={() => toggleCap(cap)} />{cap}
                </label>
              ))}
            </div>
          </div>
          <div style={{ display: 'flex', gap: '16px', flexWrap: 'wrap' }}>
            <label style={{ display: 'grid', gap: '4px', fontSize: '13px', flex: 1, minWidth: '180px' }}>预算 {budget} tokens
              <input type="range" min={1000} max={50000} step={1000} value={budget} onChange={e => setBudget(Number(e.target.value))} aria-label="预算 tokens" />
            </label>
            <label style={{ display: 'grid', gap: '4px', fontSize: '13px', flex: 1, minWidth: '180px' }}>期限 {fmtDeadline(deadlineMs)}
              <input type="range" min={60_000} max={900_000} step={60_000} value={deadlineMs} onChange={e => setDeadlineMs(Number(e.target.value))} aria-label="期限" />
            </label>
          </div>
          <div><button className="primary" disabled={busy || !purpose.trim() || caps.length === 0} onClick={() => void spawn()}>{busy ? '提交中…' : '确认派生'}</button></div>
        </fieldset>
      </section>
      {joined && (
        <section style={{ ...panel, marginTop: '14px' }} aria-live="polite">
          <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: '10px' }}>
            <h2 style={{ margin: 0, fontSize: '15px' }}>观察列表 · {joined.id}</h2>
            <div style={{ display: 'flex', gap: '10px', alignItems: 'center', fontSize: '12px' }}>
              <span style={{ color: statusColor(joined.result.status) }}>{STATUS_LABELS[joined.result.status] ?? joined.result.status}</span>
              {joined.result.truncated && <span style={{ color: '#fbbf24' }}>partial（M7-SAG-005）</span>}
              {joined.result.spentTokens !== undefined && <span style={{ color: '#8fa3bf' }}>耗 {joined.result.spentTokens} tokens</span>}
              <button type="button" onClick={() => setJoined(undefined)}>关闭</button>
            </div>
          </div>
          {(joined.result.observations ?? []).length === 0 ? (
            <div className="empty"><b>暂无观察</b><span>{joined.result.status === 'running' ? '子代理仍在运行，稍后再次查看。' : '该子代理没有产出观察记录。'}</span></div>
          ) : (
            <div style={{ display: 'grid', gap: '8px' }}>
              {(joined.result.observations ?? []).map(obs => (
                <div key={obs.seq} style={card}>
                  <div style={{ display: 'flex', gap: '8px', alignItems: 'baseline' }}>
                    <span style={{ color: '#22d3ee', fontSize: '12px', flexShrink: 0 }}>#{obs.seq}</span>
                    <span style={{ color: '#8fa3bf', fontSize: '11px', fontFamily: 'monospace', flexShrink: 0 }}>{obs.evidenceId}</span>
                  </div>
                  <p style={{ margin: '6px 0 4px', fontSize: '13px', color: '#e5e7eb' }}>{obs.summary}</p>
                  <p style={{ margin: 0, fontSize: '10px', fontFamily: 'monospace', color: '#475569' }}>digest {obs.digest}</p>
                </div>
              ))}
            </div>
          )}
        </section>
      )}
    </div>
  )
}
