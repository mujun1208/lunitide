import React, { useCallback, useEffect, useRef, useState } from 'react'
import { SettingsPage } from '../settings/SettingsPage'
import { BridgeClientError, MEETING_HEARTBEAT_INTERVAL_MS, getMeetingsBridge, getProviderBridge, type MeetingsBridge } from '../bridge/client'
import type { MeetingDTO, MeetingSegmentDTO, ProviderDTO } from '../generated/bridge'
import { pickDefaultVoice } from '../provider/modelKind'
import { loadMeetingSettings, MEETING_SETTINGS_EVENT, type MeetingSettings } from './meetingSettings'
import { ConfirmDialog } from '../ui/Dialog'
import { usePanelResize } from '../ui/usePanelResize'
import { audioSourceLabel, captureStateNotice, decodeMeetingPcmBase64, engineLoopbackPlan, MEETING_CATCHUP_HINT, meetingAsrRuntimeLine, meetingSystemAudioMissing, mixMeetingPcmS16le, noteLoopbackEnergy, pcmFrameFromSamples, planHasLiveSystemAudio, prepareMeetingCapture, recoverMeetingSystemAudio, releaseMeetingCapture, shouldFallbackLiveCaption, startMeetingSpeech, type MeetingAsrRuntime, type MeetingCapturePlan } from './meetingAsr'
import { localAsrStatus } from '../session/companion/localAsr'
import { meetingLiveListen, planOwnsEngineLoopback } from './meetingAsr'
import type { MeetingListen } from './meetingSettings'
import { ASR_INTERRUPTED_NOTICE, startMeetingAudioRecorder, verifyMeetingAudioAck, trimLiveSegments, type MeetingAudioHandle } from './meetingAudio'
import { watchCaptureTracksEnded } from './meetingCapture'
import type { CompanionSpeechHandle } from '../session/companion/speech'
import { MeetingLoopbackQueue } from './meetingLoopbackQueue'
import { MeetingSummarySource } from './MeetingSummarySource'
import { MeetingTranscriptEditor, type MeetingTranscriptEditorHandle } from './MeetingTranscriptEditor'
import { MeetingSegments } from './MeetingSegments'

const SUMMARIZE_POLL_MS = 4_000
const SYSTEM_AUDIO_RECOVER_MS = 15_000
const LOOPBACK_POLL_MS = 80
/** Web Speech and sherpa both go quiet after a long un-endpointed clip. Restart ASR, keep the WAV. */
export const MEETING_CAPTION_STALL_MS = 25_000
export const MEETING_CAPTION_STALL_POLL_MS = 2_000
/** How long a cloud/volc live caption may stay silent before we transparently
 *  fall back to this-PC sherpa. seed-asr handshake often exceeds 8s; 20s
 *  absorbs warm-up without leaving a deaf engine on for the whole meeting. */
export const MEETING_LIVE_FALLBACK_MS = 20_000
const MEETING_LIVE_FALLBACK_NOTICE = '所选引擎未返回字幕，已切换到本机识别。'
const MEETING_LIVE_UNAVAILABLE_NOTICE = '实时字幕暂不可用，本机识别也未就绪。录音继续保存。'
const VOLC_CONNECTING_NOTICE = '正在连接火山听写…'
const VOLC_LISTENING_NOTICE = '正在听写'

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

const HISTORY_OPEN_KEY = 'lunitide:meeting-history-open'

export function MeetingPage({ meetings = getMeetingsBridge(), onOpenSettings }: { meetings?: MeetingsBridge; onOpenSettings?: () => void }): React.JSX.Element {
  const [settingsOpen, setSettingsOpen] = useState(false)
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
  const [editConflict, setEditConflict] = useState<MeetingDTO>()
  const [deleteTarget, setDeleteTarget] = useState<MeetingDTO>()
  const [prefs, setPrefs] = useState<MeetingSettings>(() => loadMeetingSettings())
  const [historyOpen, setHistoryOpen] = useState(() => localStorage.getItem(HISTORY_OPEN_KEY) !== '0')
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
  const liveFallbackRef = useRef<number>(0)
  const loopbackPollRef = useRef<number>(0)
  const loopbackHoldRef = useRef(new MeetingLoopbackQueue())
  const loopbackEnergyRef = useRef({ hits: 0, zeros: 0 })
  const [systemHeard, setSystemHeard] = useState<boolean | undefined>()
  const [asrRuntime, setAsrRuntime] = useState<MeetingAsrRuntime>()
  // For engine-owned WASAPI capture there is no browser track to inspect, so the
  // only trustworthy "system audio present" signal is whether the engine actually
  // opened a loopback session (active === true). Energy silence is normal in a
  // meeting and must never be read as "missing".
  const [engineLoopbackActive, setEngineLoopbackActive] = useState<boolean | undefined>()
  const summarizePollRef = useRef<number>(0)
  const unwatchRef = useRef<() => void>(() => undefined)
  const speechGen = useRef(0)
  const userStopRef = useRef(false)
  const appendChain = useRef(Promise.resolve())
  const currentIdRef = useRef('')
  const mountedRef = useRef(true)
  const selectionEpoch = useRef(0)
  const listEpoch = useRef(0)
  const currentRef = useRef(current)
  currentRef.current = current
  const editEpoch = useRef(0)
  const draftDirty = useRef(false)
  const draftRevision = useRef(0)
  const transcriptEditor = useRef<MeetingTranscriptEditorHandle>(null)
  const draftRef = useRef({ summary: draftSummary, actions: draftActions, transcript: draftTranscript })
  draftRef.current = { summary: draftSummary, actions: draftActions, transcript: draftTranscript }
  const edit = (field: 'summary' | 'actions' | 'transcript', value: string) => {
    draftDirty.current = true
    editEpoch.current++
    draftRef.current = { ...draftRef.current, [field]: value }
    if (field === 'summary') setDraftSummary(value)
    else if (field === 'actions') setDraftActions(value)
    else setDraftTranscript(value)
  }

  useEffect(() => {
    const sync = () => setPrefs(loadMeetingSettings())
    window.addEventListener('storage', sync)
    window.addEventListener(MEETING_SETTINGS_EVENT, sync)
    return () => {
      window.removeEventListener('storage', sync)
      window.removeEventListener(MEETING_SETTINGS_EVENT, sync)
    }
  }, [])

  const refresh = useCallback(async () => {
    const epoch = ++listEpoch.current
    const listed = await meetings.list()
    if (mountedRef.current && epoch === listEpoch.current) setItems(listed.items)
    return listed.items
  }, [meetings])

  const adopt = (next: MeetingDTO, replaceDraft = false) => {
    if (!mountedRef.current || (currentIdRef.current && currentIdRef.current !== next.meetingId)) return
    if (currentRef.current?.meetingId === next.meetingId && currentRef.current.revision > next.revision) return
    const changedMeeting = currentRef.current?.meetingId !== next.meetingId
    if (currentIdRef.current !== next.meetingId) {
      loopbackEnergyRef.current = { hits: 0, zeros: 0 }
      setSystemHeard(undefined)
      setEngineLoopbackActive(undefined)
    }
    currentIdRef.current = next.meetingId
    const view = next.status === 'recording' && next.segments
      ? { ...next, segments: trimLiveSegments(next.segments) }
      : next
    setCurrent(view)
    currentRef.current = view
    listEpoch.current++
    if (changedMeeting || replaceDraft || !draftDirty.current) {
      draftDirty.current = false
      draftRevision.current = view.revision
      setEditConflict(undefined)
      setDraftSummary(view.summary || '')
      setDraftActions(view.actions || '')
      setDraftTranscript(view.transcript || '')
    }
    setItems(values => {
      const rest = values.filter(item => item.meetingId !== view.meetingId)
      return [view, ...rest]
    })
  }

  const attachSpeech = async (meeting: MeetingDTO, plan: MeetingCapturePlan) => {
    captureRef.current = plan
    const gen = ++speechGen.current
    window.clearInterval(stallWatchRef.current)
    window.clearTimeout(liveFallbackRef.current)
    let lastCaptionAt = Date.now()
    let stallRestarting = false
    // Live-caption fallback (Issue 3): cloud/volc can start yet emit nothing.
    // effectiveListen overrides the user's choice once we fall back to sherpa;
    // sawRealCaption flips true only on a genuine interim/final (never on the
    // start/onError bumps), so the watchdog can tell deafness from a quiet room.
    let effectiveListen: MeetingListen | undefined
    let sawRealCaption = false
    let liveFallbackDone = false
    // Only one listen() may be between "stopped previous handle" and "installed
    // new handle" at a time. Stall restart, onError retry (900ms) and
    // recoverAndRelisten all call listen() and each awaits startMeetingSpeech;
    // without this guard two could resolve and the second would overwrite
    // speechRef without stopping the first, leaving an orphan mic + ASR session
    // running for the rest of a long meeting.
    let listenInFlight = false
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
      if (listenInFlight) return
      listenInFlight = true
      try {
        await listenOnce()
      } finally {
        listenInFlight = false
      }
    }
    const listenOnce = async () => {
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
      const listenKind = effectiveListen ?? meetingLiveListen(prefsRef.current.listen, livePlan)
      if (listenKind === 'volc') {
        const listed = await getProviderBridge().list().catch(() => ({ items: [] as ProviderDTO[] }))
        volcProviderId = pickDefaultVoice(listed.items)?.provider.id ?? ''
        if (!volcProviderId) throw new Error('会议听写选了火山，但没有可用的语音模型。请在供应商里配置 seed-asr。')
        if (!sawRealCaption) setNotice(VOLC_CONNECTING_NOTICE)
      }
      if (!mountedRef.current || speechGen.current !== gen || currentIdRef.current !== meeting.meetingId) return
      setAsrRuntime({
        backend: listenKind,
        providerId: volcProviderId || undefined,
        externalPcm: (listenKind === 'local' || listenKind === 'volc') && !!audioRef.current,
        fellBack: listenKind === 'local' && prefsRef.current.listen !== 'local',
      })
      const handle = await startMeetingSpeech({
        listen: listenKind,
        volcProviderId: volcProviderId || undefined,
        extraStreams: livePlan.engineOwned ? undefined : livePlan.extraStreams,
        externalPcm: !!audioRef.current,
        duplex: true,
        spokenText: () => '',
        onFinal: text => {
          if (!mountedRef.current || speechGen.current !== gen || currentIdRef.current !== meeting.meetingId) return
          bumpCaption()
          sawRealCaption = true
          setNotice(prev => prev === VOLC_CONNECTING_NOTICE ? VOLC_LISTENING_NOTICE : prev)
          const id = meeting.meetingId
          const startedMs = Math.max(0, Date.now() - Date.parse(meeting.startedAt))
          appendChain.current = appendChain.current.then(() =>
            retryMeetingWrite(() => meetings.append({ meetingId: id, text, startedMs })).then(seg => {
              if (!mountedRef.current || speechGen.current !== gen || currentIdRef.current !== id) return
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
          if (!mountedRef.current || speechGen.current !== gen || currentIdRef.current !== meeting.meetingId) return
          bumpCaption()
          if (text.trim()) {
            sawRealCaption = true
            setNotice(prev => prev === VOLC_CONNECTING_NOTICE ? VOLC_LISTENING_NOTICE : prev)
          }
          setInterim(text)
        },
        onEngineHint: message => {
          if (userStopRef.current || speechGen.current !== gen || currentIdRef.current !== meeting.meetingId) return
          if (message && !sawRealCaption) setNotice(message)
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
      setNotice(prev => {
        if (notice) return notice
        return prev === VOLC_CONNECTING_NOTICE ? VOLC_LISTENING_NOTICE : prev
      })
    }
    const scheduleLiveFallback = () => {
      window.clearTimeout(liveFallbackRef.current)
      liveFallbackRef.current = window.setTimeout(async () => {
        if (userStopRef.current || speechGen.current !== gen || currentIdRef.current !== meeting.meetingId) return
        const kind = effectiveListen ?? meetingLiveListen(prefsRef.current.listen, captureRef.current ?? plan)
        const probe = await localAsrStatus().catch(() => undefined)
        if (userStopRef.current || speechGen.current !== gen || currentIdRef.current !== meeting.meetingId) return
        const decision = shouldFallbackLiveCaption({
          listen: kind,
          sawRealCaption,
          alreadyFellBack: liveFallbackDone,
          localReady: probe?.supported === true && probe.ready === true,
        })
        if (decision === 'none') return
        liveFallbackDone = true
        if (decision === 'unavailable') {
          setNotice(MEETING_LIVE_UNAVAILABLE_NOTICE)
          return
        }
        // decision === 'local': move the live path to this-PC sherpa, which
        // reads the same mixed mic+系统声 PCM the recorder already taps.
        effectiveListen = 'local'
        setNotice(MEETING_LIVE_FALLBACK_NOTICE)
        speechRef.current?.stop()
        speechRef.current = null
        pcmTapRef.current = undefined
        await listen().catch(error => {
          if (!userStopRef.current && speechGen.current === gen && currentIdRef.current === meeting.meetingId) {
            setNotice(speechNotice(error))
          }
        })
      }, MEETING_LIVE_FALLBACK_MS)
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
    scheduleLiveFallback()
  }

  useEffect(() => {
    let alive = true
    mountedRef.current = true
    const epoch = selectionEpoch.current
    refresh().then(async listed => {
      if (!alive || epoch !== selectionEpoch.current) return
      const live = listed.find(item => item.status === 'recording')
      if (!live) return
      try {
        adopt(live)
        const detail = await meetings.get({ meetingId: live.meetingId }).catch(() => live)
        if (!alive || epoch !== selectionEpoch.current) return
        adopt(detail)
        if (detail.status !== 'recording') return
        userStopRef.current = false
        const plan = await capturePlanForStarted(detail)
        if (!alive || epoch !== selectionEpoch.current) {
          releaseMeetingCapture(plan)
          return
        }
        try {
          const recorder = await startMeetingAudioRecorder({
            meetingId: live.meetingId,
            extraStreams: plan.extraStreams,
            append: async (pcm, batch) => {
              const ack = await retryMeetingWrite(() => meetings.audioAppend({ meetingId: live.meetingId, pcm, ...batch }))
              verifyMeetingAudioAck(ack, batch)
              return ack
            },
            onFrame: frame => {
              const extra = loopbackHoldRef.current.take(frame.samples.length)
              pcmTapRef.current?.(extra.length ? pcmFrameFromSamples(mixMeetingPcmS16le(frame.samples, extra)) : frame)
            },
            onError: () => {
              if (!userStopRef.current) setNotice(ASR_INTERRUPTED_NOTICE)
            },
          })
          if (!alive || epoch !== selectionEpoch.current) {
            void recorder.stop().catch(() => undefined)
            releaseMeetingCapture(plan)
            return
          }
          audioRef.current = recorder
        } catch {
          if (alive) setNotice('无法写入本机录音。实时转写仍会尝试，长会停止后可能无法补转写。')
        }
        if (!alive || epoch !== selectionEpoch.current) { releaseMeetingCapture(plan); return }
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
        if (!mountedRef.current || currentIdRef.current !== id || currentRef.current?.status !== 'recording') return
        setCurrent(value => value && value.meetingId === id && value.status === 'recording' ? { ...value, durationMs: Math.max(value.durationMs, next.durationMs) } : value)
        setItems(values => values.map(item => item.meetingId === id && item.status === 'recording' ? { ...item, durationMs: Math.max(item.durationMs, next.durationMs) } : item))
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
      loopbackHoldRef.current.clear()
      loopbackEnergyRef.current = { hits: 0, zeros: 0 }
      return
    }
    const id = live.meetingId
    let alive = true
    let polling = false
    const pulse = () => {
      if (polling) return
      polling = true
      void meetings.loopbackPoll({ meetingId: id }).then(next => {
        if (!alive || !mountedRef.current || userStopRef.current || currentIdRef.current !== id) return
        setEngineLoopbackActive(next.active)
        if (!next.active) {
          loopbackHoldRef.current.clear()
          return
        }
        const samples = decodeMeetingPcmBase64(next.pcm)
        if (!samples) return
        const frame = pcmFrameFromSamples(samples)
        const energy = noteLoopbackEnergy(loopbackEnergyRef.current, frame.peak)
        loopbackEnergyRef.current = { hits: energy.hits, zeros: energy.zeros }
        if (energy.heard !== undefined) setSystemHeard(energy.heard)
        if (pcmTapRef.current && !audioRef.current) {
          pcmTapRef.current(frame)
          return
        }
        loopbackHoldRef.current.append(samples)
      }).catch(() => undefined).finally(() => { polling = false })
    }
    pulse()
    loopbackPollRef.current = window.setInterval(pulse, LOOPBACK_POLL_MS)
    return () => { alive = false; window.clearInterval(loopbackPollRef.current) }
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
        if (!mountedRef.current || userStopRef.current || currentIdRef.current !== live.meetingId) {
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
        if (!mountedRef.current || currentIdRef.current !== id) return
        if (next.status !== 'summarizing') adopt(honestNotes(next))
        else setItems(values => values.map(item => item.meetingId === id ? { ...item, status: next.status, updatedAt: next.updatedAt } : item))
      }).catch(() => undefined)
    }
    pulse()
    summarizePollRef.current = window.setInterval(pulse, SUMMARIZE_POLL_MS)
    return () => window.clearInterval(summarizePollRef.current)
  }, [current?.status, current?.meetingId, meetings])

  useEffect(() => () => {
    mountedRef.current = false
    selectionEpoch.current++
    speechGen.current += 1
    speechRef.current?.stop()
    speechRef.current = null
    pcmTapRef.current = undefined
    unwatchRef.current()
    void audioRef.current?.stop().catch(() => undefined)
    audioRef.current = null
    releaseMeetingCapture(captureRef.current)
    window.clearInterval(tickRef.current)
    window.clearInterval(heartbeatRef.current)
    window.clearInterval(recoverRef.current)
    window.clearInterval(loopbackPollRef.current)
    window.clearInterval(summarizePollRef.current)
    window.clearInterval(stallWatchRef.current)
    window.clearTimeout(liveFallbackRef.current)
    loopbackHoldRef.current.clear()
  }, [])

  const start = async () => {
    if (busy || stopping) return
    const epoch = ++selectionEpoch.current
    currentIdRef.current = ''
    userStopRef.current = false
    setStopping(false)
    setBusy(true)
    setNotice('')
    setInterim('')
    let plan: MeetingCapturePlan | undefined
    try {
      const started = await meetings.start({ audioSource: 'microphone_and_system' })
      if (!mountedRef.current || epoch !== selectionEpoch.current) return
      adopt(started)
      plan = await capturePlanForStarted(started)
      if (!mountedRef.current || epoch !== selectionEpoch.current) { releaseMeetingCapture(plan); return }
      try {
        const recorder = await startMeetingAudioRecorder({
          meetingId: started.meetingId,
          extraStreams: plan.extraStreams,
          append: async (pcm, batch) => {
              const ack = await retryMeetingWrite(() => meetings.audioAppend({ meetingId: started.meetingId, pcm, ...batch }))
              verifyMeetingAudioAck(ack, batch)
              return ack
            },
          onFrame: frame => {
            const extra = loopbackHoldRef.current.take(frame.samples.length)
            pcmTapRef.current?.(extra.length ? pcmFrameFromSamples(mixMeetingPcmS16le(frame.samples, extra)) : frame)
          },
          onError: () => {
            if (!userStopRef.current) setNotice(ASR_INTERRUPTED_NOTICE)
          },
        })
        if (!mountedRef.current || epoch !== selectionEpoch.current) {
          void recorder.stop().catch(() => undefined)
          releaseMeetingCapture(plan)
          return
        }
        audioRef.current = recorder
      } catch {
        if (!mountedRef.current || epoch !== selectionEpoch.current) { releaseMeetingCapture(plan); return }
        setNotice('无法写入本机录音。实时转写仍会尝试，长会停止后可能无法补转写。')
      }
      if (!mountedRef.current || epoch !== selectionEpoch.current) { releaseMeetingCapture(plan); return }
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
      void audioRef.current?.stop().catch(() => undefined)
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

  const finishNotes = async (meeting: MeetingDTO, catchup = true) => {
    const meetingId = meeting.meetingId
    const epoch = selectionEpoch.current
    const active = () => mountedRef.current && epoch === selectionEpoch.current && currentIdRef.current === meetingId
    setNotice('正在转写补全…')
    const caught = catchup ? await meetings.catchup({ meetingId, expectedRevision: meeting.revision }) : meeting
    if (!active()) return
    if (caught.status === 'needs_summary' && (caught.summaryError?.startsWith('转写补全存在缺口') || caught.summaryError?.startsWith('本机补转写不可用'))) {
      adopt(caught)
      setNotice(caught.summaryError)
      return
    }
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
        expectedRevision: caught.revision,
        ...(prefsRef.current.modelId ? { modelId: prefsRef.current.modelId } : {}),
      }))
      if (!active()) return
      adopt(notes)
      setNotice(notes.status === 'ready' ? '纪要已生成，可以导出。' : notes.summaryError || '尚未生成摘要，逐字稿已保存。')
    } catch (error) {
      const latest = honestNotes(await meetings.get({ meetingId }))
      if (!active()) return
      adopt(latest)
      setNotice(latest.summaryError || (error instanceof Error ? error.message : '无法生成摘要，可重试'))
    }
  }

  const stop = async () => {
    if (!current || current.status !== 'recording' || busy || stopping || userStopRef.current) return
    const epoch = selectionEpoch.current
    const active = () => mountedRef.current && epoch === selectionEpoch.current && currentIdRef.current === current.meetingId
    userStopRef.current = true
    setStopping(true)
    const startedAt = Date.parse(current.startedAt)
    const frozenMs = Math.max(elapsed, Number.isNaN(startedAt) ? 0 : Date.now() - startedAt)
    setElapsed(frozenMs)
    window.clearInterval(tickRef.current)
    window.clearInterval(heartbeatRef.current)
    window.clearInterval(loopbackPollRef.current)
    window.clearInterval(stallWatchRef.current)
    window.clearTimeout(liveFallbackRef.current)
    window.clearInterval(recoverRef.current)
    setBusy(true)
    setNotice('正在结束录制…')
    const handle = speechRef.current
    speechRef.current = null
    pcmTapRef.current = undefined
    const savingAudio = audioRef.current?.stop().then(() => undefined, error => error instanceof Error ? error : new Error(String(error)))
    releaseMeetingCapture(captureRef.current)
    captureRef.current = undefined
    try {
      await handle?.flush?.()
    } catch {
      /* last utterance still flushed below */
    }
    speechGen.current += 1
    handle?.stop()
    unwatchRef.current()
    const saveError = await savingAudio
    if (saveError) {
      if (!active()) return
      setNotice(saveError.message)
      setStopping(false)
      setBusy(false)
      userStopRef.current = false
      return
    }
    audioRef.current = null
    setInterim('')
    setAsrRuntime(undefined)
    try {
      await appendChain.current
      const stopped = await meetings.stop({ meetingId: current.meetingId, expectedRevision: current.revision })
      if (!active()) return
      adopt({ ...stopped, durationMs: stopped.durationMs || frozenMs })
      await finishNotes(stopped)
    } catch (error) {
      if (!active()) return
      try {
        const latest = honestNotes(await meetings.get({ meetingId: current.meetingId }))
        if (!active()) return
        adopt(latest)
        setNotice(latest.summaryError || (error instanceof Error ? error.message : '无法结束录制'))
      } catch {
        if (!active()) return
        setNotice(error instanceof Error ? error.message : '无法结束录制')
      }
    } finally {
      if (active()) {
        setStopping(false)
        setBusy(false)
      }
    }
  }

  const retry = async () => {
    if (!current || busy) return
    setBusy(true)
    try {
      const saved = await persistEdits() ?? current
      // An explicit transcript edit is authoritative. Re-decoding the old
      // recording would conflict with its journal and prevent regeneration.
      const editedSource = saved.summaryError?.startsWith('逐字稿已修改') || saved.summaryError?.startsWith('补转写已更新逐字稿')
      await finishNotes(saved, !editedSource)
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

  const composeNew = () => {
    if (busy) return
    if (current?.status === 'recording' || stopping) {
      setNotice('先停止当前录制，才能开新纪要。')
      return
    }
    currentIdRef.current = ''
    selectionEpoch.current++
    draftDirty.current = false
    setEditConflict(undefined)
    setCurrent(undefined)
    setDraftSummary('')
    setDraftActions('')
    setDraftTranscript('')
    setInterim('')
    setElapsed(0)
    setNotice('')
  }

  const toggleHistory = () => {
    setHistoryOpen(open => {
      const next = !open
      localStorage.setItem(HISTORY_OPEN_KEY, next ? '1' : '0')
      return next
    })
  }

  const open = async (id: string) => {
    if (busy || current?.status === 'recording' || stopping) return
    const epoch = ++selectionEpoch.current
    currentIdRef.current = id
    try {
      const next = honestNotes(await meetings.get({ meetingId: id }))
      if (!mountedRef.current || epoch !== selectionEpoch.current) return
      adopt(next)
      setNotice('')
    } catch (error) {
      if (!mountedRef.current || epoch !== selectionEpoch.current) return
      setNotice(error instanceof Error ? error.message : '无法打开会议')
    }
  }

  const persistEdits = async (revision = draftRevision.current) => {
    if (!current || current.status === 'recording') return current
    const epoch = selectionEpoch.current
    const meetingId = current.meetingId
    const pageSaved = await transcriptEditor.current?.save()
    if (!mountedRef.current || epoch !== selectionEpoch.current || currentIdRef.current !== meetingId) throw new Error('会议已切换，本次保存已停止。')
    if (pageSaved) revision = pageSaved.revision
    const dirty = draftDirty.current
    if (!dirty) return pageSaved ?? current
    const edited = editEpoch.current
    try {
      // A paged preview is never a replacement for the complete transcript.
      const { summary, actions, transcript } = draftRef.current
      const next = await meetings.update({ meetingId, expectedRevision: revision, summary, actions,
        ...(current.transcriptComplete === false ? {} : { transcript }) })
      if (!mountedRef.current || epoch !== selectionEpoch.current || currentIdRef.current !== meetingId) return next
      draftRevision.current = next.revision
      setEditConflict(undefined)
      adopt(next, edited === editEpoch.current)
      return next
    } catch (error) {
      if (error instanceof BridgeClientError && error.code === 'MEETING_CHANGED') {
        const latest = await meetings.get({ meetingId })
        if (mountedRef.current && epoch === selectionEpoch.current && currentIdRef.current === meetingId) setEditConflict(latest)
      }
      throw error
    }
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

  const saveEdits = async (revision?: number) => {
    const epoch = selectionEpoch.current
    setBusy(true)
    try { await persistEdits(revision); if (mountedRef.current && epoch === selectionEpoch.current) setNotice('纪要已保存') }
    catch (error) { if (mountedRef.current && epoch === selectionEpoch.current) setNotice(error instanceof Error ? error.message : '无法保存') }
    finally { if (mountedRef.current && epoch === selectionEpoch.current) setBusy(false) }
  }

  const removeMeeting = async () => {
    if (!deleteTarget || busy) return
    setBusy(true)
    try {
      await meetings.delete({ meetingId: deleteTarget.meetingId, expectedRevision: deleteTarget.revision })
      setItems(values => values.filter(item => item.meetingId !== deleteTarget.meetingId))
      if (current?.meetingId === deleteTarget.meetingId) {
        currentIdRef.current = ''
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
  // Loud, honest state — but only fire on a STRUCTURAL absence of any system
  // source, never on momentary silence. There are two capture shapes:
  //   • engine-owned WASAPI loopback (Windows): no browser track exists, so the
  //     truth is the engine's loopback session — active === false means the
  //     device never opened (mic only). A live session that is merely quiet
  //     (nobody talking / paused media) is NOT missing.
  //   • browser-owned (getDisplayMedia fallback): warn when there is no live
  //     system-audio track at all.
  // The earlier version also warned on `systemHeard === false`, which produced
  // a false alarm whenever the loopback was live but momentarily silent — the
  // exact case the user hit while a song was clearly being transcribed.
  const systemAudioMissing = meetingSystemAudioMissing({
    recording,
    audioSource: current?.audioSource,
    plan: captureRef.current,
    engineLoopbackActive,
    systemHeard,
  })
  const segments: MeetingSegmentDTO[] = current?.segments ?? []
  const liveLines = segments.map(seg => seg.text.trim()).filter(Boolean)
  if (current?.transcript && liveLines.length === 0) liveLines.push(...current.transcript.split('\n').filter(Boolean))
  const source = current?.audioSource ?? 'microphone_and_system'

  return (
    <div className="meeting-shell" style={{ '--meeting-list-width': `${listWidth}px` } as React.CSSProperties}>
      <aside className="meeting-list" aria-label="会议纪要">
        <button type="button" className={`meeting-new ${current ? '' : 'on'}`} aria-pressed={!current} disabled={recording || stopping} onClick={composeNew}>＋ 新纪要</button>
        <section className={`meeting-history ${historyOpen ? 'is-open' : 'is-closed'}`}>
          <button type="button" className="meeting-history-heading" aria-expanded={historyOpen} aria-controls="meeting-history-list" onClick={toggleHistory}>
            <span aria-hidden="true">›</span>历史纪要
          </button>
          {historyOpen && (
            <div id="meeting-history-list" className="meeting-history-list">
              {items.length === 0 ? <p className="meeting-empty">还没有历史纪要。点新纪要开始录制。</p> : items.map(item => (
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
            </div>
          )}
        </section>
      </aside>
      <div className="panel-resizer split-resizer" role="separator" aria-label="调整会议列表宽度" aria-orientation="vertical" onPointerDown={startListResize} />
      <section className="meeting-main" aria-label="会议工作台">
        <header className="meeting-hero">
          <div className="meeting-heading">
            <div className="meeting-eyebrow">会议纪要 <span> / </span> {recording ? '录制中' : stopping ? '正在整理' : current ? STATUS[current.status] : '准备录制'}</div>
            <h2>{current?.title || '记录声音，留下重点'}</h2>
            <p>{current
              ? `${formatWhen(current.startedAt)} · 本机保存`
              : '收录会议、课程和电脑播放的声音，整理摘要、待办与逐字稿。'}</p>
          </div>
          <div className={`meeting-time ${recording ? 'is-recording' : ''}`}>
            <span className="meeting-time-label">{recording ? '正在录制' : '录音时长'}</span>
            <div className="meeting-clock" role="timer">{formatMeetingDuration(recording || stopping ? elapsed : current?.durationMs ?? 0)}</div>
          </div>
        </header>
        <div className="meeting-rec">
          {recording
            ? <button type="button" className="meeting-stop" disabled={busy} onClick={() => void stop()}>停止</button>
            : !current || stopping
              ? <button type="button" className="meeting-start" disabled={busy || stopping} onClick={() => void start()}>{busy || stopping ? '处理中…' : '开始录制'}</button>
              : <button type="button" className="meeting-new-inline" onClick={composeNew}>新纪要</button>}
          <span>{recording ? audioSourceLabel(systemAudioMissing ? 'microphone' : current?.audioSource, true) : ((busy || stopping) && current ? '录音已停止，正在整理纪要。' : current ? '可编辑、保存或导出这场纪要。' : '麦克风 + 系统声音 · 点击停止后结束录制')}</span>
          <button type="button" className="meeting-settings-link" onClick={() => setSettingsOpen(true)}>听写与纪要设置</button>
        </div>
        {systemAudioMissing && (
          <p className="meeting-warn" role="alert">
            ⚠ 系统声音暂未接入，当前只在录麦克风。腾讯会议、飞书、音乐视频等播放声音暂时未收录，请检查 Windows 输出设备与系统音频回环。
          </p>
        )}
        {notice && <p className="meeting-notice" role="status">{notice}</p>}
        {recording && asrRuntime && <p className="meeting-diag" role="note" aria-label="听写诊断">{meetingAsrRuntimeLine(asrRuntime)}</p>}
        {recording && planOwnsEngineLoopback(captureRef.current) && <p className="meeting-diag" role="note" aria-label="系统音频状态">{engineLoopbackActive === false ? '系统音频未接入' : engineLoopbackActive === undefined ? '正在连接系统音频' : systemHeard ? '系统音频已收到信号' : '系统音频已连接，等待信号'}</p>}
        <details className="meeting-capture-info">
          <summary>录音与转写说明</summary>
          <p>开始后持续收录麦克风与电脑播放的声音，只有点击停止才结束。实时字幕可能暂时缺漏，本机录音用于停止后的补转写。</p>
          <p>{MEETING_CATCHUP_HINT}</p>
        </details>
        <details className={`meeting-live-section ${recording || !current ? 'is-live' : ''}`} open={recording || !current || undefined} key={recording || !current ? 'live' : current.meetingId}>
          <summary>{recording || !current ? '实时转写' : '查看录制字幕与分段记录'}</summary>
          <div className="meeting-transcript" aria-live="polite" aria-label="实时逐字稿">
            {liveLines.length === 0 && !interim ? <div className="meeting-transcript-empty"><span className="meeting-sound-mark" aria-hidden="true">▂ ▅ ▃ ▇ ▃ ▅ ▂</span><p>{recording ? '正在聆听，识别到的内容会显示在这里。' : '声音从这里成为文字'}</p><small>{recording ? '录音会持续保存，无需保持讲话。' : '点击开始录制，收录麦克风与电脑里的声音。'}</small></div> : null}
            {liveLines.map((line, index) => <p key={`${index}:${line.slice(0, 24)}`}>{line}</p>)}
            {interim ? <p className="meeting-interim">{interim}</p> : null}
          </div>
          {current && <MeetingSegments meeting={current} load={meetings.segmentsList} />}
        </details>
        {current && current.status !== 'recording' && (
          <article className="meeting-doc">
            <section className="meeting-card meeting-summary-card">
              <h3>会议摘要</h3>
              <MeetingSummarySource meeting={current} load={meetings.summarySource} />
              <textarea aria-label="会议摘要" rows={8} value={draftSummary} onChange={e => edit('summary', e.target.value)} placeholder="尚未生成摘要。" />
            </section>
            <section className="meeting-card meeting-actions-card">
              <h3>决议/待办</h3>
              <textarea aria-label="决议/待办" rows={8} value={draftActions} onChange={e => edit('actions', e.target.value)} placeholder={current.status === 'ready' ? '这场没有抽出可执行待办。' : '尚未生成待办。摘要成功后会一起写出。'} />
            </section>
            <section className="meeting-card meeting-full-transcript">
              <h3>全文逐字稿</h3>
              {current.transcriptComplete === false
                ? <MeetingTranscriptEditor key={current.meetingId} ref={transcriptEditor} meeting={current} meetings={meetings} disabled={busy}
                    onSaved={next => {
                      if (currentIdRef.current !== next.meetingId || (currentRef.current?.revision ?? 0) > next.revision) return
                      draftRevision.current = next.revision
                      adopt(next)
                      // A page edit can shorten the document into a single
                      // complete page while independent summary edits remain.
                      // Their dirty flag must not preserve the old preview.
                      draftRef.current = { ...draftRef.current, transcript: next.transcript || '' }
                      setDraftTranscript(next.transcript || '')
                    }} />
                : <textarea aria-label="全文逐字稿" rows={14} value={draftTranscript} onChange={e => edit('transcript', e.target.value)} placeholder="（空）" />}
            </section>
            <p className="meeting-empty meeting-doc-source">{audioSourceLabel(source)}</p>
            <div className="meeting-export">
              {current.status === 'needs_summary' || current.status === 'transcribed' || current.status === 'summarizing' ? (
                <button type="button" disabled={busy} onClick={() => void retry()}>重试生成摘要</button>
              ) : null}
              {editConflict && <div role="alert">
                <p>会议已有新版本，你的输入仍保留。请核对最新内容后选择。</p>
                <details><summary>查看最新内容</summary><p>摘要：{editConflict.summary}</p><p>待办：{editConflict.actions}</p><textarea aria-label="最新逐字稿" readOnly value={editConflict.transcript} /></details>
                <button type="button" onClick={() => adopt(editConflict, true)}>采用最新内容</button>
                <button type="button" disabled={busy} onClick={() => void saveEdits(editConflict.revision)}>保留我的编辑并再次保存</button>
              </div>}
              <button type="button" disabled={busy} onClick={() => void saveEdits()}>保存编辑</button>
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
      {settingsOpen && (
        <div className="model-manager-overlay" role="dialog" aria-modal="true" aria-label="听写与纪要设置">
          <SettingsPage
            initialCategory="meetings"
            backLabel="返回会议"
            embedded
            recordingLock={recording}
            onBack={() => setSettingsOpen(false)}
          />
        </div>
      )}
    </div>
  )
}
