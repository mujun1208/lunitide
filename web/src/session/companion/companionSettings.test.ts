import { afterEach, describe, expect, test } from 'vitest'
import { defaultCompanionSettings, loadCompanionSettings, saveCompanionSettings } from './companionSettings'

const STORAGE_KEY = 'lunitide:companion'

afterEach(() => {
  localStorage.removeItem(STORAGE_KEY)
})

describe('companionSettings cloud default', () => {
  test('new installs default to the free Microsoft cloud engine', () => {
    expect(defaultCompanionSettings().engine).toBe('edge')
    expect(loadCompanionSettings().engine).toBe('edge')
  })

  test('rev-1 OneCore default migrates to edge once', () => {
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
    expect(JSON.parse(localStorage.getItem(STORAGE_KEY) || '{}').rev).toBe(3)
  })

  test('rev-2 installs gain full-duplex defaults on load', () => {
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
    expect(loaded.bargeIn).toBe(true)
    expect(JSON.parse(localStorage.getItem(STORAGE_KEY) || '{}').rev).toBe(3)
  })

  test('an explicit later OneCore choice is kept', () => {
    saveCompanionSettings({ ...defaultCompanionSettings(), engine: 'natural', voiceId: 'local-voice' })
    const loaded = loadCompanionSettings()
    expect(loaded.engine).toBe('natural')
    expect(loaded.voiceId).toBe('local-voice')
  })

  test('sapi and ref are never migrated', () => {
    localStorage.setItem(STORAGE_KEY, JSON.stringify({ engine: 'sapi', voiceId: 'v' }))
    expect(loadCompanionSettings().engine).toBe('sapi')
  })
})
