import { afterEach, describe, expect, test } from 'vitest'
import {
  applyLocalEngine,
  applyVoicePath,
  companionEngineProbeOrder,
  companionVoiceBargeInEnabled,
  companionPlaybackSettings,
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
    expect(loaded.wakeWord).toBe(false)
    expect(JSON.parse(localStorage.getItem(STORAGE_KEY) || '{}').rev).toBe(13)
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
    expect(JSON.parse(localStorage.getItem(STORAGE_KEY) || '{}').rev).toBe(13)
  })

  test('even a deliberate OneCore choice is moved, because it cannot work', () => {
    saveCompanionSettings({ ...defaultCompanionSettings(), engine: 'natural', voiceId: 'local-voice' })
    const loaded = loadCompanionSettings()
    expect(loaded.engine).toBe('edge')
    expect(loaded.voiceId).toBe('')
  })

  test('new installs keep home wake off', () => {
    expect(defaultCompanionSettings().wakeWord).toBe(false)
    expect(defaultCompanionSettings().wakeVad).toBe(true)
    expect(defaultCompanionSettings().instantAck).toBe(false)
    expect(defaultCompanionSettings().voiceBargeIn).toBe(false)
    expect(loadCompanionSettings().wakeWord).toBe(false)
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
    expect(JSON.parse(localStorage.getItem(STORAGE_KEY) || '{}').rev).toBe(13)
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

  test('older saves that still had home wake on are migrated off', () => {
    localStorage.setItem(STORAGE_KEY, JSON.stringify({
      ...defaultCompanionSettings(),
      wakeWord: true,
      rev: 11,
    }))
    expect(loadCompanionSettings().wakeWord).toBe(false)
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
    expect(companionEngineProbeOrder('volc')).toEqual(['volc'])
    expect(companionEngineProbeOrder('edge')).not.toContain('sapi')
    expect(companionEngineProbeOrder('ref')).not.toContain('sapi')
    expect(companionEngineProbeOrder('volc')).not.toContain('edge')
    const localReady = companionPlaybackSettings(applyVoicePath(defaultCompanionSettings(), 'local'), true)
    expect(localReady).toMatchObject({ engine: 'onnx', lockEngine: true, voicePath: 'local' })
    const localDown = companionPlaybackSettings(applyVoicePath(defaultCompanionSettings(), 'local'), false)
    expect(localDown).toMatchObject({ engine: 'edge', lockEngine: true, voiceId: '', voicePath: 'local' })
    const localSlow = companionPlaybackSettings(applyVoicePath(defaultCompanionSettings(), 'local'), true, true)
    expect(localSlow).toMatchObject({ engine: 'edge', lockEngine: true, voiceId: '', voicePath: 'local' })
    expect(companionPlaybackSettings(applyVoicePath(defaultCompanionSettings(), 'volc'), true).lockEngine).toBe(true)
    expect(applyVoicePath(defaultCompanionSettings(), 'omni').voicePath).toBe('cloud')
    expect(applyVoicePath(defaultCompanionSettings(), 'omni').engine).toBe('edge')
    expect(applyVoicePath(defaultCompanionSettings(), 'volc').voicePath).toBe('volc')
    expect(applyVoicePath(defaultCompanionSettings(), 'volc').engine).toBe('volc')
    expect(applyVoicePath(defaultCompanionSettings(), 'volc').voiceId).toBe('zh_female_xiaohe_uranus_bigtts')
    expect(applyVoicePath(defaultCompanionSettings(), 'volc', { volcTtsReady: false }).engine).toBe('edge')
    expect(applyVoicePath(defaultCompanionSettings(), 'volc', { volcTtsReady: false }).voicePath).toBe('volc')
    expect(applyVoicePath(defaultCompanionSettings(), 'volc').voiceBargeIn).toBe(false)
    // Unified half-duplex: client-side barge-in is retired on every path.
    expect(companionVoiceBargeInEnabled(applyVoicePath(defaultCompanionSettings(), 'volc'))).toBe(true)
    expect(companionVoiceBargeInEnabled(applyVoicePath(defaultCompanionSettings(), 'cloud'))).toBe(false)
    expect(companionVoiceBargeInEnabled(applyVoicePath(defaultCompanionSettings(), 'local'))).toBe(false)
    // The legacy voiceBargeIn flag is inert now — it never opens a live mic.
    expect(
      companionVoiceBargeInEnabled({ ...applyVoicePath(defaultCompanionSettings(), 'local'), voiceBargeIn: true }),
    ).toBe(false)
    expect(
      companionVoiceBargeInEnabled({ ...applyVoicePath(defaultCompanionSettings(), 'cloud'), voiceBargeIn: true }),
    ).toBe(false)
    expect(applyVoicePath({ ...defaultCompanionSettings(), voiceId: 'refpack:甜心少女.wav' }, 'volc').voiceId).toBe('zh_female_xiaohe_uranus_bigtts')
    expect(applyVoicePath({ ...defaultCompanionSettings(), voiceId: 'zh_female_vv_uranus_bigtts' }, 'cloud').voiceId).toBe('')
    // 本地 now defaults to the bundled offline Kokoro (onnx) engine.
    expect(applyVoicePath(defaultCompanionSettings(), 'local').engine).toBe('onnx')
    expect(applyVoicePath(defaultCompanionSettings(), 'local').voiceId).toBe('onnx-zf-xiaoxiao')
    expect(applyVoicePath(defaultCompanionSettings(), 'local').voiceBargeIn).toBe(false)
    expect(applyVoicePath(defaultCompanionSettings(), 'local').recognizer).toBe('local')
    expect(applyVoicePath({ ...defaultCompanionSettings(), voiceId: 'refpack:甜心少女.wav' }, 'cloud').voiceId).toBe('')
    expect(applyVoicePath({ ...defaultCompanionSettings(), voiceId: 'refpack:甜心少女.wav' }, 'omni').voicePath).toBe('cloud')
  })

  test('keeps a current-rev explicit GPT-SoVITS clone path', () => {
    // rev 13 = the user re-picked GPT-SoVITS after the Kokoro default landed,
    // so the ref choice and its refpack voice must survive untouched.
    localStorage.setItem(STORAGE_KEY, JSON.stringify({
      enabled: true,
      voicePath: 'local',
      engine: 'ref',
      voiceId: 'refpack:甜心少女.wav',
      rev: 13,
    }))
    const loaded = loadCompanionSettings()
    expect(loaded.voicePath).toBe('local')
    expect(loaded.engine).toBe('ref')
    expect(loaded.voiceId).toBe('refpack:甜心少女.wav')
    expect(loaded.voiceBargeIn).toBe(false)
  })

  test('migrates a legacy GPT-SoVITS local save onto bundled Kokoro once', () => {
    // rev < 13 pinned 本地 to GPT-SoVITS, whose models lived on a hardcoded
    // external drive. The one-time migration moves it to the install-and-use
    // offline Kokoro engine and normalizes the voice id.
    localStorage.setItem(STORAGE_KEY, JSON.stringify({
      enabled: true,
      voicePath: 'local',
      engine: 'ref',
      voiceId: 'refpack:甜心少女.wav',
    }))
    const loaded = loadCompanionSettings()
    expect(loaded.voicePath).toBe('local')
    expect(loaded.engine).toBe('onnx')
    expect(loaded.voiceId).toBe('onnx-zf-xiaoxiao')
    expect(JSON.parse(localStorage.getItem(STORAGE_KEY) || '{}').rev).toBe(13)
  })

  test('migrates a saved volc listen path onto seed-tts', () => {
    localStorage.setItem(STORAGE_KEY, JSON.stringify({
      enabled: true,
      voicePath: 'volc',
      engine: 'edge',
      voiceBargeIn: true,
    }))
    const loaded = loadCompanionSettings()
    expect(loaded.voicePath).toBe('volc')
    expect(loaded.engine).toBe('volc')
    expect(loaded.voiceId).toBe('zh_female_xiaohe_uranus_bigtts')
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

  test('migrates leftover ref engine without a path onto local Kokoro', () => {
    // A legacy ref save with no voicePath is inferred as 本地, then the rev<13
    // migration moves the timbre engine onto the bundled offline Kokoro.
    localStorage.setItem(STORAGE_KEY, JSON.stringify({
      enabled: true,
      engine: 'ref',
      voiceId: 'refpack:优质台湾腔.wav',
    }))
    const loaded = loadCompanionSettings()
    expect(loaded.voicePath).toBe('local')
    expect(loaded.engine).toBe('onnx')
    expect(loaded.voiceId).toBe('onnx-zf-xiaoxiao')
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

  test('onnx voice ids only survive an onnx↔onnx switch', () => {
    // Kokoro ids are meaningless to every other engine and vice versa.
    expect(voiceIdForEngineSwitch('onnx', 'onnx', 'onnx-zm-yunxi')).toBe('onnx-zm-yunxi')
    expect(voiceIdForEngineSwitch('edge', 'onnx', 'zh-CN-XiaoxiaoNeural')).toBe('')
    expect(voiceIdForEngineSwitch('onnx', 'edge', 'onnx-zf-xiaoxiao')).toBe('')
    expect(voiceIdForEngineSwitch('onnx', 'ref', 'onnx-zf-xiaoxiao')).toBe('')
  })

  test('applyLocalEngine swaps between Kokoro and GPT-SoVITS and keeps the local recognizer', () => {
    const base = defaultCompanionSettings()
    const onnx = applyLocalEngine(base, 'onnx')
    expect(onnx).toMatchObject({ voicePath: 'local', engine: 'onnx', recognizer: 'local' })
    expect(onnx.voiceId).toBe('onnx-zf-xiaoxiao')

    // onnx → ref falls back to the saved omni persona (a refpack clone).
    const ref = applyLocalEngine(onnx, 'ref')
    expect(ref).toMatchObject({ voicePath: 'local', engine: 'ref', recognizer: 'local' })
    expect(ref.voiceId).toBe(base.omniPersonaId)

    // ref → onnx normalizes the incompatible refpack id back to the default.
    const backToOnnx = applyLocalEngine(ref, 'onnx')
    expect(backToOnnx.engine).toBe('onnx')
    expect(backToOnnx.voiceId).toBe('onnx-zf-xiaoxiao')

    // An explicit onnx voice id is preserved across an onnx re-apply.
    const chosen = applyLocalEngine({ ...onnx, voiceId: 'onnx-zm-yunxi' }, 'onnx')
    expect(chosen.voiceId).toBe('onnx-zm-yunxi')
  })
})

describe('visualSkin', () => {
  test('defaults visualSkin to classic and rejects junk', () => {
    expect(loadCompanionSettings().visualSkin).toBe('classic')
    expect(defaultCompanionSettings().visualSkin).toBe('classic')
    saveCompanionSettings({ ...defaultCompanionSettings(), visualSkin: 'particle' })
    expect(loadCompanionSettings().visualSkin).toBe('particle')
    localStorage.setItem(STORAGE_KEY, JSON.stringify({ ...defaultCompanionSettings(), visualSkin: 'jarvis' }))
    expect(loadCompanionSettings().visualSkin).toBe('classic')
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
