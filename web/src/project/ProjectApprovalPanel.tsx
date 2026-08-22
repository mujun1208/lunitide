import React from 'react'
import { ReviewPage } from '../review/ReviewPage'

/** Embedded approval center for the project workbench right rail. */
export function ProjectApprovalPanel({ projectId }: { projectId: string }): React.JSX.Element {
  return (
    <div className="project-approval-panel" aria-label="审批中心">
      <ReviewPage projectId={projectId} embedded />
    </div>
  )
}
