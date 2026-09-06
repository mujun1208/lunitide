import { afterEach, describe, expect, it } from 'vitest'
import { chatStartToolProfile } from './toolProfile'

const GENERAL_KEY = 'lunitide:general'

afterEach(() => {
  try { localStorage.removeItem(GENERAL_KEY) } catch { /* ignore */ }
})

describe('chatStartToolProfile (UX-04 主动门控)', () => {
  it('模型不支持工具调用时跳过 toolProfile 注入', () => {
    try { localStorage.setItem(GENERAL_KEY, JSON.stringify({ toolProfile: 'colleague' })) } catch { /* ignore */ }
    expect(chatStartToolProfile(false)).toEqual({})
  })

  it('模型支持工具调用时按 localStorage 注入 toolProfile', () => {
    try { localStorage.setItem(GENERAL_KEY, JSON.stringify({ toolProfile: 'coding' })) } catch { /* ignore */ }
    expect(chatStartToolProfile(true)).toEqual({ toolProfile: 'coding' })
  })

  it('默认参数视为支持（向后兼容）', () => {
    try { localStorage.setItem(GENERAL_KEY, JSON.stringify({ toolProfile: 'minimal' })) } catch { /* ignore */ }
    expect(chatStartToolProfile()).toEqual({ toolProfile: 'minimal' })
  })

  it('无 localStorage 设置时返回空对象', () => {
    expect(chatStartToolProfile(true)).toEqual({})
  })
})