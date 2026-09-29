import { beforeEach, describe, expect, test, vi } from 'vitest'
import type { CompanionSpeechHandle } from '../session/companion/speech'

const asr = vi.hoisted(() => ({
  local: vi.fn(),
  web: vi.fn(),
  volc: vi.fn(),
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
vi.mock('../session/companion/volc/volcSpeech', () => ({
  startVolcCompanionSpeech: (...args: unknown[]) => asr.volc(...args),
}))
vi.mock('./meetingCapture', async importOriginal => {
  const actual = await importOriginal<typeof import('./meetingCapture')>()
  return { ...actual, captureThisPcSystemAudio: asr.capture }
})

import { MEETING_TURN_END_SILENCE_MS, TURN_END_SILENCE_MS, turnEndWindows } from '../session/companion/speech'
import { captureStateNotice, audioSourceLabel, decodeMeetingPcmBase64, MEETING_CATCHUP_HINT, MEETING_VOLC_END_WINDOW_MS, meetingAsrRuntimeLine, meetingSystemAudioMissing, mixMeetingPcmS16le, noteLoopbackEnergy, planHasLiveSystemAudio, prepareMeetingCapture, recoverMeetingSystemAudio, shouldFallbackLiveCaption, startMeetingSpeech } from './meetingAsr'
import { MEETING_MERGE_GAP_MS } from './meetingText'
import { NO_SYSTEM_AUDIO_NOTICE } from './meetingCapture'

const extra = { getAudioTracks: () => [{ kind: 'audio', readyState: 'live' }], getTracks: () => [] } as unknown as MediaStream

describe('startMeetingSpeech', () => {
  beforeEach(() => {
    asr.local.mockReset().mockResolvedValue(asr.handle())
    asr.web.mockReset().mockResolvedValue(asr.handle())
    asr.volc.mockReset().mockResolvedValue(asr.handle())
    asr.probe.mockReset().mockResolvedValue(undefined)
    asr.capture.mockReset()
  })

  test('explicit cloud listen skips sherpa even when it is ready', async () => {
    asr.probe.mockResolvedValue({ supported: true, ready: true })
    await startMeetingSpeech({ onFinal: vi.fn(), onError: vi.fn(), listen: 'cloud' })
    expect(asr.web).toHaveBeenCalledOnce()
    expect(asr.local).not.toHaveBeenCalled()
  })

  test('explicit volc listen requires a voice provider', async () => {
    await expect(startMeetingSpeech({ onFinal: vi.fn(), onError: vi.fn(), listen: 'volc' })).rejects.toThrow(/火山/)
    await startMeetingSpeech({ onFinal: vi.fn(), onError: vi.fn(), listen: 'volc', volcProviderId: 'p1' })
    expect(asr.volc).toHaveBeenCalledOnce()
    expect(asr.web).not.toHaveBeenCalled()
    expect(asr.volc.mock.calls[0][0].endWindowMs).toBe(MEETING_VOLC_END_WINDOW_MS)
    expect(MEETING_VOLC_END_WINDOW_MS).toBe(400)
    expect(MEETING_MERGE_GAP_MS).toBe(400)
  })

  test('volc live captions isolate the current clause from a full dump', async () => {
    const onInterim = vi.fn()
    await startMeetingSpeech({ onFinal: vi.fn(), onInterim, onError: vi.fn(), listen: 'volc', volcProviderId: 'p1' })
    const opts = asr.volc.mock.calls[0][0]
    opts.onFinal('今天合肥天气怎么样')
    opts.onInterim('今天合肥天气怎么样。算了放首歌')
    expect(onInterim).toHaveBeenLastCalledWith('算了放首歌')
  })

  test('omitted listen uses system Web Speech even when sherpa is ready', async () => {
    asr.probe.mockResolvedValue({ supported: true, ready: true })
    const onFinal = vi.fn()
    await startMeetingSpeech({ onFinal, onError: vi.fn() })
    expect(asr.web).toHaveBeenCalledOnce()
    expect(asr.local).not.toHaveBeenCalled()
    expect(asr.probe).not.toHaveBeenCalled()
    expect(asr.web.mock.calls[0][0].duplex).toBe(true)
    expect(asr.web.mock.calls[0][0].holdUtterance).toBe(true)
    expect(asr.web.mock.calls[0][0].spokenText()).toBe('')
  })

  test('explicit local listen refuses Web Speech when sherpa is not ready', async () => {
    asr.probe.mockResolvedValue({ supported: true, ready: false })
    await expect(startMeetingSpeech({ onFinal: vi.fn(), onError: vi.fn(), listen: 'local' })).rejects.toThrow(/sherpa 未就绪/)
    expect(asr.web).not.toHaveBeenCalled()
    expect(asr.local).not.toHaveBeenCalled()
  })

  test('never treats companion TTS as meeting speech', async () => {
    await startMeetingSpeech({ onFinal: vi.fn(), onError: vi.fn(), spokenText: () => '助手在说话' })
    expect(asr.web.mock.calls[0][0].spokenText()).toBe('')
  })

  test('explicit local listen uses sherpa and never falls back to Web Speech', async () => {
    asr.probe.mockResolvedValue({ supported: true, ready: true })
    await startMeetingSpeech({ onFinal: vi.fn(), onError: vi.fn(), listen: 'local' })
    expect(asr.local).toHaveBeenCalledOnce()
    expect(asr.web).not.toHaveBeenCalled()
    expect(asr.local.mock.calls[0][0].holdUtterance).toBe(true)
    expect(MEETING_CATCHUP_HINT).toMatch(/补转写只用本机/)
    expect(MEETING_CATCHUP_HINT).toMatch(/系统声会在停止后补/)
  })

  test('cleans fillers and domain terms on committed meeting lines', async () => {
    asr.probe.mockResolvedValue({ supported: true, ready: true })
    const onFinal = vi.fn()
    const handle = await startMeetingSpeech({ onFinal, onError: vi.fn(), listen: 'local' })
    asr.local.mock.calls[0][0].onFinal('呃第一步应该先写 b r d')
    handle.stop()
    expect(onFinal).toHaveBeenCalledOnce()
    expect(onFinal.mock.calls[0][0]).toMatch(/BRD/)
    expect(onFinal.mock.calls[0][0]).not.toMatch(/呃/)
  })

  test('mixes this-PC loopback into both PCM recognizers, never cloud Web Speech', async () => {
    asr.probe.mockResolvedValue({ supported: true, ready: true })
    await startMeetingSpeech({ onFinal: vi.fn(), onError: vi.fn(), listen: 'local', extraStreams: [extra] })
    expect(asr.local.mock.calls[0][0].extraStreams).toEqual([extra])
    // 火山 is PCM-capable too: it must also receive the this-PC loopback so the
    // live caption hears the other party, not only the local user.
    asr.volc.mockClear()
    await startMeetingSpeech({ onFinal: vi.fn(), onError: vi.fn(), listen: 'volc', volcProviderId: 'p1', extraStreams: [extra] })
    expect(asr.volc.mock.calls[0][0].extraStreams).toEqual([extra])
    // 云端 Web Speech cannot inject loopback — it stays mic-only.
    asr.web.mockClear()
    asr.probe.mockResolvedValue(undefined)
    await startMeetingSpeech({ onFinal: vi.fn(), onError: vi.fn(), extraStreams: [extra] })
    expect(asr.web.mock.calls[0][0].extraStreams).toBeUndefined()
  })

  test('keeps loopback on the meeting recorder when volc ASR is fed external PCM', async () => {
    await startMeetingSpeech({ onFinal: vi.fn(), onError: vi.fn(), listen: 'volc', volcProviderId: 'p1', extraStreams: [extra], externalPcm: true })
    expect(asr.volc.mock.calls[0][0].externalPcm).toBe(true)
    expect(asr.volc.mock.calls[0][0].extraStreams).toBeUndefined()
  })

  test('keeps loopback on the meeting recorder when local ASR is fed external PCM', async () => {
    asr.probe.mockResolvedValue({ supported: true, ready: true })
    await startMeetingSpeech({ onFinal: vi.fn(), onError: vi.fn(), listen: 'local', extraStreams: [extra], externalPcm: true })
    expect(asr.local.mock.calls[0][0].externalPcm).toBe(true)
    expect(asr.local.mock.calls[0][0].extraStreams).toBeUndefined()
  })

  test('meeting hold windows are longer than the companion 1.2s commit', async () => {
    await startMeetingSpeech({ onFinal: vi.fn(), onError: vi.fn() })
    expect(asr.web.mock.calls[0][0].holdUtterance).toBe(true)
    expect(TURN_END_SILENCE_MS).toBe(1200)
    expect(turnEndWindows(true).silenceMs).toBe(MEETING_TURN_END_SILENCE_MS)
    expect(turnEndWindows(true).silenceMs).toBeGreaterThan(TURN_END_SILENCE_MS)
    expect(turnEndWindows(false).silenceMs).toBe(TURN_END_SILENCE_MS)
  })
})

describe('prepareMeetingCapture', () => {
  beforeEach(() => {
    asr.capture.mockReset()
  })

  test('always tries this-PC system audio capture', async () => {
    asr.capture.mockResolvedValue(extra)
    const plan = await prepareMeetingCapture()
    expect(asr.capture).toHaveBeenCalled()
    expect(plan.audioSource).toBe('microphone_and_system')
    expect(plan.extraStreams).toEqual([extra])
  })

  test('keeps the loopback stream when capture succeeds', async () => {
    asr.capture.mockResolvedValue(extra)
    const plan = await prepareMeetingCapture({ interactive: true })
    expect(plan.audioSource).toBe('microphone_and_system')
    expect(plan.extraStreams).toEqual([extra])
    expect(plan.notice).toBe('')
  })

  test('falls back to mic-only when the picker gives video but no audio', async () => {
    const stop = vi.fn()
    asr.capture.mockResolvedValue({ getAudioTracks: () => [], getTracks: () => [{ stop }] })
    const plan = await prepareMeetingCapture()
    expect(plan.audioSource).toBe('microphone')
    expect(plan.extraStreams).toEqual([])
    expect(plan.notice).toBe(NO_SYSTEM_AUDIO_NOTICE)
    expect(stop).toHaveBeenCalled()
  })

  test('picker cancel still returns a mic-only plan instead of throwing', async () => {
    asr.capture.mockRejectedValue(new DOMException('Permission denied', 'NotAllowedError'))
    const plan = await prepareMeetingCapture({ interactive: true })
    expect(plan.audioSource).toBe('microphone')
    expect(plan.extraStreams).toEqual([])
    expect(plan.notice).toMatch(/继续录制麦克风/)
  })

  test('captureStateNotice stays honest about warning vs live loopback', () => {
    expect(captureStateNotice({ extraStreams: [extra], audioSource: 'microphone_and_system', notice: '' })).toBe('')
    expect(planHasLiveSystemAudio({ extraStreams: [extra], audioSource: 'microphone_and_system', notice: '' })).toBe(true)
    const dead = { getAudioTracks: () => [{ readyState: 'ended' }], getTracks: () => [] } as unknown as MediaStream
    expect(captureStateNotice({ extraStreams: [dead], audioSource: 'microphone_and_system', notice: '' })).toMatch(/轨道已中断/)
    expect(captureStateNotice({ extraStreams: [], audioSource: 'microphone', notice: NO_SYSTEM_AUDIO_NOTICE })).toBe(NO_SYSTEM_AUDIO_NOTICE)
    expect(planHasLiveSystemAudio({ extraStreams: [], audioSource: 'microphone_and_system', notice: '', engineOwned: true })).toBe(false)
    expect(captureStateNotice({ extraStreams: [], audioSource: 'microphone_and_system', notice: '', engineOwned: true })).toBe('')
  })

  test('three silent loopback frames flip the live label to mic-only', () => {
    let state = { hits: 0, zeros: 0 }
    let heard: boolean | undefined
    for (let i = 0; i < 3; i++) {
      const next = noteLoopbackEnergy(state, 0)
      state = { hits: next.hits, zeros: next.zeros }
      heard = next.heard
    }
    expect(heard).toBe(false)
    const live = noteLoopbackEnergy({ hits: 2, zeros: 0 }, 0.2)
    expect(live.heard).toBe(true)
  })

  test('mixes 16-bit PCM without overflowing', () => {
    const mixed = mixMeetingPcmS16le(new Int16Array([20000, -20000]), new Int16Array([20000, -20000]))
    expect([...mixed]).toEqual([32767, -32768])
    const decoded = decodeMeetingPcmBase64(btoa(String.fromCharCode(0x00, 0x10)))
    expect(decoded && decoded[0]).toBe(0x1000)
  })

  test('recoverMeetingSystemAudio upgrades a mic-only plan when loopback appears', async () => {
    asr.capture.mockResolvedValue(extra)
    const next = await recoverMeetingSystemAudio({ extraStreams: [], audioSource: 'microphone', notice: NO_SYSTEM_AUDIO_NOTICE }, { interactive: false })
    expect(next.audioSource).toBe('microphone_and_system')
    expect(next.extraStreams).toEqual([extra])
  })

  test('engine-owned loopback survives a failed browser recover', async () => {
    asr.capture.mockRejectedValue(new Error(NO_SYSTEM_AUDIO_NOTICE))
    const current = { extraStreams: [], audioSource: 'microphone_and_system' as const, notice: '', engineOwned: true }
    const next = await recoverMeetingSystemAudio(current, { interactive: false })
    expect(next).toBe(current)
    expect(next.engineOwned).toBe(true)
  })
})

describe('meetingSystemAudioMissing', () => {
  const enginePlan = { extraStreams: [], audioSource: 'microphone_and_system' as const, notice: '', engineOwned: true }

  test('engine loopback that is live but momentarily silent is NOT missing', () => {
    // The exact false-alarm case: a song is being transcribed through the engine
    // loopback (active) even though the energy meter saw silent frames.
    expect(meetingSystemAudioMissing({ recording: true, audioSource: 'microphone_and_system', plan: enginePlan, engineLoopbackActive: true, systemHeard: false })).toBe(false)
    // Still unknown at startup: do not warn before the first poll returns.
    expect(meetingSystemAudioMissing({ recording: true, audioSource: 'microphone_and_system', plan: enginePlan, engineLoopbackActive: undefined, systemHeard: undefined })).toBe(false)
  })

  test('engine loopback that never opened (active === false) IS missing', () => {
    expect(meetingSystemAudioMissing({ recording: true, audioSource: 'microphone_and_system', plan: enginePlan, engineLoopbackActive: false, systemHeard: undefined })).toBe(true)
  })

  test('browser fallback with no live system track is missing; a live track is not', () => {
    expect(meetingSystemAudioMissing({ recording: true, audioSource: 'microphone_and_system', plan: { extraStreams: [], audioSource: 'microphone_and_system', notice: '' }, engineLoopbackActive: undefined, systemHeard: undefined })).toBe(true)
    expect(meetingSystemAudioMissing({ recording: true, audioSource: 'microphone_and_system', plan: { extraStreams: [extra], audioSource: 'microphone_and_system', notice: '' }, engineLoopbackActive: undefined, systemHeard: true })).toBe(false)
  })

  test('never warns when not recording or when mic-only was chosen', () => {
    expect(meetingSystemAudioMissing({ recording: false, audioSource: 'microphone_and_system', plan: enginePlan, engineLoopbackActive: false, systemHeard: false })).toBe(false)
    expect(meetingSystemAudioMissing({ recording: true, audioSource: 'microphone', plan: undefined, engineLoopbackActive: false, systemHeard: false })).toBe(false)
  })
})

describe('shouldFallbackLiveCaption', () => {
  test('cloud/volc silent with sherpa ready → switch live path to local', () => {
    expect(shouldFallbackLiveCaption({ listen: 'cloud', sawRealCaption: false, alreadyFellBack: false, localReady: true })).toBe('local')
    expect(shouldFallbackLiveCaption({ listen: 'volc', sawRealCaption: false, alreadyFellBack: false, localReady: true })).toBe('local')
  })

  test('cloud/volc silent with sherpa NOT ready → unavailable (补转写 still lands)', () => {
    expect(shouldFallbackLiveCaption({ listen: 'cloud', sawRealCaption: false, alreadyFellBack: false, localReady: false })).toBe('unavailable')
  })

  test('a real caption already arrived → never fall back', () => {
    expect(shouldFallbackLiveCaption({ listen: 'cloud', sawRealCaption: true, alreadyFellBack: false, localReady: true })).toBe('none')
  })

  test('already fell back, or engine is already local → none', () => {
    expect(shouldFallbackLiveCaption({ listen: 'cloud', sawRealCaption: false, alreadyFellBack: true, localReady: true })).toBe('none')
    expect(shouldFallbackLiveCaption({ listen: 'local', sawRealCaption: false, alreadyFellBack: false, localReady: true })).toBe('none')
  })
})

describe('meetingAsrRuntimeLine', () => {
  test('volc shows engine, truncated provider, single-source PCM and 直采', () => {
    const line = meetingAsrRuntimeLine({ backend: 'volc', providerId: '01ARZ3NDEKTSV4RRFFQ69G5FAV', externalPcm: true, fellBack: false })
    expect(line).toContain('引擎：火山 seed-asr')
    expect(line).toContain('供应商：01ARZ3ND…')
    expect(line).toContain('音源：外部录音 PCM（单路，无双麦）')
    expect(line).toContain('字幕：直采')
  })

  test('cloud shows browser listening and no provider', () => {
    const line = meetingAsrRuntimeLine({ backend: 'cloud', externalPcm: false, fellBack: false })
    expect(line).toContain('引擎：系统听写')
    expect(line).not.toContain('供应商')
    expect(line).toContain('音源：浏览器听写')
  })

  test('a deaf-engine fallback to local is called out as 已回退本机', () => {
    const line = meetingAsrRuntimeLine({ backend: 'local', externalPcm: true, fellBack: true })
    expect(line).toContain('引擎：本机 sherpa')
    expect(line).toContain('字幕：已回退本机')
  })
})

describe('audioSourceLabel', () => {
  test('labels mix vs microphone honestly', () => {
    expect(audioSourceLabel('microphone_and_system')).toMatch(/系统声音/)
    expect(audioSourceLabel('microphone_and_system')).toMatch(/未共享给其他电脑/)
    expect(audioSourceLabel('microphone')).toMatch(/麦克风/)
    expect(audioSourceLabel('microphone', true)).toMatch(/正在录制麦克风/)
  })
})
