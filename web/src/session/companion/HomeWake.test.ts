import { describe, expect, test } from 'vitest'
import { homeWakeStatus } from './HomeWake'

describe('homeWakeStatus', () => {
  test('does not claim to be listening while the mic is still probing', () => {
    expect(homeWakeStatus('probing', true)).toMatch(/接通麦克风/)
    expect(homeWakeStatus('probing', true)).not.toMatch(/正在听/)
  })

  test('tells the user the phrase while the home mic is live', () => {
    expect(homeWakeStatus('listening', true)).toMatch(/你好月汐/)
  })

  test('paints a partial transcript instead of a frozen prompt', () => {
    expect(homeWakeStatus('listening', true, { heard: '你好月' })).toBe('听到：你好月')
  })

  test('stays quiet when wake is unsupported so the button remains the path in', () => {
    expect(homeWakeStatus('unsupported', true)).toBeNull()
    expect(homeWakeStatus('idle', true)).toBeNull()
  })

  test('points at the button when the listener failed', () => {
    expect(homeWakeStatus('error', true)).toMatch(/点下方/)
  })

  test('says it is deaf when energy arrived without glyphs', () => {
    expect(homeWakeStatus('error', true, { deaf: true })).toMatch(/没有听出字/)
  })
})
