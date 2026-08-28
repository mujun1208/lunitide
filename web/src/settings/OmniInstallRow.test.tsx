import { act, cleanup, render, screen, waitFor } from '@testing-library/react'
import { afterEach, beforeEach, expect, test, vi } from 'vitest'

const omni = vi.hoisted(() => ({
  status: vi.fn(),
  install: vi.fn(),
}))

vi.mock('../bridge/client', () => ({
  getOmniBridge: () => omni,
}))

import { OmniInstallRow } from './OmniInstallRow'

const missingRuntime = {
  supported: true,
  ready: false,
  installed: true,
  runtimeFound: false,
  hostState: 'missing_runtime' as const,
  downloadBytes: 9_000_000_000,
  title: 'MiniCPM-o 4.5 Q4',
  percent: 0,
  doneBytes: 0,
  totalBytes: 0,
}

beforeEach(() => {
  omni.status.mockReset()
  omni.install.mockReset()
})

afterEach(() => {
  vi.useRealTimers()
  cleanup()
})

test('does not stay on 正在检测 when the bridge fails', async () => {
  omni.status.mockRejectedValue(new Error('timeout'))
  render(<OmniInstallRow />)
  await waitFor(() => {
    expect(screen.getByText(/检测 MiniCPM-o 失败/)).toBeTruthy()
  })
  expect(screen.getByRole('button', { name: '重新检测' })).toBeTruthy()
})

test('does not ask the user to fetch llama-omni-server when the model is on disk', async () => {
  omni.status.mockResolvedValue(missingRuntime)
  omni.install.mockResolvedValue({
    state: 'failed',
    percent: 0,
    doneBytes: 0,
    totalBytes: 0,
    lastError: '本机推理进程未能展开',
  })
  render(<OmniInstallRow />)
  await waitFor(() => {
    expect(screen.getByText(/本机推理进程未能展开/)).toBeTruthy()
  })
  expect(screen.queryByText(/约 0\.5 GB/)).toBeNull()
  expect(screen.queryByText(/omni\/runtime/)).toBeNull()
  expect(screen.getByRole('button', { name: '重试展开' })).toBeTruthy()
})

test('model download copy does not mention a separate runtime installer', async () => {
  omni.status.mockResolvedValue({
    ...missingRuntime,
    installed: false,
    runtimeFound: true,
    hostState: 'missing_model',
  })
  render(<OmniInstallRow />)
  await waitFor(() => {
    expect(screen.getByText(/推理进程已随月汐安装/)).toBeTruthy()
  })
  expect(screen.queryByText(/含推理进程/)).toBeNull()
  expect(screen.getByRole('button', { name: '下载安装' })).toBeTruthy()
})

test('leaves 正在检测 only until the first status lands', async () => {
  omni.status.mockResolvedValue(missingRuntime)
  render(<OmniInstallRow />)
  await waitFor(() => {
    expect(screen.queryByText('正在检测 MiniCPM-o 4.5…')).toBeNull()
  })
})

test('does not stay on 正在检测 when status hangs', async () => {
  vi.useFakeTimers()
  omni.status.mockReturnValue(new Promise(() => {}))
  render(<OmniInstallRow />)
  expect(screen.getByText('正在检测 MiniCPM-o 4.5…')).toBeTruthy()
  await act(async () => {
    await vi.advanceTimersByTimeAsync(4000)
  })
  expect(screen.queryByText('正在检测 MiniCPM-o 4.5…')).toBeNull()
  expect(screen.getByText(/检测 MiniCPM-o 失败/)).toBeTruthy()
  expect(screen.getByRole('button', { name: '重新检测' })).toBeTruthy()
  vi.useRealTimers()
})
