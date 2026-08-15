import React, { useState } from 'react'
import { formatDateTime } from '../format'

export type TaskState = 'running' | 'backgrounded' | 'done' | 'failed' | 'cancelled' | 'orphaned'

export interface TaskInfo {
  taskId: string
  state: TaskState
  backgroundReason?: string
  logTail: string
  nextLogCursor: number
  startedAt: string
}

export interface TaskCenterProps {
  tasks: TaskInfo[]
  onCancel(taskId: string): void
  onReconnect(taskId: string, cursor: number): void
}

export const TASK_STATE_LABELS: Record<TaskState, string> = {
  running: '运行中',
  backgrounded: '已后台化',
  done: '完成',
  failed: '失败',
  cancelled: '已取消',
  orphaned: '孤儿',
}

export const BACKGROUND_REASON_LABELS: Record<string, string> = {
  elapsed: '超时 10 秒',
  output: '输出超 1MiB',
}

/** 取消反馈窗口：点击后立即进入取消中 UI，窗口结束后回落 */
const CANCEL_FEEDBACK_MS = 2000

export function TaskCenter({ tasks, onCancel, onReconnect }: TaskCenterProps): React.JSX.Element {
  const [cancelling, setCancelling] = useState<Set<string>>(new Set())
  const cancelTask = (taskId: string) => {
    if (cancelling.has(taskId)) return
    setCancelling(prev => new Set(prev).add(taskId))
    onCancel(taskId)
    window.setTimeout(() => {
      setCancelling(prev => {
        const next = new Set(prev)
        next.delete(taskId)
        return next
      })
    }, CANCEL_FEEDBACK_MS)
  }
  return (
    <section className="m5-task-center" aria-label="后台任务中心">
      <header className="m5-task-head"><h3>后台任务</h3></header>
      {tasks.length === 0 ? (
        <p className="m5-empty">暂无后台任务。</p>
      ) : (
        <ul className="m5-task-list">
          {tasks.map(t => {
            const isCancelling = cancelling.has(t.taskId)
            const canReconnect = t.state === 'backgrounded' || t.state === 'orphaned'
            return (
              <li key={t.taskId} className="m5-task-card" data-task-id={t.taskId}>
                <div className="m5-task-row">
                  <span className="m5-badge" data-state={t.state} data-testid={`m5-task-state-${t.taskId}`}>
                    {TASK_STATE_LABELS[t.state]}
                  </span>
                  {t.backgroundReason && (
                    <span className="m5-badge m5-badge-reason" title={t.backgroundReason}>
                      {BACKGROUND_REASON_LABELS[t.backgroundReason] ?? t.backgroundReason}
                    </span>
                  )}
                  <span className="m5-task-started">启动于 {formatDateTime(t.startedAt)}</span>
                  <span className="m5-task-actions">
                    <button
                      type="button"
                      className="m5-btn m5-btn-danger"
                      disabled={isCancelling}
                      onClick={() => cancelTask(t.taskId)}
                      data-testid={`m5-cancel-${t.taskId}`}
                    >
                      {isCancelling ? '取消中…' : '取消'}
                    </button>
                    {canReconnect && (
                      <button
                        type="button"
                        className="m5-btn"
                        onClick={() => onReconnect(t.taskId, t.nextLogCursor)}
                        data-testid={`m5-reconnect-${t.taskId}`}
                      >
                        重连
                      </button>
                    )}
                  </span>
                </div>
                <pre className="m5-log-tail" data-testid={`m5-log-${t.taskId}`}>{t.logTail}</pre>
              </li>
            )
          })}
        </ul>
      )}
    </section>
  )
}

export default TaskCenter
