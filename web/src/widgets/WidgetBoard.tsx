import type {JSX} from 'react'
import {ChecklistWidget} from './ChecklistWidget'
import {CheckinWidget, MetricWidget, ProgressWidget, TableWidget} from './DataWidgets'
import {isRegisteredWidget, rejectHostMessage, type WidgetSpec, type WidgetState} from './registry'
import {TimerWidget} from './TimerWidget'

export function WidgetBoard({widgets, onState}: {widgets: WidgetSpec[]; onState?: (id: string, state: WidgetState) => void}): JSX.Element | null {
  const allowed = widgets.filter(item => isRegisteredWidget(item.kind) && !rejectHostMessage(item.kind))
  if (!allowed.length) return null
  return (
    <div data-testid="widget-board">
      {allowed.map(spec => {
        const persist = onState ? (state: WidgetState) => onState(spec.id, state) : undefined
        if (spec.kind === 'checklist' || spec.kind === 'todo') return <ChecklistWidget key={spec.id} spec={spec} onState={persist} />
        if (spec.kind === 'timer') return <TimerWidget key={spec.id} spec={spec} onState={persist} />
        if (spec.kind === 'metric') return <MetricWidget key={spec.id} spec={spec} />
        if (spec.kind === 'table') return <TableWidget key={spec.id} spec={spec} />
        if (spec.kind === 'bar' || spec.kind === 'progress') return <ProgressWidget key={spec.id} spec={spec} />
        if (spec.kind === 'checkin') return <CheckinWidget key={spec.id} spec={spec} onState={persist} />
        return <section key={spec.id} aria-label={spec.title || spec.kind} data-widget={spec.id}><p>{spec.title || spec.kind}</p></section>
      })}
    </div>
  )
}
