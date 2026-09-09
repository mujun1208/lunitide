import { useState } from 'react'
import { FlaskConical, Trash2 } from 'lucide-react'
import { createMutationAttempt, type ExpertBridge } from '../bridge/client'
import type { ExpertListResult } from '../generated/bridge'
import { Dialog } from '../ui/Dialog'

export function ExpertLifecycleActions({ item, versionId, bridge, disabled, onDeleted }: {
  item: ExpertListResult['experts'][number]
  versionId: string
  bridge: ExpertBridge
  disabled: boolean
  onDeleted: () => Promise<void>
}) {
  const [mode, setMode] = useState<'trial' | 'delete' | null>(null)
  const [input, setInput] = useState('')
  const [output, setOutput] = useState('')
  const [error, setError] = useState('')
  const [busy, setBusy] = useState(false)
  if (item.creationOrigin !== 'manual' || !item.isOwn) return null
  const open = (next: 'trial' | 'delete') => { setMode(next); setError(''); setOutput('') }
  const submit = async () => {
    setBusy(true); setError(''); setOutput('')
    try {
      if (mode === 'trial' && bridge.try) {
        const result = await bridge.try({ expertId: item.expertId, expectedVersionId: versionId, input: input.trim() })
        setOutput(result.output)
      } else if (mode === 'delete' && bridge.delete) {
        const digest = await crypto.subtle.digest('SHA-256', new TextEncoder().encode(`expert.delete|${item.expertId}|${versionId}`))
        const confirmToken = Array.from(new Uint8Array(digest), byte => byte.toString(16).padStart(2, '0')).join('')
        const payload = { expertId: item.expertId, expectedVersionId: versionId, confirmToken }
        await bridge.delete(payload, { attempt: createMutationAttempt('expert.delete', payload) })
        setMode(null)
        await onDeleted()
      }
    } catch (reason) {
      setError(reason instanceof Error ? reason.message : '操作失败，请重试')
    } finally { setBusy(false) }
  }
  return <>
    {item.state !== 'archived' && bridge.try && <button type="button" disabled={disabled || busy || !versionId} onClick={() => open('trial')}><FlaskConical size={16} aria-hidden="true" />试用</button>}
    {bridge.delete && <button type="button" className="danger" disabled={disabled || busy || !versionId} onClick={() => open('delete')}><Trash2 size={16} aria-hidden="true" />删除</button>}
    <Dialog open={mode !== null} title={`${mode === 'trial' ? '试用' : '删除'}「${item.name}」`} onClose={() => { if (!busy) setMode(null) }}>
      <form className="editor-dialog" onSubmit={event => { event.preventDefault(); void submit() }}>
        {mode === 'trial' ? <>
          <p>文本试答 · v{item.semver} · {item.state === 'disabled' ? '尚未启用' : '已启用'}</p>
          <label>试答题目<textarea value={input} maxLength={8000} rows={5} disabled={busy} onChange={event => setInput(event.target.value)} /></label>
          {output && <div className="sd-dep" role="region" aria-label="试答结果" style={{ whiteSpace: 'pre-wrap', overflowWrap: 'anywhere' }}>{output}</div>}
        </> : <p>删除后不可恢复，历史版本与审计记录保留；名称仍保留在历史记录中。项目或会话仍引用该专家时不能删除。</p>}
        {error && <p role="alert">{error}</p>}
        <div className="dialog-actions">
          <button type="button" disabled={busy} onClick={() => setMode(null)}>取消</button>
          <button className={mode === 'delete' ? 'danger' : 'primary'} disabled={busy || (mode === 'trial' && !input.trim())}>{busy ? '处理中…' : mode === 'trial' ? '开始试答' : '确认删除'}</button>
        </div>
      </form>
    </Dialog>
  </>
}
