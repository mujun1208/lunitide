import { describe, expect, test, vi } from 'vitest'
import { companionTalkLiveLights, COMPANION_ENTRY_PROBE_MS, inspectCompanionEntry } from './companionLights'
import type { ProviderDTO } from '../../generated/bridge'

const chat: ProviderDTO = {
  id: '01ARZ3NDEKTSV4RRFFQ69G5FAW',
  name: 'Chat',
  protocol: 'openai_compatible',
  baseUrl: 'https://example.com',
  status: 'enabled',
  credentialState: 'configured',
  createdAt: '',
  updatedAt: '',
  version: 1,
  models: [{ modelId: 'qwen-plus', displayName: 'Qwen', isDefault: true, kind: 'llm', kindDefault: true }],
}

const volc: ProviderDTO = {
  ...chat,
  id: '01ARZ3NDEKTSV4RRFFQ69G5FAV',
  name: 'Volc',
  protocol: 'volc_speech',
  models: [{ modelId: 'seed-asr', displayName: 'seed-asr', isDefault: true, kind: 'voice', kindDefault: true }],
}

describe('inspectCompanionEntry', () => {
  test('cloud path lights listen/speak/think when an LLM exists', async () => {
    const report = await inspectCompanionEntry('cloud', '', {
      listProviders: async () => ({ items: [chat] }),
    })
    expect(report.allowListen).toBe(true)
    expect(report.lights.map(light => light.title)).toEqual(['听', '说', '想'])
    expect(report.lights[0]).toMatchObject({ label: '系统识别 · 说完再答 · 打断用按钮', state: 'on' })
    expect(report.lights[1]).toMatchObject({ label: '晓晓', state: 'on' })
    expect(report.lights[2]).toMatchObject({ label: 'qwen-plus', state: 'on' })
  })

  test('blocks local entry when sherpa is not ready', async () => {
    const report = await inspectCompanionEntry('local', '', {
      listProviders: async () => ({ items: [chat] }),
      localAsr: async () => ({ supported: true, ready: false }),
      refEngine: async () => ({ state: 'online' }),
    })
    expect(report.allowListen).toBe(false)
    expect(report.blockReason).toMatch(/sherpa/)
    expect(report.lights[0].state).toBe('off')
  })

  test('marks speak as starting instead of dead while SoVITS is launching', async () => {
    const report = await inspectCompanionEntry('local', '', {
      listProviders: async () => ({ items: [chat] }),
      localAsr: async () => ({ supported: true, ready: true }),
      refEngine: async () => ({ state: 'launching' }),
    })
    expect(report.allowListen).toBe(true)
    expect(report.speakReady).toBe(false)
    expect(report.lights[1]).toMatchObject({ label: 'GPT-SoVITS 启动中', state: 'warn' })
  })

  test('honest 晓晓 label when launching still has last_error', async () => {
    const report = await inspectCompanionEntry('local', '', {
      listProviders: async () => ({ items: [chat] }),
      localAsr: async () => ({ supported: true, ready: true }),
      refEngine: async () => ({ state: 'launching', last_error: '引擎未就绪：/docs 仍无响应' }),
    })
    expect(report.speakReady).toBe(false)
    expect(report.lights[1]).toMatchObject({ label: '晓晓（克隆未就绪）', state: 'warn' })
  })

  test('still allows local listen when GPT-SoVITS is offline', async () => {
    const report = await inspectCompanionEntry('local', '', {
      listProviders: async () => ({ items: [chat] }),
      localAsr: async () => ({ supported: true, ready: true }),
      refEngine: async () => ({ state: 'offline' }),
    })
    expect(report.allowListen).toBe(true)
    expect(report.speakReady).toBe(false)
    expect(report.lights[1].state).toBe('off')
    expect(report.lights[1].label).toMatch(/未就绪/)
  })

  test('surfaces the hosted engine last_error on the speak light', async () => {
    const report = await inspectCompanionEntry('local', '', {
      listProviders: async () => ({ items: [chat] }),
      localAsr: async () => ({ supported: true, ready: true }),
      refEngine: async () => ({ state: 'offline', last_error: 'jieba_fast dict.txt missing' }),
    })
    expect(report.lights[1].label).toMatch(/jieba_fast/)
  })

  test('allows local entry when sherpa and SoVITS are both ready', async () => {
    const report = await inspectCompanionEntry('local', '', {
      listProviders: async () => ({ items: [chat] }),
      localAsr: async () => ({ supported: true, ready: true }),
      refEngine: async () => ({ state: 'online' }),
    })
    expect(report.allowListen).toBe(true)
    expect(report.blockReason).toBe('')
  })

  test('blocks volc when there is no voice provider', async () => {
    const report = await inspectCompanionEntry('volc', '', {
      listProviders: async () => ({ items: [chat] }),
    })
    expect(report.allowListen).toBe(false)
    expect(report.blockReason).toMatch(/seed-asr/)
  })

  test('volc is ready when a voice provider exists', async () => {
    const report = await inspectCompanionEntry('volc', '', {
      listProviders: async () => ({ items: [chat, volc] }),
    })
    expect(report.allowListen).toBe(true)
    expect(report.lights[0]).toMatchObject({ label: '火山 seed-asr · 说完再答 · 打断用按钮', state: 'on' })
    expect(report.lights[1]).toMatchObject({ label: '晓晓（未配朗读）', state: 'on' })
    expect(report.hasVolcTts).toBe(false)
    expect(report.hasTalkModel).toBe(false)
  })

  test('flags a listed realtime model without switching the listen light', async () => {
    const withTalk = {
      ...chat,
      models: [
        ...chat.models,
        { modelId: 'gpt-4o-realtime-preview', displayName: 'Realtime', isDefault: false, kind: 'llm' as const },
      ],
    }
    const report = await inspectCompanionEntry('volc', '', {
      listProviders: async () => ({ items: [withTalk, volc] }),
    })
    expect(report.hasTalkModel).toBe(true)
    expect(report.lights[0].label).toMatch(/seed-asr/)
    expect(report.lights[0].label).not.toMatch(/通话核/)
    const live = companionTalkLiveLights(report.lights)
    expect(live[0].label).toMatch(/通话核/)
    expect(live[1].label).toBe('通话核')
  })

  test('volc speak light is seed-tts only after a tts row exists', async () => {
    const withTts = {
      ...volc,
      models: [
        { modelId: 'seed-asr', displayName: 'seed-asr', isDefault: true, kind: 'asr' as const },
        { modelId: 'zh_female_xiaohe_uranus_bigtts', displayName: '小何', isDefault: false, kind: 'tts' as const, kindDefault: true },
      ],
    }
    const report = await inspectCompanionEntry('volc', '', {
      listProviders: async () => ({ items: [chat, withTts] }),
    })
    expect(report.allowListen).toBe(true)
    expect(report.hasVolcTts).toBe(true)
    expect(report.lights[1]).toMatchObject({ label: '火山 · 小何', state: 'on' })
    expect(report.speakVoiceId).toBe('zh_female_xiaohe_uranus_bigtts')
  })

  test('volc list timeout refuses listen with VOICE-004', async () => {
    vi.useFakeTimers()
    const pending = inspectCompanionEntry('volc', '', {
      listProviders: () => new Promise(() => {}),
    })
    const report = await vi.advanceTimersByTimeAsync(COMPANION_ENTRY_PROBE_MS + 20).then(() => pending)
    expect(report.allowListen).toBe(false)
    expect(report.blockReason).toMatch(/VOICE-004/)
    vi.useRealTimers()
  })

  test('missing LLM turns 想 off and refuses listen', async () => {
    const report = await inspectCompanionEntry('cloud', '', {
      listProviders: async () => ({ items: [volc] }),
    })
    expect(report.llmReady).toBe(false)
    expect(report.allowListen).toBe(false)
    expect(report.lights[2].state).toBe('off')
  })
})
