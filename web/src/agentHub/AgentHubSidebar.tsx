import React, { useEffect, useMemo, useState } from 'react'
import { useZh } from '../i18n/language'
import { AgentHubMark } from './AgentHubMark'
import { agentHubApi, type AgentHubName, type AgentHubStatus, type AgentHubThread } from './agentHubApi'
import { agentDisplayName, HUB_AGENT_IDS, latestThreadForHarness, stateLabel } from './agentHubCopy'
import './agentHub.css'

export function AgentHubSidebar({
  onOpenThread,
  selectedThreadId,
  newThreadNonce = 0,
  selectedAgent,
  onSelectAgent,
  onOpenLegacy,
  onOpenHistory,
}: {
  onOpenThread: (threadId: string) => void
  selectedThreadId?: string
  newThreadNonce?: number
  selectedAgent?: string
  onSelectAgent?: (name: AgentHubName) => void
  onOpenLegacy?: (taskId: string) => void
  onOpenHistory?: () => void
}): React.JSX.Element {
  const zh = useZh()
  const [agents, setAgents] = useState<AgentHubStatus[]>([])
  const [threads, setThreads] = useState<AgentHubThread[]>([])
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
    }
    void load()
    return () => { alive = false }
  }, [selectedThreadId, newThreadNonce])
  const rows = useMemo(() => HUB_AGENT_IDS.map(name => ({
    name,
    agent: agents.find(item => item.name === name),
    thread: latestThreadForHarness(threads, name),
  })), [agents, threads])
  const openAgent = (name: AgentHubName, threadId?: string) => {
    onSelectAgent?.(name)
    onOpenThread(threadId ?? '')
  }
  const connect = async (name: AgentHubName) => {
    setConnecting(name)
    try {
      const detected = await agentHubApi.detect()
      setAgents(detected.agents ?? [])
      openAgent(name, latestThreadForHarness(threads, name)?.threadId)
    } catch {
      openAgent(name, latestThreadForHarness(threads, name)?.threadId)
    } finally {
      setConnecting('')
    }
  }
  return (
    <nav
      className="agent-hub-sidebar"
      aria-label={zh ? 'AgentHub' : 'AgentHub'}
      style={{ flex: 1, minHeight: 0, overflow: 'auto' }}
    >
      <button
        type="button"
        className="agent-hub-history-btn"
        onClick={() => {
          if (onOpenHistory) onOpenHistory()
          else onOpenLegacy?.('')
        }}
      >
        <span>{zh ? '历史任务' : 'History'}</span>
        <small>{zh ? '旧版任务中心' : 'Legacy tasks'}</small>
      </button>
      <div className="agent-hub-agents">
        {rows.map(row => (
          <div key={row.name} className={`agent-hub-agent-row${selectedAgent === row.name ? ' is-on' : ''}`}>
            <button
              type="button"
              className="agent-hub-agent-select"
              aria-label={zh ? `打开 ${agentDisplayName(row.name)}` : `Open ${agentDisplayName(row.name)}`}
              aria-pressed={selectedAgent === row.name}
              onClick={() => openAgent(row.name, row.thread?.threadId)}
            >
              <span className="agent-hub-logo"><AgentHubMark name={row.name} /></span>
              <span className="agent-hub-agent-copy">
                <b>{agentDisplayName(row.name)}</b>
                <em>{stateLabel(row.agent?.state ?? 'unknown', zh)}</em>
              </span>
              <span className={`agent-hub-lamp ${row.agent?.state ?? 'unknown'}`} aria-hidden="true" />
            </button>
            <button
              type="button"
              className="agent-hub-connect"
              disabled={connecting === row.name}
              aria-label={zh ? `连接 ${agentDisplayName(row.name)}` : `Connect ${agentDisplayName(row.name)}`}
              onClick={() => void connect(row.name)}
            >
              <svg viewBox="0 0 24 24" aria-hidden="true">
                <circle cx="8" cy="12" r="3.2" />
                <path d="M11.2 12h8.3M16.6 9.2v5.6" />
              </svg>
            </button>
          </div>
        ))}
      </div>
      <p className="agent-hub-side-foot">{zh ? '每个 Agent 各自记忆，互不串窗' : 'Each Agent keeps its own memory.'}</p>
    </nav>
  )
}
