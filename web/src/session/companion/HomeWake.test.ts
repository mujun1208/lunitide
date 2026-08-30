import { describe, expect, test } from 'vitest'
import { homeWakeStatus } from './HomeWake'

describe('homeWakeStatus', () => {
  test('tells the user the phrase while the home mic is live', () => {
    expect(homeWakeStatus('listening', true)).toMatch(/你好月汐/)
    expect(homeWakeStatus('probing', true)).toMatch(/正在听/)
  })

  test('stays quiet when wake is unsupported so the button remains the path in', () => {
    expect(homeWakeStatus('unsupported', true)).toBeNull()
    expect(homeWakeStatus('idle', true)).toBeNull()
  })

  test('points at the button when the listener failed', () => {
    expect(homeWakeStatus('error', true)).toMatch(/点下方/)
  })
})
