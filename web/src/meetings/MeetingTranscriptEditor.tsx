import { forwardRef, useCallback, useEffect, useImperativeHandle, useRef, useState } from 'react'
import type { MeetingsBridge } from '../bridge/client'
import type { MeetingDTO, MeetingsTranscriptGetResult } from '../generated/bridge'

function transcriptUserError(err: unknown, fallback: string): string {
  const detail = err instanceof Error ? err.message.trim() : ''
  return /[\u4e00-\u9fff]/.test(detail) ? detail : fallback
}
export interface MeetingTranscriptEditorHandle { save(): Promise<MeetingDTO | undefined> }
type Props = { meeting: MeetingDTO; meetings: MeetingsBridge; disabled?: boolean; onSaved(meeting: MeetingDTO): void }
const pageSize = 16_384

export const MeetingTranscriptEditor = forwardRef<MeetingTranscriptEditorHandle, Props>(function MeetingTranscriptEditor(props, ref) {
  const current = useRef(props)
  current.current = props
  const [page, setPage] = useState<MeetingsTranscriptGetResult>()
  const pageRef = useRef(page)
  const [draft, setDraft] = useState('')
  const draftRef = useRef('')
  const dirty = useRef(false)
  const [busy, setBusy] = useState(false)
  const busyRef = useRef(false)
  const [error, setError] = useState('')
  const [latest, setLatest] = useState<{ meeting: MeetingDTO; page: MeetingsTranscriptGetResult }>()
  const epoch = useRef(0)
  const identity = useRef(props.meeting.meetingId)
  identity.current = props.meeting.meetingId
  const assign = (next: MeetingsTranscriptGetResult) => {
    pageRef.current = next
    setPage(next)
    draftRef.current = next.text
    setDraft(next.text)
    dirty.current = false
    setLatest(undefined)
    setError('')
  }
  const loadPage = useCallback(async (offset: number, meeting = current.current.meeting) => {
    const request = ++epoch.current
    const id = meeting.meetingId
    busyRef.current = true
    setBusy(true)
    try {
      const result = await current.current.meetings.transcriptGet({ meetingId: id, transcriptRevision: meeting.transcriptRevision ?? 0, offset })
      if (request !== epoch.current || identity.current !== id) return
      if (result.meetingId !== id || result.transcriptRevision !== meeting.transcriptRevision || result.offset !== offset) throw new Error('逐字稿版本已变化，请重新读取。')
      assign(result)
    } catch (cause) {
      if (request === epoch.current && identity.current === id) setError(transcriptUserError(cause, '无法读取本页原稿'))
    } finally {
      if (request === epoch.current && identity.current === id) { busyRef.current = false; setBusy(false) }
    }
  }, [])

  useEffect(() => {
    if (dirty.current) { setError('逐字稿已有新版本，本页草稿仍保留。请核对最新原稿。'); return }
    if (busyRef.current) return
    void loadPage(pageRef.current?.offset ?? 0)
  }, [props.meeting.meetingId, props.meeting.transcriptRevision, loadPage])
  useEffect(() => () => { epoch.current++ }, [])

  const save = async () => {
    if (!dirty.current) return undefined
    if (busyRef.current) throw new Error('本页正在处理，请稍后重试。')
    const source = pageRef.current
    if (!source) throw new Error('请先读取完整的一页原稿。')
    if (Array.from(draftRef.current).length > pageSize) throw new Error('每页修改最多 16,384 字，请缩短本页后再保存。')
    const meeting = current.current.meeting
    if (source.transcriptRevision !== meeting.transcriptRevision) throw new Error('原稿版本已变化，请先核对最新原稿，本页草稿仍保留。')
    const request = ++epoch.current
    const id = meeting.meetingId
    busyRef.current = true
    setBusy(true)
    setError('')
    try {
      const next = await current.current.meetings.update({ meetingId: id, expectedRevision: meeting.revision,
        transcriptEdit: { transcriptRevision: source.transcriptRevision, offset: source.offset, deleteRunes: Array.from(source.text).length, text: draftRef.current } })
      if (request !== epoch.current || identity.current !== id) return next
      dirty.current = false
      current.current.onSaved(next)
      const offset = Math.min(source.offset, Math.floor(Math.max(0, (next.transcriptTotalRunes ?? 0) - 1) / pageSize) * pageSize)
      await loadPage(offset, next)
      return next
    } catch (cause) {
      if (request === epoch.current && identity.current === id) setError(`${transcriptUserError(cause, '无法保存本页')}。草稿仍保留，可核对最新原稿。`)
      throw cause
    } finally {
      if (request === epoch.current && identity.current === id) { busyRef.current = false; setBusy(false) }
    }
  }
  useImperativeHandle(ref, () => ({ save }))

  const inspectLatest = async () => {
    if (busyRef.current) return
    const request = ++epoch.current
    const id = current.current.meeting.meetingId
    busyRef.current = true
    setBusy(true)
    try {
      const next = await current.current.meetings.get({ meetingId: id })
      const offset = Math.min(pageRef.current?.offset ?? 0, Math.floor(Math.max(0, (next.transcriptTotalRunes ?? 0) - 1) / pageSize) * pageSize)
      const result = await current.current.meetings.transcriptGet({ meetingId: id, transcriptRevision: next.transcriptRevision ?? 0, offset })
      if (request !== epoch.current || identity.current !== id) return
      setLatest({ meeting: next, page: result })
    } catch (cause) {
      if (request === epoch.current && identity.current === id) setError(transcriptUserError(cause, '无法读取最新原稿'))
    } finally {
      if (request === epoch.current && identity.current === id) { busyRef.current = false; setBusy(false) }
    }
  }
  return <div>
    <p>这份逐字稿较长，按页读取和保存；导出包含完整原稿。每页最多 16,384 字。</p>
    {page && <>
      <p>第 {page.offset + 1}–{page.offset + Array.from(page.text).length} 字，共 {page.totalRunes} 字；原稿版本 {page.transcriptRevision}。</p>
      <textarea aria-label="本页逐字稿" disabled={busy || props.disabled} value={draft} onChange={event => { draftRef.current = event.target.value; dirty.current = event.target.value !== page.text; setDraft(event.target.value) }} />
      <div>
        <button type="button" disabled={busy || props.disabled || dirty.current || page.offset === 0} onClick={() => void loadPage(Math.max(0, page.offset - pageSize))}>上一页原稿</button>
        <button type="button" disabled={busy || props.disabled || dirty.current || page.nextOffset === 0} onClick={() => void loadPage(page.nextOffset)}>下一页原稿</button>
        <button type="button" disabled={busy || props.disabled || !dirty.current || Array.from(draft).length > pageSize} onClick={() => void save().catch(() => undefined)}>保存本页原稿</button>
      </div>
      {dirty.current && <p>本页有未保存修改，保存后才能翻页。</p>}
      {Array.from(draft).length > pageSize && <p role="alert">本页超过 16,384 字，尚未保存；请缩短本页。</p>}
    </>}
    {busy && <p role="status">正在处理本页原稿…</p>}
    {error && <div role="alert"><p>{error}</p><button type="button" disabled={busy || props.disabled} onClick={() => void inspectLatest()}>核对最新原稿</button></div>}
    {latest && <section aria-label="核对当前页">
      <p>以下是最新版本的这一页。原稿变化可能使文字位置移动，请先复制需要保留的草稿，再采用最新页继续编辑。</p>
      <textarea aria-label="最新页逐字稿" readOnly value={latest.page.text} />
      <button type="button" disabled={busy || props.disabled} onClick={() => { const next = latest; assign(next.page); current.current.onSaved(next.meeting) }}>采用最新页并放弃本页草稿</button>
    </section>}
  </div>
})
