import {cleanup, render, screen} from '@testing-library/react'
import {afterEach, describe, expect, it} from 'vitest'
import {TokenUsage} from './TokenUsage'

describe('TokenUsage', () => {
  afterEach(cleanup)
  it('shows actual switch state before a response', () => {
    const view = render(<TokenUsage zh enabled />)
    expect(screen.getByRole('status').textContent).toContain('上下文精简: 已开启')
    view.rerender(<TokenUsage zh enabled={false} />)
    expect(screen.getByRole('status').textContent).toContain('已关闭')
  })
  it('keeps full input totals and distinguishes partial cache accounting', () => {
    const usage = {inputTokens: 100, outputTokens: 20, totalTokens: 120, cachedInputTokens: 30, cacheWriteInputTokens: 10}
    const view = render(<TokenUsage zh usage={usage} />)
    expect(screen.getByRole('status').textContent).toContain('输入 100')
    expect(screen.getByRole('status').textContent).toContain('30 (部分回报)')
    expect(screen.getByRole('status').textContent).not.toContain('%')
    view.rerender(<TokenUsage zh={false} usage={{...usage, cacheUsageReported: true}} />)
    expect(screen.getByRole('status').textContent).toContain('Cache read 30')
    expect(screen.getByRole('status').textContent).not.toContain('partial')
  })
  it('does not invent cache hits or efficiency state', () => {
    const view = render(<TokenUsage zh />)
    expect(screen.queryByRole('status')).toBeNull()
    view.rerender(<TokenUsage zh usage={{inputTokens: 100, outputTokens: 20, totalTokens: 120}} />)
    expect(screen.getByRole('status').textContent).toContain('未完整回报')
    expect(screen.getByRole('status').textContent).not.toContain('已开启')
  })
})
