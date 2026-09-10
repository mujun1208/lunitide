import {cleanup, render, screen} from '@testing-library/react'
import {afterEach, describe, expect, it} from 'vitest'
import {TokenUsage} from './TokenUsage'

describe('TokenUsage', () => {
  afterEach(cleanup)
  it('shows actual switch state before a response', () => {
    const view = render(<TokenUsage zh enabled />)
    expect(screen.getByRole('status').textContent).toContain('上下文精简: 已开启')
    expect(screen.getByRole('status').textContent).toContain('只作用于请求结构与同源去重')
    expect(screen.getByRole('status').textContent).toContain('需重启')
    expect(screen.getByRole('status').textContent).not.toContain('%')
    view.rerender(<TokenUsage zh enabled={false} />)
    expect(screen.getByRole('status').textContent).toContain('已关闭')
    expect(screen.getByRole('status').textContent).toContain('不含独立摘要或供应商缓存')
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
  it('shows a collected ledger after refresh and never invents a savings percent', () => {
    render(<TokenUsage zh ledger={{
      sessionId: '01ARZ3NDEKTSV4RRFFQ69G5FAV', collected: true, integrity: 'partial',
      inputTokens: 40, outputTokens: 8, attempts: [{callId: 'c1', attemptId: 'a1', purpose: 'chat', status: 'succeeded', integrity: 'reported', inputTokens: 40, outputTokens: 8}],
    }} />)
    expect(screen.getByRole('status').textContent).toContain('输入 40')
    expect(screen.getByRole('status').textContent).toContain('完整性 部分')
    expect(screen.getByRole('status').textContent).not.toContain('partial')
    expect(screen.getByRole('status').textContent).not.toContain('%')
  })
  it('labels request trim as trimmed, unchanged, or unknown and never invents a percent', () => {
    const view = render(<TokenUsage zh ledger={{
      sessionId: '01ARZ3NDEKTSV4RRFFQ69G5FAV', collected: true, integrity: 'reported',
      inputTokens: 40, outputTokens: 8, attempts: [{
        callId: 'c1', attemptId: 'a1', purpose: 'chat', status: 'succeeded', integrity: 'reported',
        inputTokens: 40, outputTokens: 8, policyVersion: 'token-efficiency-v1', bytesBefore: 80, bytesAfter: 50,
      }],
    }} />)
    expect(screen.getByRole('status').textContent).toContain('精简了')
    expect(screen.getByRole('status').textContent).toContain('80')
    expect(screen.getByRole('status').textContent).toContain('50')
    expect(screen.getByRole('status').textContent).not.toContain('%')
    view.rerender(<TokenUsage zh ledger={{
      sessionId: '01ARZ3NDEKTSV4RRFFQ69G5FAV', collected: true, integrity: 'reported',
      inputTokens: 40, outputTokens: 8, attempts: [{
        callId: 'c1', attemptId: 'a1', purpose: 'chat', status: 'succeeded', integrity: 'reported',
        inputTokens: 40, outputTokens: 8, policyVersion: 'token-efficiency-v1', bytesBefore: 80, bytesAfter: 80,
      }],
    }} />)
    expect(screen.getByRole('status').textContent).toContain('未精简')
    expect(screen.getByRole('status').textContent).not.toContain('%')
    view.rerender(<TokenUsage zh ledger={{
      sessionId: '01ARZ3NDEKTSV4RRFFQ69G5FAV', collected: true, integrity: 'reported',
      inputTokens: 40, outputTokens: 8, attempts: [{
        callId: 'c1', attemptId: 'a1', purpose: 'chat', status: 'succeeded', integrity: 'reported',
        inputTokens: 40, outputTokens: 8,
      }],
    }} />)
    expect(screen.getByRole('status').textContent).toContain('未知')
    expect(screen.getByRole('status').textContent).not.toContain('%')
  })
  it('shows a fixed-segment fingerprint only after collection and never claims a cache hit', () => {
    const hash = '0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef'
    const view = render(<TokenUsage zh ledger={{
      sessionId: '01ARZ3NDEKTSV4RRFFQ69G5FAV', collected: false, integrity: 'unknown',
      inputTokens: 0, outputTokens: 0, attempts: [], stablePrefixHash: hash,
    }} />)
    expect(screen.getByRole('status').textContent).not.toContain('01234567')
    expect(screen.getByRole('status').textContent).not.toContain('稳定前缀')
    view.rerender(<TokenUsage zh ledger={{
      sessionId: '01ARZ3NDEKTSV4RRFFQ69G5FAV', collected: true, integrity: 'reported',
      inputTokens: 40, outputTokens: 8, attempts: [{
        callId: 'c1', attemptId: 'a1', purpose: 'chat', status: 'succeeded', integrity: 'reported',
        inputTokens: 40, outputTokens: 8,
      }], stablePrefixHash: hash,
    }} />)
    const text = screen.getByRole('status').textContent ?? ''
    expect(text).toContain('固定段')
    expect(text).toContain('01234567')
    expect(text).toContain('不含当前任务边界')
    expect(text).toContain('不表示供应商命中')
    expect(text).not.toContain('%')
    expect(text).not.toContain('命中率')
  })

  it('labels old turns without a ledger as uncollected', () => {
    render(<TokenUsage zh ledger={{
      sessionId: '01ARZ3NDEKTSV4RRFFQ69G5FAV', collected: false, integrity: 'unknown',
      inputTokens: 0, outputTokens: 0, attempts: [],
    }} />)
    expect(screen.getByRole('status').textContent).toContain('旧记录未采集')
    expect(screen.getByRole('status').textContent).not.toContain('输入 0')
  })

  it('localizes call purpose in Chinese', () => {
    render(<TokenUsage zh ledger={{
      sessionId: '01ARZ3NDEKTSV4RRFFQ69G5FAV', collected: true, integrity: 'reported',
      inputTokens: 40, outputTokens: 8, attempts: [{
        callId: 'c1', attemptId: 'a1', purpose: 'compaction', status: 'succeeded', integrity: 'reported',
        inputTokens: 40, outputTokens: 8, durationMs: 1000,
      }],
    }} />)
    const text = screen.getByRole('status').textContent ?? ''
    expect(text).toContain('压缩摘要')
    expect(text).toContain('已成功')
    expect(text).toContain('输入 40')
    expect(text).toContain('输出 8')
    expect(text).not.toContain('compaction')
    expect(text).not.toMatch(/\bin\s/)
    expect(text).not.toMatch(/\bout\s/)
    expect(text).not.toContain('%')
  })

  it('labels automation purpose in Chinese', () => {
    render(<TokenUsage zh ledger={{
      sessionId: '01ARZ3NDEKTSV4RRFFQ69G5FAV', collected: true, integrity: 'reported',
      inputTokens: 40, outputTokens: 8, attempts: [{
        callId: 'c1', attemptId: 'a1', purpose: 'automation', status: 'succeeded', integrity: 'reported',
        inputTokens: 40, outputTokens: 8,
      }],
    }} />)
    const text = screen.getByRole('status').textContent ?? ''
    expect(text).toContain('自动化')
    expect(text).not.toContain('automation')
    expect(text).not.toContain('%')
  })

  it('labels meeting purpose in Chinese', () => {
    render(<TokenUsage zh ledger={{
      sessionId: '01ARZ3NDEKTSV4RRFFQ69G5FAV', collected: true, integrity: 'reported',
      inputTokens: 40, outputTokens: 8, attempts: [{
        callId: 'c1', attemptId: 'a1', purpose: 'meeting', status: 'succeeded', integrity: 'reported',
        inputTokens: 40, outputTokens: 8,
      }],
    }} />)
    const text = screen.getByRole('status').textContent ?? ''
    expect(text).toContain('会议纪要')
    expect(text).not.toContain('meeting')
    expect(text).not.toContain('%')
  })

  it('falls back unknown purpose and integrity to Chinese', () => {
    render(<TokenUsage zh ledger={{
      sessionId: '01ARZ3NDEKTSV4RRFFQ69G5FAV', collected: true, integrity: 'mystery',
      inputTokens: 40, outputTokens: 8, attempts: [{
        callId: 'c1', attemptId: 'a1', purpose: 'mystery', status: 'mystery', integrity: 'mystery',
        inputTokens: 40, outputTokens: 8,
      }],
    } as never} />)
    const text = screen.getByRole('status').textContent ?? ''
    expect(text).toContain('其他')
    expect(text).not.toContain('mystery')
    expect(text).not.toContain('%')
  })

  it('labels unscoped purpose as 未分类 in Chinese', () => {
    render(<TokenUsage zh ledger={{
      sessionId: '01ARZ3NDEKTSV4RRFFQ69G5FAV', collected: true, integrity: 'reported',
      inputTokens: 40, outputTokens: 8, attempts: [{
        callId: 'c1', attemptId: 'a1', purpose: 'unknown', status: 'succeeded', integrity: 'reported',
        inputTokens: 40, outputTokens: 8,
      }],
    }} />)
    const text = screen.getByRole('status').textContent ?? ''
    expect(text).toContain('未分类')
    expect(text).not.toContain('unknown')
    expect(text).not.toContain('模型')
    expect(text).not.toContain('%')
  })

  it('labels recovered unknown and sent attempts instead of 其他', () => {
    const view = render(<TokenUsage zh ledger={{
      sessionId: '01ARZ3NDEKTSV4RRFFQ69G5FAV', collected: true, integrity: 'unknown',
      inputTokens: 0, outputTokens: 0, attempts: [{
        callId: 'c1', attemptId: 'a1', purpose: 'chat', status: 'unknown', integrity: 'unknown',
        inputTokens: 0, outputTokens: 0,
      }],
    }} />)
    let text = screen.getByRole('status').textContent ?? ''
    expect(text).toContain('未知')
    expect(text).not.toContain('其他')
    expect(text).not.toContain('unknown')
    expect(text).not.toContain('%')
    view.rerender(<TokenUsage zh ledger={{
      sessionId: '01ARZ3NDEKTSV4RRFFQ69G5FAV', collected: true, integrity: 'unknown',
      inputTokens: 0, outputTokens: 0, attempts: [{
        callId: 'c1', attemptId: 'a1', purpose: 'chat', status: 'sent', integrity: 'unknown',
        inputTokens: 0, outputTokens: 0,
      }],
    }} />)
    text = screen.getByRole('status').textContent ?? ''
    expect(text).toContain('已发送')
    expect(text).not.toContain('其他')
    expect(text).not.toContain('sent')
    expect(text).not.toContain('%')
  })

  it('localizes integrity and call status in Chinese', () => {
    render(<TokenUsage zh ledger={{
      sessionId: '01ARZ3NDEKTSV4RRFFQ69G5FAV', collected: true, integrity: 'reported',
      inputTokens: 40, outputTokens: 8, attempts: [{
        callId: 'c1', attemptId: 'a1', purpose: 'chat', status: 'succeeded', integrity: 'reported',
        inputTokens: 40, outputTokens: 8,
      }],
    }} />)
    const text = screen.getByRole('status').textContent ?? ''
    expect(text).toContain('完整性 已回报')
    expect(text).toContain('已成功')
    expect(text).not.toContain('reported')
    expect(text).not.toContain('succeeded')
    expect(text).not.toContain('%')
  })

  it('shows attempt duration in call details and never invents a fee or percent', () => {
    render(<TokenUsage zh ledger={{
      sessionId: '01ARZ3NDEKTSV4RRFFQ69G5FAV', collected: true, integrity: 'reported',
      inputTokens: 40, outputTokens: 8, attempts: [{
        callId: 'c1', attemptId: 'a1', purpose: 'chat', status: 'succeeded', integrity: 'reported',
        inputTokens: 40, outputTokens: 8, durationMs: 1000,
      }],
    }} />)
    const text = screen.getByRole('status').textContent ?? ''
    expect(text).toMatch(/耗时 1s|耗时 1000ms/)
    expect(text).not.toContain('%')
    expect(text).not.toContain('费用')
  })
})
