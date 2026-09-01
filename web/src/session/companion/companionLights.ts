import { getProviderBridge, getTtsBridge } from '../../bridge/client'
import type { ProviderDTO } from '../../generated/bridge'
import { hasConfiguredVolcTts, pickDefaultLLM, pickDefaultTTS, pickDefaultVoice } from '../../provider/modelKind'
import { VOLC_OFFICIAL_SPEAKERS } from './volcVoices'
import { localAsrStatus } from './localAsr'
import { shownVoicePath, type VoicePath } from './voicePersonas'

export type LightKind = 'on' | 'off' | 'warn'

export type EntryLight = {
  key: 'listen' | 'speak' | 'think'
  title: '听' | '说' | '想'
  label: string
  state: LightKind
}

export type CompanionEntryReport = {
  lights: [EntryLight, EntryLight, EntryLight]
  llmReady: boolean
  listenReady: boolean
  speakReady: boolean
  hasVolc: boolean
  hasVolcTts: boolean
  /** Official speaker id when a TTS row exists. Never the seed-tts resource id. */
  speakVoiceId?: string
  allowListen: boolean
  blockReason: string
}

export type CompanionLightProbes = {
  listProviders?: () => Promise<{ items: ProviderDTO[] }>
  localAsr?: () => Promise<{ supported?: boolean; ready?: boolean } | undefined>
  refEngine?: (endpoint?: string) => Promise<{ state: string; last_error?: string }>
}

export const COMPANION_ENTRY_PROBE_MS = 800

function withBudget<T>(work: Promise<T>, fallback: T, ms = COMPANION_ENTRY_PROBE_MS): Promise<{ value: T; timedOut: boolean }> {
  return new Promise(resolve => {
    let settled = false
    const done = (value: T, timedOut: boolean) => {
      if (settled) return
      settled = true
      resolve({ value, timedOut })
    }
    const timer = window.setTimeout(() => done(fallback, true), ms)
    work.then(
      value => {
        window.clearTimeout(timer)
        done(value, false)
      },
      () => {
        window.clearTimeout(timer)
        done(fallback, false)
      },
    )
  })
}

export function pendingCompanionLights(): CompanionEntryReport['lights'] {
  return [
    { key: 'listen', title: '听', label: '检测中', state: 'warn' },
    { key: 'speak', title: '说', label: '检测中', state: 'warn' },
    { key: 'think', title: '想', label: '检测中', state: 'warn' },
  ]
}

export async function inspectCompanionEntry(
  voicePath: VoicePath,
  refEndpoint = '',
  probes: CompanionLightProbes = {},
): Promise<CompanionEntryReport> {
  const path = shownVoicePath(voicePath)
  const listed = await withBudget(
    (probes.listProviders ?? (() => getProviderBridge().list()))(),
    { items: [] as ProviderDTO[] },
  )
  const items = listed.value.items ?? []
  const llm = items.length ? pickDefaultLLM(items) : undefined
  const llmReady = items.length === 0 ? true : !!llm
  const volc = pickDefaultVoice(items)
  const hasVolc = !!volc
  const hasVolcTts = hasConfiguredVolcTts(items)
  const ttsPick = hasVolcTts ? pickDefaultTTS(items) : undefined
  const speakVoiceId = ttsPick?.modelId
  const speakerName = speakVoiceId ? VOLC_OFFICIAL_SPEAKERS.find(s => s.id === speakVoiceId)?.name : undefined

  let listenReady = true
  let listenLabel = '系统识别'
  if (path === 'volc') {
    listenLabel = '火山 seed-asr'
    listenReady = hasVolc
  } else if (path === 'local') {
    listenLabel = '本机 sherpa'
    const asr = await withBudget(
      (probes.localAsr ?? localAsrStatus)(),
      undefined,
    )
    listenReady = !asr.timedOut && asr.value?.supported === true && asr.value.ready === true
  }

  let speakReady = true
  let speakLabel = '晓晓'
  let speakState: LightKind = 'on'
  if (path === 'volc') {
    speakLabel = hasVolcTts ? (speakerName ? `火山 · ${speakerName}` : '火山 seed-tts') : '晓晓（未配朗读）'
    speakReady = true
    speakState = 'on'
  } else if (path === 'local') {
    speakLabel = 'GPT-SoVITS'
    const ref = await withBudget(
      (probes.refEngine ?? (endpoint => getTtsBridge().ensureRefEngine({ refEndpoint: endpoint || undefined })))(refEndpoint),
      { state: 'offline' },
    )
    if (ref.timedOut) {
      speakReady = false
      speakState = 'warn'
      speakLabel = 'GPT-SoVITS 检测中'
    } else if (ref.value.state === 'online') {
      speakReady = true
      speakState = 'on'
    } else if (ref.value.state === 'launching') {
      speakReady = false
      speakState = 'warn'
      speakLabel = 'GPT-SoVITS 启动中'
    } else {
      speakReady = false
      speakState = 'off'
      const err = ref.value.last_error?.trim()
      speakLabel = err ? `本地朗读未就绪（${err.slice(0, 40)}）` : 'GPT-SoVITS 未就绪'
    }
  }

  const thinkLabel = llm?.modelId || (llmReady ? '对话模型' : '未配置')
  // Listen is independent of SoVITS: a down TTS engine must not leave the
  // stage deaf. Speak-light still reflects the engine; captions stay live.
  const allowListen = llmReady && listenReady
  let blockReason = ''
  if (!llmReady) blockReason = '请先在「模型与供应商」中启用一个对话模型，再开始听。'
  else if (path === 'local' && !listenReady) blockReason = '本机听写未就绪（sherpa）。请先安装本机识别，或改选系统 / 火山。'
  else if (path === 'volc' && !listenReady) {
    blockReason = listed.timedOut
      ? '火山听写还没确认可用。已选火山卡，不会改用系统识别。VOICE-004'
      : '火山听写没有可用的语音模型。请在供应商里配置 seed-asr。'
  }

  return {
    lights: [
      { key: 'listen', title: '听', label: listenLabel, state: listenReady ? 'on' : 'off' },
      { key: 'speak', title: '说', label: speakLabel, state: path === 'local' ? speakState : speakReady ? 'on' : 'off' },
      { key: 'think', title: '想', label: thinkLabel, state: llmReady ? 'on' : 'off' },
    ],
    llmReady,
    listenReady,
    speakReady,
    hasVolc,
    hasVolcTts,
    speakVoiceId,
    allowListen,
    blockReason,
  }
}
