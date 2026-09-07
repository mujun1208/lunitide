import { expect, test } from 'vitest'
import { MeetingLoopbackQueue } from './meetingLoopbackQueue'

test('polling faster than microphone frames preserves consecutive system speech', () => {
  const queue = new MeetingLoopbackQueue()
  queue.append(new Int16Array([10, 20]))
  queue.append(new Int16Array([30, 40, 50]))
  expect([...queue.take(3)]).toEqual([10, 20, 30])
  expect([...queue.take(3)]).toEqual([40, 50])
  expect(queue.take(3)).toHaveLength(0)
})

test('a new recording cannot consume a previous recording tail', () => {
  const queue = new MeetingLoopbackQueue()
  queue.append(new Int16Array([10, 20]))
  queue.clear()
  queue.append(new Int16Array([30]))
  expect([...queue.take(1600)]).toEqual([30])
})
