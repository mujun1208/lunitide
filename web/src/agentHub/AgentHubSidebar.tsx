import React, { useEffect, useMemo, useState } from 'react'
import { useZh } from '../i18n/language'
import { ConfirmDialog } from '../ui/Dialog'
import { AgentHubMark } from './AgentHubMark'
import { agentHubApi, type AgentHubName, type AgentHubStatus, type AgentHubThread } from './agentHubApi'
import { agentDisplayName, HUB_AGENT_IDS, stateLabel, usableLatestThreadForHarness } from './agentHubCopy'
import './agentHub.css'

export function AgentHubSidebar({
  onOpenThread,
  selectedThreadId,
  newThreadNonce = 0,
  selectedAgent,
  onSelectAgent,
  onOpenLegacy,
  onOpenHistory,
  onNewChat,
}: {
  onOpenThread: (threadId: string) => void
  selectedThreadId?: string
  newThreadNonce?: number
  selectedAgent?: string
  onSelectAgent?: (name: AgentHubName) => void
  onOpenLegacy?: (taskId: string) => void
  onOpenHistory?: () => void
  onNewChat?: () => void
}): React.JSX.Element {
  const zh = useZh()
  const [agents, setAgents] = useState<AgentHubStatus[]>([])
  const [threads, setThreads] = useState<AgentHubThread[]>([])
  const [connecting, setConnecting] = useState('')
  const [installName, setInstallName] = useState<AgentHubName>()
  const [installBusy, setInstallBusy] = useState(false)
  const [installError, setInstallError] = useState('')
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
    thread: usableLatestThreadForHarness(threads, name),
  })), [agents, threads])
  const openAgent = (name: AgentHubName, threadId?: string) => {
    onSelectAgent?.(name)
    onOpenThread(threadId ?? '')
  }
  const connect = async (name: AgentHubName) => {
    const current = agents.find(item => item.name === name)
    if (current?.state !== 'available') {
      setInstallError('')
      setInstallName(name)
      return
    }
    setConnecting(name)
    try {
      const detected = await agentHubApi.detect()
      setAgents(detected.agents ?? [])
      openAgent(name, usableLatestThreadForHarness(threads, name)?.threadId)
    } catch {
      openAgent(name, usableLatestThreadForHarness(threads, name)?.threadId)
    } finally {
      setConnecting('')
    }
  }
  const confirmInstall = async () => {
    if (!installName) return
    setInstallBusy(true)
    setInstallError('')
    try {
      const got = await agentHubApi.install({ name: installName, confirmed: true })
      setAgents(got.agents ?? [])
      if (!got.connected) {
        setInstallError(got.hint || (zh ? '本机安装已跑完，但仍未连上。' : 'The local install finished, but it is still not connected.'))
        return
      }
      const name = installName
      setInstallName(undefined)
      openAgent(name, usableLatestThreadForHarness(threads, name)?.threadId)
    } catch (err) {
      setInstallError(err instanceof Error && /[\u4e00-\u9fff]/.test(err.message) ? err.message : (zh ? '安装没有完成。' : 'Install did not finish.'))
    } finally {
      setInstallBusy(false)
    }
  }
  return (
    <nav
      className="agent-hub-sidebar"
      aria-label={zh ? 'AgentHub' : 'AgentHub'}
      style={{ flex: 1, minHeight: 0, overflow: 'auto' }}
    >
      {onNewChat ? (
        <button type="button" className="agent-hub-new-chat" onClick={onNewChat}>
          {zh ? '新对话' : 'New chat'}
        </button>
      ) : null}
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
      <ConfirmDialog
        open={Boolean(installName)}
        danger={false}
        busy={installBusy}
        error={installError}
        title={zh ? `安装并连接 ${installName ? agentDisplayName(installName) : ''}` : `Install and connect ${installName ? agentDisplayName(installName) : ''}`}
        description={zh ? '先检查本机是否已有该 CLI。没有就在本机自动安装，再登录并连接。不会打开网页。' : 'Check for the local CLI first. If it is missing, install it here, then sign in and connect. No webpage will open.'}
        confirmLabel={zh ? '确定安装' : 'Install'}
        onCancel={() => { if (!installBusy) setInstallName(undefined) }}
        onConfirm={() => { void confirmInstall() }}
      />
    </nav>
  )
}
