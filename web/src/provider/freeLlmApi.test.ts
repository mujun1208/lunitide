import { describe, expect, it } from 'vitest'
import {
  currentUsageMonth,
  freeLLMAPIBaseUrl,
  loadFreeLLMAPIHub,
  normalizeFreeLLMOrigin,
  recordFreeLLMAPIUsage,
  saveFreeLLMAPIHub,
  FREE_LLM_API_STORAGE_KEY,
} from './freeLlmApi'

describe('freeLlmApi helpers', () => {
  it('normalizes base URL to origin without /v1 suffix', () => {
    expect(normalizeFreeLLMOrigin('http://127.0.0.1:3001/v1')).toBe('http://127.0.0.1:3001')
    expect(freeLLMAPIBaseUrl('http://127.0.0.1:3001')).toBe('http://127.0.0.1:3001/v1')
  })

  it('records monthly usage in localStorage', () => {
    localStorage.removeItem(FREE_LLM_API_STORAGE_KEY)
    const hub = loadFreeLLMAPIHub()
    const next = recordFreeLLMAPIUsage(hub, { inputTokens: 100, outputTokens: 50 })
    expect(next.monthlyUsage?.month).toBe(currentUsageMonth())
    expect(next.monthlyUsage?.inputTokens).toBe(100)
    expect(next.monthlyUsage?.outputTokens).toBe(50)
    expect(next.monthlyUsage?.requests).toBe(1)
    saveFreeLLMAPIHub(recordFreeLLMAPIUsage(next, { inputTokens: 20, outputTokens: 10 }))
    const loaded = loadFreeLLMAPIHub()
    expect(loaded.monthlyUsage?.inputTokens).toBe(120)
    expect(loaded.monthlyUsage?.requests).toBe(2)
  })
})
