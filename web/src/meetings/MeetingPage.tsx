import React, { useCallback, useEffect, useRef, useState } from 'react'
import { BridgeClientError, MEETING_HEARTBEAT_INTERVAL_MS, getMeetingsBridge, type MeetingsBridge } from '../bridge/client'
import type { MeetingDTO, MeetingSegmentDTO } from '../generated/bridge'
import { ConfirmDialog } from '../ui/Dialog'
import { usePanelResize } from '../ui/usePanelResize'
import { audioSourceLabel, prepareMeetingCapture, releaseMeetingCapture, startMeetingSpeech, type MeetingCapturePlan } from './meetingAsr'
import type { CompanionSpeechHandle } from '../session/companion/speech'

const MIX_KEY = 'lunitide:meeting-include-system-audio'

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

const loadIncludeSystem = () => {
  try {
    return localStorage.getItem(MIX_KEY) === '1'
  } catch {
    return false
  }
}

async function retryMeetingWrite<T>(op: () => Promise<T>): Promise<T> {
  let last: unknown
  for (let attempt = 0; attempt < 4; attempt++) {
    try {
      return await op()
    } catch (error) {
      last = error
      const retryable = error instanceof BridgeClientError && error.retryable
      if (!retryable || attempt === 3) throw error
      await new Promise<void>(resolve => { window.setTimeout(resolve, 350 * (attempt + 1)) })
    }
  }
  throw last
}

export function MeetingPage({ meetings = getMeetingsBridge() }: { meetings?: MeetingsBridge }): React.JSX.Element {
  const [items, setItems] = useState<MeetingDTO[]>([])
  const [current, setCurrent] = useState<MeetingDTO>()
  const [interim, setInterim] = useState('')
  const [elapsed, setElapsed] = useState(0)
  const [busy, setBusy] = useState(false)
  const [notice, setNotice] = useState('')
  const [includeSystem, setIncludeSystem] = useState(loadIncludeSystem)
  const [draftSummary, setDraftSummary] = useState('')
  const [draftActions, setDraftActions] = useState('')
  const [draftTranscript, setDraftTranscript] = useState('')
  const [deleteTarget, setDeleteTarget] = useState<MeetingDTO>()
  const [listWidth, startListResize] = usePanelResize({
    storageKey: 'lunitide:meeting-list-width',
    initial: 280,
    min: 220,
    max: () => Math.min(480, Math.max(260, window.innerWidth - 420)),
  })
  const speechRef = useRef<CompanionSpeechHandle | null>(null)
  const captureRef = useRef<MeetingCapturePlan | undefined>(undefined)
  const tickRef = useRef<number>(0)
  const heartbeatRef = useRef<number>(0)
  const speechGen = useRef(0)
  const appendChain = useRef(Promise.resolve())
  const currentIdRef = useRef('')

  const refresh = useCallback(async () => {
    const listed = await meetings.list()
    setItems(listed.items)
    return listed.items
  }, [meetings])

  const adopt = (next: MeetingDTO) => {
    currentIdRef.current = next.meetingId
    setCurrent(next)
    setDraftSummary(next.summary || '')
    setDraftActions(next.actions || '')
    setDraftTranscript(next.transcript || '')
    setItems(values => {
      const rest = values.filter(item => item.meetingId !== next.meetingId)
      return [next, ...rest]
    })
  }

  const attachSpeech = async (meeting: MeetingDTO, plan: MeetingCapturePlan) => {
    captureRef.current = plan
    const gen = ++speechGen.current
    const listen = async () => {
      if (speechGen.current !== gen || currentIdRef.current !== meeting.meetingId) return
      const handle = await startMeetingSpeech({
        extraStreams: plan.extraStreams,
        duplex: true,
        spokenText: () => '',
        onFinal: text => {
          const id = meeting.meetingId
          const startedMs = Math.max(0, Date.now() - Date.parse(meeting.startedAt))
          appendChain.current = appendChain.current.then(() =>
            retryMeetingWrite(() => meetings.append({ meetingId: id, text, startedMs })).then(seg => {
              setCurrent(value => value && value.meetingId === id ? {
                ...value,
                segments: [...(value.segments ?? []), seg],
                transcript: [value.transcript, seg.text].filter(Boolean).join('\n'),
              } : value)
              setInterim('')
              setNotice(prev => /Bridge 请求超时|转写写入失败|本地语音识别中断/.test(prev) ? '' : prev)
            }),
          ).catch(error => {
            if (currentIdRef.current === id && speechGen.current === gen) {
              setNotice(error instanceof Error ? error.message : '转写写入失败')
            }
          })
        },
        onInterim: text => setInterim(text),
        onError: error => {
          if (speechGen.current !== gen || currentIdRef.current !== meeting.meetingId) return
          setNotice(error.message)
          speechRef.current = null
          window.setTimeout(() => {
            if (speechGen.current !== gen || currentIdRef.current !== meeting.meetingId) return
            void listen().catch(restartErr => {
              if (speechGen.current === gen && currentIdRef.current === meeting.meetingId) {
                setNotice(restartErr instanceof Error ? restartErr.message : '转写中断，仍在录制')
              }
            })
          }, 900)
        },
      })
      if (speechGen.current !== gen) {
        handle.stop()
        return
      }
      speechRef.current = handle
      if (plan.notice) setNotice(plan.notice)
    }
    await listen()
  }

  useEffect(() => {
    let alive = true
    refresh().then(async listed => {
      if (!alive) return
      const live = listed.find(item => item.status === 'recording')
      if (!live) return
      try {
        adopt(live)
        const detail = await meetings.get({ meetingId: live.meetingId }).catch(() => live)
        if (!alive) return
        adopt(detail)
        const wantMix = detail.audioSource === 'microphone_and_system'
        setIncludeSystem(wantMix)
        const plan = await prepareMeetingCapture(wantMix)
        if (!alive) {
          releaseMeetingCapture(plan)
          return
        }
        await attachSpeech(detail, plan)
      } catch (error) {
        if (alive) setNotice(error instanceof Error ? error.message : '无法继续上一场录制')
      }
    }).catch(error => {
      if (alive) setNotice(error instanceof Error ? error.message : '无法读取会议记录')
    })
    return () => { alive = false }
  }, [refresh, meetings])

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

  useEffect(() => {
    if (current?.status !== 'recording' || !meetings.heartbeat) {
      window.clearInterval(heartbeatRef.current)
      return
    }
    const id = current.meetingId
    const pulse = () => {
      void meetings.heartbeat({ meetingId: id }).catch(() => undefined)
    }
    pulse()
    heartbeatRef.current = window.setInterval(pulse, MEETING_HEARTBEAT_INTERVAL_MS)
    return () => window.clearInterval(heartbeatRef.current)
  }, [current?.status, current?.meetingId, meetings])

  useEffect(() => () => {
    speechGen.current += 1
    speechRef.current?.stop()
    speechRef.current = null
    releaseMeetingCapture(captureRef.current)
    window.clearInterval(tickRef.current)
    window.clearInterval(heartbeatRef.current)
  }, [])

  const start = async () => {
    if (busy) return
    setBusy(true)
    setNotice('')
    setInterim('')
    let plan: MeetingCapturePlan | undefined
    try {
      plan = await prepareMeetingCapture(includeSystem)
      const started = await meetings.start({ audioSource: plan.audioSource })
      adopt(started)
      try {
        await attachSpeech(started, plan)
      } catch (speechError) {
        await meetings.stop({ meetingId: started.meetingId }).catch(() => undefined)
        throw speechError
      }
    } catch (error) {
      speechGen.current += 1
      speechRef.current?.stop()
      speechRef.current = null
      releaseMeetingCapture(plan)
      captureRef.current = undefined
      if (!isCanceled(error) && !(error instanceof DOMException && (error.name === 'AbortError' || error.name === 'NotAllowedError'))) {
        setNotice(error instanceof Error ? error.message : '无法开始录制')
      }
      await refresh().catch(() => undefined)
    } finally {
      setBusy(false)
    }
  }

  const stop = async () => {
    if (!current || current.status !== 'recording' || busy) return
    setBusy(true)
    setNotice('正在生成会议纪要…')
    speechGen.current += 1
    const handle = speechRef.current
    speechRef.current = null
    try {
      await handle?.flush?.()
    } catch {
      /* last utterance still flushed below */
    }
    handle?.stop()
    releaseMeetingCapture(captureRef.current)
    captureRef.current = undefined
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

  const persistEdits = async () => {
    if (!current || current.status === 'recording') return current
    const dirty = draftSummary !== (current.summary || '') || draftActions !== (current.actions || '') || draftTranscript !== (current.transcript || '')
    if (!dirty) return current
    const next = await meetings.update({
      meetingId: current.meetingId,
      summary: draftSummary,
      actions: draftActions,
      transcript: draftTranscript,
    })
    adopt(next)
    return next
  }

  const exportDoc = async (format: 'markdown' | 'html' | 'txt') => {
    if (!current || busy) return
    setBusy(true)
    try {
      await persistEdits()
      const result = await meetings.exportMeeting({ meetingId: current.meetingId, format })
      setNotice(`已导出到 ${result.path}`)
    } catch (error) {
      if (!isCanceled(error)) setNotice(error instanceof Error ? error.message : '无法导出')
    } finally {
      setBusy(false)
    }
  }

  const removeMeeting = async () => {
    if (!deleteTarget || busy) return
    setBusy(true)
    try {
      await meetings.delete({ meetingId: deleteTarget.meetingId })
      setItems(values => values.filter(item => item.meetingId !== deleteTarget.meetingId))
      if (current?.meetingId === deleteTarget.meetingId) {
        setCurrent(undefined)
        setDraftSummary('')
        setDraftActions('')
        setDraftTranscript('')
      }
      setDeleteTarget(undefined)
      setNotice('会议已删除')
    } catch (error) {
      setNotice(error instanceof Error ? error.message : '无法删除会议')
    } finally {
      setBusy(false)
    }
  }

  const recording = current?.status === 'recording'
  const segments: MeetingSegmentDTO[] = current?.segments ?? []
  const liveLines = segments.map(seg => seg.text)
  if (current?.transcript && liveLines.length === 0) liveLines.push(...current.transcript.split('\n').filter(Boolean))
  const source = current?.audioSource ?? (includeSystem ? 'microphone_and_system' : 'microphone')

  return (
    <div className="meeting-shell" style={{ '--meeting-list-width': `${listWidth}px` } as React.CSSProperties}>
      <aside className="meeting-list" aria-label="历史会议">
        <header>
          <h2>会议记录</h2>
          <p>本机麦克风转写。勾选后混录这台电脑的系统声音（扬声器对面更准）。不会共享给其他电脑。</p>
        </header>
        {items.length === 0 ? <p className="meeting-empty">还没有会议。点开始录制这一场。</p> : items.map(item => (
          <div className="meeting-row" key={item.meetingId}>
            <button
              type="button"
              className={current?.meetingId === item.meetingId ? 'on' : ''}
              onClick={() => void open(item.meetingId)}
            >
              <b>{item.title}</b>
              <small>{formatWhen(item.startedAt)} · {formatMeetingDuration(item.durationMs)} · {STATUS[item.status]}</small>
            </button>
            {item.status !== 'recording' && (
              <button type="button" className="meeting-row-delete" aria-label={`删除 ${item.title}`} onClick={() => setDeleteTarget(item)}>删除</button>
            )}
          </div>
        ))}
      </aside>
      <div className="panel-resizer split-resizer" role="separator" aria-label="调整会议列表宽度" aria-orientation="vertical" onPointerDown={startListResize} />
      <section className="meeting-main" aria-label="会议工作台">
        <header className="meeting-hero">
          <div>
            <h2>{current?.title || '新的会议'}</h2>
            <p>点击开始后进入实时转写。系统声音走本机屏幕/窗口选择（只为收录声音，画面不保存）。结束后生成摘要、待办和逐字稿，可导出。</p>
          </div>
          <div className="meeting-clock" aria-live="polite">{formatMeetingDuration(recording ? elapsed : current?.durationMs ?? 0)}</div>
        </header>
        <div className="meeting-rec">
          {recording
            ? <button type="button" className="meeting-stop" disabled={busy} onClick={() => void stop()}>停止</button>
            : <button type="button" className="meeting-start" disabled={busy} onClick={() => void start()}>开始</button>}
          <label className="meeting-mix">
            <input
              type="checkbox"
              checked={includeSystem}
              disabled={recording || busy}
              onChange={event => {
                const next = event.target.checked
                setIncludeSystem(next)
                try { localStorage.setItem(MIX_KEY, next ? '1' : '0') } catch { /* ignore */ }
              }}
            />
            同时收录本机系统声音
          </label>
          <span>{recording ? audioSourceLabel(current?.audioSource, true) : includeSystem ? '开始时请在窗口选择器里点腾讯会议、飞书或浏览器标签页，以便收录对面说话（本机环回，取消则不会开这场会）' : audioSourceLabel('microphone')}</span>
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
              <textarea aria-label="会议摘要" value={draftSummary} onChange={e => setDraftSummary(e.target.value)} placeholder={current.summaryError || '尚未生成摘要。'} />
            </section>
            <section>
              <h3>决议/待办</h3>
              <textarea aria-label="决议/待办" value={draftActions} onChange={e => setDraftActions(e.target.value)} placeholder="尚未生成待办。" />
            </section>
            <section>
              <h3>全文逐字稿</h3>
              <textarea aria-label="全文逐字稿" value={draftTranscript} onChange={e => setDraftTranscript(e.target.value)} placeholder="（空）" />
            </section>
            <p className="meeting-empty">{audioSourceLabel(source)}</p>
            <div className="meeting-export">
              {current.status === 'needs_summary' || current.status === 'transcribed' ? (
                <button type="button" disabled={busy} onClick={() => void retry()}>重试生成摘要</button>
              ) : null}
              <button type="button" disabled={busy} onClick={() => void persistEdits().then(() => setNotice('纪要已保存')).catch(error => setNotice(error instanceof Error ? error.message : '无法保存'))}>保存编辑</button>
              <button type="button" disabled={busy} onClick={() => void exportDoc('markdown')}>导出 Markdown</button>
              <button type="button" disabled={busy} onClick={() => void exportDoc('html')}>导出 HTML</button>
              <button type="button" disabled={busy} onClick={() => void exportDoc('txt')}>导出文本</button>
            </div>
          </article>
        )}
      </section>
      <ConfirmDialog
        open={!!deleteTarget}
        title={`删除会议「${deleteTarget?.title ?? ''}」？`}
        description="这条会议记录、摘要、待办和逐字稿将从本机删除，不可撤销。"
        busy={busy}
        onCancel={() => setDeleteTarget(undefined)}
        onConfirm={() => void removeMeeting()}
      />
    </div>
  )
}
