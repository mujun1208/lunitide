import { cleanup, render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, expect, it, vi } from 'vitest'
import { getAppUpdateBridge, getSystemHealthBridge } from '../bridge/client'
import { AppUpdateBanner } from './AppUpdateBanner'

vi.mock('../bridge/client', () => ({
  getAppUpdateBridge: vi.fn(),
  getSystemHealthBridge: vi.fn(),
  createMutationAttempt: vi.fn((method: string, payload: object) => ({
    method, payload, idempotencyKey: '01ARZ3NDEKTSV4RRFFQ69G5FAV', fingerprint: 'fp',
  })),
}))

afterEach(() => {
  cleanup()
  localStorage.removeItem('lunitide:update-dismissed')
})

it('stays hidden when the installed version is current', async () => {
  const check = vi.fn().mockResolvedValue({ updateId: '', version: '', digest: '', mandatory: false })
  vi.mocked(getSystemHealthBridge).mockReturnValue({
    health: vi.fn().mockResolvedValue({ version: '0.4.83' }),
    diagnostics: vi.fn(),
  })
  vi.mocked(getAppUpdateBridge).mockReturnValue({ check, install: vi.fn() })
  render(<AppUpdateBanner />)
  await vi.waitFor(() => expect(check).toHaveBeenCalled())
  expect(screen.queryByText(/发现新版本/)).toBeNull()
})

it('offers one-click install when a newer package is available', async () => {
  const install = vi.fn().mockResolvedValue({ state: 'installed' })
  vi.mocked(getSystemHealthBridge).mockReturnValue({
    health: vi.fn().mockResolvedValue({ version: '0.4.81' }),
    diagnostics: vi.fn(),
  })
  vi.mocked(getAppUpdateBridge).mockReturnValue({
    check: vi.fn().mockResolvedValue({
      updateId: '01ARZ3NDEKTSV4RRFFQ69G5FAW',
      version: '0.4.83',
      digest: 'aa'.repeat(32),
      mandatory: false,
    }),
    install,
  })
  render(<AppUpdateBanner />)
  expect(await screen.findByText('发现新版本 0.4.83，可以覆盖安装，不必先卸载。')).toBeInTheDocument()
  await userEvent.click(screen.getByRole('button', { name: '立即升级' }))
  expect(install).toHaveBeenCalledWith(
    { updateId: '01ARZ3NDEKTSV4RRFFQ69G5FAW', expectedDigest: 'aa'.repeat(32) },
    expect.objectContaining({ attempt: expect.anything() }),
  )
  expect(await screen.findByText(/正在下载并安装更新/)).toBeInTheDocument()
})
