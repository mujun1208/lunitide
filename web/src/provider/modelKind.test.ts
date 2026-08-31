import { describe, expect, test } from 'vitest'
import type { ProviderDTO } from '../generated/bridge'
import { llmReadyProviders, modelKind, pickDefaultLLM, pickDefaultVoice, preferredLLMOf, voiceReadyProviders } from './modelKind'

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
})
