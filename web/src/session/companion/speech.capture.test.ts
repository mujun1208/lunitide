// speech.capture.test.ts pins the companion listen graph: getUserMedia
// stays open (real analyser levels), SpeechRecognition starts immediately
// in continuous+interim mode, and a user-gesture resumeCapture() wakes a
// suspended AudioContext so volume bars actually move.
import { afterEach, beforeEach, describe, expect, test, vi } from 'vitest'
import { MOON_RING_BINS } from './MoonSphere'
import { startCompanionSpeech } from './speech'

type Rec = {
  lang: string
  continuous: boolean
  interimResults: boolean
  start: ReturnType<typeof vi.fn>
  stop: ReturnType<typeof vi.fn>
  onresult: ((event: unknown) => void) | null
  onerror: ((event?: { error?: string }) => void) | null
  onend: (() => void) | null
  onspeechstart: (() => void) | null
  onspeechend: (() => void) | null
  onsoundstart: (() => void) | null
  onaudiostart: (() => void) | null
}

let recognition: Rec
let micTrack: { enabled: boolean }
let trackStop: ReturnType<typeof vi.fn>
let resume: ReturnType<typeof vi.fn>
let contextState: AudioContextState

function heard(transcript: string) {
  return {
    resultIndex: 0,
    results: Object.assign([{ 0: { transcript, confidence: 0.9 }, length: 1, isFinal: true }], { length: 1 }),
  }
}

beforeEach(() => {
  recognition = {
    lang: '',
    continuous: false,
    interimResults: false,
    start: vi.fn(),
    stop: vi.fn(),
    onresult: null,
    onerror: null,
    onend: null,
    onspeechstart: null,
    onspeechend: null,
    onsoundstart: null,
    onaudiostart: null,
  }
  class FakeRecognition {
    constructor() {
      return recognition
    }
  }
  Object.defineProperty(window, 'SpeechRecognition', { configurable: true, value: FakeRecognition })
  trackStop = vi.fn()
  const track = {
    enabled: true,
    stop: trackStop,
    clone() {
      return { enabled: true, stop: vi.fn(), clone: track.clone, getSettings: () => ({ deviceId: 'mic-1' }) }
    },
    getSettings: () => ({ deviceId: 'mic-1' }),
  }
  micTrack = track
  Object.defineProperty(navigator, 'mediaDevices', {
    configurable: true,
    value: {
      getUserMedia: vi.fn().mockResolvedValue({
        getAudioTracks: () => [track],
        getTracks: () => [track],
      }),
    },
  })
  contextState = 'suspended'
  resume = vi.fn().mockImplementation(async () => {
    contextState = 'running'
  })
  class FakeAudioContext {
    get state() {
      return contextState
    }
    resume = resume
    close = vi.fn().mockResolvedValue(undefined)
    createAnalyser() {
      return {
        fftSize: 256,
        smoothingTimeConstant: 0,
        frequencyBinCount: 128,
        getByteTimeDomainData: (data: Uint8Array) => {
          for (let i = 0; i < data.length; i++) data[i] = i % 2 ? 188 : 68
        },
        getByteFrequencyData: (data: Uint8Array) => {
          data.fill(200)
        },
      }
    }
    createMediaStreamSource() {
      return { connect: vi.fn(), disconnect: vi.fn() }
    }
  }
  Object.defineProperty(window, 'AudioContext', { configurable: true, value: FakeAudioContext })
  let frames = 0
  vi.stubGlobal('requestAnimationFrame', (cb: FrameRequestCallback) => {
    frames += 1
    const id = frames
    if (id <= 5) queueMicrotask(() => cb(id))
    return id
  })
  vi.stubGlobal('cancelAnimationFrame', vi.fn())
})

afterEach(() => {
  vi.unstubAllGlobals()
  vi.useRealTimers()
  delete (window as { SpeechRecognition?: unknown }).SpeechRecognition
  delete (window as { AudioContext?: unknown }).AudioContext
})

describe('startCompanionSpeech capture graph', () => {
  test('keeps the mic open, starts continuous SR, resumes audio, and paints real levels', async () => {
    const onLevels = vi.fn()
    const handle = await startCompanionSpeech({
      duplex: true,
      onFinal: vi.fn(),
      onError: vi.fn(),
      onLevels,
    })
    expect(recognition.start).toHaveBeenCalled()
    expect(trackStop).not.toHaveBeenCalled()
    expect(recognition.continuous).toBe(true)
    expect(recognition.interimResults).toBe(true)
    expect(resume).toHaveBeenCalled()
    await vi.waitFor(() => expect(onLevels).toHaveBeenCalled())
    const levels = onLevels.mock.calls.at(-1)?.[0] as number[]
    expect(levels).toHaveLength(MOON_RING_BINS)
    expect(Math.max(...levels)).toBeGreaterThan(0.3)
    handle.stop()
  })

  test('resumeCapture restarts a dead recognizer after a user gesture', async () => {
    const handle = await startCompanionSpeech({
      duplex: true,
      onFinal: vi.fn(),
      onError: vi.fn(),
    })
    recognition.start.mockClear()
    recognition.onend?.()
    handle.resumeCapture()
    expect(recognition.start).toHaveBeenCalled()
    handle.stop()
  })

  test('stops the recognizer while she speaks, and starts a fresh one after', async () => {
    // It used to be left running to stay warm, because muting the microphone
    // was believed to keep it from hearing her. Web Speech captures audio
    // itself and never saw those tracks, so it transcribed the whole reply
    // off the speaker and handed it over the moment the guard lifted — the
    // user's next question arrived in her words.
    const handle = await startCompanionSpeech({
      duplex: true,
      onFinal: vi.fn(),
      onError: vi.fn(),
    })
    recognition.start.mockClear()
    recognition.stop.mockClear()

    handle.setAssistantPlayback(true)
    expect(recognition.stop).toHaveBeenCalled()

    handle.setAssistantPlayback(false)
    expect(recognition.start).toHaveBeenCalled()
    handle.stop()
  })

  test('does not report what it heard of her as the user talking', async () => {
    const onInterim = vi.fn()
    const onFinal = vi.fn()
    const spoken = '我能帮你可多啦，办公上也能搭把手，像做表格文档、生成PPT这些。'
    const handle = await startCompanionSpeech({
      duplex: true,
      onFinal,
      onInterim,
      onError: vi.fn(),
      spokenText: () => spoken,
    })

    handle.setAssistantPlayback(true)
    recognition.onresult?.(heard('办公上也能搭把手像做表格文档生成PPT这些'))
    handle.setAssistantPlayback(false)
    onInterim.mockClear()
    // Arriving late, after the guard has lifted, which is when the engine
    // used to hand over everything it had buffered during the reply.
    recognition.onresult?.(heard('办公上也能搭把手像做表格文档生成PPT这些'))

    expect(onFinal).not.toHaveBeenCalled()
    for (const [caption] of onInterim.mock.calls) expect(caption).toBe('')
    handle.stop()
  })

  test('mutes the mic for the whole reply, so nothing heard can end her turn', async () => {
    // The microphone used to stay open here so the user could cut in by
    // talking. Deciding from a transcript whether a couple of characters were
    // the user or her own voice returning through the speaker is a guess, and
    // losing it truncated her answer mid-word — a television, someone else in
    // the room, or a late recognition of her own sentence all did it.
    // Interrupting is the 打断 button's job.
    const onFinal = vi.fn()
    const spoken = '今天合肥多云，气温二十六度，出门记得带把伞。'
    const handle = await startCompanionSpeech({
      duplex: true,
      onFinal,
      onError: vi.fn(),
      spokenText: () => spoken,
    })

    handle.setAssistantPlayback(true)
    expect(micTrack.enabled).toBe(false)
    recognition.onresult?.(heard('出门记得带把伞'))
    recognition.onresult?.(heard('等一下，换个话题'))
    expect(onFinal).not.toHaveBeenCalled()

    handle.setAssistantPlayback(false)
    expect(micTrack.enabled).toBe(true)
    handle.stop()
  })

  test('after TTS a dead recognizer restarts immediately', async () => {
    const handle = await startCompanionSpeech({
      duplex: true,
      onFinal: vi.fn(),
      onError: vi.fn(),
    })
    recognition.start.mockClear()
    handle.setAssistantPlayback(true)
    recognition.onend?.()
    handle.setAssistantPlayback(false)
    expect(recognition.start).toHaveBeenCalled()
    handle.stop()
  })

  test('starts SpeechRecognition without waiting for AudioContext resume', async () => {
    resume.mockImplementation(() => new Promise(() => {}))
    const handle = await startCompanionSpeech({
      duplex: true,
      onFinal: vi.fn(),
      onError: vi.fn(),
    })
    expect(recognition.start).toHaveBeenCalled()
    handle.stop()
  })

  test('duplex commit keeps the recognizer running for the next turn', async () => {
    const onFinal = vi.fn()
    const handle = await startCompanionSpeech({
      duplex: true,
      onFinal,
      onError: vi.fn(),
    })
    recognition.onresult?.({
      resultIndex: 0,
      results: Object.assign([{ 0: { transcript: '今天合肥天气怎么样', confidence: 0.92 }, length: 1, isFinal: true }], { length: 1 }),
    })
    recognition.stop.mockClear()
    await new Promise(resolve => setTimeout(resolve, 150))
    expect(onFinal).toHaveBeenCalledWith('今天合肥天气怎么样')
    expect(recognition.stop).not.toHaveBeenCalled()
    handle.stop()
  })

  test('does not commit a mid-command fragment when Windows ends the session', async () => {
    const onFinal = vi.fn()
    const handle = await startCompanionSpeech({
      duplex: true,
      onFinal,
      onError: vi.fn(),
    })
    recognition.onresult?.({
      resultIndex: 0,
      results: Object.assign([{ 0: { transcript: '你可以帮我', confidence: 0.9 }, length: 1, isFinal: true }], { length: 1 }),
    })
    recognition.start.mockClear()
    recognition.onend?.()
    await new Promise(resolve => setTimeout(resolve, 80))
    expect(onFinal).not.toHaveBeenCalled()
    expect(recognition.start).toHaveBeenCalled()
    handle.stop()
  })

  test('does not commit「合肥的」after a short Windows pause while the user is still talking', async () => {
    const onFinal = vi.fn()
    const handle = await startCompanionSpeech({
      duplex: true,
      onFinal,
      onError: vi.fn(),
    })
    recognition.onresult?.({
      resultIndex: 0,
      results: Object.assign([{ 0: { transcript: '合肥的', confidence: 0.88 }, length: 1, isFinal: true }], { length: 1 }),
    })
    recognition.onspeechend?.()
    await new Promise(resolve => setTimeout(resolve, 700))
    expect(onFinal).not.toHaveBeenCalled()
    handle.stop()
  })

  test('starts SpeechRecognition before getUserMedia resolves', async () => {
    let release!: (stream: MediaStream) => void
    const pendingStream = new Promise<MediaStream>(resolve => {
      release = resolve
    })
    vi.mocked(navigator.mediaDevices.getUserMedia).mockReturnValueOnce(pendingStream)
    const pending = startCompanionSpeech({
      duplex: true,
      onFinal: vi.fn(),
      onError: vi.fn(),
    })
    await vi.waitFor(() => expect(recognition.start).toHaveBeenCalled())
    release({
      getAudioTracks: () => [{
        enabled: true,
        stop: trackStop,
        clone: () => ({ enabled: true, stop: vi.fn(), clone: () => ({}), getSettings: () => ({ deviceId: 'mic-1' }) }),
        getSettings: () => ({ deviceId: 'mic-1' }),
      }],
      getTracks: () => [],
    } as unknown as MediaStream)
    const handle = await pending
    handle.stop()
  })

  test('does not commit「你可以」after speechend while more speech may follow', async () => {
    const onFinal = vi.fn()
    const handle = await startCompanionSpeech({
      duplex: true,
      onFinal,
      onError: vi.fn(),
    })
    recognition.onresult?.({
      resultIndex: 0,
      results: Object.assign([{ 0: { transcript: '你可以', confidence: 0.9 }, length: 1, isFinal: true }], { length: 1 }),
    })
    recognition.onspeechend?.()
    await new Promise(resolve => setTimeout(resolve, 700))
    expect(onFinal).not.toHaveBeenCalled()
    handle.stop()
  })
})
