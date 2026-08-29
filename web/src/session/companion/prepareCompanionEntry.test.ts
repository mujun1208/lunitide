import { afterEach, describe, expect, test } from 'vitest'
import { applyVoicePath, defaultCompanionSettings, saveCompanionSettings } from './companionSettings'
import { MICROPHONE_DEVICE_KEY, saveMicrophoneId, selectedMicrophoneId } from '../../settings/microphone'
import { prepareCompanionEntry, resolveCompanionVoicePath } from './prepareCompanionEntry'

afterEach(() => {
  localStorage.clear()
})

describe('prepareCompanionEntry', () => {
  test('clears a saved microphone so 对话模式 matches the Windows default input', async () => {
    saveMicrophoneId('not-the-default')
    const prepared = await prepareCompanionEntry(defaultCompanionSettings())
    expect(selectedMicrophoneId()).toBe('')
    expect(localStorage.getItem(MICROPHONE_DEVICE_KEY)).toBeNull()
    expect(prepared.voicePath).toBe('cloud')
    expect(prepared.usedFallback).toBe(false)
  })

  test('retired MiniCPM-o saves enter 云端 instead of duplex', async () => {
    const stored = { ...defaultCompanionSettings(), voicePath: 'omni' as const }
    const prepared = await prepareCompanionEntry(stored)
    expect(prepared.voicePath).toBe('cloud')
    expect(prepared.settings.voicePath).toBe('cloud')
    expect(prepared.omniRequested).toBe(true)
    expect(prepared.omniReady).toBe(false)
    expect(prepared.usedFallback).toBe(false)
  })

  test('keeps an explicit 本地模型 card without a settings trip', async () => {
    const stored = applyVoicePath(defaultCompanionSettings(), 'local')
    saveCompanionSettings(stored)
    const prepared = await prepareCompanionEntry(stored)
    expect(prepared.voicePath).toBe('local')
    expect(prepared.usedFallback).toBe(false)
  })

  test('defaults an unset path to 云端', async () => {
    const resolved = await resolveCompanionVoicePath(defaultCompanionSettings())
    expect(resolved.voicePath).toBe('cloud')
  })
})
