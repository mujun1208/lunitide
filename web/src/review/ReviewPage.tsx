import React, { useEffect, useState, useCallback } from 'react'
import { reviewBridge, planBridge, type ReviewBridge, type PlanBridge } from '../bridge/client'
import type { ReviewDTO, ReviewStatus, RiskLevel, PlanDTO } from '../generated/bridge'
import { BridgeClientError } from '../bridge/client'

const REVIEW_STATUS_LABELS: Record<ReviewStatus, string> = {
  pending: '待审批', approved: '已批准', rejected: '已拒绝', expired: '已过期', changed_after_approval: '变更后失效'
}
const RISK_LABELS: Record<RiskLevel, string> = { low: '低', medium: '中', high: '高', critical: '极高' }

const statusColor = (s: ReviewStatus): string => {
  if (s === 'approved') return '#34d399'
  if (s === 'pending' || s === 'changed_after_approval') return '#fbbf24'
  if (s === 'rejected' || s === 'expired') return '#f87171'
  return '#8fa3bf'
}
const riskColor = (r: RiskLevel): string => r === 'low' ? '#34d399' : r === 'medium' ? '#60a5fa' : r === 'high' ? '#fbbf24' : '#f87171'

const isRetryable = (e: unknown): boolean => e instanceof BridgeClientError && e.retryable

export function ReviewPage({ projectId, bridge = reviewBridge, plans = planBridge }: { projectId: string; bridge?: ReviewBridge; plans?: PlanBridge }): React.JSX.Element {
  const [plansList, setPlansList] = useState<PlanDTO[]>([])
  const [selectedPlanId, setSelectedPlanId] = useState('')
  const [reviews, setReviews] = useState<ReviewDTO[]>([])
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState<string>()
  const [retryable, setRetryable] = useState(false)
  const [busy, setBusy] = useState(false)
  const [notes, setNotes] = useState<Record<string, string>>({})

  const loadPlans = useCallback(async () => {
    if (!projectId) return
    try {
      const r = await plans.list({ projectId })
      setPlansList(r.items)
    } catch { /* 计划列表加载失败不阻塞审批页面 */ }
  }, [projectId, plans])

  const load = useCallback(async (pid: string) => {
    if (!pid) return
    setLoading(true); setError(undefined); setRetryable(false)
    try { const r = await bridge.list({ planId: pid }); setReviews(r.items) }
    catch (e) { setError(e instanceof Error ? e.message : '加载失败'); setRetryable(isRetryable(e)) }
    finally { setLoading(false) }
  }, [bridge])

  useEffect(() => { void loadPlans() }, [loadPlans])
  useEffect(() => { if (selectedPlanId) void load(selectedPlanId); else setReviews([]) }, [selectedPlanId, load])

  const doReview = async (op: 'approve' | 'reject', reviewId: string) => {
    setBusy(true); setError(undefined); setRetryable(false)
    try {
      const reviewerNote = notes[reviewId]
      if (op === 'approve') await bridge.approve({ reviewId, ...(reviewerNote ? { reviewerNote } : {}) })
      else await bridge.reject({ reviewId, ...(reviewerNote ? { reviewerNote } : {}) })
      setNotes(prev => { const next = { ...prev }; delete next[reviewId]; return next })
      await load(selectedPlanId)
    } catch (e) { setError(e instanceof Error ? e.message : '操作失败'); setRetryable(isRetryable(e)) }
    finally { setBusy(false) }
  }

  const panelStyle: React.CSSProperties = { border: '1px solid #1f2937', borderRadius: '16px', background: '#0e1c30', padding: '20px' }
  const cardStyle: React.CSSProperties = { padding: '14px', border: '1px solid #1f2937', borderRadius: '12px', background: '#111827' }

  return (
    <div className="shell">
      <header className="brand"><div><p className="eyebrow">GOVERNANCE REVIEW</p><h1>治理审批</h1><p>审批计划与节点操作请求。</p></div></header>
      {error && <div className="error" role="alert"><b>{error}</b>{retryable && <button onClick={() => void load(selectedPlanId)} style={{ marginLeft: '12px' }}>重试</button>}</div>}
      <section style={panelStyle}>
        <div style={{ display: 'flex', gap: '10px', alignItems: 'flex-end', marginBottom: '18px' }}>
          <label style={{ display: 'grid', gap: '4px', fontSize: '13px', color: '#e5e7eb', flex: 1 }}>
            选择计划
            {!projectId ? (
              <input disabled placeholder="请先选择项目" />
            ) : plansList.length === 0 ? (
              <input disabled placeholder="该项目下暂无计划" />
            ) : (
              <select value={selectedPlanId} onChange={e => setSelectedPlanId(e.target.value)}>
                <option value="">— 选择计划 —</option>
                {plansList.map(p => <option key={p.id} value={p.id}>{p.name} ({p.status})</option>)}
              </select>
            )}
          </label>
          {selectedPlanId && <button onClick={() => void load(selectedPlanId)} disabled={loading}>{loading ? '查询中…' : '刷新'}</button>}
        </div>
        {!selectedPlanId ? (
          <div className="empty"><b>请选择计划</b><span>从下拉列表中选择一个计划查看其审批记录。</span></div>
        ) : reviews.length === 0 ? (
          <div className="empty"><b>暂无审批记录</b><span>该计划下还没有审批记录。</span></div>
        ) : (
          <div style={{ display: 'grid', gap: '10px' }}>
            {reviews.map(review => (
              <div key={review.id} style={cardStyle}>
                <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'flex-start', gap: '8px' }}>
                  <div>
                    <strong style={{ fontSize: '14px' }}>{review.actionType}</strong>
                    <p style={{ margin: '4px 0 0', color: '#8fa3bf', fontSize: '12px', fontFamily: 'monospace' }}>
                      action: {review.actionDigest.slice(0, 16)}… · input: {review.inputDigest.slice(0, 16)}…
                    </p>
                  </div>
                  <div style={{ display: 'flex', flexDirection: 'column', alignItems: 'flex-end', gap: '4px' }}>
                    <span style={{ color: statusColor(review.status), fontSize: '12px' }}>{REVIEW_STATUS_LABELS[review.status]}</span>
                    <span style={{ color: riskColor(review.riskLevel), fontSize: '11px' }}>风险: {RISK_LABELS[review.riskLevel]}</span>
                  </div>
                </div>
                <div style={{ marginTop: '6px', fontSize: '11px', color: '#8fa3bf' }}>
                  创建: {new Date(review.createdAt).toLocaleString()}{review.expiresAt ? ` · 过期: ${new Date(review.expiresAt).toLocaleString()}` : ''}{review.reviewedAt ? ` · 审批于: ${new Date(review.reviewedAt).toLocaleString()}` : ''}
                </div>
                {review.reviewerNote && <div style={{ marginTop: '6px', fontSize: '12px', color: '#e5e7eb' }}>审批备注: {review.reviewerNote}</div>}
                {review.status === 'pending' && (
                  <div style={{ marginTop: '10px', display: 'flex', gap: '8px', alignItems: 'center', flexWrap: 'wrap' }}>
                    <input
                      value={notes[review.id] ?? ''}
                      onChange={e => setNotes(prev => ({ ...prev, [review.id]: e.target.value }))}
                      placeholder="审批备注（可选）"
                      style={{ flex: 1, minWidth: '200px' }}
                    />
                    <button disabled={busy} className="primary" onClick={() => void doReview('approve', review.id)}>批准</button>
                    <button disabled={busy} onClick={() => void doReview('reject', review.id)} style={{ color: '#f87171' }}>拒绝</button>
                  </div>
                )}
              </div>
            ))}
          </div>
        )}
      </section>
    </div>
  )
}
