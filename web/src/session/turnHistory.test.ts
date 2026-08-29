import { describe, expect, it } from 'vitest'
import { countAssistantMessages, turnHistorySettlement } from './turnHistory'

describe('turnHistory', () => {
  it('counts assistant messages', () => {
    expect(countAssistantMessages([
      { role: 'user' },
      { role: 'assistant' },
      { role: 'tool' },
      { role: 'assistant' },
    ])).toBe(2)
  })

  it('detects persisted assistant history after reload', () => {
    expect(turnHistorySettlement(0, 1)).toEqual({ persisted: true })
    expect(turnHistorySettlement(1, 1, '无法执行。')).toEqual({ persisted: false, fallbackNotice: '无法执行。' })
  })
})
