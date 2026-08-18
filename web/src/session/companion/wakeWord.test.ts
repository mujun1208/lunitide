import { act, renderHook } from '@testing-library/react'
import { afterEach, expect, it, vi } from 'vitest'
import { matchWakeWord, useWakeWord } from './wakeWord'

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
  expect(matchWakeWord('你好，月汐！帮我看看今天的天气')).toEqual({ hit: true, prompt: '帮我看看今天的天气' })
  expect(matchWakeWord('你好月汐，现在几点了？')).toEqual({ hit: true, prompt: '现在几点了' })
  expect(matchWakeWord('你好月汐')).toEqual({ hit: true, prompt: '' })
})

it('does not match ordinary speech or look-alike phrases', () => {
  for (const phrase of ['今天天气不错', '你好，世界', '再见月汐', '你好月']) {
    expect(matchWakeWord(phrase)).toEqual({ hit: false, prompt: '' })
  }
  // "月汐你好" now matches with name-only wake: "月汐" is in WAKE_NAMES
  expect(matchWakeWord('月汐你好')).toEqual({ hit: true, prompt: '你好' })
})

it('matches common ASR homophone transcribes of the wake name', () => {
  // Windows online ASR frequently returns 月希/月西/月溪/月熙/月惜/悦汐
  // for the spoken 「月汐」— the cross-product phrase list must catch them.
  for (const phrase of ['你好月希', '你好，月西', '嗨月溪', '您好月熙', '你好月惜', '你好悦汐', 'hello月希']) {
    expect(matchWakeWord(phrase).hit).toBe(true)
  }
  // Name-only wake is now allowed (no greeting prefix required) for
  // better UX — users can say just "月汐" to wake the companion.
  // The prompt extraction still works: "月希今天天气" → wake hit with
  // prompt "今天天气".
  expect(matchWakeWord('月希今天天气')).toEqual({ hit: true, prompt: '今天天气' })
})

type FakeEvent = { results: ArrayLike<{ 0: { transcript: string }; isFinal: boolean }> }

class FakeRecognition {
  static instances: FakeRecognition[] = []
  lang = ''
  continuous = false
  interimResults = false
  onresult: ((event: FakeEvent) => void) | null = null
  onerror: ((event?: { error?: string }) => void) | null = null
  onend: (() => void) | null = null
  start = vi.fn()
  stop = vi.fn()
  constructor() {
    FakeRecognition.instances.push(this)
  }
}

function installFakeRecognition() {
  FakeRecognition.instances = []
  Object.defineProperty(window, 'SpeechRecognition', { value: FakeRecognition, configurable: true })
  Object.defineProperty(navigator, 'mediaDevices', { value: { getUserMedia: vi.fn() }, configurable: true })
}

afterEach(() => {
  vi.useRealTimers()
  delete (window as { SpeechRecognition?: unknown }).SpeechRecognition
  delete (navigator as { mediaDevices?: unknown }).mediaDevices
})

it('reports unsupported when the runtime has no speech recognition', () => {
  const view = renderHook(() => useWakeWord({ enabled: true, onWake: vi.fn() }))
  expect(view.result.current).toBe('unsupported')
})

it('fires onWake once when the phrase lands in an interim transcript', async () => {
  installFakeRecognition()
  const onWake = vi.fn()
  const view = renderHook(() => useWakeWord({ enabled: true, onWake }))
  await act(async () => {}) // flush the microphone permission probe
  // Probing until the first transcript arrives: the permission probe
  // passed and recognition is armed, but no result has landed yet.
  expect(view.result.current).toBe('probing')
  const first = FakeRecognition.instances.at(-1)!
  act(() => {
    first.onresult?.({ results: [{ 0: { transcript: '你好月汐今天天气怎么样' }, isFinal: false }] })
  })
  // The first transcript flips probing → listening, then the wake hit fires.
  expect(view.result.current).toBe('listening')
  expect(onWake).toHaveBeenCalledExactlyOnceWith('今天天气怎么样')
  expect(first.stop).toHaveBeenCalled()
})

it('keeps listening across healthy session restarts', async () => {
  vi.useFakeTimers()
  installFakeRecognition()
  const view = renderHook(() => useWakeWord({ enabled: true, onWake: vi.fn() }))
  await act(async () => {})
  const first = FakeRecognition.instances.at(-1)!
  act(() => {
    first.onresult?.({ results: [{ 0: { transcript: '今天天气不错' }, isFinal: true }] })
    first.onend?.()
  })
  await act(async () => {
    vi.advanceTimersByTime(400)
  })
  expect(view.result.current).toBe('listening')
  expect(FakeRecognition.instances.length).toBe(2)
})

it('surfaces an error after repeated fast-fail sessions instead of spinning as fake listening', async () => {
  vi.useFakeTimers()
  installFakeRecognition()
  const view = renderHook(() => useWakeWord({ enabled: true, onWake: vi.fn() }))
  await act(async () => {})
  // No transcript ever lands, so the hook stays in probing (not fake
  // listening) while the fast-fail sessions burn through retries.
  expect(view.result.current).toBe('probing')
  for (let round = 0; round < 9; round++) {
    const current = FakeRecognition.instances.at(-1)!
    act(() => {
      current.onerror?.({ error: 'network' })
      current.onend?.() // dies instantly, no transcript — a fast-fail session
    })
    await act(async () => {
      vi.advanceTimersByTime(1500)
    })
  }
  expect(view.result.current).toBe('error')
})

it('stops permanently on language-not-supported', async () => {
  installFakeRecognition()
  const view = renderHook(() => useWakeWord({ enabled: true, onWake: vi.fn() }))
  await act(async () => {})
  act(() => {
    FakeRecognition.instances.at(-1)!.onerror?.({ error: 'language-not-supported' })
  })
  expect(view.result.current).toBe('error')
})
