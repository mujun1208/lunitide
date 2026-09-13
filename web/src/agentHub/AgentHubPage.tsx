import React, { useCallback, useEffect, useRef, useState } from 'react'
import { useZh } from '../i18n/language'
import { AgentHubDetail } from './AgentHubDetail'
import { AgentHubHome } from './AgentHubHome'
import { AgentHubTasks } from './AgentHubTasks'
import { AgentHubThread } from './AgentHubThread'
import './agentHub.css'
import { agentHubApi, type AgentHubArtifact, type AgentHubCounts, type AgentHubStatus, type AgentHubTask } from './agentHubApi'

type HubTab = 'tasks' | 'detail'

export function AgentHubPage({
  selectedThreadId,
  newThreadNonce = 0,
  onOpenThread,
}: {
  selectedThreadId?: string
  newThreadNonce?: number
  onOpenThread?: (threadId: string) => void
} = {}): React.JSX.Element {
  const zh = useZh()
  const [legacy, setLegacy] = useState(false)
  const [localThreadId, setLocalThreadId] = useState<string>()
  const nonceSeen = useRef(newThreadNonce)
  useEffect(() => {
    if (newThreadNonce === nonceSeen.current) return
    nonceSeen.current = newThreadNonce
    setLocalThreadId(undefined)
    setLegacy(false)
  }, [newThreadNonce])
  useEffect(() => {
    if (selectedThreadId) setLegacy(false)
  }, [selectedThreadId])
  const threadId = onOpenThread ? selectedThreadId : (selectedThreadId ?? localThreadId)
  const openThread = (id: string) => {
    setLocalThreadId(id)
    setLegacy(false)
    onOpenThread?.(id)
  }
  const [tab, setTab] = useState<HubTab>('tasks')
  const [agents, setAgents] = useState<AgentHubStatus[]>([])
  const [tasks, setTasks] = useState<AgentHubTask[]>([])
  const [counts, setCounts] = useState<AgentHubCounts>({ queued: 0, running: 0, success: 0, failed: 0 })
  const [artifacts, setArtifacts] = useState<AgentHubArtifact[]>([])
  const [taskId, setTaskId] = useState('')
  const [banner, setBanner] = useState<{ taskId: string; title: string }>()
  const [error, setError] = useState('')
  const listsBusy = useRef(false)
  const detectBusy = useRef(false)
  const refreshLists = useCallback(async () => {
    if (listsBusy.current) return
    listsBusy.current = true
    try {
      const [listed, files] = await Promise.all([
        agentHubApi.list(),
        agentHubApi.listArtifacts(),
      ])
      setTasks(listed.items ?? [])
      setCounts(listed.counts ?? { queued: 0, running: 0, success: 0, failed: 0 })
      setArtifacts(files.items ?? [])
      setError('')
    } catch (err) {
      setError(err instanceof Error && /[\u4e00-\u9fff]/.test(err.message) ? err.message : (zh ? '调度台暂时不可用。' : 'Agent Hub is unavailable.'))
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
  return (
    <div className="agent-hub">
      <header className="agent-hub-head">
        <div>
          <h1>{zh ? 'Agent 调度台' : 'Agent Hub'}</h1>
          <p className="agent-hub-quota">{zh ? '消耗的是该 CLI 自己的会员额度' : 'Usage is billed to that CLI subscription, not Lunitide.'}</p>
        </div>
        <button type="button" aria-pressed={legacy} onClick={() => setLegacy(value => !value)}>{zh ? '旧版任务' : 'Legacy tasks'}</button>
        {legacy && !threadId && (
          <nav className="agent-hub-tabs" aria-label={zh ? '调度台页面' : 'Agent Hub pages'}>
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
        <AgentHubHome onOpened={openThread} />
      )}
    </div>
  )
}

export default AgentHubPage
