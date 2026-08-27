import React, { useEffect, useState, useCallback } from 'react'
import { reviewBridge, planBridge, type ReviewBridge, type PlanBridge } from '../bridge/client'
import type { ReviewDTO, ReviewStatus, RiskLevel, PlanDTO } from '../generated/bridge'
import { BridgeClientError } from '../bridge/client'
import { presentReview } from './reviewPresentation'

const REVIEW_STATUS_LABELS: Record<ReviewStatus, string> = {
  pending: '待审批', approved: '已批准', rejected: '已拒绝', expired: '已过期', changed_after_approval: '变更后失效',
}
const RISK_LABELS: Record<RiskLevel, string> = { low: 'LOW RISK', medium: 'MEDIUM RISK', high: 'HIGH RISK', critical: 'HIGH RISK' }
const riskClass = (r: RiskLevel): string => (r === 'high' || r === 'critical') ? 'high' : r === 'medium' ? 'med' : 'low'

const isRetryable = (e: unknown): boolean => e instanceof BridgeClientError && e.retryable

const expireText = (iso?: string): string => {
  if (!iso) return ''
  const remain = new Date(iso).getTime() - Date.now()
  if (remain <= 0) return '⏰ 已过期'
  const minutes = Math.floor(remain / 60000)
  if (minutes < 60) return `⏰ ${minutes} 分钟后过期`
  const hours = Math.floor(minutes / 60)
  if (hours < 48) return `⏰ ${hours} 小时后过期`
  return `⏰ ${Math.floor(hours / 24)} 天后过期`
}

const expiringSoon = (reviews: ReviewDTO[]): number =>
  reviews.filter(r =>
    r.status === 'pending'
    && r.expiresAt
    && new Date(r.expiresAt).getTime() - Date.now() > 0
    && new Date(r.expiresAt).getTime() - Date.now() < 30 * 60000,
  ).length

type ApprTab = 'pending' | 'done' | 'auto' | 'policy'

export function ReviewPage({
  projectId,
  bridge = reviewBridge,
  plans = planBridge,
  embedded = false,
}: {
  projectId: string
  bridge?: ReviewBridge
  plans?: PlanBridge
  embedded?: boolean
}): React.JSX.Element {
  const [tab, setTab] = useState<ApprTab>('pending')
  const [plansList, setPlansList] = useState<PlanDTO[]>([])
  const [selectedPlanId, setSelectedPlanId] = useState('')
  const [reviews, setReviews] = useState<ReviewDTO[]>([])
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState<string>()
  const [retryable, setRetryable] = useState(false)
  const [busy, setBusy] = useState(false)
  const [notes, setNotes] = useState<Record<string, string>>(() => ({}))

  const loadPlans = useCallback(async () => {
    if (!projectId) return
    try {
      const r = await plans.list({ projectId })
      setPlansList(r.items)
      const active = r.items.find(p => p.status === 'active') ?? r.items[0]
      setSelectedPlanId(current => current || active?.id || '')
    } catch { /* optional */ }
  }, [projectId, plans])

  const load = useCallback(async (pid: string) => {
    if (!pid) return
    setLoading(true); setError(undefined); setRetryable(false)
    try {
      const r = await bridge.list({ planId: pid })
      setReviews(r.items)
    } catch (e) {
      setError(e instanceof Error ? e.message : '加载失败')
      setRetryable(isRetryable(e))
    } finally {
      setLoading(false)
    }
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
    } catch (e) {
      setError(e instanceof Error ? e.message : '操作失败')
      setRetryable(isRetryable(e))
    } finally {
      setBusy(false)
    }
  }

  const pending = reviews.filter(r => r.status === 'pending')
  const done = reviews.filter(r => r.status !== 'pending')
  const planName = (id?: string) => plansList.find(p => p.id === id)?.name ?? (id ? id.slice(0, 8) : '—')
  const shown = tab === 'pending' ? pending : done
  const meta = selectedPlanId
    ? `待我审批 ${pending.length}${expiringSoon(pending) ? ` · ${expiringSoon(pending)} 项即将过期` : ''}`
    : '请先选择计划'

  return (
    <div className={`approval-center ${embedded ? 'approval-center-embedded' : ''}`}>
      <header className="expert-view-head approval-center-head">
        <div>
          {!embedded && <div className="view-title">审批中心</div>}
          <div className="view-meta">{meta}</div>
        </div>
        <div className="memory-tabs approval-tabs" role="tablist" aria-label="审批分区">
          <button type="button" role="tab" className={`memory-tab ${tab === 'pending' ? 'on' : ''}`} aria-selected={tab === 'pending'} onClick={() => setTab('pending')}>待我审批 · {pending.length}</button>
          <button type="button" role="tab" className={`memory-tab ${tab === 'done' ? 'on' : ''}`} aria-selected={tab === 'done'} onClick={() => setTab('done')}>已处理 · {done.length}</button>
          <button type="button" role="tab" className={`memory-tab ${tab === 'auto' ? 'on' : ''}`} aria-selected={tab === 'auto'} onClick={() => setTab('auto')}>自动决策</button>
          <button type="button" role="tab" className={`memory-tab ${tab === 'policy' ? 'on' : ''}`} aria-selected={tab === 'policy'} onClick={() => setTab('policy')}>策略</button>
        </div>
      </header>

      {error && (
        <div className="error" role="alert">
          <b>{error}</b>
          {retryable && <button onClick={() => void load(selectedPlanId)}>重试</button>}
        </div>
      )}

      {(tab === 'pending' || tab === 'done') && (
        <>
          <div className="memory-toolbar approval-toolbar">
            <label className="appr-plan">
              计划
              {!projectId ? <input disabled placeholder="请先选择项目" />
                : plansList.length === 0 ? <input disabled placeholder="该项目下暂无计划" />
                : (
                  <select value={selectedPlanId} onChange={e => setSelectedPlanId(e.target.value)} aria-label="选择计划">
                    <option value="">— 选择计划 —</option>
                    {plansList.map(p => <option key={p.id} value={p.id}>{p.name}</option>)}
                  </select>
                )}
            </label>
            {selectedPlanId && (
              <button type="button" onClick={() => void load(selectedPlanId)} disabled={loading}>
                {loading ? '查询中…' : '刷新'}
              </button>
            )}
          </div>

          {!selectedPlanId ? (
            <div className="empty approval-empty">
              <b>请选择计划</b>
              <span>审批记录按计划归集；从下拉列表选择一个计划查看。DAG 中「等待审批」节点也会在这里暂停。</span>
            </div>
          ) : loading ? (
            <p role="status">正在载入审批…</p>
          ) : shown.length === 0 ? (
            <div className="empty approval-empty">
              <b>{tab === 'pending' ? '暂无待审批请求' : '暂无已处理记录'}</b>
              <span>{tab === 'pending' ? '新的高风险操作会在这里等待你的批准。' : '处理过的审批会归档在这里。'}</span>
            </div>
          ) : (
            <div className="appr-list">
              {shown.map(review => {
                const view = presentReview(review)
                return (
                  <article key={review.id} className={`appr-item appr-rich ${riskClass(review.riskLevel)}`}>
                    <div className="appr-head">
                      <span className="a-type">{view.title}</span>
                      <span className={`a-risk ${riskClass(review.riskLevel)}`}>{RISK_LABELS[review.riskLevel]}</span>
                      <span className="mono a-id">{view.crId ?? review.id.slice(0, 8).toUpperCase()}</span>
                      {review.status === 'pending' && review.expiresAt && (
                        <span className="a-expire">{expireText(review.expiresAt)}</span>
                      )}
                      {review.status !== 'pending' && (
                        <span className="a-expire">
                          {REVIEW_STATUS_LABELS[review.status]}
                          {review.reviewedAt ? ` · ${new Date(review.reviewedAt).toLocaleString()}` : ''}
                        </span>
                      )}
                    </div>
                    <div className="appr-rich-body">
                      <p><b>将发生什么：</b>{view.whatWillHappen}</p>
                      <p><b>验证证据：</b>{view.evidence}</p>
                      <p><b>失败如何恢复：</b>{view.recovery}</p>
                      <p className="appr-tech">
                        操作类型 <span className="kv">{review.actionType}</span>
                        {' · '}
                        计划 <span className="kv">{planName(review.planId)}</span>
                        {' · '}
                        策略 <span className="kv">policy v{review.policyVersion}</span>
                        {review.nodeId && <> · 节点 <span className="kv">{review.nodeId.slice(0, 8)}</span></>}
                      </p>
                    </div>
                    {review.reviewerNote && <div className="appr-body"><b>审批备注</b>：{review.reviewerNote}</div>}
                    {review.status === 'pending' && (
                      <div className="appr-actions">
                        <input
                          value={notes[review.id] ?? ''}
                          onChange={e => setNotes(prev => ({ ...prev, [review.id]: e.target.value }))}
                          placeholder="审批备注（可选）"
                          aria-label={`审批备注 ${review.id}`}
                        />
                        <button type="button" className="ui-btn danger" disabled={busy} onClick={() => void doReview('reject', review.id)}>
                          {view.rejectLabel}
                        </button>
                        <button type="button" className="ui-btn primary" disabled={busy} onClick={() => void doReview('approve', review.id)}>
                          {busy ? '处理中…' : view.approveLabel}
                        </button>
                      </div>
                    )}
                    {review.status === 'pending' && (
                      <p className="appr-footnote">批准不绕过执行前重验；若参数、环境或策略发生漂移，批准将立即失效。</p>
                    )}
                  </article>
                )
              })}
            </div>
          )}

          <div className="callout approval-principles">
            <b>审批原则</b>：超时默认暂停；操作参数发生变化后原批准自动失效（变更后失效状态）。批准不绕过执行前重验；自动放行记录可在「自动决策」页查看。
          </div>
        </>
      )}

      {tab === 'auto' && (
        <>
          <div className="blocked-banner" role="status">⚠ 概念预览 —— 自动放行记录的查询 RPC 未开放；自动决策日志目前仅在引擎审计层留痕。</div>
          <article className="screen-route" data-route="/governance/auto-decisions"><b>自动决策</b><p>策略命中的自动放行/自动拒绝记录；含规则版本、命中条件与豁免说明。</p></article>
          <div className="state-contract" aria-label="状态契约">{['auto-approved', 'auto-rejected', 'rule-version', 'waiver'].map(s => <span key={s}>{s}</span>)}</div>
        </>
      )}

      {tab === 'policy' && (
        <>
          <div className="blocked-banner" role="status">⚠ 概念预览 —— 审批策略版本管理（policy v{reviews[0]?.policyVersion ?? 1} 为引擎内置只读版本）尚未开放独立配置面。</div>
          <article className="screen-route" data-route="/governance/policies"><b>审批策略</b><p>风险分级、超时行为（默认暂停）、N-of-M 与 SoD 规则的版本化配置。</p></article>
          <div className="state-contract" aria-label="状态契约">{['draft', 'validating', 'published', 'superseded'].map(s => <span key={s}>{s}</span>)}</div>
        </>
      )}
    </div>
  )
}
