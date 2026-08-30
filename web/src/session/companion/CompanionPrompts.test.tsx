import { act, cleanup, fireEvent, render } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, test, vi } from 'vitest'
import { CompanionPrompts, shouldShowCompanionPrompts } from './CompanionPrompts'
import { COMPANION_PROMPTS_ZH } from './visual/moonVisual'

afterEach(() => {
  cleanup()
  vi.useRealTimers()
})

describe('shouldShowCompanionPrompts', () => {
  test('shows on idle or quiet listening, hides after voice or a user round', () => {
    const quiet = { hasUserRound: false, hasInterim: false, voiceHeard: false }
    expect(shouldShowCompanionPrompts({ state: 'idle', ...quiet })).toBe(true)
    expect(shouldShowCompanionPrompts({ state: 'listening', ...quiet })).toBe(true)
    expect(shouldShowCompanionPrompts({ state: 'listening', ...quiet, voiceHeard: true })).toBe(false)
    expect(shouldShowCompanionPrompts({ state: 'idle', ...quiet, hasUserRound: true })).toBe(false)
    expect(shouldShowCompanionPrompts({ state: 'thinking', ...quiet })).toBe(false)
    expect(shouldShowCompanionPrompts({ state: 'speaking', ...quiet })).toBe(false)
  })
})

describe('CompanionPrompts', () => {
  beforeEach(() => {
    vi.useFakeTimers()
  })

  test('stays hidden until the idle delay, then a click sends the prompt', () => {
    const onPick = vi.fn()
    const { container, rerender } = render(<CompanionPrompts visible language="zh" onPick={onPick} />)
    expect(container.querySelector('.companion-prompt')).toBeNull()
    act(() => {
      vi.advanceTimersByTime(1200)
    })
    const button = container.querySelector('.companion-prompt') as HTMLButtonElement
    expect(button).toBeTruthy()
    expect(COMPANION_PROMPTS_ZH).toContain(button.textContent)
    fireEvent.click(button)
    expect(onPick).toHaveBeenCalledTimes(1)
    expect(COMPANION_PROMPTS_ZH).toContain(onPick.mock.calls[0][0])
    rerender(<CompanionPrompts visible={false} language="zh" onPick={onPick} />)
    expect(container.querySelector('.companion-prompt')).toBeNull()
  })
})
