import React, { useEffect, useMemo, useState } from 'react'
import { useZh } from '../i18n/language'
import { agentHubApi, type AgentHubName, type AgentHubThread } from './agentHubApi'
import { agentDisplayName } from './agentHubCopy'

export function AgentHubThreadHistory({
  selectedThreadId,
  onOpen,
}: {
  selectedAgent?: AgentHubName
  selectedThreadId?: string
  onOpen: (threadId: string) => void
}): React.JSX.Element {
  const zh = useZh()
  const [items, setItems] = useState<AgentHubThread[]>([])
  const [error, setError] = useState('')
  const [filter, setFilter] = useState<'all' | AgentHubName>('all')
  useEffect(() => {
    let alive = true
    void agentHubApi.threadList({}).then(got => {
      if (!alive) return
      setItems(got.items ?? [])
      setError('')
    }).catch(err => {
      if (!alive) return
      setError(err instanceof Error && /[\u4e00-\u9fff]/.test(err.message) ? err.message : (zh ? '历史加载失败。' : 'Could not load history.'))
    })
    return () => { alive = false }
  }, [zh])
  const rows = useMemo(() => {
    const list = items
      .filter(item => filter === 'all' || item.harnessId === filter)
      .slice()
      .sort((a, b) => (b.updatedAt || b.createdAt || '').localeCompare(a.updatedAt || a.createdAt || ''))
    return list
  }, [items, filter])
  return (
    <section className="agent-hub-history" aria-label={zh ? '历史对话' : 'Thread history'}>
      <div className="agent-hub-history-filters">
        <label>
          <span className="sr-only">{zh ? '按 Agent 筛选' : 'Filter by agent'}</span>
          <select value={filter} onChange={event => setFilter(event.target.value as typeof filter)}>
            <option value="all">{zh ? '全部 Agent' : 'All agents'}</option>
            <option value="codex">Codex</option>
            <option value="cursor">Cursor</option>
            <option value="kimi">Kimi</option>
          </select>
        </label>
        <p className="agent-hub-history-count">{zh ? `${rows.length} 条对话` : `${rows.length} threads`}</p>
      </div>
      {error ? <p className="agent-hub-error" role="alert">{error}</p> : null}
      {rows.length === 0 && !error ? (
        <p className="agent-hub-hint">{zh ? '还没有对话。选一个 Agent 发一条消息就会出现在这里。' : 'No threads yet. Chat with an Agent and it will show up here.'}</p>
      ) : (
        <ul className="agent-hub-history-list">
          {rows.map(item => (
            <li key={item.threadId}>
              <button
                type="button"
                className={selectedThreadId === item.threadId ? 'is-on' : undefined}
                onClick={() => onOpen(item.threadId)}
              >
                <b>{item.title || (zh ? '未命名对话' : 'Untitled')}</b>
                <span>{agentDisplayName(item.harnessId)} · {item.status}</span>
              </button>
            </li>
          ))}
        </ul>
      )}
    </section>
  )
}
