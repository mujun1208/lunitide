import { describe, expect, it } from 'vitest'
import { modelSupportsFunctionCalling } from './modelKind'

describe('modelSupportsFunctionCalling (UX-04 主动门控)', () => {
  it('默认对普通 LLM 视为支持工具调用', () => {
    expect(modelSupportsFunctionCalling({ modelId: 'gpt-4o' })).toBe(true)
    expect(modelSupportsFunctionCalling({ modelId: 'deepseek-chat' })).toBe(true)
    expect(modelSupportsFunctionCalling({ modelId: 'qwen-max', displayName: '通义千问 Max' })).toBe(true)
  })

  it('对已知仅推理、会拒绝 tools 的模型返回 false', () => {
    expect(modelSupportsFunctionCalling({ modelId: 'deepseek-reasoner' })).toBe(false)
    expect(modelSupportsFunctionCalling({ modelId: 'deepseek-r1' })).toBe(false)
    expect(modelSupportsFunctionCalling({ modelId: 'deepseek_r1' })).toBe(false)
    expect(modelSupportsFunctionCalling({ modelId: 'x', displayName: 'DeepSeek R1' })).toBe(false)
  })

  it('缺失或空 modelId 时保守视为支持（不误伤）', () => {
    expect(modelSupportsFunctionCalling(undefined)).toBe(true)
    expect(modelSupportsFunctionCalling(null)).toBe(true)
    expect(modelSupportsFunctionCalling({ modelId: '' })).toBe(true)
    expect(modelSupportsFunctionCalling({ modelId: '   ' })).toBe(true)
  })

  it('不误伤名字里含 r1 但非 deepseek 推理模型的普通模型', () => {
    expect(modelSupportsFunctionCalling({ modelId: 'gemini-1.5-pro' })).toBe(true)
    expect(modelSupportsFunctionCalling({ modelId: 'claude-3-5-sonnet' })).toBe(true)
  })
})