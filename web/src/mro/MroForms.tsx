import React, { useEffect, useState } from 'react'
import { Dialog } from '../ui/Dialog'
import { useZh } from '../i18n/language'

// QuickField is one input in a QuickForm. Values are collected as strings and
// the domain adapter parses them (numbers, csv, booleans) at submit time.
export type QuickField = {
  name: string
  label: string
  kind?: 'text' | 'number' | 'date' | 'textarea' | 'select' | 'checkbox'
  options?: Array<{ value: string; label: string }>
  required?: boolean
  placeholder?: string
  hint?: string
}

// QuickFormSpec drives the single reusable dialog. Each MRO domain builds a spec
// with fields + a submit adapter; the workbench renders exactly one QuickForm.
export type QuickFormSpec = {
  title: string
  submitLabel?: string
  intro?: string
  fields: QuickField[]
  preview?: (values: Record<string, string>) => React.ReactNode
  submit: (values: Record<string, string>) => Promise<void> | void
}

function defaults(fields: QuickField[]): Record<string, string> {
  const out: Record<string, string> = {}
  for (const f of fields) out[f.name] = f.kind === 'select' ? f.options?.[0]?.value ?? '' : ''
  return out
}

function Field({ field, value, onChange }: { field: QuickField; value: string; onChange: (v: string) => void }): React.JSX.Element {
  if (field.kind === 'textarea') {
    return <textarea value={value} rows={4} placeholder={field.placeholder} aria-label={field.label} onChange={e => onChange(e.target.value)} />
  }
  if (field.kind === 'select') {
    return (
      <select value={value} aria-label={field.label} onChange={e => onChange(e.target.value)}>
        {(field.options ?? []).map(o => <option key={o.value} value={o.value}>{o.label}</option>)}
      </select>
    )
  }
  if (field.kind === 'checkbox') {
    return <input type="checkbox" checked={value === 'true'} aria-label={field.label} onChange={e => onChange(e.target.checked ? 'true' : '')} />
  }
  const type = field.kind === 'number' ? 'number' : field.kind === 'date' ? 'date' : 'text'
  return <input type={type} value={value} placeholder={field.placeholder} aria-label={field.label} onChange={e => onChange(e.target.value)} />
}

// QuickForm renders a spec-driven modal form. Passing spec=null keeps it closed.
export function QuickForm({ spec, onClose }: { spec: QuickFormSpec | null; onClose: () => void }): React.JSX.Element {
  const zh = useZh()
  const [values, setValues] = useState<Record<string, string>>({})
  const [error, setError] = useState('')
  const [busy, setBusy] = useState(false)
  useEffect(() => {
    if (spec) { setValues(defaults(spec.fields)); setError(''); setBusy(false) }
  }, [spec])
  const fields = spec?.fields ?? []
  const set = (name: string, value: string) => setValues(v => ({ ...v, [name]: value }))
  const missing = fields.some(f => f.required && !String(values[f.name] ?? '').trim())
  const submit = async () => {
    if (!spec) return
    setError(''); setBusy(true)
    try {
      await spec.submit(values)
      onClose()
    } catch (e) {
      setError(e instanceof Error ? e.message : (zh ? '保存失败' : 'Save failed'))
    } finally {
      setBusy(false)
    }
  }
  return (
    <Dialog open={!!spec} title={spec?.title ?? ''} onClose={onClose}>
      <form className="mro-quick-form" onSubmit={e => { e.preventDefault(); void submit() }}>
        {spec?.intro && <p className="mro-form-intro">{spec.intro}</p>}
        {fields.map(f => (
          <label key={f.name} className={f.kind === 'checkbox' ? 'mro-form-check' : undefined}>
            <span>{f.label}{f.required ? ' *' : ''}</span>
            <Field field={f} value={values[f.name] ?? ''} onChange={v => set(f.name, v)} />
            {f.hint && <small className="mro-form-hint">{f.hint}</small>}
          </label>
        ))}
        {spec?.preview?.(values)}
        {error && <p role="alert">{error}</p>}
        <div className="dialog-actions">
          <button type="button" onClick={onClose}>{zh ? '取消' : 'Cancel'}</button>
          <button className="primary" disabled={missing || busy}>{busy ? (zh ? '保存中…' : 'Saving…') : (spec?.submitLabel ?? (zh ? '保存' : 'Save'))}</button>
        </div>
      </form>
    </Dialog>
  )
}
