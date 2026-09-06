import { useEffect, useRef, useState } from 'react'
import type { MeetingsBridge } from '../bridge/client'
import type { MeetingDTO, MeetingsSummarySourceGetResult } from '../generated/bridge'

export function MeetingSummarySource({ meeting, load }: { meeting: MeetingDTO; load: MeetingsBridge['summarySource'] }) {
  const [page, setPage] = useState<MeetingsSummarySourceGetResult>()
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  const identity = `${meeting.meetingId}:${meeting.summarySourceDigest ?? ''}`
  const identityRef = useRef(identity)
  identityRef.current = identity
  const epoch = useRef(0)
  useEffect(() => {
    epoch.current++
    setPage(undefined)
    setBusy(false)
    setError('')
    return () => { epoch.current++ }
  }, [identity])

  const sourceRevision = meeting.summarySourceRevision ?? 0
  const known = sourceRevision > 0 && !!meeting.summarySourceDigest
  const stale = known && sourceRevision !== meeting.transcriptRevision
  const show = async (offset: number) => {
    if (!known || busy) return
    const requestEpoch = epoch.current
    const requestIdentity = identity
    setBusy(true)
    setError('')
    try {
      const result = await load({ meetingId: meeting.meetingId, sourceDigest: meeting.summarySourceDigest!, offset })
      if (requestEpoch !== epoch.current || requestIdentity !== identityRef.current) return
      if (result.meetingId !== meeting.meetingId || result.sourceDigest !== meeting.summarySourceDigest || result.offset !== offset) {
        throw new Error('摘要来源已变化，请刷新会议后重新查看。')
      }
      setPage(result)
    } catch (cause) {
      if (requestEpoch === epoch.current && requestIdentity === identityRef.current) {
        setError(cause instanceof Error ? cause.message : '无法读取摘要来源，请重试。')
      }
    } finally {
      if (requestEpoch === epoch.current && requestIdentity === identityRef.current) setBusy(false)
    }
  }

  if (!meeting.summary && !meeting.actions) return null
  return <div className="meeting-summary-source">
    <p role={stale || !known ? 'note' : undefined}>
      {!known ? '摘要输入版本未知，请核对内容或重新生成。' : stale
        ? `逐字稿已修改。保留的旧摘要依据版本 ${sourceRevision}，当前原稿为版本 ${meeting.transcriptRevision}，请重新生成。`
        : `摘要依据逐字稿版本 ${sourceRevision}。`}
      {meeting.summaryEdited ? ' 摘要或待办已人工编辑。' : ''}
    </p>
    {known && <button type="button" disabled={busy} onClick={() => void show(page?.offset ?? 0)}>{busy ? '正在读取摘要来源…' : page ? '重新读取本页来源' : '查看摘要所用原稿'}</button>}
    {error && <p role="alert">{error}</p>}
    {page && <section aria-label="摘要所用原稿">
      <p>生成时标题：{page.title}</p>
      <p>第 {page.offset + 1}–{page.offset + Array.from(page.transcript).length} 字，共 {page.totalRunes} 字。每页最多 16,384 字。</p>
      <textarea aria-label="摘要来源原稿" readOnly value={page.transcript} />
      <div>
        <button type="button" disabled={busy || page.offset === 0} onClick={() => void show(Math.max(0, page.offset - 16_384))}>上一页来源</button>
        <button type="button" disabled={busy || page.nextOffset === 0} onClick={() => void show(page.nextOffset)}>下一页来源</button>
        <button type="button" onClick={() => { epoch.current++; setBusy(false); setPage(undefined); setError('') }}>收起来源</button>
      </div>
    </section>}
  </div>
}
