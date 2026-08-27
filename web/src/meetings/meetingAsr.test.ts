import { beforeEach, describe, expect, test, vi } from 'vitest'
import type { CompanionSpeechHandle } from '../session/companion/speech'

const asr = vi.hoisted(() => ({
  local: vi.fn(),
  web: vi.fn(),
  probe: vi.fn(),
  handle: (): CompanionSpeechHandle => ({
    stop: vi.fn(),
    setAssistantPlayback: vi.fn(),
    setCommitPaused: vi.fn(),
    pulseRecognition: vi.fn(),
    forceCommit: vi.fn(),
    resumeCapture: vi.fn(),
  }),
}))

vi.mock('../session/companion/localAsr', () => ({ localAsrStatus: asr.probe }))
vi.mock('../session/companion/localSpeech', () => ({ startLocalCompanionSpeech: asr.local }))
vi.mock('../session/companion/speech', async importOriginal => {
  const actual = await importOriginal<typeof import('../session/companion/speech')>()
  return { ...actual, startCompanionSpeech: asr.web }
})

import { startMeetingSpeech } from './meetingAsr'

describe('startMeetingSpeech', () => {
  beforeEach(() => {
    asr.local.mockReset().mockResolvedValue(asr.handle())
    asr.web.mockReset().mockResolvedValue(asr.handle())
    asr.probe.mockReset().mockResolvedValue(undefined)
  })

  test('uses Web Speech when local sherpa is not ready', async () => {
    const onFinal = vi.fn()
    await startMeetingSpeech({ onFinal, onError: vi.fn() })
    expect(asr.web).toHaveBeenCalledOnce()
    expect(asr.local).not.toHaveBeenCalled()
    expect(asr.web.mock.calls[0][0].duplex).toBe(true)
    expect(asr.web.mock.calls[0][0].spokenText()).toBe('')
  })

  test('never treats companion TTS as meeting speech', async () => {
    await startMeetingSpeech({ onFinal: vi.fn(), onError: vi.fn(), spokenText: () => '助手在说话' })
    expect(asr.web.mock.calls[0][0].spokenText()).toBe('')
  })

  test('prefers companion local ASR when the model is ready', async () => {
    asr.probe.mockResolvedValue({ supported: true, ready: true })
    await startMeetingSpeech({ onFinal: vi.fn(), onError: vi.fn() })
    expect(asr.local).toHaveBeenCalledOnce()
    expect(asr.web).not.toHaveBeenCalled()
  })
})
