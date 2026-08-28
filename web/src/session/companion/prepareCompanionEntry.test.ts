import { afterEach, describe, expect, test, vi } from 'vitest'
import { applyVoicePath, defaultCompanionSettings, saveCompanionSettings } from './companionSettings'
import { MICROPHONE_DEVICE_KEY, saveMicrophoneId, selectedMicrophoneId } from '../../settings/microphone'

const omni = vi.hoisted(() => ({
  probe: vi.fn(),
}))

vi.mock('../omni/omniAudio', () => ({
  probeOmniChannel: () => omni.probe(),
}))

import { prepareCompanionEntry, resolveCompanionVoicePath } from './prepareCompanionEntry'

afterEach(() => {
  localStorage.clear()
  vi.restoreAllMocks()
})

describe('prepareCompanionEntry', () => {
  test('clears a saved microphone so 对话模式 matches the Windows default input', async () => {
    saveMicrophoneId('not-the-default')
    omni.probe.mockResolvedValue(false)
    const prepared = await prepareCompanionEntry(defaultCompanionSettings())
    expect(selectedMicrophoneId()).toBe('')
    expect(localStorage.getItem(MICROPHONE_DEVICE_KEY)).toBeNull()
    expect(prepared.voicePath).toBe('cloud')
    expect(prepared.usedFallback).toBe(false)
  })

  test('keeps MiniCPM-o when that card is explicit and the channel is ready', async () => {
    omni.probe.mockResolvedValue(true)
    const stored = applyVoicePath(defaultCompanionSettings(), 'omni')
    const prepared = await prepareCompanionEntry(stored)
    expect(prepared.voicePath).toBe('omni')
    expect(prepared.omniReady).toBe(true)
    expect(prepared.usedFallback).toBe(false)
  })

  test('falls back to 云端 when MiniCPM-o was chosen but is not ready', async () => {
    omni.probe.mockResolvedValue(false)
    const stored = applyVoicePath(defaultCompanionSettings(), 'omni')
    const prepared = await prepareCompanionEntry(stored)
    expect(prepared.voicePath).toBe('cloud')
    expect(prepared.omniRequested).toBe(true)
    expect(prepared.usedFallback).toBe(true)
  })

  test('keeps an explicit 本地模型 card without a settings trip', async () => {
    omni.probe.mockResolvedValue(false)
    const stored = applyVoicePath(defaultCompanionSettings(), 'local')
    saveCompanionSettings(stored)
    const prepared = await prepareCompanionEntry(stored)
    expect(prepared.voicePath).toBe('local')
    expect(prepared.usedFallback).toBe(false)
  })

  test('defaults an unset path to 云端', async () => {
    omni.probe.mockResolvedValue(true)
    const resolved = await resolveCompanionVoicePath(defaultCompanionSettings())
    expect(resolved.voicePath).toBe('cloud')
  })
})
