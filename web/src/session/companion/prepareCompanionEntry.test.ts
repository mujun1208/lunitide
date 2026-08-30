import { afterEach, describe, expect, test } from 'vitest'
import type { ProviderDTO } from '../../generated/bridge'
import { applyVoicePath, defaultCompanionSettings, loadCompanionSettings, saveCompanionSettings } from './companionSettings'
import { MICROPHONE_DEVICE_KEY, saveMicrophoneId, selectedMicrophoneId } from '../../settings/microphone'
import { prepareCompanionEntry, resolveCompanionVoicePath } from './prepareCompanionEntry'
import type { CompanionLightProbes } from './companionLights'

const STORAGE_KEY = 'lunitide:companion'

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

const cloudProbes: CompanionLightProbes = {
  listProviders: async () => ({ items: [chat] }),
}

const localReadyProbes: CompanionLightProbes = {
  listProviders: async () => ({ items: [chat] }),
  localAsr: async () => ({ supported: true, ready: true }),
  refEngine: async () => ({ state: 'online' }),
}

afterEach(() => {
  localStorage.clear()
})

describe('prepareCompanionEntry', () => {
  test('clears a saved microphone so 对话模式 matches the Windows default input', async () => {
    saveMicrophoneId('not-the-default')
    const prepared = await prepareCompanionEntry(defaultCompanionSettings(), cloudProbes)
    expect(selectedMicrophoneId()).toBe('')
    expect(localStorage.getItem(MICROPHONE_DEVICE_KEY)).toBeNull()
    expect(prepared.voicePath).toBe('cloud')
  })

  test('retired omni saves load as cloud through settings migration', async () => {
    localStorage.setItem(STORAGE_KEY, JSON.stringify({ enabled: true, voicePath: 'omni', engine: 'edge' }))
    const prepared = await prepareCompanionEntry(loadCompanionSettings(), cloudProbes)
    expect(prepared.voicePath).toBe('cloud')
    expect(prepared.settings.voicePath).toBe('cloud')
    expect(prepared.settings.engine).toBe('edge')
  })

  test('keeps an explicit 本地模型 card without a settings trip', async () => {
    const stored = applyVoicePath(defaultCompanionSettings(), 'local')
    saveCompanionSettings(stored)
    const prepared = await prepareCompanionEntry(stored, localReadyProbes)
    expect(prepared.voicePath).toBe('local')
  })

  test('keeps an explicit 火山 card without collapsing it to 云端', async () => {
    const stored = applyVoicePath(defaultCompanionSettings(), 'volc')
    saveCompanionSettings(stored)
    const prepared = await prepareCompanionEntry(stored, {
      listProviders: async () => ({ items: [chat, volc] }),
    })
    expect(prepared.voicePath).toBe('volc')
    expect(prepared.settings.engine).toBe('edge')
    expect(prepared.settings.voiceBargeIn).toBe(true)
  })

  test('defaults an unset path to 云端 when there is no voice provider', async () => {
    const resolved = await resolveCompanionVoicePath(defaultCompanionSettings())
    expect(resolved).toBe('cloud')
  })

  test('recommends 火山 when nothing is saved and seed-asr exists', async () => {
    const prepared = await prepareCompanionEntry(defaultCompanionSettings(), {
      listProviders: async () => ({ items: [chat, volc] }),
    })
    expect(prepared.voicePath).toBe('volc')
    expect(prepared.settings.voicePath).toBe('volc')
  })

  test('keeps an explicit 云端 card even when seed-asr exists', async () => {
    const stored = applyVoicePath(defaultCompanionSettings(), 'cloud')
    saveCompanionSettings(stored)
    const prepared = await prepareCompanionEntry(stored, {
      listProviders: async () => ({ items: [chat, volc] }),
    })
    expect(prepared.voicePath).toBe('cloud')
  })
})
