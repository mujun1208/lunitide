// ttsPlayer.enginecap.test.ts pins the engine-contract boundary: the
// tts.synthesize bridge schema rejects anything over 500 characters, so a
// long reply must never reach it as one request. Before this split a
// 600-character answer failed schema validation, counted three times
// against the failure circuit breaker, and the whole turn went silent.
import { afterEach, beforeEach, describe, expect, test, vi } from 'vitest'
import type { TtsBridge, TtsSynthesizePayload, TtsSynthesizeResult } from '../../bridge/client'
import { ENGINE_MAX_CHARS, REF_ENGINE_START_BUDGET_MS, REF_ENGINE_START_RETRIES, TtsPlayer, splitForEngine } from './ttsPlayer'
import { defaultCompanionSettings } from './companionSettings'

const bridge = {
  voices: vi.fn(),
  synthesize: vi.fn(),
  cancel: vi.fn(),
  refAudios: vi.fn(),
  ensureRefEngine: vi.fn(),
  installRefEngine: vi.fn(),
  stream: vi.fn(),
} satisfies Record<keyof TtsBridge, ReturnType<typeof vi.fn>>

vi.mock('../../bridge/client', () => ({ getTtsBridge: () => bridge }))

const WAV_STUB = btoa(String.fromCharCode(...new Array(44).fill(0)))
const okResult = (): TtsSynthesizeResult => ({ wav_base64: WAV_STUB, duration_hint: 1 })
const runes = (value: string) => Array.from(value).length

beforeEach(() => {
  vi.stubGlobal('URL', { ...URL, createObjectURL: vi.fn(() => 'blob:mock'), revokeObjectURL: vi.fn() })
  vi.spyOn(HTMLMediaElement.prototype, 'play').mockResolvedValue(undefined)
  vi.spyOn(HTMLMediaElement.prototype, 'pause').mockImplementation(() => {})
  bridge.synthesize.mockReset().mockResolvedValue(okResult())
  bridge.cancel.mockReset()
})

afterEach(() => {
  vi.unstubAllGlobals()
  vi.restoreAllMocks()
})

describe('splitForEngine', () => {
  test('leaves anything within the cap untouched', () => {
    expect(splitForEngine('今天天气不错。')).toEqual(['今天天气不错。'])
  })

  test('drops whitespace-only text instead of sending an empty request', () => {
    expect(splitForEngine('   ')).toEqual([])
  })

  test('splits an over-long reply on clause boundaries, each part within the cap', () => {
    const sentence = '这是一个很长的句子用来测试切分逻辑，'
    const long = sentence.repeat(60)
    expect(runes(long)).toBeGreaterThan(ENGINE_MAX_CHARS)

    const parts = splitForEngine(long)
    expect(parts.length).toBeGreaterThan(1)
    for (const part of parts) expect(runes(part)).toBeLessThanOrEqual(ENGINE_MAX_CHARS)
    expect(parts.join('')).toBe(long)
    // Clause boundaries survive, so playback still sounds like sentences.
    for (const part of parts.slice(0, -1)) expect(part.endsWith('，')).toBe(true)
  })

  test('hard-slices a single clause that has no boundary at all', () => {
    const wall = '啊'.repeat(ENGINE_MAX_CHARS * 2 + 7)
    const parts = splitForEngine(wall)
    for (const part of parts) expect(runes(part)).toBeLessThanOrEqual(ENGINE_MAX_CHARS)
    expect(parts.join('')).toBe(wall)
  })
})

describe('TtsPlayer keeps every request within the bridge schema', () => {
  test('a long streamed reply reaches the engine as several valid requests', async () => {
    const player = new TtsPlayer()
    const settings = { ...defaultCompanionSettings(), voiceId: 'zh-female' }
    // Sentence-at-a-time streaming, the way CompanionStage feeds it.
    for (let i = 0; i < 40; i++) {
      player.enqueue([`第${i}句这里再补一些字让整段超过引擎上限。`], settings, {})
    }
    await vi.waitFor(() => expect(bridge.synthesize).toHaveBeenCalled())

    for (const call of bridge.synthesize.mock.calls) {
      const payload = call[0] as TtsSynthesizePayload
      expect(runes(payload.text)).toBeGreaterThan(0)
      expect(runes(payload.text)).toBeLessThanOrEqual(ENGINE_MAX_CHARS)
    }
    player.dispose()
  })

  test('speak() splits an over-long segment instead of failing the turn', async () => {
    const player = new TtsPlayer()
    const settings = { ...defaultCompanionSettings(), voiceId: 'zh-female' }
    const long = '这是一段很长的回答内容，'.repeat(60)

    // Not awaited: jsdom never fires `ended`, so playback would sit on the
    // player's per-segment timeout. Synthesis is what this test is about.
    void player.speak([long], settings, {})
    await vi.waitFor(() => expect(bridge.synthesize.mock.calls.length).toBeGreaterThan(1))

    for (const call of bridge.synthesize.mock.calls) {
      expect(runes((call[0] as TtsSynthesizePayload).text)).toBeLessThanOrEqual(ENGINE_MAX_CHARS)
    }
    player.dispose()
  })

  test('companion SoVITS starting budget is 10s not two minutes', () => {
    expect(REF_ENGINE_START_RETRIES).toBe(2)
    expect(REF_ENGINE_START_BUDGET_MS).toBe(10_000)
  })
})
