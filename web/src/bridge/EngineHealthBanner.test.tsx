import { act, cleanup, fireEvent, render, screen } from '@testing-library/react'
import { afterEach, expect, it, vi } from 'vitest'
import { EngineHealthBanner } from './EngineHealthBanner'
import { ENGINE_RECOVERED_EVENT, ENGINE_UNAVAILABLE_EVENT } from './engineHealth'

afterEach(() => {
  cleanup()
  vi.useRealTimers()
})

it('renders one banner for two ENGINE_UNAVAILABLE events', async () => {
  render(<EngineHealthBanner probe={() => Promise.resolve({})} />)
  window.dispatchEvent(new CustomEvent(ENGINE_UNAVAILABLE_EVENT, { detail: { code: 'ENGINE_UNAVAILABLE', correlationId: 'a' } }))
  window.dispatchEvent(new CustomEvent(ENGINE_UNAVAILABLE_EVENT, { detail: { code: 'ENGINE_UNAVAILABLE', correlationId: 'b' } }))
  expect(await screen.findAllByRole('alert')).toHaveLength(1)
  expect(screen.getByText('核心引擎已断开，正在自动重连…')).toBeInTheDocument()
})

it('retries the probe when 重试连接 is clicked', async () => {
  const probe = vi.fn().mockResolvedValue({ items: [] })
  render(<EngineHealthBanner probe={probe} />)
  window.dispatchEvent(new CustomEvent(ENGINE_UNAVAILABLE_EVENT, { detail: { code: 'ENGINE_UNAVAILABLE', correlationId: 'c' } }))
  fireEvent.click(await screen.findByRole('button', { name: '重试连接' }))
  await vi.waitFor(() => expect(probe).toHaveBeenCalledOnce())
  expect(await screen.findByText('核心引擎已恢复')).toBeInTheDocument()
})

it('auto-probes while down then shows 已恢复 and emits ENGINE_RECOVERED', async () => {
  vi.useFakeTimers()
  const probe = vi.fn().mockRejectedValueOnce(new Error('still down')).mockResolvedValue({ items: [] })
  const recovered = vi.fn()
  const onRecovered = vi.fn()
  window.addEventListener(ENGINE_RECOVERED_EVENT, recovered)
  render(<EngineHealthBanner probe={probe} onRecovered={onRecovered} />)
  act(() => {
    window.dispatchEvent(new CustomEvent(ENGINE_UNAVAILABLE_EVENT, { detail: { code: 'ENGINE_UNAVAILABLE', correlationId: 'd' } }))
  })
  expect(screen.getByText('核心引擎已断开，正在自动重连…')).toBeInTheDocument()
  await act(async () => {
    await vi.advanceTimersByTimeAsync(2000)
  })
  await act(async () => {
    await vi.advanceTimersByTimeAsync(2000)
  })
  expect(screen.getByText('核心引擎已恢复')).toBeInTheDocument()
  expect(onRecovered).toHaveBeenCalled()
  expect(recovered).toHaveBeenCalled()
  await act(async () => {
    await vi.advanceTimersByTimeAsync(2000)
  })
  expect(screen.queryByText('核心引擎已恢复')).toBeNull()
  window.removeEventListener(ENGINE_RECOVERED_EVENT, recovered)
})
