import React, { useEffect, useRef, useState } from 'react'
import { useZh } from '../i18n/language'
import { AgentHubFileInspector } from './AgentHubFileInspector'
import { agentHubApi, type AgentHubArtifact, type AgentHubEvent, type AgentHubPreview, type AgentHubTaskDetail } from './agentHubApi'
import { mergeEvents, pptDeckMissing, shortWorkDir, statusLabel, taskElapsed, visibleHubArtifacts } from './agentHubCopy'

export function AgentHubDetail({
  taskId,
  onBanner,
}: {
  taskId?: string
  onBanner?: (taskId: string, title: string) => void
}): React.JSX.Element {
  const zh = useZh()
  const [detail, setDetail] = useState<AgentHubTaskDetail>()
  const [preview, setPreview] = useState<AgentHubPreview>()
  const [showScan, setShowScan] = useState(false)
  const [error, setError] = useState('')
  const seen = useRef(new Set<number>())
  const announced = useRef('')
  useEffect(() => {
    seen.current = new Set()
    announced.current = ''
    setDetail(undefined)
    setPreview(undefined)
    setError('')
  }, [taskId])
  useEffect(() => {
    if (!taskId) return
    let alive = true
    const pull = async () => {
      try {
        const next = await agentHubApi.get({ taskId })
        if (!alive) return true
        setDetail(current => {
          const events = mergeEvents(current?.events ?? [], next?.events ?? [])
          events.forEach(item => seen.current.add(item.seq))
          return { ...next, events }
        })
        const done = next.task.status === 'success' || next.task.status === 'failed' || next.task.status === 'timeout' || next.task.status === 'cancelled'
        if (done && announced.current !== next.task.taskId) {
          announced.current = next.task.taskId
          onBanner?.(next.task.taskId, next.task.prompt)
        }
        setError('')
        return done
      } catch (err) {
        if (alive) setError(err instanceof Error && /[\u4e00-\u9fff]/.test(err.message) ? err.message : (zh ? '任务详情读取失败。' : 'Could not load this task.'))
        return false
      }
    }
    const tick = async () => {
      const done = await pull()
      if (!alive || done) return
      timer = window.setTimeout(() => { void tick() }, 400)
    }
    let timer = 0
    void tick()
    return () => { alive = false; window.clearTimeout(timer) }
  }, [taskId, onBanner, zh])
  if (!taskId) return <p className="agent-hub-hint">{zh ? '从工作台或任务中心打开一条任务。' : 'Open a task from the workbench or task list.'}</p>
  if (!detail) return <p role="status">{error || (zh ? '正在读取任务…' : 'Loading task…')}</p>
  const { task } = detail
  const artifacts = visibleHubArtifacts(detail.artifacts, showScan)
  const done = task.status === 'success' || task.status === 'failed' || task.status === 'timeout' || task.status === 'cancelled'
  const noDeck = done && pptDeckMissing(task.agent, task.prompt, detail.artifacts)
  const tokens = task.tokensUsed > 0 ? String(task.tokensUsed) : (zh ? 'CLI 未回报' : 'CLI did not report tokens')
  return (
    <section>
      <div className="agent-hub-detail-head">
        <div>
          <h2 className="dh-title">{task.prompt}</h2>
          <p className="agent-hub-hint">{task.agent} · {statusLabel(task.status, zh)} · {taskElapsed(task, Date.now())} · {tokens}{task.exitCode != null ? ` · exit ${task.exitCode}` : ''} · {shortWorkDir(task.workDir)}</p>
          {task.errorMsg && <p className="agent-hub-error">{task.errorMsg}</p>}
          {noDeck && <p className="agent-hub-hint">{zh ? '没有文稿。打开目录查看本轮文件，或看时间线说明。' : 'No deck was produced. Open the folder or read the timeline.'}</p>}
        </div>
        <div className="agent-hub-actions">
          {(task.status === 'running' || task.status === 'queued') && (
            <button type="button" onClick={() => void agentHubApi.cancel({ taskId: task.taskId })}>{zh ? '取消' : 'Cancel'}</button>
          )}
          <button type="button" onClick={() => void agentHubApi.open({ taskId: task.taskId, reveal: true })}>{zh ? '打开目录' : 'Open folder'}</button>
        </div>
      </div>
      <div className="agent-hub-detail-grid">
        <article className="agent-hub-panel">
          <h3>{zh ? '时间线' : 'Timeline'}</h3>
          <Timeline events={detail.events} />
        </article>
        <article className="agent-hub-panel">
          <h3>{zh ? '产物' : 'Artifacts'}</h3>
          <label>
            <input
              type="checkbox"
              aria-label="显示目录内其它文件"
              checked={showScan}
              onChange={event => setShowScan(event.target.checked)}
            />
            {zh ? '显示目录内其它文件' : 'Show other files in folder'}
          </label>
          <div>
            {artifacts.map(item => (
              <button key={item.path} type="button" className="agent-hub-art" onClick={() => void openPreview(task.taskId, item, setPreview)}>
                <span><b>{item.name}</b>{item.path !== item.name && <small>{item.path}</small>}</span>
                <small>{item.source}</small>
              </button>
            ))}
            {artifacts.length === 0 && <p className="agent-hub-hint">{zh ? '还没有产物。' : 'No artifacts yet.'}</p>}
          </div>
        </article>
      </div>
      <AgentHubFileInspector preview={preview} onOpen={() => { if (preview) void agentHubApi.open({ taskId: task.taskId, path: preview.path }) }} />
    </section>
  )
}

function Timeline({ events }: { events: AgentHubEvent[] }): React.JSX.Element {
  return (
    <div className="agent-hub-tl">
      <ul>
        {events.map(item => (
          <li key={item.seq}>
            <i className="agent-hub-dot" />
            <span><b>{item.title || item.type}</b><small>{item.detail}</small></span>
            <time>{item.ts.slice(11, 19)}</time>
          </li>
        ))}
      </ul>
    </div>
  )
}

async function openPreview(taskId: string, item: AgentHubArtifact, setPreview: (value: AgentHubPreview) => void): Promise<void> {
  setPreview(await agentHubApi.preview({ taskId, path: item.path }))
}
