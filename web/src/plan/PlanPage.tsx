import React, { useEffect, useState, useCallback } from 'react'
import { planBridge, type PlanBridge } from '../bridge/client'
import type { PlanDTO, PlanNodeDTO, PlanStatus, NodeStatus, RiskLevel } from '../generated/bridge'

const PLAN_STATUS_LABELS: Record<PlanStatus, string> = {
  draft: '草稿', active: '执行中', paused: '已暂停', completed: '已完成', cancelled: '已取消', failed: '已失败'
}
const NODE_STATUS_LABELS: Record<NodeStatus, string> = {
  pending: '等待中', ready: '就绪', running: '执行中', paused: '已暂停', completed: '已完成', failed: '已失败', cancelled: '已取消', blocked: '已阻塞'
}
const RISK_LABELS: Record<RiskLevel, string> = { low: '低', medium: '中', high: '高', critical: '极高' }
const RISK_OPTIONS: RiskLevel[] = ['low', 'medium', 'high', 'critical']

const statusColor = (s: string): string => {
  if (['active', 'running', 'ready', 'completed'].includes(s)) return '#34d399'
  if (['paused', 'pending', 'draft'].includes(s)) return '#fbbf24'
  if (['failed', 'blocked', 'cancelled'].includes(s)) return '#f87171'
  return '#8fa3bf'
}
const riskColor = (r: RiskLevel): string => r === 'low' ? '#34d399' : r === 'medium' ? '#60a5fa' : r === 'high' ? '#fbbf24' : '#f87171'

const inputStyle: React.CSSProperties = { width: '100%', padding: '6px 8px', backgroundColor: '#0a0e1a', color: '#e5e7eb', border: '1px solid #334155', borderRadius: '4px', boxSizing: 'border-box' }
const btnStyle: React.CSSProperties = { padding: '6px 12px', backgroundColor: '#1e293b', color: '#e5e7eb', border: '1px solid #334155', borderRadius: '4px', cursor: 'pointer' }
const primaryBtnStyle: React.CSSProperties = { ...btnStyle, backgroundColor: '#2563eb', borderColor: '#3b82f6' }

export function PlanPage({ projectId, bridge = planBridge }: { projectId: string; bridge?: PlanBridge }): React.JSX.Element {
  const [plans, setPlans] = useState<PlanDTO[]>([])
  const [selectedPlan, setSelectedPlan] = useState<PlanDTO | null>(null)
  const [nodes, setNodes] = useState<PlanNodeDTO[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string>()
  const [busy, setBusy] = useState(false)

  const [showCreatePlan, setShowCreatePlan] = useState(false)
  const [planName, setPlanName] = useState('')
  const [planDescription, setPlanDescription] = useState('')

  const [showCreateNode, setShowCreateNode] = useState(false)
  const [nodeName, setNodeName] = useState('')
  const [nodeDescription, setNodeDescription] = useState('')
  const [nodeRiskLevel, setNodeRiskLevel] = useState<RiskLevel>('low')
  const [nodeWorkerRole, setNodeWorkerRole] = useState('')
  const [nodeSequence, setNodeSequence] = useState('1')

  const loadPlans = useCallback(async () => {
    if (!projectId) { setLoading(false); return }
    setLoading(true); setError(undefined)
    try { const r = await bridge.list({ projectId }); setPlans(r.items) }
    catch (e) { setError(e instanceof Error ? e.message : '加载失败') }
    finally { setLoading(false) }
  }, [projectId, bridge])

  const loadNodes = useCallback(async (planId: string) => {
    try { const r = await bridge.listNodes({ planId }); setNodes(r.items) }
    catch (e) { setError(e instanceof Error ? e.message : '加载节点失败') }
  }, [bridge])

  useEffect(() => { loadPlans() }, [loadPlans])
  useEffect(() => { if (selectedPlan) loadNodes(selectedPlan.id); else setNodes([]) }, [selectedPlan, loadNodes])

  const doPlanOp = async (op: 'activate' | 'pause' | 'resume' | 'complete', planId: string) => {
    setBusy(true); setError(undefined)
    try {
      if (op === 'activate') await bridge.activate({ planId })
      else if (op === 'pause') await bridge.pause({ planId })
      else if (op === 'resume') await bridge.resume({ planId })
      else await bridge.complete({ planId })
      await loadPlans()
      if (selectedPlan?.id === planId) await loadNodes(planId)
    } catch (e) { setError(e instanceof Error ? e.message : '操作失败') }
    finally { setBusy(false) }
  }

  const doNodeOp = async (op: 'startNode' | 'completeNode' | 'failNode', nodeId: string) => {
    if (!selectedPlan) return
    setBusy(true); setError(undefined)
    try {
      if (op === 'startNode') await bridge.startNode({ nodeId })
      else if (op === 'completeNode') await bridge.completeNode({ nodeId })
      else await bridge.failNode({ nodeId })
      await loadNodes(selectedPlan.id)
    } catch (e) { setError(e instanceof Error ? e.message : '操作失败') }
    finally { setBusy(false) }
  }

  const doCreatePlan = async (e: React.FormEvent) => {
    e.preventDefault()
    if (!projectId || !planName.trim()) return
    setBusy(true); setError(undefined)
    try {
      await bridge.create({ projectId, name: planName.trim(), description: planDescription })
      setPlanName(''); setPlanDescription(''); setShowCreatePlan(false)
      await loadPlans()
    } catch (e) { setError(e instanceof Error ? e.message : '创建失败') }
    finally { setBusy(false) }
  }

  const doCreateNode = async (e: React.FormEvent) => {
    e.preventDefault()
    if (!selectedPlan || !nodeName.trim()) return
    setBusy(true); setError(undefined)
    try {
      await bridge.createNode({
        planId: selectedPlan.id, name: nodeName.trim(), description: nodeDescription,
        riskLevel: nodeRiskLevel, workerRole: nodeWorkerRole.trim() || 'worker',
        sequence: Number(nodeSequence) || 1,
      })
      setNodeName(''); setNodeDescription(''); setNodeWorkerRole(''); setNodeSequence('1'); setNodeRiskLevel('low')
      setShowCreateNode(false)
      await loadNodes(selectedPlan.id)
    } catch (e) { setError(e instanceof Error ? e.message : '创建失败') }
    finally { setBusy(false) }
  }

  if (!projectId) {
    return <div className="shell"><div className="empty"><b>请先选择项目</b><span>在项目总览中选择一个项目后即可管理计划。</span></div></div>
  }

  const panelStyle: React.CSSProperties = { border: '1px solid #1f2937', borderRadius: '16px', background: '#0e1c30', padding: '20px' }
  const cardStyle: React.CSSProperties = { padding: '14px', border: '1px solid #1f2937', borderRadius: '12px', background: '#111827' }

  return (
    <div className="shell">
      <header className="brand"><div><p className="eyebrow">PLAN MANAGEMENT</p><h1>计划管理</h1><p>管理执行计划与节点生命周期。</p></div></header>
      {error && <div className="error" role="alert"><b>{error}</b></div>}
      <div style={{ display: 'grid', gridTemplateColumns: 'minmax(280px,360px) 1fr', gap: '20px' }}>
        <section style={panelStyle}>
          <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', marginBottom: '14px' }}>
            <h2 style={{ margin: 0, fontSize: '18px' }}>计划列表</h2>
            <div style={{ display: 'flex', gap: '6px' }}>
              <button style={btnStyle} onClick={() => setShowCreatePlan(v => !v)}>新建计划</button>
              <button onClick={() => void loadPlans()} disabled={loading} aria-label="刷新计划">↻</button>
            </div>
          </div>
          {showCreatePlan && (
            <form onSubmit={e => void doCreatePlan(e)} style={{ marginBottom: '14px', padding: '12px', border: '1px solid #334155', borderRadius: '8px', background: '#0a0e1a', display: 'grid', gap: '8px' }}>
              <label style={{ display: 'grid', gap: '4px', fontSize: '13px' }}>计划名称
                <input style={inputStyle} value={planName} onChange={e => setPlanName(e.target.value)} aria-label="计划名称" placeholder="输入计划名称" />
              </label>
              <label style={{ display: 'grid', gap: '4px', fontSize: '13px' }}>计划描述
                <textarea style={{ ...inputStyle, resize: 'vertical', minHeight: '60px' }} value={planDescription} onChange={e => setPlanDescription(e.target.value)} aria-label="计划描述" placeholder="输入计划描述" />
              </label>
              <div style={{ display: 'flex', gap: '8px' }}>
                <button type="submit" style={primaryBtnStyle} disabled={busy || !planName.trim()}>{busy ? '创建中…' : '创建计划'}</button>
                <button type="button" style={btnStyle} onClick={() => setShowCreatePlan(false)}>取消</button>
              </div>
            </form>
          )}
          {loading ? <p style={{ color: '#8fa3bf' }}>正在载入…</p> :
           plans.length === 0 ? <div className="empty"><b>暂无计划</b><span>该项目下还没有计划。</span></div> :
           <div style={{ display: 'grid', gap: '8px' }}>
             {plans.map(plan => (
               <div key={plan.id} onClick={() => setSelectedPlan(plan)} style={{ ...cardStyle, cursor: 'pointer', transition: '0.15s', ...(selectedPlan?.id === plan.id ? { borderColor: '#60a5fa', background: 'rgba(96,165,250,0.08)' } : {}) }}>
                 <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'flex-start', gap: '8px' }}>
                   <strong style={{ fontSize: '14px' }}>{plan.name}</strong>
                   <span style={{ color: statusColor(plan.status), fontSize: '12px', whiteSpace: 'nowrap' }}>{PLAN_STATUS_LABELS[plan.status]}</span>
                 </div>
                 {plan.description && <p style={{ margin: '6px 0 0', color: '#8fa3bf', fontSize: '12px' }}>{plan.description}</p>}
                 <div style={{ marginTop: '8px', display: 'flex', gap: '6px', flexWrap: 'wrap' }}>
                   {plan.status === 'draft' && <button disabled={busy} onClick={e => { e.stopPropagation(); void doPlanOp('activate', plan.id) }}>激活</button>}
                   {plan.status === 'active' && <button disabled={busy} onClick={e => { e.stopPropagation(); void doPlanOp('pause', plan.id) }}>暂停</button>}
                   {plan.status === 'active' && <button disabled={busy} onClick={e => { e.stopPropagation(); void doPlanOp('complete', plan.id) }}>完成</button>}
                   {plan.status === 'paused' && <button disabled={busy} onClick={e => { e.stopPropagation(); void doPlanOp('resume', plan.id) }}>恢复</button>}
                 </div>
               </div>
             ))}
           </div>}
        </section>
        <section style={panelStyle}>
          <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', marginBottom: '14px' }}>
            <h2 style={{ margin: 0, fontSize: '18px' }}>{selectedPlan ? `${selectedPlan.name} · 节点` : '节点列表'}</h2>
            {selectedPlan && <button style={btnStyle} onClick={() => setShowCreateNode(v => !v)}>新建节点</button>}
          </div>
          {selectedPlan && showCreateNode && (
            <form onSubmit={e => void doCreateNode(e)} style={{ marginBottom: '14px', padding: '12px', border: '1px solid #334155', borderRadius: '8px', background: '#0a0e1a', display: 'grid', gap: '8px' }}>
              <label style={{ display: 'grid', gap: '4px', fontSize: '13px' }}>节点名称
                <input style={inputStyle} value={nodeName} onChange={e => setNodeName(e.target.value)} aria-label="节点名称" placeholder="输入节点名称" />
              </label>
              <label style={{ display: 'grid', gap: '4px', fontSize: '13px' }}>节点描述
                <textarea style={{ ...inputStyle, resize: 'vertical', minHeight: '60px' }} value={nodeDescription} onChange={e => setNodeDescription(e.target.value)} aria-label="节点描述" placeholder="输入节点描述" />
              </label>
              <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: '8px' }}>
                <label style={{ display: 'grid', gap: '4px', fontSize: '13px' }}>风险等级
                  <select style={inputStyle} value={nodeRiskLevel} onChange={e => setNodeRiskLevel(e.target.value as RiskLevel)} aria-label="风险等级">
                    {RISK_OPTIONS.map(r => <option key={r} value={r}>{RISK_LABELS[r]}</option>)}
                  </select>
                </label>
                <label style={{ display: 'grid', gap: '4px', fontSize: '13px' }}>执行顺序
                  <input style={inputStyle} type="number" min="1" value={nodeSequence} onChange={e => setNodeSequence(e.target.value)} aria-label="执行顺序" />
                </label>
              </div>
              <label style={{ display: 'grid', gap: '4px', fontSize: '13px' }}>执行角色
                <input style={inputStyle} value={nodeWorkerRole} onChange={e => setNodeWorkerRole(e.target.value)} aria-label="执行角色" placeholder="如 coder / reviewer" />
              </label>
              <div style={{ display: 'flex', gap: '8px' }}>
                <button type="submit" style={primaryBtnStyle} disabled={busy || !nodeName.trim()}>{busy ? '创建中…' : '创建节点'}</button>
                <button type="button" style={btnStyle} onClick={() => setShowCreateNode(false)}>取消</button>
              </div>
            </form>
          )}
          {!selectedPlan ? <div className="empty"><b>请选择计划</b><span>从左侧选择一个计划查看其节点。</span></div> :
           nodes.length === 0 ? <div className="empty"><b>暂无节点</b></div> :
           <div style={{ display: 'grid', gap: '10px' }}>
             {nodes.map(node => (
               <div key={node.id} style={cardStyle}>
                 <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'flex-start', gap: '8px' }}>
                   <div>
                     <strong style={{ fontSize: '14px' }}>{node.sequence}. {node.name}</strong>
                     {node.description && <p style={{ margin: '4px 0 0', color: '#8fa3bf', fontSize: '12px' }}>{node.description}</p>}
                   </div>
                   <div style={{ display: 'flex', flexDirection: 'column', alignItems: 'flex-end', gap: '4px' }}>
                     <span style={{ color: statusColor(node.status), fontSize: '12px' }}>{NODE_STATUS_LABELS[node.status]}</span>
                     <span style={{ color: riskColor(node.riskLevel), fontSize: '11px' }}>风险: {RISK_LABELS[node.riskLevel]}</span>
                   </div>
                 </div>
                 <div style={{ marginTop: '6px', fontSize: '11px', color: '#8fa3bf' }}>角色: {node.workerRole}{node.budgetTokens ? ` · 预算 ${node.budgetTokens}` : ''}</div>
                 <div style={{ marginTop: '8px', display: 'flex', gap: '6px', flexWrap: 'wrap' }}>
                   {(node.status === 'pending' || node.status === 'ready') && <button disabled={busy} onClick={() => void doNodeOp('startNode', node.id)}>启动</button>}
                   {node.status === 'running' && <button disabled={busy} onClick={() => void doNodeOp('completeNode', node.id)}>完成</button>}
                   {node.status === 'running' && <button disabled={busy} onClick={() => void doNodeOp('failNode', node.id)} style={{ color: '#f87171' }}>标记失败</button>}
                 </div>
               </div>
             ))}
           </div>}
        </section>
      </div>
    </div>
  )
}
