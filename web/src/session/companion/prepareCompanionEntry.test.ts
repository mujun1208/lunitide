import { afterEach, describe, expect, test } from 'vitest'
import { applyVoicePath, defaultCompanionSettings, loadCompanionSettings, saveCompanionSettings } from './companionSettings'
import { MICROPHONE_DEVICE_KEY, saveMicrophoneId, selectedMicrophoneId } from '../../settings/microphone'
import { prepareCompanionEntry, resolveCompanionVoicePath } from './prepareCompanionEntry'

const STORAGE_KEY = 'lunitide:companion'

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
  })

  test('retired omni saves load as cloud through settings migration', async () => {
    localStorage.setItem(STORAGE_KEY, JSON.stringify({ enabled: true, voicePath: 'omni', engine: 'edge' }))
    const prepared = await prepareCompanionEntry(loadCompanionSettings())
    expect(prepared.voicePath).toBe('cloud')
    expect(prepared.settings.voicePath).toBe('cloud')
    expect(prepared.settings.engine).toBe('edge')
  })

  test('keeps an explicit 本地模型 card without a settings trip', async () => {
    const stored = applyVoicePath(defaultCompanionSettings(), 'local')
    saveCompanionSettings(stored)
    const prepared = await prepareCompanionEntry(stored)
    expect(prepared.voicePath).toBe('local')
  })

  test('defaults an unset path to 云端', async () => {
    const resolved = await resolveCompanionVoicePath(defaultCompanionSettings())
    expect(resolved).toBe('cloud')
  })
})
