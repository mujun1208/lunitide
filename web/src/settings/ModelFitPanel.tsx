import React from 'react'
import { presentModelFit, type ModelFitPhase } from './modelFitUi'

export function ModelFitPanel({
  phase,
  source,
  status,
}: {
  phase: ModelFitPhase
  source: 'fixture' | 'live'
  status?: string
}): React.JSX.Element {
  const view = presentModelFit({ phase, source, status })
  return (
    <section aria-label="模型适配" data-live-qualified={view.liveQualified ? 'yes' : 'no'}>
      <p>{view.label}</p>
    </section>
  )
}
