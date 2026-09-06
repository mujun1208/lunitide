import { useEffect, useRef, useState } from 'react'
import type { MeetingsBridge } from '../bridge/client'
import type { MeetingDTO, MeetingsSegmentsListResult } from '../generated/bridge'

export function MeetingSegments({ meeting, load }: { meeting: MeetingDTO; load: MeetingsBridge['segmentsList'] }) {
  const [page, setPage] = useState<MeetingsSegmentsListResult>()
  const [cursors, setCursors] = useState<number[]>([0])
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  const epoch = useRef(0)
  const identity = `${meeting.meetingId}:${meeting.revision}`
  const identityRef = useRef(identity)
  identityRef.current = identity
  useEffect(() => { epoch.current++; setPage(undefined); setCursors([0]); setError(''); setBusy(false); return () => { epoch.current++ } }, [identity])
  const show = async (afterSeq: number, nextCursors: number[], throughSeq?: number) => {
    const request = ++epoch.current
    setBusy(true)
    setError('')
    try {
      const result = await load({ meetingId: meeting.meetingId, expectedRevision: meeting.revision, afterSeq, ...(throughSeq === undefined ? {} : { throughSeq }) })
      if (request !== epoch.current || identity !== identityRef.current) return
      if (result.revision !== meeting.revision) throw new Error('分段记录版本已变化，请重新读取。')
      setPage(result)
      setCursors(nextCursors)
    } catch (cause) {
      if (request === epoch.current && identity === identityRef.current) setError(cause instanceof Error ? cause.message : '无法读取分段记录')
    } finally {
      if (request === epoch.current && identity === identityRef.current) setBusy(false)
    }
  }
  return <div>
    <button type="button" disabled={busy} onClick={() => void show(0, [0], undefined)}>查看完整分段记录</button>
    {error && <p role="alert">{error}</p>}
    {page && <section aria-label="原始分段记录">
      <p>每页 10 段，保留原始识别内容；本次查看截至第 {page.throughSeq} 段。</p>
      {page.items.map(segment => <p key={segment.segmentId}>{segment.seq} · {(segment.startedMs / 1000).toFixed(1)}秒：{segment.text}</p>)}
      <button type="button" disabled={busy || cursors.length <= 1} onClick={() => void show(cursors[cursors.length - 2], cursors.slice(0, -1), page.throughSeq)}>上一页分段</button>
      <button type="button" disabled={busy || !page.hasMore} onClick={() => void show(page.nextSeq, [...cursors, page.nextSeq], page.throughSeq)}>下一页分段</button>
    </section>}
  </div>
}
