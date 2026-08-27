import React, { useCallback, useEffect, useRef, useState } from 'react'
import { getMeetingsBridge, type MeetingsBridge } from '../bridge/client'
import type { MeetingDTO, MeetingSegmentDTO } from '../generated/bridge'
import { startMeetingSpeech } from './meetingAsr'
import type { CompanionSpeechHandle } from '../session/companion/speech'

const STATUS: Record<MeetingDTO['status'], string> = {
  recording: '录制中',
  transcribed: '已转写',
  summarizing: '生成纪要中',
  ready: '已完成',
  needs_summary: '尚未生成摘要',
}

export function formatMeetingDuration(ms: number): string {
  const safe = Number.isFinite(ms) && ms > 0 ? Math.floor(ms) : 0
  const sec = Math.floor(safe / 1000)
  const h = Math.floor(sec / 3600)
  const m = Math.floor((sec % 3600) / 60)
  const s = sec % 60
  if (h > 0) return `${h}:${String(m).padStart(2, '0')}:${String(s).padStart(2, '0')}`
  return `${m}:${String(s).padStart(2, '0')}`
}

const formatWhen = (iso: string) => {
  if (!iso) return '—'
  const d = new Date(iso)
  if (Number.isNaN(d.getTime())) return iso
  return d.toLocaleString('zh-CN', { month: '2-digit', day: '2-digit', hour: '2-digit', minute: '2-digit' })
}

const isCanceled = (error: unknown) => error instanceof Error && /取消/.test(error.message)

export function MeetingPage({ meetings = getMeetingsBridge() }: { meetings?: MeetingsBridge }): React.JSX.Element {
  const [items, setItems] = useState<MeetingDTO[]>([])
  const [current, setCurrent] = useState<MeetingDTO>()
  const [interim, setInterim] = useState('')
  const [elapsed, setElapsed] = useState(0)
  const [busy, setBusy] = useState(false)
  const [notice, setNotice] = useState('')
  const speechRef = useRef<CompanionSpeechHandle | null>(null)
  const tickRef = useRef<number>(0)
  const appendChain = useRef(Promise.resolve())

  const refresh = useCallback(async () => {
    const listed = await meetings.list()
    setItems(listed.items)
    return listed.items
  }, [meetings])

  useEffect(() => {
    let alive = true
    refresh().then(listed => {
      if (!alive) return
      const live = listed.find(item => item.status === 'recording')
      if (live) setCurrent(live)
    }).catch(error => {
      if (alive) setNotice(error instanceof Error ? error.message : '无法读取会议记录')
    })
    return () => { alive = false }
  }, [refresh])

  useEffect(() => {
    if (current?.status !== 'recording' || !current.startedAt) {
      window.clearInterval(tickRef.current)
      return
    }
    const started = Date.parse(current.startedAt)
    const pulse = () => setElapsed(Number.isNaN(started) ? 0 : Math.max(0, Date.now() - started))
    pulse()
    tickRef.current = window.setInterval(pulse, 250)
    return () => window.clearInterval(tickRef.current)
  }, [current?.status, current?.startedAt])

  useEffect(() => () => {
    speechRef.current?.stop()
    window.clearInterval(tickRef.current)
  }, [])

  const adopt = (next: MeetingDTO) => {
    setCurrent(next)
    setItems(values => {
      const rest = values.filter(item => item.meetingId !== next.meetingId)
      return [next, ...rest]
    })
  }

  const start = async () => {
    if (busy) return
    setBusy(true)
    setNotice('')
    setInterim('')
    try {
      const started = await meetings.start({})
      adopt(started)
      try {
        const handle = await startMeetingSpeech({
          duplex: true,
          spokenText: () => '',
          onFinal: text => {
            const id = started.meetingId
            const startedMs = Math.max(0, Date.now() - Date.parse(started.startedAt))
            appendChain.current = appendChain.current.then(() =>
              meetings.append({ meetingId: id, text, startedMs }).then(seg => {
                setCurrent(value => value && value.meetingId === id ? {
                  ...value,
                  segments: [...(value.segments ?? []), seg],
                  transcript: [value.transcript, seg.text].filter(Boolean).join('\n'),
                } : value)
                setInterim('')
              }),
            ).catch(error => setNotice(error instanceof Error ? error.message : '转写写入失败'))
          },
          onInterim: text => setInterim(text),
          onError: error => setNotice(error.message),
        })
        speechRef.current = handle
      } catch (speechError) {
        await meetings.stop({ meetingId: started.meetingId }).catch(() => undefined)
        throw speechError
      }
    } catch (error) {
      speechRef.current?.stop()
      speechRef.current = null
      setNotice(error instanceof Error ? error.message : '无法开始录制')
      await refresh().catch(() => undefined)
    } finally {
      setBusy(false)
    }
  }

  const stop = async () => {
    if (!current || current.status !== 'recording' || busy) return
    setBusy(true)
    setNotice('正在生成会议纪要…')
    speechRef.current?.stop()
    speechRef.current = null
    setInterim('')
    try {
      await appendChain.current
      const stopped = await meetings.stop({ meetingId: current.meetingId })
      adopt(stopped)
      const notes = await meetings.summarize({ meetingId: current.meetingId })
      adopt(notes)
      setNotice(notes.status === 'ready' ? '纪要已生成，可以导出。' : notes.summaryError || '尚未生成摘要，逐字稿已保存。')
    } catch (error) {
      setNotice(error instanceof Error ? error.message : '无法结束录制')
    } finally {
      setBusy(false)
    }
  }

  const retry = async () => {
    if (!current || busy) return
    setBusy(true)
    setNotice('正在生成会议纪要…')
    try {
      const notes = await meetings.summarize({ meetingId: current.meetingId })
      adopt(notes)
      setNotice(notes.status === 'ready' ? '纪要已生成，可以导出。' : notes.summaryError || '尚未生成摘要')
    } catch (error) {
      setNotice(error instanceof Error ? error.message : '无法生成摘要')
    } finally {
      setBusy(false)
    }
  }

  const open = async (id: string) => {
    if (busy || current?.status === 'recording') return
    try {
      adopt(await meetings.get({ meetingId: id }))
      setNotice('')
    } catch (error) {
      setNotice(error instanceof Error ? error.message : '无法打开会议')
    }
  }

  const exportDoc = async (format: 'markdown' | 'html' | 'txt') => {
    if (!current || busy) return
    setBusy(true)
    try {
      const result = await meetings.exportMeeting({ meetingId: current.meetingId, format })
      setNotice(`已导出到 ${result.path}`)
    } catch (error) {
      if (!isCanceled(error)) setNotice(error instanceof Error ? error.message : '无法导出')
    } finally {
      setBusy(false)
    }
  }

  const recording = current?.status === 'recording'
  const segments: MeetingSegmentDTO[] = current?.segments ?? []
  const liveLines = segments.map(seg => seg.text)
  if (current?.transcript && liveLines.length === 0) liveLines.push(...current.transcript.split('\n').filter(Boolean))

  return (
    <div className="meeting-shell">
      <aside className="meeting-list" aria-label="历史会议">
        <header>
          <h2>会议记录</h2>
          <p>本机麦克风转写。未混录系统扬声器。</p>
        </header>
        {items.length === 0 ? <p className="meeting-empty">还没有会议。点开始录制这一场。</p> : items.map(item => (
          <button
            type="button"
            key={item.meetingId}
            className={current?.meetingId === item.meetingId ? 'on' : ''}
            onClick={() => void open(item.meetingId)}
          >
            <b>{item.title}</b>
            <small>{formatWhen(item.startedAt)} · {formatMeetingDuration(item.durationMs)} · {STATUS[item.status]}</small>
          </button>
        ))}
      </aside>
      <section className="meeting-main" aria-label="会议工作台">
        <header className="meeting-hero">
          <div>
            <h2>{current?.title || '新的会议'}</h2>
            <p>点击开始后，本机麦克风进入实时转写。结束后生成摘要、待办和逐字稿，可导出。</p>
          </div>
          <div className="meeting-clock" aria-live="polite">{formatMeetingDuration(recording ? elapsed : current?.durationMs ?? 0)}</div>
        </header>
        <div className="meeting-rec">
          {recording
            ? <button type="button" className="meeting-stop" disabled={busy} onClick={() => void stop()}>停止</button>
            : <button type="button" className="meeting-start" disabled={busy} onClick={() => void start()}>开始</button>}
          <span>{recording ? '正在录制本机麦克风' : '仅本机麦克风，未混录系统扬声器'}</span>
        </div>
        {notice && <p className="meeting-notice" role="status">{notice}</p>}
        <div className="meeting-transcript" aria-live="polite" aria-label="实时逐字稿">
          {liveLines.length === 0 && !interim && !recording ? <p className="meeting-empty">还没有逐字稿。</p> : null}
          {liveLines.map((line, index) => <p key={`${index}:${line.slice(0, 24)}`}>{line}</p>)}
          {interim ? <p className="meeting-interim">{interim}</p> : null}
        </div>
        {current && current.status !== 'recording' && (
          <article className="meeting-doc">
            <section>
              <h3>会议摘要</h3>
              <p>{current.summary || current.summaryError || '尚未生成摘要。'}</p>
            </section>
            <section>
              <h3>决议/待办</h3>
              <pre>{current.actions || '尚未生成待办。'}</pre>
            </section>
            <section>
              <h3>全文逐字稿</h3>
              <pre>{current.transcript || '（空）'}</pre>
            </section>
            <div className="meeting-export">
              {current.status === 'needs_summary' || current.status === 'transcribed' ? (
                <button type="button" disabled={busy} onClick={() => void retry()}>重试生成摘要</button>
              ) : null}
              <button type="button" disabled={busy} onClick={() => void exportDoc('markdown')}>导出 Markdown</button>
              <button type="button" disabled={busy} onClick={() => void exportDoc('html')}>导出 HTML</button>
              <button type="button" disabled={busy} onClick={() => void exportDoc('txt')}>导出文本</button>
            </div>
          </article>
        )}
      </section>
    </div>
  )
}
