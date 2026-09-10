import React, { useCallback, useEffect, useRef, useState } from 'react'
import {
  BridgeClientError,
  createMutationAttempt,
  planBridge as defaultPlanBridge,
  stageBridge as defaultStageBridge,
  type StageBridge,
  type PlanBridge,
} from '../bridge/client'
import type { PlanDTO, PlanRunDTO, ProjectDTO } from '../generated/bridge'
import { loadChecklistDoc } from './checklistStore'

function planPanelUserError(err: unknown, fallback: string): string {
  const detail = err instanceof Error ? err.message.trim() : ''
  return /[\u4e00-\u9fff]/.test(detail) ? detail : fallback
}
const problem = (e: unknown) =>
  e instanceof BridgeClientError
    ? e
    : new BridgeClientError(planPanelUserError(e, '请求失败'), 'CLIENT_ERROR', false, 'renderer')

const icon = (status: PlanRunDTO['status']) =>
  status === 'succeeded' ? '✓' : status === 'failed' || status === 'cancelled' ? '×' : '○'

export function ProjectPlanPanel({
  project,
  phase,
  checklistPhase,
  checklistType,
  checklistTitle,
  readOnly = false,
  bridge = defaultPlanBridge,
  stages = defaultStageBridge,
}: {
  project: ProjectDTO
  phase: number
  checklistPhase: number
  checklistType: string
  checklistTitle: string
  readOnly?: boolean
  bridge?: PlanBridge
  stages?: StageBridge
}): React.JSX.Element {
  const [plan, setPlan] = useState<PlanDTO | undefined>()
  const [nodeId, setNodeId] = useState('')
  const [runs, setRuns] = useState<PlanRunDTO[]>([])
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  const [note, setNote] = useState('')
  const [acknowledged, setAcknowledged] = useState<Record<string, boolean>>({})
  const scope = useRef(0)

  const refreshRuns = useCallback(async (planId: string, generation: number) => {
    const tree = await bridge.runTree({ planId })
    if (scope.current === generation) setRuns(tree.items)
  }, [bridge])

  useEffect(() => {
    let cancelled = false
	const generation = ++scope.current
    setPlan(undefined)
    setNodeId('')
    setRuns([])
    setError('')
    setNote('')
	setBusy(false)
    const load = async () => {
      const stageList = await stages.list({ projectId: project.id })
      if (cancelled) return
      let stage = stageList.items.find(item => item.phase === phase)
      if (!stage && !readOnly) {
        const payload = { projectId: project.id, phase, title: `阶段 ${phase}` }
        try {
          stage = await stages.create(payload, { attempt: createMutationAttempt('stage.create', payload) })
        } catch (error) {
          if (!(error instanceof BridgeClientError) || error.code !== 'STAGE_PHASE_CONFLICT') throw error
          stage = (await stages.list({ projectId: project.id })).items.find(item => item.phase === phase)
        }
      }
      if (cancelled || !stage) return
      const listed = await bridge.list({ projectId: project.id })
      if (cancelled) return
      let current = listed.items.find(item => item.stageId === stage.id)
      if (!current && !readOnly) {
        const payload = { projectId: project.id, stageId: stage.id, name: `${project.name} · 阶段${phase}计划`, description: checklistTitle }
        current = (await bridge.create(payload, { attempt: createMutationAttempt('plan.create', payload) })).plan
      }
      if (cancelled || !current) return
      const [nodes, tree] = await Promise.all([bridge.listNodes({ planId: current.id }), bridge.runTree({ planId: current.id })])
      if (cancelled) return
      setPlan(current)
      setNodeId(nodes.items.find(node => !node.parentNodeId)?.id ?? '')
      setRuns(tree.items)
    }
    void load().catch(error => { if (!cancelled) setError(problem(error).message) })
    return () => { cancelled = true; if (scope.current === generation) scope.current++ }
  }, [bridge, stages, checklistTitle, phase, project.id, project.name, readOnly])

  useEffect(() => {
    if (!plan || !runs.some(run => run.executionStatus === 'running' || run.executionStatus === 'cancel_requested')) return
    const generation = scope.current
    let cancelled = false
    let timer: ReturnType<typeof setTimeout> | undefined
    const poll = async () => {
      try {
        const tree = await bridge.runTree({ planId: plan.id })
        if (!cancelled && scope.current === generation) setRuns(tree.items)
      } catch (e) { if (!cancelled && scope.current === generation) setError(problem(e).message) }
      if (!cancelled) timer = setTimeout(() => { void poll() }, 1000)
    }
    timer = setTimeout(() => { void poll() }, 1000)
    return () => { cancelled = true; clearTimeout(timer) }
  }, [bridge, plan, runs])

  const execute = async (run: PlanRunDTO, action: 'start' | 'cancel' | 'retry') => {
    if (readOnly || busy || !plan) return
    const generation = scope.current
    setBusy(true)
    setError('')
    try {
      if (action === 'cancel') await bridge.cancelRun({ runId: run.id })
      else if (action === 'retry') {
        if (!run.executionVersion) throw new Error('执行版本缺失，请刷新后重试。')
        await bridge.retryRun({ runId: run.id, expectedVersion: run.executionVersion, acknowledgeUncertain: acknowledged[run.id] ?? false })
      } else {
        const current = await bridge.get({ id: plan.id })
        if (scope.current !== generation) return
        if (current.status === 'draft') await bridge.activate({ planId: plan.id })
        else if (current.status === 'paused') await bridge.resume({ planId: plan.id })
        if (scope.current !== generation) return
        await bridge.startRun({ runId: run.id })
      }
      await refreshRuns(plan.id, generation)
    } catch (e) { if (scope.current === generation) setError(problem(e).message) }
    finally { if (scope.current === generation) setBusy(false) }
  }

  const syncFromChecklist = async () => {
    if (readOnly || busy || !plan || !nodeId) return
	const generation = scope.current
    setBusy(true)
    setError('')
    try {
      const { doc } = await loadChecklistDoc(project.id, checklistPhase, checklistType)
	  if (scope.current !== generation) return
      if (!doc.items.length) {
        setError('当前清单为空，请先维护检查清单。')
        return
      }
      const existingTitles = new Set(runs.map(r => r.todo.title))
      let created = 0
      for (const item of doc.items) {
		if (scope.current !== generation) return
        const title = `${item.id} · ${item.title}`
        if (existingTitles.has(title)) continue
        await bridge.createTodo({
          planId: plan.id,
          nodeId,
          role: checklistType === 'test_checklist' ? 'tester' : 'implementer',
          title,
          description: item.module ?? item.priority ?? '',
        })
        created++
      }
      await refreshRuns(plan.id, generation)
      if (scope.current === generation) setNote(`已从${checklistTitle}同步 ${created} 条计划任务`)
    } catch (e) {
      if (scope.current === generation) setError(problem(e).message)
    } finally {
      if (scope.current === generation) setBusy(false)
    }
  }

  return (
    <section className="project-plan-panel" aria-label="工作计划">
      <header className="checklist-head">
        <div>
          <b>工作计划清单</b>
          <small>{runs.length ? `${runs.length} 条计划记录` : '暂无任务'}{note ? ` · ${note}` : ''}</small>
        </div>
        <div className="checklist-actions">
          {!readOnly && <button type="button" disabled={busy || !plan || !nodeId} onClick={() => void syncFromChecklist()}>从清单同步</button>}
        </div>
      </header>
      <p className="gate-note">启动后将在独立任务工作区执行文件操作和允许的命令，并使用所配置模型。只有工具回执与实际产物验证通过，任务才会完成；项目交付仍需阶段验收。</p>
      {error && <p className="error" role="alert"><b>{error}</b></p>}
      {runs.length ? (
        <ol className="plan-summary-list">
          {runs.map(r => (
            <li key={r.id} className={`status-${r.status}`} style={{ paddingLeft: `${r.depth * 16}px` }}>
              <div className="plan-summary-item">
                <span aria-hidden="true">{icon(r.status)}</span>
                <span><b>{r.todo.title}</b>{r.todo.description && <em>{r.todo.description}</em>}<small>{r.executionStatus ? ({running:'正在执行',cancel_requested:'正在停止',succeeded:'产物已验证',failed:'执行失败',cancelled:'已停止',interrupted:'执行已中断',outcome_unknown:'操作结果待核对'} as Record<string,string>)[r.executionStatus] : r.status === 'queued' ? '待执行' : '历史协调记录（未验证产物）'}</small>
                  {r.summary && <p>{r.summary}</p>}{r.failure && <p>{r.failure}</p>}
                  {r.artifacts?.map(artifact => <small key={artifact.path}>{artifact.path} · {artifact.bytes} 字节 · SHA256 {artifact.sha256}</small>)}
                </span>
                {!readOnly && <div className="checklist-actions">
                  {!r.executionStatus && r.status === 'queued' && <button type="button" disabled={busy} onClick={() => { void execute(r, 'start') }}>启动执行</button>}
                  {r.executionStatus === 'running' && <button type="button" disabled={busy} onClick={() => { void execute(r, 'cancel') }}>停止任务</button>}
                  {r.executionStatus === 'outcome_unknown' && <label><input type="checkbox" checked={acknowledged[r.id] ?? false} onChange={event => setAcknowledged(value => ({ ...value, [r.id]: event.target.checked }))} />已核对前次操作，允许新建任务执行</label>}
                  {r.executionVersion && ['failed','cancelled','interrupted','outcome_unknown'].includes(r.executionStatus ?? '') && <button type="button" disabled={busy || (r.executionStatus === 'outcome_unknown' && !acknowledged[r.id])} onClick={() => { void execute(r, 'retry') }}>重新执行</button>}
                </div>}
              </div>
            </li>
          ))}
        </ol>
      ) : (
        <p className="checklist-empty">{readOnly ? '此阶段暂无计划记录。' : '点击「从清单同步」将检查清单条目导入计划。'}</p>
      )}
    </section>
  )
}
