import { expect, it, vi } from 'vitest'

const bridge = vi.hoisted(() => ({ start: vi.fn(), append: vi.fn(), stop: vi.fn(), finish: vi.fn() }))
vi.mock('../../../bridge/client', () => ({ getVoiceBridge: () => bridge }))
vi.mock('../pcmCapture', () => ({ startPcmCapture: vi.fn(async () => ({ stop: vi.fn(), flush: vi.fn() })) }))
import { startVolcAsr } from './volcAsr'
import conversation from './fixtures/continuous-mandarin-1.json'
import repeated from './fixtures/repeated-mandarin-1.json'

it('delivers successive appended phrases from one provider segment as separate committed product turns', async () => {
  bridge.start.mockResolvedValue({ sessionId: 'one-provider-stream' })
  bridge.stop.mockResolvedValue({})
  bridge.append.mockResolvedValue({ text: '', final: false })
  const captions = vi.fn()
  const handle = await startVolcAsr('01ARZ3NDEKTSV4RRFFQ69G5FAV', { externalPcm: true, onTranscript: captions })
  const frame = { base64: 'AAAA', samples: new Int16Array(1600), peak: 0.2 }
  const settle = () => new Promise(resolve => setTimeout(resolve, 0))
  try {
    for (const [text, expected, endMs] of [['查天气。', '查天气。', 2000], ['查天气。停。', '停。', 3500], ['查天气。停。停。', '停。', 4600]] as const) {
      bridge.append.mockResolvedValueOnce({ text, final: false, utterances: [{ text, startMs: 0, endMs, final: false }] })
      handle.pushFrame?.(frame)
      await settle()
      expect(captions).toHaveBeenLastCalledWith(expected, false, true)
      await expect(handle.commit({ useStreamed: true })).resolves.toBe(expected)
      await settle()
    }
    captions.mockClear()
    bridge.append.mockResolvedValueOnce({ text: '停。', final: true, utterances: [{ text: '停。', startMs: 4000, endMs: 4600, final: true }] })
    handle.pushFrame?.(frame)
    await settle()
    expect(captions).not.toHaveBeenCalled()
    expect(bridge.start).toHaveBeenCalledOnce()
    expect(bridge.finish).not.toHaveBeenCalled()
  } finally { handle.cancel() }
})

it.each([conversation, { ...repeated, requests: repeated.requests.slice(2) }])('delivers recorded full snapshots through the PCM bridge and keeps one websocket across replies', async recording => {
  bridge.start.mockClear().mockResolvedValue({ sessionId: 'recorded-stream' })
  bridge.finish.mockClear()
  bridge.append.mockReset().mockResolvedValue({ text: '', final: false })
  const captions = vi.fn()
  const handle = await startVolcAsr('01ARZ3NDEKTSV4RRFFQ69G5FAV', { externalPcm: true, onTranscript: captions })
  const frame = { base64: 'AAAA', samples: new Int16Array(1600), peak: 0.2 }
  const settle = () => new Promise(resolve => setTimeout(resolve, 0))
  const submitted: string[] = []
  try {
    for (const event of recording.events) {
      captions.mockClear()
      // Exercise the compatible provider response without time positions.
      bridge.append.mockResolvedValueOnce({ text: event.Text, final: event.Final })
      handle.pushFrame?.(frame)
      await settle()
      const latest = captions.mock.lastCall
      if (latest?.[1]) {
        submitted.push(await handle.commit({ useStreamed: true }))
        handle.setMuted(true)
        await settle()
        handle.setMuted(false)
      }
    }
    expect(submitted).toEqual(recording.requests)
    expect(bridge.start).toHaveBeenCalledOnce()
    expect(bridge.finish).not.toHaveBeenCalled()
  } finally { handle.cancel() }
})
