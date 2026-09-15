import React, { useCallback, useEffect, useRef, useState } from 'react'
import { useZh } from '../i18n/language'
import { AgentHubDetail } from './AgentHubDetail'
import { AgentHubHome } from './AgentHubHome'
import { AgentHubTasks } from './AgentHubTasks'
import { AgentHubThread } from './AgentHubThread'
import './agentHub.css'
import { agentDisplayName, stateLabel, usableLatestThreadForHarness } from './agentHubCopy'
import { agentHubApi, type AgentHubCounts, type AgentHubName, type AgentHubStatus, type AgentHubTask } from './agentHubApi'

type HubTab = 'tasks' | 'detail'

export function AgentHubPage({
  selectedThreadId,
  newThreadNonce = 0,
  onOpenThread,
  selectedAgent,
  onSelectAgent,
  legacyTaskId,
  showLegacy = false,
}: {
  selectedThreadId?: string
  newThreadNonce?: number
  onOpenThread?: (threadId: string) => void
  selectedAgent?: AgentHubName
  onSelectAgent?: (name: AgentHubName) => void
  legacyTaskId?: string
  showLegacy?: boolean
} = {}): React.JSX.Element {
  const zh = useZh()
  const [legacy, setLegacy] = useState(Boolean(legacyTaskId) || showLegacy)
  const [localThreadId, setLocalThreadId] = useState<string>()
  const nonceSeen = useRef(newThreadNonce)
  const skipAutoOpen = useRef(false)
  useEffect(() => {
    if (newThreadNonce === nonceSeen.current) return
    nonceSeen.current = newThreadNonce
    skipAutoOpen.current = true
    setLocalThreadId(undefined)
    setLegacy(false)
  }, [newThreadNonce])
  useEffect(() => {
    if (selectedThreadId) {
      setLegacy(false)
      return
    }
    if (legacyTaskId) {
      setTaskId(legacyTaskId)
      setTab('detail')
      setLegacy(true)
      return
    }
    setLegacy(Boolean(showLegacy))
    if (showLegacy) setTab('tasks')
  }, [selectedThreadId, showLegacy, legacyTaskId])
  const threadId = onOpenThread ? selectedThreadId : (selectedThreadId ?? localThreadId)
  const openThread = (id: string) => {
    if (!id) skipAutoOpen.current = true
    setLocalThreadId(id || undefined)
    setLegacy(false)
    onOpenThread?.(id)
  }
  const [threadHarness, setThreadHarness] = useState<AgentHubName>()
  useEffect(() => {
    if (threadId || legacy) return
    if (skipAutoOpen.current) {
      skipAutoOpen.current = false
      return
    }
    let alive = true
    const name = selectedAgent ?? 'cursor'
    void Promise.resolve(agentHubApi.threadList({}) ?? { items: [] }).then(got => {
      if (!alive) return
      const latest = usableLatestThreadForHarness(got?.items ?? [], name)
      if (latest) openThread(latest.threadId)
    }).catch(() => undefined)
    return () => { alive = false }
  }, [selectedAgent, threadId, legacy])
  useEffect(() => {
    if (!threadId) {
      setThreadHarness(undefined)
      return
    }
    let alive = true
    void agentHubApi.threadGet({ threadId }).then(got => {
      if (!alive) return
      const name = got.thread.harnessId
      if (name !== 'codex' && name !== 'cursor' && name !== 'kimi' && name !== 'loopback') return
      setThreadHarness(name)
      onSelectAgent?.(name)
    }).catch(() => undefined)
    return () => { alive = false }
  }, [threadId, onSelectAgent])
  const [tab, setTab] = useState<HubTab>('tasks')
  const [agents, setAgents] = useState<AgentHubStatus[]>([])
  const [tasks, setTasks] = useState<AgentHubTask[]>([])
  const [counts, setCounts] = useState<AgentHubCounts>({ queued: 0, running: 0, success: 0, failed: 0 })
  const [taskId, setTaskId] = useState('')
  const [banner, setBanner] = useState<{ taskId: string; title: string }>()
  const [error, setError] = useState('')
  const listsBusy = useRef(false)
  const detectBusy = useRef(false)
  const refreshLists = useCallback(async () => {
    if (listsBusy.current) return
    listsBusy.current = true
    try {
      const listed = await agentHubApi.list()
      setTasks(listed.items ?? [])
      setCounts(listed.counts ?? { queued: 0, running: 0, success: 0, failed: 0 })
      setError('')
    } catch (err) {
      setError(err instanceof Error && /[\u4e00-\u9fff]/.test(err.message) ? err.message : (zh ? 'AgentHub 暂时不可用。' : 'AgentHub is unavailable.'))
    } finally {
      listsBusy.current = false
    }
  }, [zh])
  const refreshDetect = useCallback(async () => {
    if (detectBusy.current) return
    detectBusy.current = true
    try {
      const detected = await agentHubApi.detect()
      setAgents(detected.agents ?? [])
    } catch {
      // Keep the last good detect result; a slow CLI --version must not blank the page.
    } finally {
      detectBusy.current = false
    }
  }, [])
  const seenLive = useRef(new Set<string>())
  useEffect(() => { void refreshDetect(); void refreshLists() }, [refreshDetect, refreshLists])
  useEffect(() => {
    const liveCount = tasks.filter(item => item.status === 'running' || item.status === 'queued').length
    const timer = window.setInterval(() => { void refreshLists() }, liveCount > 0 ? 400 : 4000)
    return () => window.clearInterval(timer)
  }, [refreshLists, tasks])
  useEffect(() => {
    const timer = window.setInterval(() => { void refreshDetect() }, 4000)
    return () => window.clearInterval(timer)
  }, [refreshDetect])
  useEffect(() => {
    for (const item of tasks) {
      if (item.status === 'running' || item.status === 'queued') seenLive.current.add(item.taskId)
    }
    for (const item of tasks) {
      if (!seenLive.current.has(item.taskId)) continue
      if (item.status === 'success' || item.status === 'failed' || item.status === 'timeout' || item.status === 'cancelled') {
        seenLive.current.delete(item.taskId)
        setBanner({ taskId: item.taskId, title: item.prompt })
      }
    }
  }, [tasks])
  const openTask = (id: string) => { setTaskId(id); setTab('detail'); setLegacy(true) }
  const titleId = threadHarness ?? selectedAgent ?? 'cursor'
  const current = agents.find(item => item.name === titleId)
  return (
    <div className="agent-hub">
      <header className="agent-hub-head">
        <div className="agent-hub-title">
          <span className={`agent-hub-lamp ${current?.state ?? 'unknown'}`} aria-hidden="true" />
          <h1>{agentDisplayName(titleId)}</h1>
          <small>{stateLabel(current?.state ?? 'unknown', zh)}</small>
        </div>
        {threadId ? (
          <div className="agent-hub-thread-nav">
            <button type="button" onClick={() => openThread('')}>{zh ? '返回' : 'Back'}</button>
            <button type="button" onClick={() => openThread('')}>{zh ? '新对话' : 'New chat'}</button>
          </div>
        ) : null}
        {legacy && !threadId && (
          <nav className="agent-hub-tabs" aria-label={zh ? 'AgentHub 页面' : 'AgentHub pages'}>
            {([
              ['tasks', zh ? '任务中心' : 'Tasks'],
              ['detail', zh ? '任务详情' : 'Detail'],
            ] as const).map(([id, label]) => (
              <button key={id} type="button" role="tab" aria-selected={tab === id} onClick={() => setTab(id)}>{label}</button>
            ))}
          </nav>
        )}
      </header>
      {banner && (
        <button type="button" className="agent-hub-banner" onClick={() => openTask(banner.taskId)}>
          <b>{zh ? '任务已结束' : 'Task finished'}</b>
          <span>{banner.title}</span>
        </button>
      )}
      {error && <p className="agent-hub-error" role="alert">{error}</p>}
      {agents.length === 0 && !error && <p className="agent-hub-hint" role="status">{zh ? '正在探测本机 CLI…' : 'Looking for local CLIs…'}</p>}
      {threadId ? (
        <AgentHubThread threadId={threadId} />
      ) : legacy ? (
        <>
          {tab === 'tasks' && <AgentHubTasks items={tasks} counts={counts} onOpened={openTask} onChanged={() => void refreshLists()} />}
          {tab === 'detail' && <AgentHubDetail taskId={taskId || undefined} onBanner={(id, title) => setBanner({ taskId: id, title })} />}
        </>
      ) : (
        <AgentHubHome onOpened={openThread} selectedAgent={selectedAgent} onSelectAgent={onSelectAgent} />
      )}
    </div>
  )
}

export default AgentHubPage
