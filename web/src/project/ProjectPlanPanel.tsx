import React, { useCallback, useEffect, useState } from 'react'
import {
  BridgeClientError,
  createMutationAttempt,
  planBridge as defaultPlanBridge,
  type PlanBridge,
} from '../bridge/client'
import type { PlanDTO, PlanRunDTO, ProjectDTO } from '../generated/bridge'
import { loadChecklistDoc, saveChecklistDoc } from './checklistStore'
import type { ChecklistDoc } from './checklistTypes'

const problem = (e: unknown) =>
  e instanceof BridgeClientError
    ? e
    : new BridgeClientError(e instanceof Error ? e.message : '请求失败', 'CLIENT_ERROR', false, 'renderer')

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
}: {
  project: ProjectDTO
  phase: number
  checklistPhase: number
  checklistType: string
  checklistTitle: string
  readOnly?: boolean
  bridge?: PlanBridge
}): React.JSX.Element {
  const [plan, setPlan] = useState<PlanDTO | undefined>()
  const [nodeId, setNodeId] = useState('')
  const [runs, setRuns] = useState<PlanRunDTO[]>([])
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  const [note, setNote] = useState('')

  const refreshRuns = useCallback(async (planId: string) => {
    const tree = await bridge.runTree({ planId })
    setRuns(tree.items)
  }, [bridge])

  const ensurePlan = useCallback(async () => {
    const listed = await bridge.list({ projectId: project.id })
    let current = listed.items.find(p => p.name.includes(`阶段${phase}`)) ?? listed.items[0]
    if (!current) {
      const payload = { projectId: project.id, name: `${project.name} · 阶段${phase}计划`, description: checklistTitle }
      const created = await bridge.create(payload, { attempt: createMutationAttempt('plan.create', payload) })
      current = created.plan
      await bridge.activate({ planId: current.id })
    }
    setPlan(current)
    const nodes = await bridge.listNodes({ planId: current.id })
    let root = nodes.items[0]
    if (!root) {
      const payload = { planId: current.id, name: 'Root', description: '', riskLevel: 'low' as const, workerRole: 'planner', sequence: 0 }
      root = (await bridge.createNode(payload, { attempt: createMutationAttempt('node.create', payload) })).node
    }
    setNodeId(root.id)
    await refreshRuns(current.id)
  }, [bridge, checklistTitle, phase, project.id, project.name, refreshRuns])

  useEffect(() => { void ensurePlan().catch(e => setError(problem(e).message)) }, [ensurePlan])

  const syncFromChecklist = async () => {
    if (readOnly || busy || !plan || !nodeId) return
    setBusy(true)
    setError('')
    try {
      const { doc } = await loadChecklistDoc(project.id, checklistPhase, checklistType)
      if (!doc.items.length) {
        setError('当前清单为空，请先维护检查清单。')
        return
      }
      const existingTitles = new Set(runs.map(r => r.todo.title))
      let created = 0
      for (const item of doc.items) {
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
      await refreshRuns(plan.id)
      setNote(`已从${checklistTitle}同步 ${created} 条计划任务`)
    } catch (e) {
      setError(problem(e).message)
    } finally {
      setBusy(false)
    }
  }

  const applyRunStatusToChecklist = async () => {
    if (readOnly || busy || !plan) return
    setBusy(true)
    setError('')
    try {
      const { doc } = await loadChecklistDoc(project.id, checklistPhase, checklistType)
      const next: ChecklistDoc = {
        version: 1,
        items: doc.items.map(item => {
          const run = runs.find(r => r.todo.title.startsWith(`${item.id} ·`))
          if (!run) return item
          if (checklistType === 'test_checklist') {
            if (run.status === 'succeeded') return { ...item, status: 'test_pass' as const }
            if (run.status === 'failed') return { ...item, status: 'test_fail' as const }
            if (run.status === 'running') return { ...item, status: 'in_progress' as const }
          } else if (checklistType === 'dev_checklist') {
            if (run.status === 'succeeded') return { ...item, status: 'dev_done' as const }
            if (run.status === 'running') return { ...item, status: 'in_progress' as const }
          }
          return item
        }),
      }
      await saveChecklistDoc(project, checklistPhase, checklistType, checklistTitle, next)
      setNote('计划状态已回写检查清单')
    } catch (e) {
      setError(problem(e).message)
    } finally {
      setBusy(false)
    }
  }

  return (
    <section className="project-plan-panel" aria-label="工作计划">
      <header className="checklist-head">
        <div>
          <b>工作计划清单</b>
          <small>{runs.length ? `${runs.filter(r => r.status === 'succeeded').length}/${runs.length} 已完成` : '暂无任务'}{note ? ` · ${note}` : ''}</small>
        </div>
        <div className="checklist-actions">
          {!readOnly && <button type="button" disabled={busy} onClick={() => void syncFromChecklist()}>从清单同步</button>}
          {!readOnly && <button type="button" disabled={busy || !runs.length} onClick={() => void applyRunStatusToChecklist()}>回写清单状态</button>}
        </div>
      </header>
      {error && <p className="error" role="alert"><b>{error}</b></p>}
      {runs.length ? (
        <ol className="plan-summary-list">
          {runs.map(r => (
            <li key={r.id} className={`status-${r.status}`} style={{ paddingLeft: `${r.depth * 16}px` }}>
              <div className="plan-summary-item">
                <span aria-hidden="true">{icon(r.status)}</span>
                <span><b>{r.todo.title}</b>{r.todo.description && <em>{r.todo.description}</em>}<small>{r.status}</small></span>
              </div>
            </li>
          ))}
        </ol>
      ) : (
        <p className="checklist-empty">点击「从清单同步」将检查清单条目导入计划。</p>
      )}
    </section>
  )
}
