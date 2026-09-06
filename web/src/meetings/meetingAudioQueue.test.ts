import { webcrypto } from 'node:crypto'
import { IDBFactory } from 'fake-indexeddb'
import { afterEach, beforeEach, expect, test, vi } from 'vitest'
import { MeetingAudioQueue } from './meetingAudioQueue'

beforeEach(() => { vi.stubGlobal('crypto', webcrypto) })
afterEach(() => { vi.unstubAllGlobals() })

test('renderer reopen recovers a short committed PCM tail with the same identity until ACK', async () => {
  const factory = new IDBFactory()
  let queue = await MeetingAudioQueue.open(factory)
  await queue.enqueue('meeting-a', 'capture-original', new Int16Array([10, 20, -30]))
  queue.close()
  queue = await MeetingAudioQueue.open(factory)
  const first = (await queue.next('meeting-a', 'capture-new', false))!
  expect(first.identity).toMatchObject({ captureSessionId: 'capture-original', chunkSeq: 0, sampleStart: 0, sampleCount: 3 })
  // Simulate backend commit followed by lost ACK and renderer termination.
  queue.close()
  queue = await MeetingAudioQueue.open(factory)
  const replay = (await queue.next('meeting-a', 'capture-new', true))!
  expect(replay).toEqual(first)
  await expect(queue.acknowledge(replay, { ...replay.identity, chunkSeq: 1 })).rejects.toThrow('确认不匹配')
  expect(await queue.next('meeting-a', 'capture-new', true)).toEqual(first)
  await queue.acknowledge(replay, replay.identity)
  queue.close()
  queue = await MeetingAudioQueue.open(factory)
  expect(await queue.next('meeting-a', 'capture-new', true)).toBeUndefined()
  queue.close()
})

test('durable batching retains sample positions, meeting boundaries and untouched later frames', async () => {
  const queue = await MeetingAudioQueue.open(new IDBFactory())
  await queue.enqueue('meeting-b', 'capture-b', new Int16Array([999]))
  for (let index = 0; index < 13; index++) await queue.enqueue('meeting-a', 'capture-a', new Int16Array(1600).fill(index))
  const first = (await queue.next('meeting-a', 'capture-a', false))!
  expect(first.identity).toMatchObject({ sampleStart: 0, sampleCount: 19200, chunkSeq: 0 })
  expect(atob(first.pcm).length).toBe(38400)
  await queue.acknowledge(first, first.identity)
  expect(await queue.next('meeting-a', 'capture-a', false)).toBeUndefined()
  const tail = (await queue.next('meeting-a', 'capture-a', true))!
  expect(tail.identity).toMatchObject({ sampleStart: 19200, sampleCount: 1600, chunkSeq: 1 })
  await queue.acknowledge(tail, tail.identity)
  expect((await queue.next('meeting-b', 'capture-new', true))!.identity.sampleCount).toBe(1)
  queue.close()
})

test('two reopened queue connections reserve exactly the same immutable batch', async () => {
  const factory = new IDBFactory()
  const first = await MeetingAudioQueue.open(factory)
  const second = await MeetingAudioQueue.open(factory)
  await first.enqueue('meeting-a', 'capture-a', new Int16Array([1, 2]))
  const [a, b] = await Promise.all([first.next('meeting-a', '', true), second.next('meeting-a', '', true)])
  expect(a).toEqual(b)
  await Promise.all([first.acknowledge(a!, a!.identity), second.acknowledge(b!, b!.identity)])
  expect(await first.next('meeting-a', '', true)).toBeUndefined()
  first.close(); second.close()
})
