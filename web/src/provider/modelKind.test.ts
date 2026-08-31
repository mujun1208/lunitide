import { describe, expect, test } from 'vitest'
import type { ProviderDTO } from '../generated/bridge'
import { blankVoiceModels, extraVoiceModel, hasConfiguredVolcTts, isAsrVoiceModel, isTtsVoiceModel, kindLabel, llmReadyProviders, modelKind, nextVoiceRole, persistKind, pickDefaultLLM, pickDefaultTTS, pickDefaultVoice, preferredLLMOf, voiceReadyProviders } from './modelKind'

const now = '2026-01-01T00:00:00Z'
const base = (over: Partial<ProviderDTO> & Pick<ProviderDTO, 'id' | 'name' | 'models'>): ProviderDTO => ({
  protocol: 'openai_compatible',
  baseUrl: 'https://example.test',
  status: 'enabled',
  credentialState: 'configured',
  createdAt: now,
  updatedAt: now,
  version: 1,
  ...over,
})

describe('modelKind helpers', () => {
  test('treats missing kind as llm', () => {
    expect(modelKind({})).toBe('llm')
    expect(modelKind({ kind: 'vision' })).toBe('vision')
    expect(modelKind({ kind: 'voice' })).toBe('voice')
    expect(modelKind({ kind: 'nope' })).toBe('llm')
  })

  test('chat pickers only see llm models and keep mixed providers', () => {
    const mixed = base({
      id: '01ARZ3NDEKTSV4RRFFQ69G5FAV',
      name: 'Mixed',
      models: [
        { modelId: 'chat', displayName: 'Chat', isDefault: true, kind: 'llm' },
        { modelId: 'draw', displayName: 'Draw', isDefault: false, kind: 'image' },
      ],
    })
    const visionOnly = base({
      id: '01ARZ3NDEKTSV4RRFFQ69G5FAW',
      name: 'Vision',
      models: [{ modelId: 'ocr', displayName: 'OCR', isDefault: true, kind: 'vision' }],
    })
    const ready = llmReadyProviders([mixed, visionOnly])
    expect(ready).toHaveLength(1)
    expect(ready[0].models.map(m => m.modelId)).toEqual(['chat'])
    expect(pickDefaultLLM([mixed, visionOnly])).toEqual({ provider: ready[0], modelId: 'chat' })
  })

  test('pickDefaultLLM prefers the global kind default over list order', () => {
    const first = base({
      id: '01ARZ3NDEKTSV4RRFFQ69G5FAX',
      name: 'Backup',
      models: [{ modelId: 'backup', displayName: 'Backup', isDefault: true, kind: 'llm' }],
    })
    const second = base({
      id: '01ARZ3NDEKTSV4RRFFQ69G5FAY',
      name: 'Default',
      models: [{ modelId: 'main', displayName: 'Main', isDefault: true, kind: 'llm', kindDefault: true }],
    })
    expect(pickDefaultLLM([first, second])).toEqual({ provider: second, modelId: 'main' })
    expect(preferredLLMOf(second)).toEqual({ providerId: second.id, modelId: 'main' })
    expect(preferredLLMOf({ ...second, status: 'disabled' })).toBeUndefined()
  })

  test('voice pickers only see volc speech providers', () => {
    const volc = base({
      id: '01ARZ3NDEKTSV4RRFFQ69G5FAZ',
      name: 'Volc',
      protocol: 'volc_speech',
      models: [{ modelId: 'seed-asr-2.0', displayName: 'seed-asr 2.0', isDefault: true, kind: 'voice', kindDefault: true }],
    })
    const chat = base({
      id: '01ARZ3NDEKTSV4RRFFQ69G5FAV',
      name: 'Chat',
      models: [{ modelId: 'chat', displayName: 'Chat', isDefault: true, kind: 'llm' }],
    })
    expect(voiceReadyProviders([volc, chat])).toHaveLength(1)
    expect(pickDefaultVoice([chat, volc])).toEqual({ provider: volc, modelId: 'seed-asr-2.0' })
    expect(llmReadyProviders([volc, chat])).toHaveLength(1)
    expect(llmReadyProviders([volc, chat])[0].models.map(m => m.modelId)).toEqual(['chat'])
  })

  test('maps asr to the voice catalog and never picks TTS speakers for listen', () => {
    expect(modelKind({ kind: 'asr' })).toBe('voice')
    expect(modelKind({ kind: 'tts' })).toBe('voice')
    expect(isAsrVoiceModel({ kind: 'voice', modelId: 'seed-asr-2.0' })).toBe(true)
    expect(isAsrVoiceModel({ kind: 'asr', modelId: 'volc.seedasr.sauc.duration' })).toBe(true)
    expect(isAsrVoiceModel({ kind: 'tts', modelId: 'zh_female_xiaohe_uranus_bigtts' })).toBe(false)
    expect(isAsrVoiceModel({ kind: 'voice', modelId: 'zh_female_xiaohe_uranus_bigtts' })).toBe(false)
    const mixed = base({
      id: '01ARZ3NDEKTSV4RRFFQ69G5FB0',
      name: 'Volc mixed',
      protocol: 'volc_speech',
      models: [
        { modelId: 'zh_female_xiaohe_uranus_bigtts', displayName: '小何', isDefault: true, kind: 'tts', kindDefault: true },
        { modelId: 'seed-asr-2.0', displayName: 'seed-asr 2.0', isDefault: false, kind: 'asr' },
      ],
    })
    expect(pickDefaultVoice([mixed])?.modelId).toBe('seed-asr-2.0')
    expect(voiceReadyProviders([mixed])[0].models.map(m => m.modelId)).toEqual(['seed-asr-2.0'])
    expect(llmReadyProviders([mixed])).toHaveLength(0)
    expect(persistKind({ kind: 'voice' })).toBe('asr')
    expect(persistKind({ kind: 'tts' })).toBe('tts')
    expect(kindLabel({ kind: 'asr' })).toBe('听写')
    expect(kindLabel({ kind: 'tts' })).toBe('朗读')
    expect(nextVoiceRole([{ kind: 'asr' }])).toBe('tts')
    expect(isTtsVoiceModel({ kind: 'tts' })).toBe(true)
    expect(isTtsVoiceModel({ kind: 'asr' })).toBe(false)
    expect(hasConfiguredVolcTts([mixed])).toBe(true)
    expect(pickDefaultTTS([mixed])?.modelId).toBe('zh_female_xiaohe_uranus_bigtts')
    const resourceDefault = base({
      id: '01ARZ3NDEKTSV4RRFFQ69G5FB2',
      name: 'Volc resource default',
      protocol: 'volc_speech',
      models: [
        { modelId: 'volc.seedasr.sauc.duration', displayName: 'seed-asr 2.0', isDefault: true, kind: 'asr', kindDefault: true },
        { modelId: 'seed-tts-2.0', displayName: '豆包语音合成 2.0', isDefault: false, kind: 'tts', kindDefault: true },
        { modelId: 'zh_female_xiaohe_uranus_bigtts', displayName: '小何', isDefault: false, kind: 'tts' },
      ],
    })
    expect(pickDefaultTTS([resourceDefault])?.modelId).toBe('zh_female_xiaohe_uranus_bigtts')
    expect(pickDefaultTTS([base({
      id: '01ARZ3NDEKTSV4RRFFQ69G5FB3',
      name: 'Volc resource only',
      protocol: 'volc_speech',
      models: [
        { modelId: 'volc.seedasr.sauc.duration', displayName: 'seed-asr 2.0', isDefault: true, kind: 'asr', kindDefault: true },
        { modelId: 'seed-tts-2.0', displayName: '豆包语音合成 2.0', isDefault: false, kind: 'tts', kindDefault: true },
      ],
    })])?.modelId).toBe('zh_female_xiaohe_uranus_bigtts')
    expect(hasConfiguredVolcTts([base({
      id: '01ARZ3NDEKTSV4RRFFQ69G5FB1',
      name: 'Asr only',
      protocol: 'volc_speech',
      models: [{ modelId: 'seed-asr-2.0', displayName: 'seed-asr 2.0', isDefault: true, kind: 'voice' }],
    })])).toBe(false)
  })

  test('new volc draft ships listen plus official 小何 speak', () => {
    const seeded = blankVoiceModels()
    expect(seeded).toEqual([
      expect.objectContaining({ modelId: 'volc.seedasr.sauc.duration', kind: 'asr', isDefault: true, kindDefault: true }),
      expect.objectContaining({ modelId: 'seed-tts-2.0', kind: 'tts', isDefault: false, kindDefault: true, displayName: '豆包语音合成 2.0' }),
    ])
    expect(extraVoiceModel(seeded)).toMatchObject({ modelId: 'zh_female_xiaohe_uranus_bigtts', kind: 'tts', displayName: '小何' })
    expect(extraVoiceModel([])).toMatchObject({ modelId: 'volc.seedasr.sauc.duration', kind: 'asr' })
  })
})
