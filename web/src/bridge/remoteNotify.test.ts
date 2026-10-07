import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { createNotifier, startRemoteNotify, type NotifiedSession } from './remoteNotify'

// 轮询通知：后台才轮询、updatedAt 前进才通知、回前台清空基线、poll 抛错静默。
describe('startRemoteNotify', () => {
  beforeEach(() => {
    vi.useFakeTimers()
    vi.stubGlobal('document', { visibilityState: 'hidden' })
  })
  afterEach(() => {
    vi.useRealTimers()
    vi.unstubAllGlobals()
  })

  const sessions = (updates: Record<string, string>): NotifiedSession[] =>
    Object.entries(updates).map(([id, updatedAt]) => ({ id, title: `会话${id}`, updatedAt }))

  it('notifies when a known session updatedAt advances while hidden', async () => {
    let data = sessions({ a: '1', b: '1' })
    const notify = vi.fn()
    const stop = startRemoteNotify(async () => data, notify, 1000)
    await vi.advanceTimersByTimeAsync(0)
    expect(notify).not.toHaveBeenCalled()
    data = sessions({ a: '1', b: '2' })
    await vi.advanceTimersByTimeAsync(1000)
    expect(notify).toHaveBeenCalledTimes(1)
    expect(notify).toHaveBeenCalledWith('Lunitide', '会话b有新回复')
    stop()
  })

  it('does not notify for new sessions without baseline', async () => {
    let data: NotifiedSession[] = []
    const notify = vi.fn()
    const stop = startRemoteNotify(async () => data, notify, 1000)
    await vi.advanceTimersByTimeAsync(0)
    data = sessions({ a: '1' })
    await vi.advanceTimersByTimeAsync(1000)
    expect(notify).not.toHaveBeenCalled()
    stop()
  })

  it('skips polling and clears baseline while visible', async () => {
    vi.stubGlobal('document', { visibilityState: 'visible' })
    const poll = vi.fn(async () => sessions({ a: '1' }))
    const notify = vi.fn()
    const stop = startRemoteNotify(poll, notify, 1000)
    await vi.advanceTimersByTimeAsync(3000)
    expect(poll).not.toHaveBeenCalled()
    expect(notify).not.toHaveBeenCalled()
    stop()
  })

  it('swallows poll failures and keeps looping', async () => {
    let failing = true
    const notify = vi.fn()
    const stop = startRemoteNotify(async () => {
      if (failing) throw new Error('offline')
      return sessions({ a: '1' })
    }, notify, 1000)
    await vi.advanceTimersByTimeAsync(1000)
    failing = false
    await vi.advanceTimersByTimeAsync(1000)
    expect(notify).not.toHaveBeenCalled() // 失败轮不建基线，恢复后先建基线
    await vi.advanceTimersByTimeAsync(1000)
    expect(notify).not.toHaveBeenCalled()
    stop()
  })

  it('stops after the returned stop function', async () => {
    const poll = vi.fn(async () => sessions({ a: '1' }))
    const stop = startRemoteNotify(poll, vi.fn(), 1000)
    stop()
    await vi.advanceTimersByTimeAsync(5000)
    expect(poll).toHaveBeenCalledTimes(1)
  })
})

describe('createNotifier', () => {
  it('delivers through the injected sender', () => {
    const deliver = vi.fn()
    const notify = createNotifier(deliver)
    notify('标题', '内容')
    expect(deliver).toHaveBeenCalledWith('标题', '内容')
  })
})
