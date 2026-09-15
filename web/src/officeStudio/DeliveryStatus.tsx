import React from 'react'
import { presentFormalDecision, type FormalDecision } from './officeQualityUi'
import type { OfficeQuality } from './officeStudioApi'

export function DeliveryStatus({
  decision,
  quality,
}: {
  decision?: FormalDecision
  quality?: OfficeQuality
}): React.JSX.Element {
  const view = presentFormalDecision(decision, quality ? { quality, validations: [] } : undefined)
  return (
    <section aria-label="交付状态" data-verified={view.verified ? 'yes' : 'no'}>
      <p>{view.state}</p>
      {view.decisionId ? <p>{view.decisionId}</p> : null}
      {view.missingChecks.length ? (
        <ul aria-label="所缺检查">
          {view.missingChecks.map((id) => (
            <li key={id}>{id}</li>
          ))}
        </ul>
      ) : null}
    </section>
  )
}
