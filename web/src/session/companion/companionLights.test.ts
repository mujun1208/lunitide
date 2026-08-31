import { describe, expect, test, vi } from 'vitest'
import { COMPANION_ENTRY_PROBE_MS, inspectCompanionEntry } from './companionLights'
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
    expect(report.lights[0]).toMatchObject({ label: '系统识别', state: 'on' })
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

  test('still allows local listen when GPT-SoVITS is offline', async () => {
    const report = await inspectCompanionEntry('local', '', {
      listProviders: async () => ({ items: [chat] }),
      localAsr: async () => ({ supported: true, ready: true }),
      refEngine: async () => ({ state: 'offline' }),
    })
    expect(report.allowListen).toBe(true)
    expect(report.speakReady).toBe(false)
    expect(report.lights[1].state).toBe('off')
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
    expect(report.lights[0]).toMatchObject({ label: '火山 seed-asr', state: 'on' })
    expect(report.lights[1]).toMatchObject({ label: '晓晓（未配朗读）', state: 'on' })
    expect(report.hasVolcTts).toBe(false)
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
