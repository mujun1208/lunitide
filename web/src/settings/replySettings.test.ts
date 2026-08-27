import { afterEach, describe, expect, test } from 'vitest'
import {
  chatStartReplyFields,
  loadReplySettings,
  saveReplySettings,
} from './replySettings'

afterEach(() => {
  localStorage.removeItem('lunitide:general')
})

describe('replySettings', () => {
  test('companion turns omit overlays', () => {
    saveReplySettings({ replyStyle: 'teacher', structuredTemplate: 'kv' })
    expect(chatStartReplyFields(true)).toEqual({})
    expect(chatStartReplyFields(false)).toEqual({ replyStyle: 'teacher', structuredTemplate: 'kv' })
  })

  test('save merges into existing general settings', () => {
    localStorage.setItem('lunitide:general', JSON.stringify({ enterToSend: false, replyStyle: 'default' }))
    saveReplySettings({ replyStyle: 'npc' })
    const stored = JSON.parse(localStorage.getItem('lunitide:general') || '{}')
    expect(stored.enterToSend).toBe(false)
    expect(stored.replyStyle).toBe('npc')
    expect(loadReplySettings().replyStyle).toBe('npc')
  })
})
