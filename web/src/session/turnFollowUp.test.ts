import { expect, it } from 'vitest'
import { inFlightLiveChat, isStopCommand } from './turnFollowUp'

it('treats 停止 as cancel, not a normal follow-up', () => {
  expect(isStopCommand('停止')).toBe(true)
  expect(isStopCommand('停止。')).toBe(true)
  expect(isStopCommand('stop')).toBe(true)
  expect(isStopCommand('做好了没有')).toBe(false)
  expect(isStopCommand('改方案用深色封面')).toBe(false)
})

it('detects an in-flight live chat that must not be replaced', () => {
  expect(inFlightLiveChat({ terminal: false }, false)).toBe(true)
  expect(inFlightLiveChat(undefined, true)).toBe(true)
  expect(inFlightLiveChat({ terminal: true }, false)).toBe(false)
  expect(inFlightLiveChat(undefined, false)).toBe(false)
})
