import React from 'react'

export type TaskStepStatus = 'pending' | 'in_progress' | 'completed'
export type TaskStep = {content: string; status: TaskStepStatus}

const LINE = /^\d+\.\s+\[(x| )\] \((pending|in_progress|completed)\|(?:high|medium|low)\)\s+(.+)$/

function asRecord(value: unknown): Record<string, unknown> | undefined {
  return value !== null && typeof value === 'object' && !Array.isArray(value) ? (value as Record<string, unknown>) : undefined
}

function statusOf(value: unknown, mark?: string): TaskStepStatus {
  if (value === 'completed' || value === 'in_progress' || value === 'pending') return value
  return mark === 'x' ? 'completed' : 'pending'
}

/** Read a todo.write checklist from its rendered text or JSON arguments. */
export function parseTaskSteps(summary?: string): TaskStep[] {
  const text = summary?.trim()
  if (!text) return []
  const start = text.indexOf('{')
  const end = text.lastIndexOf('}')
  if (start >= 0 && end > start) {
    try {
      const root = asRecord(JSON.parse(text.slice(start, end + 1)))
      const todos = root && Array.isArray(root.todos) ? root.todos : []
      const fromJSON = todos.flatMap(item => {
        const row = asRecord(item)
        const content = typeof row?.content === 'string' ? row.content.trim() : ''
        if (!content) return []
        return [{content, status: statusOf(row?.status)}]
      })
      if (fromJSON.length) return fromJSON.slice(0, 50)
    } catch {
      /* fall through to the rendered checklist */
    }
  }
  const steps: TaskStep[] = []
  for (const line of text.split('\n')) {
    const match = LINE.exec(line.trim())
    if (!match) continue
    steps.push({content: match[3].trim(), status: statusOf(match[2], match[1])})
  }
  return steps.slice(0, 50)
}

export function latestTodoSummary(activities: readonly {name: string; summary?: string}[]): string | undefined {
  for (let i = activities.length - 1; i >= 0; i--) {
    if (activities[i].name === 'todo.write' && parseTaskSteps(activities[i].summary).length) return activities[i].summary
  }
  return undefined
}

export function TaskStepsFromSummary({summary}: {summary?: string}): React.JSX.Element | null {
  const steps = parseTaskSteps(summary)
  if (!steps.length) return null
  return (
    <ol className="task-steps" aria-label="任务步骤">
      {steps.map((step, index) => (
        <li key={`${index}:${step.content}`} className={step.status === 'completed' ? 'done' : step.status === 'in_progress' ? 'now' : undefined}>
          <i aria-hidden="true">{step.status === 'completed' ? '✓' : step.status === 'in_progress' ? '→' : '·'}</i>
          <span>{step.content}</span>
        </li>
      ))}
    </ol>
  )
}
