import { afterEach, expect, it } from 'vitest'
import type { StreamEvent } from '../bridge/client'
import { applyLiveChatEvent, resetLiveChatForTests, startLiveChat } from './liveChat'

const digest = 'a'.repeat(64)
const base = { v: '1.0' as const, kind: 'event' as const, id: '01ARZ3NDEKTSV4RRFFQ69G5FAV', streamId: '01ARZ3NDEKTSV4RRFFQ69G5FAD', sequence: 1 }

afterEach(resetLiveChatForTests)

it('keeps the stream live and updates command output summaries', () => {
  const entry = startLiveChat('session-1')
  const started: StreamEvent = { ...base, type: 'tool_started', tool: { callId: 'call-1', name: 'command.run', argsDigest: digest } }
  const output: StreamEvent = { ...base, sequence: 2, type: 'tool_output', tool: { callId: 'call-1', name: 'command.run', argsDigest: digest, summary: 'go: downloading' } }
  const completed: StreamEvent = { ...base, sequence: 3, type: 'completed' }
  applyLiveChatEvent(entry, started)
  applyLiveChatEvent(entry, output)
  expect(entry.terminal).toBe(false)
  expect(entry.state.chatStatus).toBe('streaming')
  expect(entry.state.toolActivities).toEqual([
    { callId: 'call-1', name: 'command.run', argsDigest: digest, status: 'tool_started', summary: 'go: downloading' },
  ])
  applyLiveChatEvent(entry, completed)
  expect(entry.terminal).toBe(true)
  expect(entry.state.chatStatus).toBe('done')
})
