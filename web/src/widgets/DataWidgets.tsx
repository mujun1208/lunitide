import type {JSX} from 'react'
import type {WidgetSpec, WidgetState} from './registry'

export function MetricWidget({spec}: {spec: WidgetSpec}): JSX.Element {
  const label = spec.title || '指标'
  const value = [spec.state?.value, spec.state?.unit].filter(Boolean).join(' ')
  return (
    <section aria-label={label} data-widget={spec.id}>
      <p>{label}</p>
      <p aria-label={`${label}数值`}>{value || '—'}</p>
    </section>
  )
}

export function TableWidget({spec}: {spec: WidgetSpec}): JSX.Element {
  const label = spec.title || '表格'
  const rows = (spec.state?.rows || '').split('\n').map(row => row.trim()).filter(Boolean)
  return (
    <section aria-label={label} data-widget={spec.id}>
      <p>{label}</p>
      <table>
        <tbody>
          {rows.map((row, index) => (
            <tr key={index}>
              {row.split('|').map((cell, cellIndex) => <td key={cellIndex}>{cell.trim()}</td>)}
            </tr>
          ))}
        </tbody>
      </table>
    </section>
  )
}

export function ProgressWidget({spec}: {spec: WidgetSpec}): JSX.Element {
  const label = spec.title || spec.kind
  const percent = Math.min(100, Math.max(0, spec.state?.percent ?? 0))
  return (
    <section aria-label={label} aria-valuenow={percent} data-widget={spec.id}>
      <p>{label}</p>
      <p>{percent}%</p>
    </section>
  )
}

export function CheckinWidget({spec, onState}: {spec: WidgetSpec; onState?: (state: WidgetState) => void}): JSX.Element {
  const label = spec.title || '打卡'
  return (
    <section aria-label={label} data-widget={spec.id}>
      <p>{label}</p>
      <p aria-label="打卡">{spec.state?.value || '未打卡'}</p>
      <button type="button" onClick={() => onState?.({...spec.state, value: new Date().toISOString().slice(0, 10)})}>打卡</button>
    </section>
  )
}
