import { afterEach, describe, expect, test } from 'vitest'
import {
  companionEngineProbeOrder,
  defaultCompanionSettings,
  formatInterruptHotkey,
  interruptHotkeyFromEvent,
  loadCompanionSettings,
  matchesInterruptHotkey,
  saveCompanionSettings,
  voiceIdForEngineSwitch,
} from './companionSettings'

const STORAGE_KEY = 'lunitide:companion'

afterEach(() => {
  localStorage.removeItem(STORAGE_KEY)
})

describe('companionSettings voice default', () => {
  test('new installs default to cloud Edge for reliable speech', () => {
    expect(defaultCompanionSettings().engine).toBe('edge')
    expect(loadCompanionSettings().engine).toBe('edge')
  })

  test('rev-1 OneCore installs migrate to edge when no voice was chosen', () => {
    localStorage.setItem(STORAGE_KEY, JSON.stringify({
      enabled: true,
      autoSpeak: true,
      wakeWord: true,
      voiceId: '',
      rate: 4,
      volume: 80,
      engine: 'natural',
      refEndpoint: '',
    }))
    const loaded = loadCompanionSettings()
    expect(loaded.engine).toBe('edge')
    expect(loaded.wakeWord).toBe(false)
    expect(JSON.parse(localStorage.getItem(STORAGE_KEY) || '{}').rev).toBe(9)
  })

  test('an install left on OneCore is moved off it, voice id and all', () => {
    // Keeping their choice was the kinder-looking option right up until the
    // engine turned out to be unreachable: SAPI does not enumerate the
    // mirrored OneCore tokens, so honouring the preference meant a companion
    // that showed captions and never spoke. The voice id goes too — those
    // tokens do not exist for any engine still on offer.
    localStorage.setItem(STORAGE_KEY, JSON.stringify({
      enabled: true,
      autoSpeak: true,
      wakeWord: true,
      voiceId: 'HKEY_LOCAL_MACHINE\\x',
      rate: 4,
      volume: 80,
      engine: 'natural',
      refEndpoint: '',
    }))
    const loaded = loadCompanionSettings()
    expect(loaded.engine).toBe('edge')
    expect(loaded.voiceId).toBe('')
  })

  test('rev-2 installs gain full-duplex defaults and a manual interrupt hotkey', () => {
    localStorage.setItem(STORAGE_KEY, JSON.stringify({
      enabled: true,
      autoSpeak: true,
      wakeWord: true,
      voiceId: '',
      rate: 4,
      volume: 80,
      engine: 'edge',
      refEndpoint: '',
      rev: 2,
    }))
    const loaded = loadCompanionSettings()
    expect(loaded.fullDuplex).toBe(true)
    expect(loaded.interruptHotkey).toEqual({ key: 'Tab', ctrl: false, alt: false, shift: false })
    expect(JSON.parse(localStorage.getItem(STORAGE_KEY) || '{}').rev).toBe(9)
  })

  test('even a deliberate OneCore choice is moved, because it cannot work', () => {
    saveCompanionSettings({ ...defaultCompanionSettings(), engine: 'natural', voiceId: 'local-voice' })
    const loaded = loadCompanionSettings()
    expect(loaded.engine).toBe('edge')
    expect(loaded.voiceId).toBe('')
  })

  test('new installs default wake word off', () => {
    expect(defaultCompanionSettings().wakeWord).toBe(false)
    expect(loadCompanionSettings().wakeWord).toBe(false)
  })
})

describe('companion engine fallback helpers', () => {
  test('falls back to the engines that can actually answer', () => {
    expect(companionEngineProbeOrder('edge')).toEqual(['edge', 'sapi'])
    expect(companionEngineProbeOrder('ref')).toEqual(['ref', 'edge', 'sapi'])
    // OneCore is not probed at all: the probe would cost a round trip to
    // land back on 'sapi', which is already next in the list.
    expect(companionEngineProbeOrder('edge')).not.toContain('natural')
  })

  test('drops cloud voice ids when falling back to OneCore', () => {
    expect(voiceIdForEngineSwitch('edge', 'natural', 'zh-CN-XiaoxiaoNeural::chat')).toBe('')
    expect(voiceIdForEngineSwitch('natural', 'edge', 'HKEY_LOCAL_MACHINE\\x')).toBe('')
  })
})

describe('interrupt hotkey', () => {
  test('formats modifiers and Tab', () => {
    expect(formatInterruptHotkey({ key: 'Tab', ctrl: false, alt: false, shift: false })).toBe('Tab')
    expect(formatInterruptHotkey({ key: 'x', ctrl: true, alt: false, shift: false })).toBe('Ctrl+X')
  })

  test('matches the captured key combo and ignores Escape', () => {
    const hotkey = { key: 'Tab', ctrl: false, alt: false, shift: false }
    expect(matchesInterruptHotkey(new KeyboardEvent('keydown', { key: 'Tab' }), hotkey)).toBe(true)
    expect(matchesInterruptHotkey(new KeyboardEvent('keydown', { key: ' ' }), hotkey)).toBe(false)
    expect(interruptHotkeyFromEvent(new KeyboardEvent('keydown', { key: 'Escape' }))).toBeNull()
    expect(interruptHotkeyFromEvent(new KeyboardEvent('keydown', { key: 'x', ctrlKey: true }))).toEqual({
      key: 'x',
      ctrl: true,
      alt: false,
      shift: false,
    })
  })
})
