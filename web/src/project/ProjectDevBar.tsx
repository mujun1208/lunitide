import React, { useEffect, useState } from 'react'
import { AgentHubThread } from '../agentHub/AgentHubThread'
import { agentHubApi, type AgentHubName, type AgentHubStatus, type AgentHubThreadDetail } from '../agentHub/agentHubApi'
import type { ProjectDTO } from '../generated/bridge'
import { projectSpineApi, type ProjectExecutor, type TaskBrief } from './projectSpineApi'

const EXECUTORS: Array<{ id: ProjectExecutor; label: string }> = [
  { id: 'lunitide', label: '月汐平台' },
  { id: 'cursor', label: 'Cursor 对接' },
  { id: 'codex', label: 'Codex 对接' },
]

function agentHint(agents: AgentHubStatus[], name: ProjectExecutor): string {
  if (name === 'lunitide') return ''
  const found = agents.find(item => item.name === name)
  if (!found || found.state !== 'available') return '未检测到，装好并登录后可选'
  return ''
}

export function hubWorkspaceRoot(project: Pick<ProjectDTO, 'rootPath'>): string {
  return (project.rootPath ?? '').trim()
}

export function ProjectDevBar({
  project,
  sessionId,
  currentBrief,
  currentItemId,
  hubThreadId,
  lastAssistant,
  interfaceReady,
  onProjectUpdated,
  onTaskOpened,
  onHubThread,
}: {
  project: ProjectDTO
  sessionId?: string
  currentBrief?: string
  currentItemId?: string
  hubThreadId?: string
  lastAssistant?: string
  interfaceReady?: boolean
  onProjectUpdated?: (project: ProjectDTO) => void
  onTaskOpened?: (brief: TaskBrief, executor: ProjectExecutor) => void
  onHubThread?: (threadId: string) => void
}): React.JSX.Element {
  const [agents, setAgents] = useState<AgentHubStatus[]>([])
  const [executor, setExecutor] = useState<ProjectExecutor>(project.defaultExecutor ?? 'lunitide')
  const [summary, setSummary] = useState('')
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  const [surface, setSurface] = useState<'task' | 'hub'>('task')
  const [hubDetail, setHubDetail] = useState<AgentHubThreadDetail>()
  const [selfTestPass, setSelfTestPass] = useState(false)
  const hardBlocked = project.dbStatus !== 'ready' || interfaceReady === false

  useEffect(() => {
    void agentHubApi.detect().then(got => setAgents(got.agents ?? [])).catch(() => setAgents([]))
  }, [])
  useEffect(() => {
    setExecutor(project.defaultExecutor ?? 'lunitide')
  }, [project.defaultExecutor])
  useEffect(() => {
    setSummary('')
    setSelfTestPass(false)
  }, [currentItemId])
  useEffect(() => {
    if (hubThreadId) return
    const excerpt = (lastAssistant ?? '').trim()
    if (!excerpt) return
    setSummary(current => current.trim() ? current : excerpt.slice(0, 2000))
  }, [lastAssistant, hubThreadId, currentItemId])
  useEffect(() => {
    if (!hubThreadId) {
      setHubDetail(undefined)
      return
    }
    let cancelled = false
    const pull = () => {
      void agentHubApi.threadGet({ threadId: hubThreadId }).then(got => {
        if (cancelled) return
        setHubDetail(got)
        setSummary(current => current.trim() ? current : lastHubSummary(got))
      }).catch(() => {
        if (!cancelled) setHubDetail(undefined)
      })
    }
    pull()
    const timer = window.setInterval(pull, 4000)
    return () => {
      cancelled = true
      window.clearInterval(timer)
    }
  }, [hubThreadId])

  const choose = async (next: ProjectExecutor) => {
    if (busy || next === executor) return
    const hint = agentHint(agents, next)
    if (hint) {
      setError(hint)
      return
    }
    setBusy(true)
    setError('')
    try {
      const saved = await projectSpineApi.executorSet({ id: project.id, version: project.version, executor: next })
      setExecutor(next)
      onProjectUpdated?.(saved)
    } catch (e) {
      setError(e instanceof Error ? e.message : '无法设置执行器')
    } finally {
      setBusy(false)
    }
  }

  const openHub = async (brief: TaskBrief) => {
    if (executor === 'lunitide') return
    const created = await agentHubApi.threadCreate({
      harnessId: executor as AgentHubName,
      scene: 'write_project',
      projectId: project.id,
      workspaceRoot: hubWorkspaceRoot(project),
      title: `${brief.itemId} ${brief.title}`.slice(0, 200),
      accessMode: 'approval',
    })
    onHubThread?.(created.thread.threadId)
    setSurface('hub')
    if (brief.text) {
      await agentHubApi.threadPrompt({ threadId: created.thread.threadId, text: brief.text })
    }
  }

  const hubFaulted = isHubFaulted(hubDetail?.thread.status)
  const reportAndComplete = async (complete: boolean) => {
    if (!currentItemId || busy) return
    const text = summary.trim()
    if (!text) {
      setError(hubFaulted ? '请填写失败原因' : '请先填写本轮结果摘要')
      return
    }
    if (complete && !hubFaulted && !selfTestPass) {
      setError('请先勾选：已对照功能详细设计自测通过')
      return
    }
    setBusy(true)
    setError('')
    try {
      await projectSpineApi.taskReport({
        projectId: project.id,
        itemId: currentItemId,
        summary: text,
        executor,
        hubThreadId,
      })
      if (complete && !hubFaulted) {
        await projectSpineApi.taskComplete({ projectId: project.id, itemId: currentItemId, selfTestPass: true })
      }
      setSummary('')
    } catch (e) {
      setError(e instanceof Error ? e.message : '回写失败')
    } finally {
      setBusy(false)
    }
  }

  return (
    <section className="checklist-panel" aria-label="开发执行器">
      <header className="checklist-head">
        <div>
          <b>开发执行器</b>
          <small>{currentItemId ? `当前任务 ${currentItemId}` : '先在清单里点进入开发'}</small>
        </div>
        <div className="checklist-actions">
          {EXECUTORS.map(item => {
            const hint = agentHint(agents, item.id)
            return (
              <button
                key={item.id}
                type="button"
                className={executor === item.id ? 'primary' : undefined}
                disabled={busy || !!hint}
                title={hint}
                onClick={() => void choose(item.id)}
              >
                {item.label}{hint ? `（${hint}）` : ''}
              </button>
            )
          })}
          {hubThreadId && (
            <button type="button" onClick={() => setSurface(v => v === 'hub' ? 'task' : 'hub')}>
              {surface === 'hub' ? '回清单' : '外脑'}
            </button>
          )}
        </div>
      </header>
      {error && <p className="error" role="alert"><b>{error}</b></p>}
      {hardBlocked && <p className="error" role="status">先完成库表核齐与接口阶段确认，才能开始开发任务。</p>}
      {currentBrief && <pre className="checklist-note">{currentBrief}</pre>}
      {currentItemId && (
        <div className="checklist-actions">
          <textarea
            className="pm-reason"
            rows={2}
            maxLength={2000}
            value={summary}
            onChange={e => setSummary(e.target.value)}
            placeholder={hubFaulted ? '外脑失败，填写原因后回写（不会标完成）' : '回写结果摘要（确认后才算完成）'}
          />
          <label className="gate-note">
            <input type="checkbox" checked={selfTestPass} onChange={e => setSelfTestPass(e.target.checked)} />
            已对照功能详细设计自测通过
          </label>
          <button type="button" className="primary" disabled={busy || (hardBlocked && !hubFaulted)} onClick={() => void reportAndComplete(!hubFaulted)}>
            {hubFaulted ? '回写失败原因' : '回写结果并完成'}
          </button>
        </div>
      )}
      {surface === 'hub' && hubThreadId && <AgentHubThread threadId={hubThreadId} />}
      <span hidden>{sessionId}{onTaskOpened ? '1' : ''}</span>
    </section>
  )
}

function isHubFaulted(status?: string): boolean {
  return status === 'faulted' || status === 'failed'
}

function lastHubSummary(detail: AgentHubThreadDetail): string {
  const messages = [...(detail.messages ?? [])].reverse()
  const hit = messages.find(item => item.role === 'assistant' || item.role === 'notice' || item.role === 'system')
  const text = (hit?.content ?? '').trim()
  return text.slice(0, 2000)
}

export async function openProjectTask(
  project: ProjectDTO,
  itemId: string,
  executor?: ProjectExecutor,
  sessionId?: string,
): Promise<{ brief: TaskBrief; executor: ProjectExecutor; hubThreadId?: string }> {
  return projectSpineApi.taskOpen({
    projectId: project.id,
    itemId,
    ...(executor ? { executor } : {}),
    workSessionId: sessionId,
  })
}
