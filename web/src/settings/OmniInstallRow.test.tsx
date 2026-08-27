import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
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

test('offers to install the runtime when the model is already on disk', async () => {
  omni.status.mockResolvedValue(missingRuntime)
  omni.install.mockResolvedValue({
    state: 'downloading',
    percent: 10,
    doneBytes: 10,
    totalBytes: 100,
  })
  render(<OmniInstallRow />)
  await waitFor(() => {
    expect(screen.getByText(/还差本机推理进程/)).toBeTruthy()
  })
  const button = screen.getByRole('button', { name: '继续安装' })
  expect(button.hasAttribute('disabled')).toBe(false)
  fireEvent.click(button)
  await waitFor(() => {
    expect(omni.install).toHaveBeenCalled()
  })
})

test('leaves 正在检测 only until the first status lands', async () => {
  omni.status.mockResolvedValue(missingRuntime)
  render(<OmniInstallRow />)
  await waitFor(() => {
    expect(screen.queryByText('正在检测 MiniCPM-o 4.5…')).toBeNull()
  })
})
