import React, { useEffect, useMemo, useState } from 'react'
import { useZh } from '../i18n/language'
import { ConfirmDialog } from '../ui/Dialog'
import { AgentHubMark } from './AgentHubMark'
import { agentHubApi, type AgentHubName, type AgentHubStatus, type AgentHubThread } from './agentHubApi'
import { agentDisplayName, HUB_AGENT_IDS, stateLabel, usableLatestThreadForHarness } from './agentHubCopy'
import { clearInstallJob, getInstallJob, startAgentInstall, subscribeInstallJobs } from './agentHubInstallStore'
import './agentHub.css'

function jobLabel(name: AgentHubName, agent: AgentHubStatus | undefined, zh: boolean): string {
  const job = getInstallJob(name)
  if (job?.status === 'running') return zh ? '安装中…' : 'Installing…'
  return stateLabel(agent?.state ?? 'unknown', zh)
}

export function AgentHubSidebar({
  onOpenThread,
  selectedThreadId,
  newThreadNonce = 0,
  selectedAgent,
  onSelectAgent,
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
  const [tick, setTick] = useState(0)
  useEffect(() => subscribeInstallJobs(() => setTick(n => n + 1)), [])
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
        // Keep the last good detect result.
      }
    }
    void load()
    return () => { alive = false }
  }, [selectedThreadId, newThreadNonce, tick])
  const rows = useMemo(() => HUB_AGENT_IDS.map(name => ({
    name,
    agent: agents.find(item => item.name === name),
    thread: usableLatestThreadForHarness(threads, name),
  })), [agents, threads])
  const openAgent = (name: AgentHubName, threadId?: string) => {
    onSelectAgent?.(name)
    onOpenThread(threadId ?? '')
  }
  const resumeThreadId = (name: AgentHubName) => {
    if (!selectedThreadId) return ''
    return usableLatestThreadForHarness(threads, name)?.threadId
  }
  const connect = async (name: AgentHubName) => {
    if (getInstallJob(name)?.status === 'running') {
      setInstallName(name)
      setInstallBusy(true)
      return
    }
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
      openAgent(name, resumeThreadId(name))
    } catch {
      openAgent(name, resumeThreadId(name))
    } finally {
      setConnecting('')
    }
  }
  const confirmInstall = async () => {
    if (!installName) return
    setInstallBusy(true)
    setInstallError('')
    const name = installName
    const job = await startAgentInstall(name)
    if (job.agents) setAgents(job.agents)
    if (job.status === 'done') {
      setInstallName(undefined)
      setInstallBusy(false)
      clearInstallJob(name)
      openAgent(name, resumeThreadId(name))
      return
    }
    setInstallError(job.hint || job.error || (zh ? '安装没有完成。' : 'Install did not finish.'))
    setInstallBusy(false)
  }
  useEffect(() => {
    if (!installName) return
    const job = getInstallJob(installName)
    if (job?.status === 'running') setInstallBusy(true)
    if (job?.status === 'done') {
      if (job.agents) setAgents(job.agents)
      setInstallName(undefined)
      setInstallBusy(false)
      clearInstallJob(installName)
      openAgent(installName, resumeThreadId(installName))
    }
  }, [tick, installName])
  return (
    <nav className="agent-hub-sidebar" aria-label="Work">
      {onNewChat ? (
        <button type="button" className="new-chat agent-hub-new-chat" onClick={onNewChat}>
          <span>＋&nbsp; {zh ? '新对话' : 'New chat'}</span>
          <kbd>Ctrl N</kbd>
        </button>
      ) : null}
      <button
        type="button"
        className="agent-hub-history-btn"
        onClick={() => onOpenHistory?.()}
      >
        <span>{zh ? '历史对话' : 'History'}</span>
        <small>{zh ? '全部 Agent 的会话' : 'Threads from every Agent'}</small>
      </button>
      <div className="agent-hub-agents">
        {rows.map(row => (
          <div key={row.name} className={`agent-hub-agent-row${selectedAgent === row.name ? ' is-on' : ''}`}>
            <button
              type="button"
              className="agent-hub-agent-select"
              aria-label={zh ? `打开 ${agentDisplayName(row.name)}` : `Open ${agentDisplayName(row.name)}`}
              aria-pressed={selectedAgent === row.name}
              onClick={() => openAgent(row.name, resumeThreadId(row.name))}
            >
              <span className="agent-hub-logo"><AgentHubMark name={row.name} /></span>
              <span className="agent-hub-agent-copy">
                <b>{agentDisplayName(row.name)}</b>
                <em>{jobLabel(row.name, row.agent, zh)}</em>
              </span>
              <span className={`agent-hub-lamp ${getInstallJob(row.name)?.status === 'running' ? 'installing' : (row.agent?.state ?? 'unknown')}`} aria-hidden="true" />
            </button>
            <button
              type="button"
              className="agent-hub-connect"
              disabled={connecting === row.name || getInstallJob(row.name)?.status === 'running'}
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
      <ConfirmDialog
        open={Boolean(installName)}
        danger={false}
        busy={installBusy}
        error={installError}
        title={zh ? `安装并连接 ${installName ? agentDisplayName(installName) : ''}` : `Install and connect ${installName ? agentDisplayName(installName) : ''}`}
        description={zh
          ? '先检查本机是否已有该 CLI。没有就在本机自动安装，再登录并连接。切换页面不会中断安装。'
          : 'Check for the local CLI first. Leaving this page will not stop the install.'}
        confirmLabel={installBusy ? (zh ? '处理中…' : 'Working…') : (zh ? '确定安装' : 'Install')}
        onCancel={() => { if (!installBusy) setInstallName(undefined) }}
        onConfirm={() => { if (!installBusy) void confirmInstall() }}
      />
    </nav>
  )
}
