import React, { useEffect, useMemo, useState } from 'react'
import { useZh } from '../i18n/language'
import { agentHubApi, type AgentHubStatus, type AgentHubThread } from './agentHubApi'
import './agentHub.css'

export function AgentHubSidebar({
  onOpenThread,
}: {
  onOpenThread: (threadId: string) => void
}): React.JSX.Element {
  const zh = useZh()
  const [agents, setAgents] = useState<AgentHubStatus[]>([])
  const [threads, setThreads] = useState<AgentHubThread[]>([])
  const [query, setQuery] = useState('')
  useEffect(() => {
    let alive = true
    const load = async () => {
      try {
        const [detected, listed] = await Promise.all([
          agentHubApi.detect(),
          agentHubApi.threadList({}),
        ])
        if (!alive) return
        setAgents(detected.agents ?? [])
        setThreads(listed.items ?? [])
      } catch {
        if (alive) setThreads([])
      }
    }
    void load()
    return () => { alive = false }
  }, [])
  const groups = useMemo(() => {
    const names = [...new Set([
      ...agents.map(item => item.name),
      ...threads.map(item => item.harnessId),
    ])]
    const q = query.trim().toLocaleLowerCase()
    return names.map(name => ({
      name,
      items: threads.filter(item => item.harnessId === name && (!q || item.title.toLocaleLowerCase().includes(q))),
    }))
  }, [agents, threads, query])
  const pin = async (item: AgentHubThread) => {
    const next = await agentHubApi.threadUpdate({ threadId: item.threadId, pinned: !item.pinned })
    setThreads(values => values.map(value => value.threadId === next.thread.threadId ? next.thread : value))
  }
  return (
    <nav
      className="agent-hub-sidebar"
      aria-label={zh ? '外接 Agent' : 'Agents'}
      style={{ flex: 1, minHeight: 0, overflow: 'auto' }}
    >
      <label className="agent-hub-sidebar-search">
        <input value={query} onChange={event => setQuery(event.target.value)} aria-label={zh ? '搜索会话' : 'Search threads'} />
      </label>
      {groups.map(group => (
        <section key={group.name}>
          <h2 className="conversation-heading">{group.name}</h2>
          {group.items.map(item => (
            <div key={item.threadId} className={`conversation-row${item.pinned ? ' is-pinned' : ''}`}>
              <button type="button" className="conversation-open" onClick={() => onOpenThread(item.threadId)}>{item.title}</button>
              <button
                type="button"
                className="conversation-more"
                aria-label={`${item.pinned ? (zh ? '取消置顶' : 'Unpin') : (zh ? '置顶' : 'Pin')} ${item.title}`}
                onClick={() => void pin(item)}
              >
                {item.pinned ? '⌃' : '📌'}
              </button>
            </div>
          ))}
        </section>
      ))}
    </nav>
  )
}
