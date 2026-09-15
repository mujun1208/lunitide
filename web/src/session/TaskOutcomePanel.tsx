import React, { useState } from 'react'
import { presentTaskOutcome, type StreamStatus, type TaskOutcome } from './taskOutcome'
import './taskOutcome.css'

export function TaskOutcomePanel({
  streamStatus,
  outcome,
  artifactPaths = [],
  requiredFiles,
  onContinue,
}: {
  streamStatus: StreamStatus
  outcome?: TaskOutcome
  artifactPaths?: string[]
  requiredFiles?: boolean
  onContinue?: (taskId: string) => void
}): React.JSX.Element {
  const view = presentTaskOutcome({ outcome, streamStatus, artifactPaths, requiredFiles })
  const [open, setOpen] = useState(false)
  const files = [...new Set([...artifactPaths, ...(outcome?.artifactRefs ?? [])])].filter(path => path.trim())
  const missingChecks = outcome?.evidenceRefs ?? []
  const remaining = outcome?.remainingSteps ?? []
  const budgetPaused = outcome?.state === 'paused' || outcome?.reasonCode === 'TASK_BUDGET_EXHAUSTED'
  return (
    <section className={`task-outcome-panel is-${view.tone}`} data-complete={view.greenComplete ? 'green' : 'no'} aria-label="任务结果">
      {view.spinner ? <p role="status" aria-label="任务进行中" className="task-outcome-spinner">进行中</p> : null}
      {view.replyEnded ? <p className="task-outcome-reply">已回复</p> : null}
      <p className="task-outcome-task" role="status">
        {view.greenComplete ? <span className="task-outcome-mark" aria-hidden="true">✓</span> : null}
        <b>{view.label}</b>
      </p>
      <details open={open} onToggle={event => setOpen(event.currentTarget.open)}>
        <summary>查看步骤与文件</summary>
        {remaining.length ? (
          <div>
            <strong>剩余步骤</strong>
            <ul>{remaining.map(step => <li key={step}>{step}</li>)}</ul>
          </div>
        ) : null}
        {files.length ? (
          <div>
            <strong>已产出文件</strong>
            <ul>{files.map(path => <li key={path}>{path}</li>)}</ul>
          </div>
        ) : null}
        {missingChecks.length && !view.greenComplete ? (
          <div>
            <strong>所缺验证</strong>
            <ul>{missingChecks.map(item => <li key={item}>{item}</li>)}</ul>
          </div>
        ) : null}
      </details>
      {budgetPaused && outcome?.taskId && onContinue ? (
        <button type="button" onClick={() => onContinue(outcome.taskId)}>继续此任务</button>
      ) : null}
    </section>
  )
}
