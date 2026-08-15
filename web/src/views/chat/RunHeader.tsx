import React from 'react'
import { formatBytes, formatDateTime } from '../format'

export interface RunWorkspaceInfo {
  displayPath: string
  usedBytes: number
  quotaSoft: number
  quotaHard: number
  state: string
  leaseExpiry: string
}

export interface RunHeaderProps {
  state: string
  lastEventSeq: number
  workspace?: RunWorkspaceInfo
}

/** Root Run 9 态中文映射，未知名兜底原样显示 */
export const RUN_STATE_LABELS: Record<string, string> = {
  pending: '等待中',
  running: '运行中',
  awaiting_input: '等待输入',
  completed: '已完成',
  failed: '已失败',
  cancelled: '已取消',
  timed_out: '已超时',
  awaiting_review: '待审阅',
  interrupted: '已中断',
}

/** 临时工作区 7 状态中文映射，未知名兜底原样显示 */
export const WORKSPACE_STATE_LABELS: Record<string, string> = {
  active: '使用中',
  readonly_full: '只读超限',
  expiring: '即将到期',
  cleaning: '清理中',
  cleaning_failed: '清理失败',
  retained: '已保留',
  deleted: '已删除',
}

function pct(value: number, total: number): number {
  if (!Number.isFinite(value) || !Number.isFinite(total) || total <= 0) return 0
  return Math.min(100, Math.max(0, (value / total) * 100))
}

export function RunHeader({ state, lastEventSeq, workspace }: RunHeaderProps): React.JSX.Element {
  return (
    <header className="m5-run-header">
      <span className="m5-badge" data-state={state} data-testid="m5-run-state">
        {RUN_STATE_LABELS[state] ?? state}
      </span>
      <span className="m5-run-seq" data-testid="m5-run-seq">事件 #{lastEventSeq}</span>
      {workspace && (
        <div className="m5-ws" data-testid="m5-workspace">
          <span className="m5-badge m5-badge-ws" data-state={workspace.state} data-testid="m5-ws-state">
            {WORKSPACE_STATE_LABELS[workspace.state] ?? workspace.state}
          </span>
          <span className="m5-ws-path" title={workspace.displayPath}>{workspace.displayPath}</span>
          <span className="m5-quota-text">
            {formatBytes(workspace.usedBytes)} / {formatBytes(workspace.quotaHard)}
          </span>
          <div
            className="m5-quota-bar"
            role="progressbar"
            aria-label="临时空间用量"
            aria-valuenow={workspace.usedBytes}
            aria-valuetext={`${formatBytes(workspace.usedBytes)} / ${formatBytes(workspace.quotaHard)}`}
          >
            <div className="m5-quota-soft" style={{ width: `${pct(workspace.quotaSoft, workspace.quotaHard)}%` }} />
            <div className="m5-quota-used" style={{ width: `${pct(workspace.usedBytes, workspace.quotaHard)}%` }} data-testid="m5-quota-used" />
            <div className="m5-quota-mark" style={{ left: `${pct(workspace.quotaSoft, workspace.quotaHard)}%` }} title={`软配额 ${formatBytes(workspace.quotaSoft)}`} />
          </div>
          <span className="m5-lease">清理期限 {formatDateTime(workspace.leaseExpiry)}</span>
        </div>
      )}
    </header>
  )
}

export default RunHeader
