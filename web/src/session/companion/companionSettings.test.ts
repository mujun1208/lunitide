import { afterEach, describe, expect, test } from 'vitest'
import {
  applyVoicePath,
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
  test('keeps omni off the TTS fallback list', () => {
    expect(companionEngineProbeOrder('edge')).toEqual(['edge'])
    expect(companionEngineProbeOrder('ref')).toEqual(['ref', 'edge'])
    expect(companionEngineProbeOrder('sapi')).toEqual(['edge'])
    expect(companionEngineProbeOrder('edge')).not.toContain('sapi')
    expect(companionEngineProbeOrder('ref')).not.toContain('sapi')
    expect(applyVoicePath(defaultCompanionSettings(), 'omni').voicePath).toBe('cloud')
    expect(applyVoicePath(defaultCompanionSettings(), 'omni').engine).toBe('edge')
    expect(applyVoicePath(defaultCompanionSettings(), 'local').engine).toBe('ref')
    expect(applyVoicePath(defaultCompanionSettings(), 'local').voiceId).toBe('refpack:优质台湾腔.wav')
    expect(applyVoicePath({ ...defaultCompanionSettings(), voiceId: 'refpack:甜心少女.wav' }, 'cloud').voiceId).toBe('')
    expect(applyVoicePath({ ...defaultCompanionSettings(), voiceId: 'refpack:甜心少女.wav' }, 'omni').voicePath).toBe('cloud')
  })

  test('keeps a saved local clone path', () => {
    localStorage.setItem(STORAGE_KEY, JSON.stringify({
      enabled: true,
      voicePath: 'local',
      engine: 'ref',
      voiceId: 'refpack:甜心少女.wav',
    }))
    const loaded = loadCompanionSettings()
    expect(loaded.voicePath).toBe('local')
    expect(loaded.engine).toBe('ref')
    expect(loaded.voiceId).toBe('refpack:甜心少女.wav')
  })

  test('migrates leftover classic SAPI onto Edge', () => {
    localStorage.setItem(STORAGE_KEY, JSON.stringify({
      enabled: true,
      engine: 'sapi',
      voiceId: 'HKEY_LOCAL_MACHINE\\x',
    }))
    const loaded = loadCompanionSettings()
    expect(loaded.voicePath).toBe('cloud')
    expect(loaded.engine).toBe('edge')
    expect(loaded.voiceId).toBe('')
  })

  test('migrates leftover ref engine without a path onto local', () => {
    localStorage.setItem(STORAGE_KEY, JSON.stringify({
      enabled: true,
      engine: 'ref',
      voiceId: 'refpack:优质台湾腔.wav',
    }))
    const loaded = loadCompanionSettings()
    expect(loaded.voicePath).toBe('local')
    expect(loaded.engine).toBe('ref')
    expect(loaded.voiceId).toBe('refpack:优质台湾腔.wav')
  })

  test('migrates leftover MiniCPM-o / FLM saves onto 云端', () => {
    localStorage.setItem(STORAGE_KEY, JSON.stringify({
      enabled: true,
      voicePath: 'flm',
      flmPersonaId: 'refpack:甜心少女.wav',
      engine: 'edge',
    }))
    const loaded = loadCompanionSettings()
    expect(loaded.voicePath).toBe('cloud')
    expect(loaded.engine).toBe('edge')
    expect(loaded.omniPersonaId).toBe('refpack:甜心少女.wav')
    localStorage.setItem(STORAGE_KEY, JSON.stringify({
      enabled: true,
      voicePath: 'omni',
      engine: 'edge',
    }))
    expect(loadCompanionSettings().voicePath).toBe('cloud')
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

  test('loading current-rev settings does not re-save in a loop', () => {
    saveCompanionSettings(defaultCompanionSettings())
    const seen: Event[] = []
    const onSave = (event: Event) => seen.push(event)
    window.addEventListener('lunitide:companion-settings', onSave)
    loadCompanionSettings()
    loadCompanionSettings()
    window.removeEventListener('lunitide:companion-settings', onSave)
    expect(seen).toHaveLength(0)
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
