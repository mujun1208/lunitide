import React, { useCallback, useEffect, useRef, useState } from 'react'
import { BridgeClientError, MEETING_HEARTBEAT_INTERVAL_MS, getMeetingsBridge, getProviderBridge, type MeetingsBridge } from '../bridge/client'
import type { MeetingDTO, MeetingSegmentDTO, ModelDTO, ProviderDTO } from '../generated/bridge'
import { llmReadyProviders, pickDefaultLLM, pickDefaultVoice } from '../provider/modelKind'
import { loadMeetingSettings, saveMeetingSettings, type MeetingListen, type MeetingSettings } from './meetingSettings'
import { ConfirmDialog } from '../ui/Dialog'
import { usePanelResize } from '../ui/usePanelResize'
import { audioSourceLabel, captureStateNotice, decodeMeetingPcmBase64, engineLoopbackPlan, MEETING_CATCHUP_HINT, mixMeetingPcmS16le, pcmFrameFromSamples, planHasLiveSystemAudio, prepareMeetingCapture, recoverMeetingSystemAudio, releaseMeetingCapture, startMeetingSpeech, type MeetingCapturePlan } from './meetingAsr'
import { ASR_INTERRUPTED_NOTICE, startMeetingAudioRecorder, trimLiveSegments, type MeetingAudioHandle } from './meetingAudio'
import { watchCaptureTracksEnded } from './meetingCapture'
import type { CompanionSpeechHandle } from '../session/companion/speech'

const SUMMARIZE_POLL_MS = 4_000
const SYSTEM_AUDIO_RECOVER_MS = 15_000
const LOOPBACK_POLL_MS = 80
/** Web Speech and sherpa both go quiet after a long un-endpointed clip. Restart ASR, keep the WAV. */
export const MEETING_CAPTION_STALL_MS = 25_000
export const MEETING_CAPTION_STALL_POLL_MS = 2_000

function speechNotice(error: unknown): string {
  return error instanceof Error && error.message ? error.message : ASR_INTERRUPTED_NOTICE
}

async function capturePlanForStarted(meeting: MeetingDTO): Promise<MeetingCapturePlan> {
  if (meeting.audioSource === 'microphone_and_system') return engineLoopbackPlan()
  return prepareMeetingCapture({ interactive: false })
}

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

function honestNotes(meeting: MeetingDTO): MeetingDTO {
  if (meeting.status === 'recording' || meeting.status === 'summarizing' || meeting.status === 'needs_summary') return meeting
  if (meeting.status === 'ready' && (meeting.summary || meeting.actions)) return meeting
  if (!meeting.summary && !meeting.actions) {
    return {
      ...meeting,
      status: 'needs_summary',
      summaryError: meeting.summaryError || '尚未生成摘要，逐字稿已保存。可重试生成摘要。',
    }
  }
  return meeting
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
  const [stopping, setStopping] = useState(false)
  const [notice, setNotice] = useState('')
  const [draftSummary, setDraftSummary] = useState('')
  const [draftActions, setDraftActions] = useState('')
  const [draftTranscript, setDraftTranscript] = useState('')
  const [deleteTarget, setDeleteTarget] = useState<MeetingDTO>()
  const [prefs, setPrefs] = useState<MeetingSettings>(() => loadMeetingSettings())
  const [llmChoices, setLlmChoices] = useState<Array<{ provider: ProviderDTO; model: ModelDTO }>>([])
  const prefsRef = useRef(prefs)
  prefsRef.current = prefs
  const [listWidth, startListResize] = usePanelResize({
    storageKey: 'lunitide:meeting-list-width',
    initial: 280,
    min: 220,
    max: () => Math.min(480, Math.max(260, window.innerWidth - 420)),
  })
  const speechRef = useRef<CompanionSpeechHandle | null>(null)
  const captureRef = useRef<MeetingCapturePlan | undefined>(undefined)
  const audioRef = useRef<MeetingAudioHandle | null>(null)
  const pcmTapRef = useRef<((frame: { base64: string; samples: Int16Array; peak: number }) => void) | undefined>(undefined)
  const tickRef = useRef<number>(0)
  const heartbeatRef = useRef<number>(0)
  const recoverRef = useRef<number>(0)
  const stallWatchRef = useRef<number>(0)
  const loopbackPollRef = useRef<number>(0)
  const loopbackHoldRef = useRef<Int16Array | undefined>(undefined)
  const summarizePollRef = useRef<number>(0)
  const unwatchRef = useRef<() => void>(() => undefined)
  const speechGen = useRef(0)
  const userStopRef = useRef(false)
  const appendChain = useRef(Promise.resolve())
  const currentIdRef = useRef('')

  useEffect(() => {
    void getProviderBridge().list().then(listed => {
      const ready = llmReadyProviders(listed.items)
      const choices = ready.flatMap(provider => provider.models.map(model => ({ provider, model })))
      setLlmChoices(choices)
      setPrefs(current => {
        if (current.modelId && choices.some(item => item.model.modelId === current.modelId)) return current
        const picked = pickDefaultLLM(listed.items)
        if (!picked) return current
        return saveMeetingSettings({ ...current, modelId: picked.modelId })
      })
    }).catch(() => undefined)
  }, [])

  const updatePrefs = (patch: Partial<MeetingSettings>) => {
    setPrefs(current => saveMeetingSettings({ ...current, ...patch }))
  }

  const refresh = useCallback(async () => {
    const listed = await meetings.list()
    setItems(listed.items)
    return listed.items
  }, [meetings])

  const adopt = (next: MeetingDTO) => {
    currentIdRef.current = next.meetingId
    const view = next.status === 'recording' && next.segments
      ? { ...next, segments: trimLiveSegments(next.segments) }
      : next
    setCurrent(view)
    setDraftSummary(view.summary || '')
    setDraftActions(view.actions || '')
    setDraftTranscript(view.transcript || '')
    setItems(values => {
      const rest = values.filter(item => item.meetingId !== view.meetingId)
      return [view, ...rest]
    })
  }

  const attachSpeech = async (meeting: MeetingDTO, plan: MeetingCapturePlan) => {
    captureRef.current = plan
    const gen = ++speechGen.current
    window.clearInterval(stallWatchRef.current)
    let lastCaptionAt = Date.now()
    let stallRestarting = false
    const bumpCaption = () => { lastCaptionAt = Date.now() }
    const bindSystemWatch = (live: MeetingCapturePlan) => {
      unwatchRef.current()
      const unsubs = live.extraStreams.map(stream => watchCaptureTracksEnded(stream, () => {
        if (speechGen.current !== gen || currentIdRef.current !== meeting.meetingId || userStopRef.current) return
        void recoverAndRelisten()
      }))
      unwatchRef.current = () => unsubs.forEach(stop => stop())
    }
    const recoverAndRelisten = async () => {
      if (userStopRef.current || speechGen.current !== gen || currentIdRef.current !== meeting.meetingId) return
      if (captureRef.current?.engineOwned) return
      const recovered = await recoverMeetingSystemAudio(captureRef.current, { interactive: false })
      if (speechGen.current !== gen) {
        if (recovered !== captureRef.current) releaseMeetingCapture(recovered)
        return
      }
      captureRef.current = recovered
      recovered.extraStreams.forEach(stream => audioRef.current?.attachExtraStream(stream))
      bindSystemWatch(recovered)
      const notice = captureStateNotice(recovered)
      if (notice) setNotice(notice)
      else setNotice(prev => /系统声音|共享音频|立体声混音/.test(prev) ? '' : prev)
      speechRef.current?.stop()
      speechRef.current = null
      await listen().catch(error => {
        if (!userStopRef.current && speechGen.current === gen && currentIdRef.current === meeting.meetingId) {
          setNotice(speechNotice(error))
        }
      })
    }
    const listen = async () => {
      if (speechGen.current !== gen || currentIdRef.current !== meeting.meetingId) return
      let livePlan = captureRef.current ?? plan
      if (!planHasLiveSystemAudio(livePlan)) {
        const recovered = await recoverMeetingSystemAudio(livePlan, { interactive: false })
        if (speechGen.current !== gen) {
          if (recovered !== livePlan) releaseMeetingCapture(recovered)
          return
        }
        livePlan = recovered
        captureRef.current = livePlan
      }
      bindSystemWatch(livePlan)
      let volcProviderId = ''
      const listenKind = prefsRef.current.listen
      if (listenKind === 'volc') {
        const listed = await getProviderBridge().list().catch(() => ({ items: [] as ProviderDTO[] }))
        volcProviderId = pickDefaultVoice(listed.items)?.provider.id ?? ''
        if (!volcProviderId) throw new Error('会议听写选了火山，但没有可用的语音模型。请在供应商里配置 seed-asr。')
      }
      const handle = await startMeetingSpeech({
        listen: listenKind,
        volcProviderId: volcProviderId || undefined,
        extraStreams: livePlan.engineOwned ? undefined : livePlan.extraStreams,
        externalPcm: !!audioRef.current,
        duplex: true,
        spokenText: () => '',
        onFinal: text => {
          bumpCaption()
          const id = meeting.meetingId
          const startedMs = Math.max(0, Date.now() - Date.parse(meeting.startedAt))
          appendChain.current = appendChain.current.then(() =>
            retryMeetingWrite(() => meetings.append({ meetingId: id, text, startedMs })).then(seg => {
              setCurrent(value => value && value.meetingId === id ? {
                ...value,
                segments: trimLiveSegments([...(value.segments ?? []), seg]),
              } : value)
              setInterim('')
              setNotice(prev => /Bridge 请求超时|转写写入失败|本地语音识别中断|实时转写中断/.test(prev) ? '' : prev)
            }),
          ).catch(error => {
            if (currentIdRef.current === id && speechGen.current === gen) {
              setNotice(error instanceof Error ? error.message : '转写写入失败')
            }
          })
        },
        onInterim: text => {
          bumpCaption()
          setInterim(text)
        },
        onError: () => {
          if (userStopRef.current || speechGen.current !== gen || currentIdRef.current !== meeting.meetingId) return
          bumpCaption()
          setNotice(ASR_INTERRUPTED_NOTICE)
          speechRef.current = null
          pcmTapRef.current = undefined
          window.setTimeout(() => {
            if (userStopRef.current || speechGen.current !== gen || currentIdRef.current !== meeting.meetingId) return
            void listen().catch(error => {
              if (!userStopRef.current && speechGen.current === gen && currentIdRef.current === meeting.meetingId) {
                setNotice(speechNotice(error))
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
      pcmTapRef.current = handle.pushPcm
      bumpCaption()
      const notice = captureStateNotice(livePlan) || livePlan.notice
      if (notice) setNotice(notice)
    }
    stallWatchRef.current = window.setInterval(() => {
      if (userStopRef.current || speechGen.current !== gen || stallRestarting) return
      if (Date.now() - lastCaptionAt < MEETING_CAPTION_STALL_MS) return
      stallRestarting = true
      bumpCaption()
      speechRef.current?.stop()
      speechRef.current = null
      pcmTapRef.current = undefined
      void listen().catch(error => {
        if (!userStopRef.current && speechGen.current === gen) setNotice(speechNotice(error))
      }).finally(() => { stallRestarting = false })
    }, MEETING_CAPTION_STALL_POLL_MS)
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
        if (detail.status !== 'recording') return
        userStopRef.current = false
        const plan = await capturePlanForStarted(detail)
        if (!alive) {
          releaseMeetingCapture(plan)
          return
        }
        try {
          audioRef.current = await startMeetingAudioRecorder({
            extraStreams: plan.extraStreams,
            append: pcm => retryMeetingWrite(() => meetings.audioAppend({ meetingId: live.meetingId, pcm })),
            onFrame: frame => {
              const extra = loopbackHoldRef.current
              loopbackHoldRef.current = undefined
              pcmTapRef.current?.(extra ? pcmFrameFromSamples(mixMeetingPcmS16le(frame.samples, extra)) : frame)
            },
            onError: () => {
              if (!userStopRef.current) setNotice(ASR_INTERRUPTED_NOTICE)
            },
          })
        } catch {
          if (alive) setNotice('无法写入本机录音。实时转写仍会尝试，长会停止后可能无法补转写。')
        }
        try {
          await attachSpeech(detail, plan)
        } catch (error) {
          if (alive) setNotice(speechNotice(error))
        }
      } catch (error) {
        if (alive) setNotice(error instanceof Error ? error.message : '无法继续上一场录制')
      }
    }).catch(error => {
      if (alive) setNotice(error instanceof Error ? error.message : '无法读取会议记录')
    })
    return () => { alive = false }
  }, [refresh, meetings])

  useEffect(() => {
    if (current?.status !== 'recording' || !current.startedAt || stopping) {
      window.clearInterval(tickRef.current)
      return
    }
    const started = Date.parse(current.startedAt)
    const pulse = () => setElapsed(Number.isNaN(started) ? 0 : Math.max(0, Date.now() - started))
    pulse()
    tickRef.current = window.setInterval(pulse, 250)
    return () => window.clearInterval(tickRef.current)
  }, [current?.status, current?.startedAt, stopping])

  useEffect(() => {
    if (current?.status !== 'recording' || !meetings.heartbeat || stopping) {
      window.clearInterval(heartbeatRef.current)
      return
    }
    const id = current.meetingId
    const pulse = () => {
      void meetings.heartbeat({ meetingId: id }).then(next => {
        if (currentIdRef.current !== id) return
        setCurrent(value => value && value.meetingId === id ? { ...value, durationMs: next.durationMs, updatedAt: next.updatedAt } : value)
        setItems(values => values.map(item => item.meetingId === id ? { ...item, durationMs: next.durationMs, updatedAt: next.updatedAt } : item))
      }).catch(() => undefined)
    }
    pulse()
    heartbeatRef.current = window.setInterval(pulse, MEETING_HEARTBEAT_INTERVAL_MS)
    return () => window.clearInterval(heartbeatRef.current)
  }, [current?.status, current?.meetingId, meetings, stopping])

  useEffect(() => {
    const live = current
    if (live?.status !== 'recording' || live.audioSource !== 'microphone_and_system' || !meetings.loopbackPoll || stopping) {
      window.clearInterval(loopbackPollRef.current)
      loopbackHoldRef.current = undefined
      return
    }
    const id = live.meetingId
    const pulse = () => {
      void meetings.loopbackPoll({ meetingId: id }).then(next => {
        if (currentIdRef.current !== id) return
        if (!next.active) {
          loopbackHoldRef.current = undefined
          return
        }
        const samples = decodeMeetingPcmBase64(next.pcm)
        if (!samples) return
        if (pcmTapRef.current && !audioRef.current) {
          pcmTapRef.current(pcmFrameFromSamples(samples))
          return
        }
        const prev = loopbackHoldRef.current
        loopbackHoldRef.current = prev ? mixMeetingPcmS16le(prev, samples) : samples
      }).catch(() => undefined)
    }
    pulse()
    loopbackPollRef.current = window.setInterval(pulse, LOOPBACK_POLL_MS)
    return () => window.clearInterval(loopbackPollRef.current)
  }, [current?.status, current?.meetingId, current?.audioSource, meetings, stopping])

  useEffect(() => {
    const live = current
    if (live?.status !== 'recording' || live.audioSource === 'microphone_and_system' || stopping) {
      window.clearInterval(recoverRef.current)
      return
    }
    const tick = () => {
      if (planHasLiveSystemAudio(captureRef.current)) return
      void recoverMeetingSystemAudio(captureRef.current, { interactive: false }).then(recovered => {
        if (currentIdRef.current !== live.meetingId) {
          if (recovered !== captureRef.current) releaseMeetingCapture(recovered)
          return
        }
        if (!planHasLiveSystemAudio(recovered) || recovered === captureRef.current) return
        captureRef.current = recovered
        recovered.extraStreams.forEach(stream => audioRef.current?.attachExtraStream(stream))
        setNotice('')
        speechRef.current?.stop()
        speechRef.current = null
        void attachSpeech(live, recovered).catch(() => undefined)
      })
    }
    recoverRef.current = window.setInterval(tick, SYSTEM_AUDIO_RECOVER_MS)
    return () => window.clearInterval(recoverRef.current)
  }, [current?.status, current?.meetingId, stopping])

  useEffect(() => {
    if (current?.status !== 'summarizing' || !current.meetingId) {
      window.clearInterval(summarizePollRef.current)
      return
    }
    const id = current.meetingId
    const pulse = () => {
      void meetings.get({ meetingId: id }).then(next => {
        if (currentIdRef.current !== id) return
        if (next.status !== 'summarizing') adopt(honestNotes(next))
        else setItems(values => values.map(item => item.meetingId === id ? { ...item, status: next.status, updatedAt: next.updatedAt } : item))
      }).catch(() => undefined)
    }
    pulse()
    summarizePollRef.current = window.setInterval(pulse, SUMMARIZE_POLL_MS)
    return () => window.clearInterval(summarizePollRef.current)
  }, [current?.status, current?.meetingId, meetings])

  useEffect(() => () => {
    speechGen.current += 1
    speechRef.current?.stop()
    speechRef.current = null
    pcmTapRef.current = undefined
    unwatchRef.current()
    void audioRef.current?.stop()
    audioRef.current = null
    releaseMeetingCapture(captureRef.current)
    window.clearInterval(tickRef.current)
    window.clearInterval(heartbeatRef.current)
    window.clearInterval(recoverRef.current)
    window.clearInterval(loopbackPollRef.current)
    window.clearInterval(summarizePollRef.current)
    window.clearInterval(stallWatchRef.current)
    loopbackHoldRef.current = undefined
  }, [])

  const start = async () => {
    if (busy || stopping) return
    userStopRef.current = false
    setStopping(false)
    setBusy(true)
    setNotice('')
    setInterim('')
    let plan: MeetingCapturePlan | undefined
    try {
      const started = await meetings.start({ audioSource: 'microphone_and_system' })
      adopt(started)
      plan = await capturePlanForStarted(started)
      try {
        audioRef.current = await startMeetingAudioRecorder({
          extraStreams: plan.extraStreams,
          append: pcm => retryMeetingWrite(() => meetings.audioAppend({ meetingId: started.meetingId, pcm })),
          onFrame: frame => {
            const extra = loopbackHoldRef.current
            loopbackHoldRef.current = undefined
            pcmTapRef.current?.(extra ? pcmFrameFromSamples(mixMeetingPcmS16le(frame.samples, extra)) : frame)
          },
          onError: () => {
            if (!userStopRef.current) setNotice(ASR_INTERRUPTED_NOTICE)
          },
        })
      } catch {
        setNotice('无法写入本机录音。实时转写仍会尝试，长会停止后可能无法补转写。')
      }
      try {
        await attachSpeech(started, plan)
      } catch (error) {
        setNotice(speechNotice(error))
      }
    } catch (error) {
      speechGen.current += 1
      speechRef.current?.stop()
      speechRef.current = null
      pcmTapRef.current = undefined
      unwatchRef.current()
      void audioRef.current?.stop()
      audioRef.current = null
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

  const finishNotes = async (meetingId: string) => {
    setNotice('正在转写补全…')
    const caught = await meetings.catchup({ meetingId })
    if (caught.status === 'ready') {
      adopt(caught)
      setNotice('纪要已生成，可以导出。')
      return
    }
    adopt({ ...caught, status: 'summarizing' })
    setNotice('正在生成会议纪要…')
    try {
      const notes = honestNotes(await meetings.summarize({
        meetingId,
        ...(prefsRef.current.modelId ? { modelId: prefsRef.current.modelId } : {}),
      }))
      adopt(notes)
      setNotice(notes.status === 'ready' ? '纪要已生成，可以导出。' : notes.summaryError || '尚未生成摘要，逐字稿已保存。')
    } catch (error) {
      const latest = honestNotes(await meetings.get({ meetingId }))
      adopt(latest)
      setNotice(latest.summaryError || (error instanceof Error ? error.message : '无法生成摘要，可重试'))
    }
  }

  const stop = async () => {
    if (!current || current.status !== 'recording' || busy || stopping || userStopRef.current) return
    userStopRef.current = true
    setStopping(true)
    const startedAt = Date.parse(current.startedAt)
    const frozenMs = Math.max(elapsed, Number.isNaN(startedAt) ? 0 : Date.now() - startedAt)
    setElapsed(frozenMs)
    window.clearInterval(tickRef.current)
    window.clearInterval(heartbeatRef.current)
    window.clearInterval(loopbackPollRef.current)
    window.clearInterval(stallWatchRef.current)
    window.clearInterval(recoverRef.current)
    setBusy(true)
    setNotice('正在结束录制…')
    speechGen.current += 1
    const handle = speechRef.current
    speechRef.current = null
    pcmTapRef.current = undefined
    try {
      await handle?.flush?.()
    } catch {
      /* last utterance still flushed below */
    }
    handle?.stop()
    unwatchRef.current()
    try {
      await audioRef.current?.flush()
      await audioRef.current?.stop()
    } catch {
      /* WAV tail is best-effort; stop still persists what landed */
    }
    audioRef.current = null
    releaseMeetingCapture(captureRef.current)
    captureRef.current = undefined
    setInterim('')
    try {
      await appendChain.current
      const stopped = await meetings.stop({ meetingId: current.meetingId })
      adopt({ ...stopped, durationMs: stopped.durationMs || frozenMs })
      await finishNotes(current.meetingId)
    } catch (error) {
      try {
        const latest = honestNotes(await meetings.get({ meetingId: current.meetingId }))
        adopt(latest)
        setNotice(latest.summaryError || (error instanceof Error ? error.message : '无法结束录制'))
      } catch {
        setNotice(error instanceof Error ? error.message : '无法结束录制')
      }
    } finally {
      setStopping(false)
      setBusy(false)
    }
  }

  const retry = async () => {
    if (!current || busy) return
    setBusy(true)
    try {
      await finishNotes(current.meetingId)
    } catch (error) {
      try {
        const latest = honestNotes(await meetings.get({ meetingId: current.meetingId }))
        adopt(latest)
        setNotice(latest.summaryError || (error instanceof Error ? error.message : '无法生成摘要'))
      } catch {
        setNotice(error instanceof Error ? error.message : '无法生成摘要')
      }
    } finally {
      setBusy(false)
    }
  }

  const open = async (id: string) => {
    if (busy || current?.status === 'recording') return
    try {
      adopt(honestNotes(await meetings.get({ meetingId: id })))
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

  const recording = current?.status === 'recording' && !stopping
  const segments: MeetingSegmentDTO[] = current?.segments ?? []
  const liveLines = segments.map(seg => seg.text)
  if (current?.transcript && liveLines.length === 0) liveLines.push(...current.transcript.split('\n').filter(Boolean))
  const source = current?.audioSource ?? 'microphone_and_system'

  return (
    <div className="meeting-shell" style={{ '--meeting-list-width': `${listWidth}px` } as React.CSSProperties}>
      <aside className="meeting-list" aria-label="历史会议">
        <header>
          <h2>会议记录</h2>
          <p>一点「开始录制」即收录麦克风与系统声音，一直录到你点停止。长会以本机录音为准。补转写只用本机识别，不跟听写里的系统/火山走。不区分说话人；逐字稿是一条时间流，多人时可能张冠李戴。不会共享给其他电脑。</p>
        </header>
        {items.length === 0 ? <p className="meeting-empty">还没有会议。点开始录制这一场。</p> : items.map(item => (
          <div className="meeting-row" key={item.meetingId}>
            <button
              type="button"
              className={current?.meetingId === item.meetingId ? 'on' : ''}
              onClick={() => void open(item.meetingId)}
            >
              <b>{item.title}</b>
              <small>{formatWhen(item.startedAt)} · {formatMeetingDuration(item.status === 'recording' && item.meetingId === current?.meetingId ? elapsed : item.durationMs)} · {item.meetingId === current?.meetingId && stopping ? '整理中' : STATUS[item.status]}</small>
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
            <p>开始录制后写入本机录音，实时转写只作字幕。只有点停止才会结束。{MEETING_CATCHUP_HINT} 再生成摘要、待办和逐字稿。</p>
          </div>
          <div className="meeting-clock" aria-live="polite">{formatMeetingDuration(recording || stopping ? elapsed : current?.durationMs ?? 0)}</div>
        </header>
        <div className="meeting-rec">
          {recording
            ? <button type="button" className="meeting-stop" disabled={busy} onClick={() => void stop()}>停止</button>
            : <button type="button" className="meeting-start" disabled={busy || stopping} onClick={() => void start()}>{busy || stopping ? '处理中…' : '开始录制'}</button>}
          <span>{recording ? audioSourceLabel(current?.audioSource, true) : ((busy || stopping) && current ? '录音已停止，正在整理纪要。' : '开始录制后一直收录，直到你点停止。')}</span>
        </div>
        <div className="meeting-prefs">
          <fieldset disabled={recording || stopping || busy}>
            <legend>听写</legend>
            {(['cloud', 'volc', 'local'] as MeetingListen[]).map(value => (
              <label key={value}>
                <input type="radio" name="meeting-listen" checked={prefs.listen === value} onChange={() => updatePrefs({ listen: value })} />
                {value === 'cloud' ? '系统' : value === 'volc' ? '火山' : '本机'}
              </label>
            ))}
          </fieldset>
          <label className="meeting-prefs-model">纪要模型
            <select aria-label="纪要模型" value={prefs.modelId} onChange={e => updatePrefs({ modelId: e.target.value })} disabled={recording || stopping || busy}>
              <option value="">自动（已启用的对话模型）</option>
              {llmChoices.map(item => (
                <option key={`${item.provider.id}:${item.model.modelId}`} value={item.model.modelId}>
                  {item.model.displayName || item.model.modelId}
                </option>
              ))}
            </select>
          </label>
          <p>听写管实时字幕。纪要只管整理已经转写出的字。停止后的补转写只用本机，换纪要模型不会让乱码变准。</p>
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
              <textarea aria-label="会议摘要" value={draftSummary} onChange={e => setDraftSummary(e.target.value)} placeholder="尚未生成摘要。" />
            </section>
            <section>
              <h3>决议/待办</h3>
              <textarea aria-label="决议/待办" value={draftActions} onChange={e => setDraftActions(e.target.value)} placeholder={current.status === 'ready' ? '这场没有抽出可执行待办。' : '尚未生成待办。摘要成功后会一起写出。'} />
            </section>
            <section>
              <h3>全文逐字稿</h3>
              <textarea aria-label="全文逐字稿" value={draftTranscript} onChange={e => setDraftTranscript(e.target.value)} placeholder="（空）" />
            </section>
            <p className="meeting-empty">{audioSourceLabel(source)}</p>
            <div className="meeting-export">
              {current.status === 'needs_summary' || current.status === 'transcribed' || current.status === 'summarizing' ? (
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
