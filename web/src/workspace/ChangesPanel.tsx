import React from 'react'
import type { WorkspaceToolActivity } from './Workspace'
import { extractTaskFiles, isChangeTool } from './codePanelUtils'

export function ChangesPanel({ toolActivities = [] }: { toolActivities?: WorkspaceToolActivity[] }): React.JSX.Element {
  const files = extractTaskFiles(toolActivities)
  const activities = [...toolActivities].reverse().filter(a => isChangeTool(a.name))

  return (
    <section className="changes-panel" aria-label="变更记录">
      <header className="changes-panel-head">
        <div>
          <strong>变更 · {files.length}</strong>
          <small>Agent 与工具写入的文件改动</small>
        </div>
      </header>
      {files.length > 0 && (
        <div className="changes-files">
          <h4>文件</h4>
          <ul>{files.map(f => (
            <li key={f.path}>
              <code>{f.path}</code>
              <span className={`changes-badge status-${f.status}`}>{f.status}</span>
            </li>
          ))}</ul>
        </div>
      )}
      <div className="changes-log">
        <h4>活动</h4>
        {activities.length ? (
          <ol>{activities.slice(0, 24).map(activity => (
            <li key={activity.callId}>
              <b>{activity.name}</b>
              <small>{activity.summary ?? activity.status}</small>
              {activity.artifact?.path && <code>{activity.artifact.path}</code>}
            </li>
          ))}</ol>
        ) : (
          <p className="changes-empty">本轮会话还没有代码变更。</p>
        )}
      </div>
    </section>
  )
}
