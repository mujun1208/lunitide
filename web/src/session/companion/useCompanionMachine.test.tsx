// useCompanionMachine.test.tsx asserts the M9.5 transition
// matrix (T-9.5.2.1 DoD): legal transitions pass, every other
// combination is rejected by the guard (M95-005) while the current
// state is kept.
import { act, renderHook } from '@testing-library/react'
import { describe, expect, test } from 'vitest'
import { useCompanionMachine, type CompanionEvent } from './useCompanionMachine'

const EVENTS: CompanionEvent['type'][] = [
  'MIC_ACTIVATE',
  'MIC_CANCEL',
  'RECOGNIZED_FINAL',
  'REPLY_COMPLETED',
  'REPLY_TERMINAL',
  'PLAYBACK_ENDED',
  'INTERRUPT',
  'MIC_CLICK_WHILE_SPEAKING',
  'AWAIT_MORE',
]

const event = (type: CompanionEvent['type']): CompanionEvent =>
  type === 'REPLY_COMPLETED' ? { type, speakable: true } : ({ type } as CompanionEvent)

describe('useCompanionMachine transition matrix', () => {
  const positives: Array<[string, CompanionEvent['type'], string]> = [
    ['idle', 'MIC_ACTIVATE', 'listening'],
    ['listening', 'MIC_CANCEL', 'idle'],
    ['listening', 'RECOGNIZED_FINAL', 'thinking'],
    ['listening', 'MIC_ACTIVATE', 'listening'],
    ['thinking', 'REPLY_COMPLETED', 'speaking'],
    ['thinking', 'REPLY_TERMINAL', 'idle'],
    ['thinking', 'INTERRUPT', 'idle'],
    ['speaking', 'PLAYBACK_ENDED', 'idle'],
    ['speaking', 'INTERRUPT', 'idle'],
    ['speaking', 'MIC_CLICK_WHILE_SPEAKING', 'listening'],
    ['speaking', 'AWAIT_MORE', 'thinking'],
  ]

  test.each(positives)('%s x %s -> %s', (from, type, expected) => {
    const path: CompanionEvent['type'][] = []
    if (from === 'listening') path.push('MIC_ACTIVATE')
    if (from === 'thinking') path.push('MIC_ACTIVATE', 'RECOGNIZED_FINAL')
    if (from === 'speaking') path.push('MIC_ACTIVATE', 'RECOGNIZED_FINAL', 'REPLY_COMPLETED')
    const { result } = renderHook(() => useCompanionMachine())
    act(() => {
      for (const step of path) result.current.dispatch(event(step))
    })
    expect(result.current.state).toBe(from)
    act(() => {
      result.current.dispatch(event(type))
    })
    expect(result.current.state).toBe(expected)
  })

  test('every non-matrix combination is rejected and keeps the state', () => {
    const allowed = new Set(positives.map(([from, type]) => `${from}|${type}`))
    for (const from of ['idle', 'listening', 'thinking', 'speaking'] as const) {
      for (const type of EVENTS) {
        if (allowed.has(`${from}|${type}`)) continue
        const path: CompanionEvent['type'][] = []
        if (from === 'listening') path.push('MIC_ACTIVATE')
        if (from === 'thinking') path.push('MIC_ACTIVATE', 'RECOGNIZED_FINAL')
        if (from === 'speaking') path.push('MIC_ACTIVATE', 'RECOGNIZED_FINAL', 'REPLY_COMPLETED')
        const { result } = renderHook(() => useCompanionMachine())
        act(() => {
          for (const step of path) result.current.dispatch(event(step))
        })
        expect(result.current.state).toBe(from)
        const rejectedBefore = result.current.rejected
        act(() => {
          result.current.dispatch(event(type))
        })
        expect(result.current.state).toBe(from)
        expect(result.current.rejected).toBe(rejectedBefore + 1)
        expect(result.current.wouldReject(event(type))).toBe(true)
      }
    }
  })

  test('typed send follows idle→listening→thinking through the frozen matrix', () => {
    const { result } = renderHook(() => useCompanionMachine())
    act(() => {
      result.current.dispatch({ type: 'MIC_ACTIVATE' })
      result.current.dispatch({ type: 'RECOGNIZED_FINAL' })
    })
    expect(result.current.state).toBe('thinking')
  })
})
