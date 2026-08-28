import { beforeEach, describe, expect, test, vi } from 'vitest'
import type { CompanionSpeechHandle } from '../session/companion/speech'

const asr = vi.hoisted(() => ({
  local: vi.fn(),
  web: vi.fn(),
  probe: vi.fn(),
  capture: vi.fn(),
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
vi.mock('./meetingCapture', async importOriginal => {
  const actual = await importOriginal<typeof import('./meetingCapture')>()
  return { ...actual, captureThisPcSystemAudio: asr.capture }
})

import { audioSourceLabel, prepareMeetingCapture, startMeetingSpeech } from './meetingAsr'

const extra = { getAudioTracks: () => [{ kind: 'audio' }], getTracks: () => [] } as unknown as MediaStream

describe('startMeetingSpeech', () => {
  beforeEach(() => {
    asr.local.mockReset().mockResolvedValue(asr.handle())
    asr.web.mockReset().mockResolvedValue(asr.handle())
    asr.probe.mockReset().mockResolvedValue(undefined)
    asr.capture.mockReset()
  })

  test('uses Web Speech when local sherpa is not ready', async () => {
    const onFinal = vi.fn()
    await startMeetingSpeech({ onFinal, onError: vi.fn() })
    expect(asr.web).toHaveBeenCalledOnce()
    expect(asr.local).not.toHaveBeenCalled()
    expect(asr.web.mock.calls[0][0].duplex).toBe(true)
    expect(asr.web.mock.calls[0][0].holdUtterance).toBe(true)
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
    expect(asr.local.mock.calls[0][0].holdUtterance).toBe(true)
  })

  test('cleans fillers and domain terms on committed meeting lines', async () => {
    asr.probe.mockResolvedValue({ supported: true, ready: true })
    const onFinal = vi.fn()
    const handle = await startMeetingSpeech({ onFinal, onError: vi.fn() })
    asr.local.mock.calls[0][0].onFinal('呃第一步应该先写 b r d')
    handle.stop()
    expect(onFinal).toHaveBeenCalledOnce()
    expect(onFinal.mock.calls[0][0]).toMatch(/BRD/)
    expect(onFinal.mock.calls[0][0]).not.toMatch(/呃/)
  })

  test('mixes this-PC loopback into local ASR only', async () => {
    asr.probe.mockResolvedValue({ supported: true, ready: true })
    await startMeetingSpeech({ onFinal: vi.fn(), onError: vi.fn(), extraStreams: [extra] })
    expect(asr.local.mock.calls[0][0].extraStreams).toEqual([extra])
    asr.local.mockClear()
    asr.probe.mockResolvedValue(undefined)
    await startMeetingSpeech({ onFinal: vi.fn(), onError: vi.fn(), extraStreams: [extra] })
    expect(asr.web.mock.calls[0][0].extraStreams).toBeUndefined()
  })
})

describe('prepareMeetingCapture', () => {
  beforeEach(() => {
    asr.probe.mockReset().mockResolvedValue(undefined)
    asr.capture.mockReset()
  })

  test('stays microphone-only when the mix is off', async () => {
    await expect(prepareMeetingCapture(false)).resolves.toEqual({ extraStreams: [], audioSource: 'microphone', notice: '' })
    expect(asr.capture).not.toHaveBeenCalled()
  })

  test('does not open the picker without a local recognizer', async () => {
    const plan = await prepareMeetingCapture(true)
    expect(plan.audioSource).toBe('microphone')
    expect(plan.notice).toMatch(/本机识别/)
    expect(asr.capture).not.toHaveBeenCalled()
  })

  test('keeps the loopback stream when the picker returns system audio', async () => {
    asr.probe.mockResolvedValue({ supported: true, ready: true })
    asr.capture.mockResolvedValue(extra)
    const plan = await prepareMeetingCapture(true)
    expect(plan.audioSource).toBe('microphone_and_system')
    expect(plan.extraStreams).toEqual([extra])
    expect(plan.notice).toBe('')
  })

  test('falls back honestly when the picker gives video but no audio', async () => {
    asr.probe.mockResolvedValue({ supported: true, ready: true })
    const stop = vi.fn()
    asr.capture.mockResolvedValue({ getAudioTracks: () => [], getTracks: () => [{ stop }] })
    const plan = await prepareMeetingCapture(true)
    expect(plan.audioSource).toBe('microphone')
    expect(plan.extraStreams).toEqual([])
    expect(plan.notice).toMatch(/共享音频/)
    expect(stop).toHaveBeenCalled()
  })

  test('rethrows a canceled picker so the meeting is not created', async () => {
    asr.probe.mockResolvedValue({ supported: true, ready: true })
    asr.capture.mockRejectedValue(new DOMException('Permission denied', 'NotAllowedError'))
    await expect(prepareMeetingCapture(true)).rejects.toMatchObject({ name: 'NotAllowedError' })
  })
})

describe('audioSourceLabel', () => {
  test('labels mix vs microphone honestly', () => {
    expect(audioSourceLabel('microphone_and_system')).toMatch(/系统声音/)
    expect(audioSourceLabel('microphone_and_system')).toMatch(/未共享给其他电脑/)
    expect(audioSourceLabel('microphone')).toMatch(/仅本机麦克风，未混录系统扬声器/)
    expect(audioSourceLabel('microphone', true)).toMatch(/正在录制本机麦克风/)
    expect(audioSourceLabel('microphone', true)).not.toMatch(/系统扬声器/)
  })
})
