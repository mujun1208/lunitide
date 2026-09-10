import {useState, type JSX} from 'react'
import {splitWidgetList, type WidgetSpec, type WidgetState} from './registry'

function checkedSet(raw?: string): Record<string, boolean> {
  const out: Record<string, boolean> = {}
  for (const item of splitWidgetList(raw)) out[item] = true
  return out
}

export function ChecklistWidget({spec, title, onState}: {spec: WidgetSpec; title?: string; onState?: (state: WidgetState) => void}): JSX.Element {
  const label = title || spec.title || '清单'
  const fromState = splitWidgetList(spec.state?.items)
  const fromTitle = splitWidgetList(label)
  const names = fromState.length ? fromState : fromTitle.length > 1 ? fromTitle : []
  const [done, setDone] = useState<Record<string, boolean>>(() => checkedSet(spec.state?.checked))
  const toggle = (item: string) => {
    setDone(cur => {
      const next = {...cur, [item]: !cur[item]}
      onState?.({
        ...spec.state,
        items: names.join(','),
        checked: names.filter(name => next[name]).join(','),
      })
      return next
    })
  }
  return (
    <section aria-label={label} data-widget={spec.id}>
      <p>{label}</p>
      {names.map(item => (
        <label key={item}>
          <input
            type="checkbox"
            checked={!!done[item]}
            onChange={() => toggle(item)}
          />
          {item}
        </label>
      ))}
    </section>
  )
}
