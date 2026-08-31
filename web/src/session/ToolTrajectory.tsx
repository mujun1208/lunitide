import React from 'react'

export type ToolTrajectoryItem = {
  callId: string
  name: string
  status: string
  summary?: string
}

const STATUS_LABEL: Record<string, string> = {
  tool_started: '运行中',
  tool_output: '运行中',
  tool_completed: '完成',
  approval_required: '待批准',
  approved: '已批准',
  rejected: '拒绝',
  failed: '失败',
}

export function toolTrajectoryStatus(status: string): string {
  return STATUS_LABEL[status] ?? status
}

export function ToolTrajectory({ items }: { items: readonly ToolTrajectoryItem[] }): React.JSX.Element | null {
  if (!items.length) return null
  return (
    <details className="tool-trajectory" open>
      <summary>工具轨迹 · {items.length}</summary>
      <ol aria-label="工具轨迹">
        {items.map(item => (
          <li key={item.callId}>
            <span>{item.name}</span>
            <span> · {toolTrajectoryStatus(item.status)}</span>
            {item.summary?.trim() ? <small> {item.summary.trim().slice(0, 80)}</small> : null}
          </li>
        ))}
      </ol>
    </details>
  )
}
