import { describe, expect, it } from 'vitest'
import type { ProviderDraft } from './vendorPresets'
import {
  applyVendorPreset,
  modelsForProtocol,
  presetsForTab,
  protocolLabel,
  VENDOR_PRESETS,
} from './vendorPresets'

const blank = (): ProviderDraft => ({
  name: '',
  protocol: 'openai_compatible',
  baseUrl: 'https://',
  status: 'enabled',
  models: [{ modelId: '', displayName: '', isDefault: true, kind: 'llm', kindDefault: true }],
})

describe('vendor presets', () => {
  it('covers the platforms the settings page must start from', () => {
    const ids = VENDOR_PRESETS.map((preset) => preset.id)
    expect(ids).toEqual(
      expect.arrayContaining([
        'deepseek',
        'glm',
        'glm-agent-plan',
        'ark',
        'ark-agent-plan',
        'ark-agent-plan-responses',
        'ark-coding-plan',
        'ark-coding-plan-responses',
        'bailian',
        'bailian-responses',
        'anthropic',
        'openai',
        'openai-responses',
      ]),
    )
  })

  it('labels stored protocols for the catalog table', () => {
    expect(protocolLabel('openai_compatible')).toBe('OpenAI-compatible')
    expect(protocolLabel('openai_responses')).toBe('Responses API')
    expect(protocolLabel('anthropic')).toBe('Anthropic')
    expect(protocolLabel('volc_speech')).toBe('火山语音')
  })

  it('fills DeepSeek chat completions without inventing a second /v1', () => {
    const preset = VENDOR_PRESETS.find((item) => item.id === 'deepseek')
    if (!preset) throw new Error('deepseek preset')
    const next = applyVendorPreset(blank(), preset, 'llm')
    expect(next).toMatchObject({
      name: 'DeepSeek',
      protocol: 'openai_compatible',
      baseUrl: 'https://api.deepseek.com',
    })
    expect(next.models[0]).toMatchObject({ modelId: 'deepseek-chat', kind: 'llm', isDefault: true })
  })

  it('fills Volc Agent Plan Responses onto POST /responses bases', () => {
    const preset = VENDOR_PRESETS.find((item) => item.id === 'ark-agent-plan-responses')
    if (!preset) throw new Error('plan responses preset')
    const next = applyVendorPreset(blank(), preset, 'llm')
    expect(next.protocol).toBe('openai_responses')
    expect(next.baseUrl).toBe('https://ark.cn-beijing.volces.com/api/plan/v3')
  })

  it('keeps Coding Plan on /api/coding/v3', () => {
    const preset = VENDOR_PRESETS.find((item) => item.id === 'ark-coding-plan')
    if (!preset) throw new Error('coding preset')
    expect(applyVendorPreset(blank(), preset, 'llm').baseUrl).toBe(
      'https://ark.cn-beijing.volces.com/api/coding/v3',
    )
  })

  it('does not show Responses starters on the image tab', () => {
    expect(presetsForTab('image').every((preset) => preset.protocol !== 'openai_responses')).toBe(true)
    expect(presetsForTab('llm').some((preset) => preset.protocol === 'openai_responses')).toBe(true)
  })

  it('fills GLM on the Zhipu OpenAI-compatible origin', () => {
    const preset = VENDOR_PRESETS.find((item) => item.id === 'glm')
    if (!preset) throw new Error('glm preset')
    const next = applyVendorPreset(blank(), preset, 'llm')
    expect(next).toMatchObject({
      name: '智谱 GLM',
      protocol: 'openai_compatible',
      baseUrl: 'https://open.bigmodel.cn/api/paas/v4',
    })
    expect(next.models[0]).toMatchObject({ modelId: 'glm-5.3', kind: 'llm' })
  })

  it('drops media kinds when switching a draft to Responses API', () => {
    const models = modelsForProtocol(
      'openai_responses',
      [
        { modelId: 'chat', displayName: 'Chat', isDefault: true, kind: 'llm' },
        { modelId: 'pic', displayName: 'Pic', isDefault: false, kind: 'image' },
      ],
      'llm',
    )
    expect(models).toEqual([expect.objectContaining({ modelId: 'chat', kind: 'llm' })])
  })

  it('moves the default onto a remaining chat model when Responses drops media', () => {
    const models = modelsForProtocol(
      'openai_responses',
      [
        { modelId: 'pic', displayName: 'Pic', isDefault: true, kind: 'image' },
        { modelId: 'chat', displayName: 'Chat', isDefault: false, kind: 'llm' },
      ],
      'llm',
    )
    expect(models).toEqual([expect.objectContaining({ modelId: 'chat', kind: 'llm', isDefault: true })])
  })
})
