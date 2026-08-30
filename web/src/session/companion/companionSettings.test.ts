import { afterEach, describe, expect, test } from 'vitest'
import {
  applyVoicePath,
  companionEngineProbeOrder,
  defaultCompanionSettings,
  formatInterruptHotkey,
  interruptHotkeyFromEvent,
  hasExplicitCompanionVoicePath,
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
    expect(hasExplicitCompanionVoicePath()).toBe(false)
    saveCompanionSettings(defaultCompanionSettings())
    expect(hasExplicitCompanionVoicePath()).toBe(true)
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
    expect(loaded.wakeWord).toBe(true)
    expect(JSON.parse(localStorage.getItem(STORAGE_KEY) || '{}').rev).toBe(11)
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
    expect(JSON.parse(localStorage.getItem(STORAGE_KEY) || '{}').rev).toBe(11)
  })

  test('even a deliberate OneCore choice is moved, because it cannot work', () => {
    saveCompanionSettings({ ...defaultCompanionSettings(), engine: 'natural', voiceId: 'local-voice' })
    const loaded = loadCompanionSettings()
    expect(loaded.engine).toBe('edge')
    expect(loaded.voiceId).toBe('')
  })

  test('new installs default wake word on', () => {
    expect(defaultCompanionSettings().wakeWord).toBe(true)
    expect(defaultCompanionSettings().wakeVad).toBe(true)
    expect(defaultCompanionSettings().instantAck).toBe(false)
    expect(defaultCompanionSettings().voiceBargeIn).toBe(false)
    expect(loadCompanionSettings().wakeWord).toBe(true)
    expect(loadCompanionSettings().wakeVad).toBe(true)
    expect(loadCompanionSettings().instantAck).toBe(false)
    expect(loadCompanionSettings().voiceBargeIn).toBe(false)
  })

  test('older saves without instantAck or voiceBargeIn keep the product defaults', () => {
    localStorage.setItem(STORAGE_KEY, JSON.stringify({
      ...defaultCompanionSettings(),
      instantAck: undefined,
      voiceBargeIn: undefined,
      rev: 10,
    }))
    expect(loadCompanionSettings().instantAck).toBe(false)
    expect(loadCompanionSettings().voiceBargeIn).toBe(false)
  })

  test('rev-10 saves that had the 嗯 pad on are migrated off once', () => {
    localStorage.setItem(STORAGE_KEY, JSON.stringify({
      ...defaultCompanionSettings(),
      instantAck: true,
      rev: 10,
    }))
    expect(loadCompanionSettings().instantAck).toBe(false)
    expect(JSON.parse(localStorage.getItem(STORAGE_KEY) || '{}').rev).toBe(11)
  })

  test('a current-rev on choice for the pad stays on', () => {
    saveCompanionSettings({ ...defaultCompanionSettings(), instantAck: true })
    expect(loadCompanionSettings().instantAck).toBe(true)
  })

  test('older saves without wakeVad keep the live-voice gate on', () => {
    localStorage.setItem(STORAGE_KEY, JSON.stringify({
      ...defaultCompanionSettings(),
      wakeVad: undefined,
      rev: 10,
    }))
    expect(loadCompanionSettings().wakeVad).toBe(true)
  })

  test('rev-9 saves that had wake forced off are restored once the listener is wired', () => {
    localStorage.setItem(STORAGE_KEY, JSON.stringify({
      ...defaultCompanionSettings(),
      wakeWord: false,
      rev: 9,
    }))
    expect(loadCompanionSettings().wakeWord).toBe(true)
  })

  test('a current-rev off choice stays off', () => {
    saveCompanionSettings({ ...defaultCompanionSettings(), wakeWord: false })
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
    expect(applyVoicePath(defaultCompanionSettings(), 'volc').voicePath).toBe('volc')
    expect(applyVoicePath(defaultCompanionSettings(), 'volc').engine).toBe('edge')
    expect(applyVoicePath(defaultCompanionSettings(), 'volc').voiceBargeIn).toBe(true)
    expect(applyVoicePath({ ...defaultCompanionSettings(), voiceId: 'refpack:甜心少女.wav' }, 'volc').voiceId).toBe('')
    expect(applyVoicePath(defaultCompanionSettings(), 'local').engine).toBe('ref')
    expect(applyVoicePath(defaultCompanionSettings(), 'local').voiceId).toBe('refpack:优质台湾腔.wav')
    expect(applyVoicePath(defaultCompanionSettings(), 'local').voiceBargeIn).toBe(true)
    expect(applyVoicePath(defaultCompanionSettings(), 'local').recognizer).toBe('local')
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
    expect(loaded.voiceBargeIn).toBe(true)
  })

  test('keeps a saved volc listen path on Edge TTS', () => {
    localStorage.setItem(STORAGE_KEY, JSON.stringify({
      enabled: true,
      voicePath: 'volc',
      engine: 'edge',
      voiceBargeIn: true,
    }))
    const loaded = loadCompanionSettings()
    expect(loaded.voicePath).toBe('volc')
    expect(loaded.engine).toBe('edge')
    expect(loaded.voiceBargeIn).toBe(true)
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
