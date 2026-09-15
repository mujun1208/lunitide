import React, { useEffect, useMemo, useState } from 'react'
import { useZh } from '../i18n/language'
import { agentHubApi, type AgentHubName, type AgentHubStatus, type AgentHubTask, type AgentHubThread } from './agentHubApi'
import { shortWorkDir, stateLabel } from './agentHubCopy'
import './agentHub.css'

export function AgentHubSidebar({
  onOpenThread,
  selectedThreadId,
  newThreadNonce = 0,
  selectedAgent,
  onSelectAgent,
  onOpenLegacy,
}: {
  onOpenThread: (threadId: string) => void
  selectedThreadId?: string
  newThreadNonce?: number
  selectedAgent?: string
  onSelectAgent?: (name: AgentHubName) => void
  onOpenLegacy?: (taskId: string) => void
}): React.JSX.Element {
  const zh = useZh()
  const [agents, setAgents] = useState<AgentHubStatus[]>([])
  const [threads, setThreads] = useState<AgentHubThread[]>([])
  const [legacy, setLegacy] = useState<AgentHubTask[]>([])
  const [query, setQuery] = useState('')
  const [renameId, setRenameId] = useState('')
  const [renameTitle, setRenameTitle] = useState('')
  const [connecting, setConnecting] = useState('')
  useEffect(() => {
    let alive = true
    const load = async () => {
      try {
        const listed = await agentHubApi.threadList({})
        if (!alive) return
        setThreads(listed.items ?? [])
      } catch {
        if (alive) setThreads([])
      }
      try {
        const detected = await agentHubApi.detect()
        if (!alive) return
        setAgents(detected.agents ?? [])
      } catch {
        // Keep the last good detect result; a slow CLI --version must not blank the list.
      }
      try {
        const tasks = await agentHubApi.list()
        if (!alive) return
        setLegacy(tasks.items ?? [])
      } catch {
        if (alive) setLegacy([])
      }
    }
    void load()
    return () => { alive = false }
  }, [selectedThreadId, newThreadNonce])
  const groups = useMemo(() => {
    const names = [...new Set([
      ...agents.map(item => item.name),
      ...threads.map(item => item.harnessId),
    ])]
    const q = query.trim().toLocaleLowerCase()
    return names.map(name => ({
      name,
      agent: agents.find(item => item.name === name),
      items: threads.filter(item => item.harnessId === name && (!q || item.title.toLocaleLowerCase().includes(q))),
    }))
  }, [agents, threads, query])
  const connect = async (name: AgentHubName) => {
    setConnecting(name)
    try {
      const detected = await agentHubApi.detect()
      setAgents(detected.agents ?? [])
      onSelectAgent?.(name)
    } catch {
      // Re-detect only; a failed probe must not become an unhandled rejection.
    } finally {
      setConnecting('')
    }
  }
  const pin = async (item: AgentHubThread) => {
    const next = await agentHubApi.threadUpdate({ threadId: item.threadId, pinned: !item.pinned })
    setThreads(values => values.map(value => value.threadId === next.thread.threadId ? next.thread : value))
  }
  const saveTitle = async (item: AgentHubThread) => {
    const title = renameTitle.trim()
    if (!title) return
    const next = await agentHubApi.threadUpdate({ threadId: item.threadId, title })
    setThreads(values => values.map(value => value.threadId === next.thread.threadId ? next.thread : value))
    setRenameId('')
  }
  const remove = async (item: AgentHubThread) => {
    const ok = window.confirm(zh ? `删除会话「${item.title}」？` : `Delete thread “${item.title}”?`)
    if (!ok) return
    await agentHubApi.threadDelete({ threadId: item.threadId })
    setThreads(values => values.filter(value => value.threadId !== item.threadId))
  }
  return (
    <nav
      className="agent-hub-sidebar"
      aria-label={zh ? 'AgentHub' : 'AgentHub'}
      style={{ flex: 1, minHeight: 0, overflow: 'auto' }}
    >
      <label className="agent-hub-sidebar-search">
        <input value={query} onChange={event => setQuery(event.target.value)} aria-label={zh ? '搜索会话' : 'Search threads'} />
      </label>
      {legacy.length > 0 && (
        <section>
          <h2 className="conversation-heading">{zh ? '旧版任务' : 'Legacy tasks'}</h2>
          {legacy.map(item => (
            <button
              key={item.taskId}
              type="button"
              className="conversation-open"
              onClick={() => onOpenLegacy?.(item.taskId)}
            >
              {item.agent} · {shortWorkDir(item.workDir || item.prompt)}
            </button>
          ))}
        </section>
      )}
      {groups.map(group => (
        <section key={group.name}>
          <div className="agent-hub-agent-row">
            <button
              type="button"
              className="agent-hub-agent-select"
              aria-pressed={selectedAgent === group.name}
              onClick={() => onSelectAgent?.(group.name as AgentHubName)}
            >
              <span className={`agent-hub-lamp ${group.agent?.state ?? 'unknown'}`} aria-hidden="true" />
              <h2 className="conversation-heading">{group.name}</h2>
              <small>{stateLabel(group.agent?.state ?? 'unknown', zh)}</small>
            </button>
            <button
              type="button"
              className="agent-hub-connect"
              disabled={connecting === group.name}
              onClick={() => void connect(group.name as AgentHubName)}
            >
              {zh ? '连接' : 'Connect'}
            </button>
          </div>
          {group.items.map(item => (
            <div key={item.threadId} className={`conversation-row${item.pinned ? ' is-pinned' : ''}`}>
              {renameId === item.threadId ? (
                <>
                  <input
                    value={renameTitle}
                    onChange={event => setRenameTitle(event.target.value)}
                    aria-label={zh ? '会话标题' : 'Thread title'}
                  />
                  <button type="button" onClick={() => void saveTitle(item)}>{zh ? '保存标题' : 'Save title'}</button>
                </>
              ) : (
                <button type="button" className="conversation-open" onClick={() => onOpenThread(item.threadId)}>{item.title}</button>
              )}
              <button
                type="button"
                className="conversation-more"
                aria-label={`${item.pinned ? (zh ? '取消置顶' : 'Unpin') : (zh ? '置顶' : 'Pin')} ${item.title}`}
                onClick={() => void pin(item)}
              >
                {item.pinned ? '⌃' : '📌'}
              </button>
              <button
                type="button"
                className="conversation-more"
                aria-label={`${zh ? '重命名' : 'Rename'} ${item.title}`}
                onClick={() => { setRenameId(item.threadId); setRenameTitle(item.title) }}
              >
                ✎
              </button>
              <button
                type="button"
                className="conversation-more"
                aria-label={`${zh ? '删除' : 'Delete'} ${item.title}`}
                onClick={() => void remove(item)}
              >
                ×
              </button>
            </div>
          ))}
        </section>
      ))}
    </nav>
  )
}
