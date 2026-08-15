import React, { useState } from 'react'

export type DiffChangeKind = 'add' | 'modify' | 'delete'

export interface DiffLine {
  kind: 'ctx' | 'add' | 'del'
  text: string
}

export interface DiffItem {
  path: string
  change: DiffChangeKind
  lines?: DiffLine[]
}

export interface DiffReviewProps {
  changesetId: string
  /** changeset 状态：staged / applied / reverted / conflict，未知名兜底原样 */
  state: string
  items: DiffItem[]
  onAccept(path: string): Promise<void>
  onReject(path: string): Promise<void>
  conflicted?: boolean
}

export const CHANGESET_STATE_LABELS: Record<string, string> = {
  staged: '已暂存',
  applied: '已应用',
  reverted: '已回退',
  conflict: '冲突',
}

export const CHANGE_LABELS: Record<DiffChangeKind, string> = {
  add: '新增',
  modify: '修改',
  delete: '删除',
}

export function DiffReview({ changesetId, state, items, onAccept, onReject, conflicted = false }: DiffReviewProps): React.JSX.Element {
  const [pending, setPending] = useState<Record<string, 'accept' | 'reject'>>({})
  const run = async (path: string, op: 'accept' | 'reject') => {
    if (conflicted || pending[path]) return
    setPending(p => ({ ...p, [path]: op }))
    try {
      if (op === 'accept') await onAccept(path)
      else await onReject(path)
    } finally {
      setPending(p => { const next = { ...p }; delete next[path]; return next })
    }
  }
  return (
    <section className="m5-diff-review" aria-label={`变更集 ${changesetId} 审阅`}>
      <header className="m5-diff-head">
        <h3>逐文件审阅</h3>
        <span className="m5-badge" data-state={state} data-testid="m5-changeset-state">
          {CHANGESET_STATE_LABELS[state] ?? state}
        </span>
      </header>
      {conflicted && (
        <div className="m5-conflict-banner" role="alert" data-testid="m5-conflict-banner">
          工作区基线已漂移，本变更集不会应用
        </div>
      )}
      <ul className="m5-diff-list">
        {items.map(item => (
          <li key={item.path} className="m5-diff-card" data-path={item.path}>
            <div className="m5-diff-card-head">
              <code className="m5-diff-path">{item.path}</code>
              <span className="m5-badge m5-badge-change" data-change={item.change}>
                {CHANGE_LABELS[item.change]}
              </span>
              <span className="m5-diff-actions">
                <button
                  type="button"
                  className="m5-btn m5-btn-primary"
                  disabled={conflicted || pending[item.path] !== undefined}
                  onClick={() => void run(item.path, 'accept')}
                  data-testid={`m5-accept-${item.path}`}
                >
                  {pending[item.path] === 'accept' ? '接受中…' : '接受'}
                </button>
                <button
                  type="button"
                  className="m5-btn m5-btn-danger"
                  disabled={conflicted || pending[item.path] !== undefined}
                  onClick={() => void run(item.path, 'reject')}
                  data-testid={`m5-reject-${item.path}`}
                >
                  {pending[item.path] === 'reject' ? '拒绝中…' : '拒绝'}
                </button>
              </span>
            </div>
            {item.lines && item.lines.length > 0 && (
              <pre className="m5-diff-lines">
                {item.lines.map((l, i) => (
                  <div key={i} className={`m5-diff-line m5-diff-line-${l.kind}`}>
                    <span className="m5-diff-marker" aria-hidden="true">{l.kind === 'add' ? '+' : l.kind === 'del' ? '−' : ' '}</span>
                    <span className="m5-diff-text">{l.text}</span>
                  </div>
                ))}
              </pre>
            )}
          </li>
        ))}
      </ul>
      {items.length === 0 && <p className="m5-empty">变更集没有可审阅的文件。</p>}
    </section>
  )
}

export default DiffReview
