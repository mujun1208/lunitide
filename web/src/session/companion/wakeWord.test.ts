import { act, renderHook } from '@testing-library/react'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import type { CompanionSpeechHandle, CompanionSpeechOptions } from './speech'
import { HOME_WAKE_DEAF_MS, isMicPermissionError, matchWakeWord, useWakeWord } from './wakeWord'

const speech = vi.hoisted(() => ({
  start: vi.fn(),
  options: undefined as CompanionSpeechOptions | undefined,
  handle: (): CompanionSpeechHandle => ({
    stop: vi.fn(),
    setAssistantPlayback: vi.fn(),
    setCommitPaused: vi.fn(),
    pulseRecognition: vi.fn(),
    forceCommit: vi.fn(),
    resumeCapture: vi.fn(),
  }),
}))

vi.mock('./speech', () => ({
  startCompanionSpeech: (options: CompanionSpeechOptions) => {
    speech.options = options
    return speech.start(options)
  },
}))

vi.mock('./localSpeech', () => ({
  startLocalCompanionSpeech: (options: CompanionSpeechOptions) => {
    speech.options = options
    return speech.start(options)
  },
}))

vi.mock('./volc/volcSpeech', () => ({
  startVolcCompanionSpeech: (options: CompanionSpeechOptions) => {
    speech.options = options
    return speech.start(options)
  },
}))

vi.mock('./prepareCompanionEntry', () => ({
  prepareCompanionEntry: async () => ({
    settings: { voicePath: 'cloud', recognizer: 'cloud' },
    voicePath: 'cloud',
    lights: [],
    llmReady: true,
    listenReady: true,
    speakReady: true,
    hasVolc: false,
    allowListen: true,
    blockReason: '',
  }),
}))

vi.mock('../../bridge/client', () => ({
  getProviderBridge: () => ({ list: async () => ({ items: [] }) }),
}))

function installMic() {
  Object.defineProperty(navigator, 'mediaDevices', {
    value: { getUserMedia: vi.fn().mockResolvedValue({ getTracks: () => [{ stop: vi.fn() }] }) },
    configurable: true,
  })
}

beforeEach(() => {
  speech.options = undefined
  speech.start.mockReset()
  speech.start.mockResolvedValue(speech.handle())
  installMic()
})

afterEach(() => {
  vi.useRealTimers()
  delete (navigator as { mediaDevices?: unknown }).mediaDevices
})

it('matches the canonical wake phrase with punctuation and spacing', () => {
  for (const phrase of ['你好，月汐！', '你好 月汐', '你好月汐', '你好，月汐。', '  你好，月汐！  ']) {
    expect(matchWakeWord(phrase).hit).toBe(true)
  }
})

it('matches wake variants case-insensitively', () => {
  expect(matchWakeWord('Hello 月汐').hit).toBe(true)
  expect(matchWakeWord('嗨，月汐').hit).toBe(true)
  expect(matchWakeWord('哈喽月汐').hit).toBe(true)
})

it('extracts the trailing request as the companion prompt', () => {
  expect(matchWakeWord('你好，月汐！帮我看看今天的天气')).toEqual({ hit: true, prompt: '帮我看看今天的天气', kind: 'phrase' })
  expect(matchWakeWord('你好月汐，现在几点了？')).toEqual({ hit: true, prompt: '现在几点了', kind: 'phrase' })
  expect(matchWakeWord('你好月汐')).toEqual({ hit: true, prompt: '', kind: 'phrase' })
})

it('matches a wake phrase even when ASR prepends filler', () => {
  expect(matchWakeWord('那个你好月汐')).toEqual({ hit: true, prompt: '', kind: 'phrase' })
  expect(matchWakeWord('嗯你好月汐打开网页')).toEqual({ hit: true, prompt: '打开网页', kind: 'phrase' })
})

it('matches 月伴 entry phrases', () => {
  expect(matchWakeWord('进入月伴').hit).toBe(true)
  expect(matchWakeWord('打开月伴模式搜周杰伦')).toEqual({ hit: true, prompt: '搜周杰伦', kind: 'phrase' })
})

it('detects microphone permission denials', () => {
  expect(isMicPermissionError({ name: 'NotAllowedError', message: 'denied' })).toBe(true)
  expect(isMicPermissionError({ code: 'MICROPHONE_PERMISSION_DENIED', message: '麦克风权限被拒绝' })).toBe(true)
  expect(isMicPermissionError({ code: 'network', message: 'network' })).toBe(false)
})

it('does not match ordinary speech or look-alike phrases', () => {
  for (const phrase of ['今天天气不错', '你好，世界', '再见月汐', '你好月']) {
    expect(matchWakeWord(phrase)).toEqual({ hit: false, prompt: '', kind: 'none' })
  }
  expect(matchWakeWord('月汐你好')).toEqual({ hit: true, prompt: '你好', kind: 'name' })
})

it('matches common ASR homophone transcribes of the wake name', () => {
  for (const phrase of ['你好月希', '你好，月西', '嗨月溪', '您好月熙', '你好月惜', '你好悦汐', 'hello月希', '你好我是月汐', '您好，我是月希', '你好月昔', '嗨月兮']) {
    expect(matchWakeWord(phrase).hit).toBe(true)
  }
  expect(matchWakeWord('月汐打开网页')).toEqual({ hit: true, prompt: '打开网页', kind: 'name' })
  expect(matchWakeWord('月希今天天气')).toEqual({ hit: false, prompt: '', kind: 'none' })
})

it('reports unsupported when the runtime has no microphone', () => {
  delete (navigator as { mediaDevices?: unknown }).mediaDevices
  const view = renderHook(() => useWakeWord({ enabled: true, onWake: vi.fn() }))
  expect(view.result.current).toBe('unsupported')
})

it('does not wake when the energy gate says the hit is speaker playback', async () => {
  const onWake = vi.fn()
  renderHook(() =>
    useWakeWord({
      enabled: true,
      onWake,
      vad: true,
      readVad: () => ({ speechLikely: false, playbackLikely: true, tooQuiet: false }),
    }),
  )
  await act(async () => {})
  act(() => {
    speech.options?.onInterim?.('你好月汐今天天气怎么样')
  })
  expect(onWake).not.toHaveBeenCalled()
})

it('still wakes on a greeted phrase when the energy gate hears live speech', async () => {
  const onWake = vi.fn()
  renderHook(() =>
    useWakeWord({
      enabled: true,
      onWake,
      vad: true,
      readVad: () => ({ speechLikely: true, playbackLikely: false, tooQuiet: false }),
    }),
  )
  await act(async () => {})
  act(() => {
    speech.options?.onInterim?.('你好月汐打开桌面')
  })
  expect(onWake).toHaveBeenCalledExactlyOnceWith('打开桌面')
})

it('fires onWake once when the phrase lands in an interim transcript', async () => {
  const onWake = vi.fn()
  const view = renderHook(() => useWakeWord({ enabled: true, onWake }))
  await act(async () => {})
  expect(view.result.current).toBe('listening')
  act(() => {
    speech.options?.onInterim?.('你好月汐今天天气怎么样')
  })
  expect(onWake).toHaveBeenCalledExactlyOnceWith('今天天气怎么样')
})

it('surfaces a deaf error when energy arrives without glyphs', async () => {
  vi.useFakeTimers()
  const onDeaf = vi.fn()
  const view = renderHook(() => useWakeWord({ enabled: true, onWake: vi.fn(), onDeaf }))
  await act(async () => {})
  act(() => {
    speech.options?.onVoiceEnergy?.()
  })
  expect(view.result.current).toBe('listening')
  await act(async () => {
    vi.advanceTimersByTime(HOME_WAKE_DEAF_MS)
  })
  expect(onDeaf).toHaveBeenCalled()
  expect(view.result.current).toBe('error')
})

it('keeps listening across a silent recognizer restart', async () => {
  const handle = speech.handle()
  speech.start.mockResolvedValue(handle)
  const view = renderHook(() => useWakeWord({ enabled: true, onWake: vi.fn() }))
  await act(async () => {})
  act(() => {
    speech.options?.onEndWithoutFinal?.()
  })
  expect(view.result.current).toBe('listening')
  expect(handle.resumeCapture).toHaveBeenCalled()
})

it('surfaces a denied state when the microphone permission is refused', async () => {
  const view = renderHook(() => useWakeWord({ enabled: true, onWake: vi.fn() }))
  await act(async () => {})
  act(() => {
    speech.options?.onError?.({ name: 'NotAllowedError', code: 'not-allowed', message: '麦克风权限被拒绝', retryable: false, source: 'renderer' } as never)
  })
  expect(view.result.current).toBe('denied')
})

it('surfaces an error from the shared ASR path instead of spinning as fake listening', async () => {
  const view = renderHook(() => useWakeWord({ enabled: true, onWake: vi.fn() }))
  await act(async () => {})
  expect(view.result.current).toBe('listening')
  act(() => {
    speech.options?.onError?.({ code: 'network', message: 'network', retryable: true, source: 'renderer' } as never)
  })
  expect(view.result.current).toBe('error')
})
