import {cleanup, render, screen} from '@testing-library/react'
import {afterEach, describe, expect, it} from 'vitest'
import {TurnStatsLine, turnStatsLine} from './TurnStats'

afterEach(cleanup)

describe('turnStatsLine', () => {
  it('formats elapsed time and thousands-separated tokens in Chinese', () => {
    expect(turnStatsLine(192000, {inputTokens: 12930, outputTokens: 8704, totalTokens: 21634}, true))
      .toBe('本轮耗时 3m 12s · 输入 12,930 · 输出 8,704 · 合计 21,634 tokens')
  })
  it('formats the English footer', () => {
    expect(turnStatsLine(4500, {inputTokens: 50, outputTokens: 4, totalTokens: 54}, false))
      .toBe('Elapsed 4s · Input 50 · Output 4 · Total 54 tokens')
  })
  it('keeps elapsed only when tokens were never reported', () => {
    expect(turnStatsLine(1200, {inputTokens: 0, outputTokens: 0, totalTokens: 0}, true)).toBe('本轮耗时 1s')
  })
  it('keeps tokens only when duration is unknown', () => {
    expect(turnStatsLine(undefined, {inputTokens: 10, outputTokens: 2, totalTokens: 12}, true)).toBe('输入 10 · 输出 2 · 合计 12 tokens')
  })
  it('renders nothing without data', () => {
    expect(turnStatsLine(undefined, undefined, true)).toBe('')
  })
})

describe('TurnStatsLine', () => {
  it('renders history stats from the MessageDTO payload', () => {
    render(<TurnStatsLine zh stats={{durationMs: 192000, inputTokens: 12930, outputTokens: 8704, totalTokens: 21634}} />)
    expect(screen.getByRole('status')).toHaveTextContent('本轮耗时 3m 12s · 输入 12,930 · 输出 8,704 · 合计 21,634 tokens')
  })
  it('renders live usage with backend duration, preferring it over the local clock', () => {
    render(<TurnStatsLine zh durationMs={999999} usage={{inputTokens: 50, outputTokens: 4, totalTokens: 54, durationMs: 4500}} />)
    expect(screen.getByRole('status')).toHaveTextContent('本轮耗时 4s · 输入 50 · 输出 4 · 合计 54 tokens')
  })
  it('falls back to the local elapsed clock when the usage event omits duration', () => {
    render(<TurnStatsLine zh durationMs={62000} usage={{inputTokens: 50, outputTokens: 4, totalTokens: 54}} />)
    expect(screen.getByRole('status')).toHaveTextContent('本轮耗时 1m 2s')
  })
  it('renders nothing without any data', () => {
    const {container} = render(<TurnStatsLine zh />)
    expect(container).toBeEmptyDOMElement()
  })
})
