import React, { useCallback, useEffect, useState } from 'react'
import { subagentBridge } from '../bridge/client'
import type { SubagentTreeResult } from '../generated/bridge'
import { loadSubagentSettings } from '../settings/subagentSettings'

type Row = SubagentTreeResult['subagents'][number]

const STATUS_LABEL: Record<string, string> = {
  queued: '排队中',
  running: '运行中',
  completed: '已完成',
  failed: '失败',
  cancelled: '已取消',
  orphaned: '已孤立',
}

function parseTaggedPurpose(purpose: string): { profile: string; text: string } {
  const m = purpose.match(/^\[([^\]]+)\]\s*(.*)$/s)
  if (!m) return { profile: '子代理', text: purpose }
  return { profile: m[1], text: m[2] || purpose }
}

export function SubagentPanel({ sessionId, refreshKey = 0 }: { sessionId: string; refreshKey?: number }): React.JSX.Element {
  const [rows, setRows] = useState<Row[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')
  const [expanded, setExpanded] = useState<string>()
  const [summary, setSummary] = useState<Record<string, string>>({})

  const load = useCallback(async () => {
    setError('')
    try {
      const r = await subagentBridge.tree({ rootRunId: sessionId, limit: 50 })
      setRows(r.subagents ?? [])
    } catch (e) {
      setError(e instanceof Error ? e.message : '子智能体列表载入失败')
    } finally {
      setLoading(false)
    }
  }, [sessionId])

  useEffect(() => {
    setLoading(true)
    void load()
  }, [load, refreshKey])

  useEffect(() => {
    if (!rows.some(r => r.status === 'running' || r.status === 'queued')) return
    const id = window.setInterval(() => void load(), 2500)
    return () => window.clearInterval(id)
  }, [rows, load])

  const showSummary = async (id: string) => {
    if (expanded === id) {
      setExpanded(undefined)
      return
    }
    setExpanded(id)
    if (summary[id]) return
    try {
      const r = await subagentBridge.join({ subagentId: id, waitMs: 1000, maxSummaryBytes: 8192 })
      const text = (r as { summary?: string }).summary ?? (r.observations?.map(o => o.summary).join('\n') ?? '')
      setSummary(v => ({ ...v, [id]: text || '暂无摘要' }))
    } catch (e) {
      setSummary(v => ({ ...v, [id]: e instanceof Error ? e.message : '读取摘要失败' }))
    }
  }

  const settings = loadSubagentSettings()
  const enabledCount = Object.values(settings.overrides).filter(o => o.enabled !== false).length + settings.customProfiles.length

  return (
    <section className="subagent-panel" aria-label="子智能体">
      <header className="subagent-panel-head">
        <div>
          <b>子智能体</b>
          <small>主 Agent 派出的并行只读工人 · 已启用 {enabledCount} 个 profile</small>
        </div>
        <button type="button" onClick={() => void load()} disabled={loading}>刷新</button>
      </header>
      {loading && !rows.length ? <p role="status">正在载入…</p> : null}
      {error ? <p role="alert">{error}</p> : null}
      {!loading && !rows.length ? (
        <p className="subagent-empty">本轮还没有派出子智能体。复杂调研时主 Agent 会调用 <code>subagent.spawn</code>；也可在设置里调整 profile 与委派策略。</p>
      ) : (
        <ul className="subagent-list">
          {rows.map(row => {
            const { profile, text } = parseTaggedPurpose(row.purpose)
            return (
              <li key={row.id} className={`subagent-row status-${row.status}`}>
                <div className="subagent-row-main">
                  <span className="subagent-profile-tag">{profile}</span>
                  <b title={text}>{text}</b>
                  <small>{STATUS_LABEL[row.status] ?? row.status} · {row.spentTokens} tokens · {row.observationCount} 条观察</small>
                </div>
                <button type="button" aria-expanded={expanded === row.id} onClick={() => void showSummary(row.id)}>
                  {expanded === row.id ? '收起摘要' : '查看摘要'}
                </button>
                {expanded === row.id && <pre className="subagent-summary">{summary[row.id] ?? '载入中…'}</pre>}
              </li>
            )
          })}
        </ul>
      )}
    </section>
  )
}
