import React, { useCallback, useEffect, useMemo, useState } from 'react'
import { planBridge, reviewBridge, type PlanBridge, type ReviewBridge } from '../bridge/client'
import type { PlanDTO, PlanNodeDTO, PlanStatus, NodeStatus, ReviewDTO } from '../generated/bridge'

const PLAN_STATUS: Record<PlanStatus, string> = {
  draft: '草稿', active: '执行中', paused: '已暂停', completed: '已完成', cancelled: '已取消', failed: '已失败',
}
const NODE_STATUS: Record<NodeStatus, string> = {
  pending: '等待中', ready: '就绪', running: '执行中', paused: '已暂停', completed: '已完成',
  failed: '已失败', cancelled: '已取消', blocked: '等待审批',
}

const nodeCardClass = (status: NodeStatus, ready: boolean): string => {
  if (status === 'completed') return 'done'
  if (status === 'running') return 'running'
  if (status === 'blocked' || status === 'paused') return 'approval'
  if (ready) return 'ready'
  return 'waiting'
}

function phaseGroups(nodes: PlanNodeDTO[]): Array<{ title: string; hint: string; nodes: PlanNodeDTO[] }> {
  if (!nodes.length) return []
  const sorted = [...nodes].sort((a, b) => a.sequence - b.sequence || a.name.localeCompare(b.name))
  const size = Math.max(1, Math.ceil(sorted.length / 3))
  const titles = ['准备上下文', '生成与验证', '保存运行结果']
  const hints = ['计划、底座、记忆与技能就绪', '生成布局与校验，含人工审批节点', '验证通过后自动持久化结果']
  return [0, 1, 2].map(index => ({
    title: titles[index] ?? `阶段 ${index + 1}`,
    hint: hints[index] ?? '',
    nodes: sorted.slice(index * size, (index + 1) * size),
  })).filter(group => group.nodes.length > 0)
}

export function PlanDagPanel({
  projectId,
  bridge = planBridge,
  reviews = reviewBridge,
  onOpenApproval,
}: {
  projectId: string
  bridge?: PlanBridge
  reviews?: ReviewBridge
  onOpenApproval?: () => void
}): React.JSX.Element {
  const [plans, setPlans] = useState<PlanDTO[]>([])
  const [planId, setPlanId] = useState('')
  const [nodes, setNodes] = useState<PlanNodeDTO[]>([])
  const [pendingReviews, setPendingReviews] = useState<ReviewDTO[]>([])
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')

  const plan = plans.find(p => p.id === planId)
  const groups = useMemo(() => phaseGroups(nodes), [nodes])
  const completed = nodes.filter(n => n.status === 'completed').length

  const refresh = useCallback(async (id: string) => {
    if (!id) { setNodes([]); setPendingReviews([]); return }
    try {
      const [nodeResult, reviewResult] = await Promise.all([
        bridge.listNodes({ planId: id }),
        reviews.list({ planId: id }).catch(() => ({ items: [] as ReviewDTO[] })),
      ])
      setNodes(nodeResult.items)
      setPendingReviews(reviewResult.items.filter(r => r.status === 'pending'))
      setError('')
    } catch (e) {
      setError(e instanceof Error ? e.message : '加载计划失败')
    }
  }, [bridge, reviews])

  useEffect(() => {
    if (!projectId) return
    void bridge.list({ projectId }).then(result => {
      setPlans(result.items)
      const active = result.items.find(p => p.status === 'active') ?? result.items[0]
      setPlanId(current => current || active?.id || '')
    }).catch(e => setError(e instanceof Error ? e.message : '计划列表加载失败'))
  }, [bridge, projectId])

  useEffect(() => { void refresh(planId) }, [planId, refresh])

  const planOp = async (op: 'activate' | 'pause' | 'resume' | 'complete') => {
    if (!planId || busy) return
    setBusy(true)
    try {
      if (op === 'activate') await bridge.activate({ planId })
      else if (op === 'pause') await bridge.pause({ planId })
      else if (op === 'resume') await bridge.resume({ planId })
      else await bridge.complete({ planId })
      const listed = await bridge.list({ projectId })
      setPlans(listed.items)
      await refresh(planId)
    } catch (e) {
      setError(e instanceof Error ? e.message : '操作失败')
    } finally {
      setBusy(false)
    }
  }

  const nodeReview = (nodeId?: string) => pendingReviews.find(r => r.nodeId === nodeId)

  if (!projectId) {
    return <div className="plan-dag-empty"><b>暂无项目上下文</b><span>进入项目工作台后可查看执行 DAG。</span></div>
  }

  return (
    <section className="plan-dag-panel" aria-label="计划 DAG">
      <header className="plan-dag-head">
        <div>
          <strong>{plan?.name ?? '执行计划'}</strong>
          <small>
            {plan ? `${PLAN_STATUS[plan.status]} · ${completed}/${nodes.length || '—'} 已完成` : '选择或创建计划'}
            {plan ? ` · v${plan.version}` : ''}
          </small>
        </div>
        <div className="plan-dag-actions">
          <select aria-label="选择计划" value={planId} onChange={e => setPlanId(e.target.value)}>
            {!plans.length && <option value="">暂无计划</option>}
            {plans.map(item => <option key={item.id} value={item.id}>{item.name}</option>)}
          </select>
          <button type="button" disabled={!planId || busy} onClick={() => void refresh(planId)}>运行详情</button>
          <button type="button" disabled={!planId || busy} onClick={() => void refresh(planId)}>重新规划</button>
          {plan?.status === 'active' && (
            <button type="button" className="primary" disabled={busy} onClick={() => void planOp('pause')}>暂停计划</button>
          )}
          {plan?.status === 'paused' && (
            <button type="button" className="primary" disabled={busy} onClick={() => void planOp('resume')}>恢复计划</button>
          )}
          {plan?.status === 'draft' && (
            <button type="button" className="primary" disabled={busy} onClick={() => void planOp('activate')}>启动计划</button>
          )}
        </div>
      </header>
      {error && <p className="error" role="alert"><b>{error}</b></p>}
      {!nodes.length ? (
        <div className="plan-dag-empty"><b>该计划还没有 DAG 节点</b><span>在开发/测试阶段从检查清单同步计划，或在设置 → 计划管理中创建节点。</span></div>
      ) : (
        <>
          <div className="plan-dag-phases">
            {groups.map(group => {
              const done = group.nodes.filter(n => n.status === 'completed').length
              return (
                <article key={group.title} className="plan-dag-phase-card">
                  <b>{group.title}</b>
                  <span>{done}/{group.nodes.length} 已完成</span>
                  <p>{group.hint}</p>
                </article>
              )
            })}
          </div>
          <div className="plan-dag-grid">
            {nodes.sort((a, b) => a.sequence - b.sequence).map(node => {
              const parent = node.parentNodeId ? nodes.find(n => n.id === node.parentNodeId) : undefined
              const ready = node.status === 'pending' && (!node.parentNodeId || parent?.status === 'completed')
              const cls = nodeCardClass(node.status, ready)
              const review = nodeReview(node.id)
              const dep = parent ? `dep: ${parent.sequence}. ${parent.name}` : undefined
              return (
                <article key={node.id} className={`plan-dag-node ${cls}`}>
                  <header>
                    <span className="plan-dag-node-num">{node.sequence}</span>
                    <span className="plan-dag-node-status">{NODE_STATUS[ready ? 'ready' : node.status]}</span>
                  </header>
                  <b>{node.name}</b>
                  {node.description && <p>{node.description}</p>}
                  <dl>
                    <div><dt>类型</dt><dd>{node.workerRole}</dd></div>
                    <div><dt>风险</dt><dd>{node.riskLevel}</dd></div>
                    {dep && <div><dt>依赖</dt><dd>{dep}</dd></div>}
                  </dl>
                  {(node.status === 'blocked' || review) && (
                    <button type="button" className="plan-dag-approval-link" onClick={onOpenApproval}>
                      打开审批 →
                    </button>
                  )}
                </article>
              )
            })}
          </div>
        </>
      )}
      <footer className="plan-dag-foot callout">
        <b>DAG 说明</b>：节点按 sequence 与 parent 依赖自动编排；高风险或跨域步骤会暂停并等待人工审批，批准后继续下游节点。
      </footer>
    </section>
  )
}
