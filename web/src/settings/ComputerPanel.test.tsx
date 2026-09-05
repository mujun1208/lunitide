import { cleanup, render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, expect, it, vi } from 'vitest'
import type { CcBridge } from '../bridge/client'
import type { CcGetConfigResult } from '../generated/bridge'
import { ComputerPanel } from './SettingsPage'

afterEach(cleanup)

const now = '2026-01-01T00:00:00Z'
const cfg = (partial: Partial<CcGetConfigResult> = {}): CcGetConfigResult => ({
  enabled: false, securityLevel: 'standard', allowCritical: false, processBlocklist: ['cmd.exe'],
  maxActionsPerMinute: 60, confirmTimeoutSeconds: 60, emergencyStopped: false, updatedAt: now, ...partial,
})

function api(overrides: Partial<CcBridge> = {}): CcBridge {
  return {
    getConfig: vi.fn().mockResolvedValue(cfg()),
    updateConfig: vi.fn().mockImplementation(async payload => cfg({ ...payload, enabled: payload.enabled ?? false })),
    getAuditLog: vi.fn().mockResolvedValue({ items: [] }),
    emergencyStop: vi.fn().mockResolvedValue(cfg({ emergencyStopped: true })),
    ...overrides,
  }
}

it('walks the enable wizard and writes the new config', async () => {
  const updateConfig = vi.fn().mockResolvedValue(cfg({ enabled: true, securityLevel: 'strict', armedUntil: now }))
  const user = userEvent.setup()
  render(<ComputerPanel bridge={api({ updateConfig })} />)
  expect(await screen.findByText('已停用')).toBeInTheDocument()
  await user.click(screen.getByRole('button', { name: '三步启用…' }))
  expect(screen.getByRole('button', { name: '下一步：选择安全级别' })).toBeDisabled()
  await user.click(screen.getByLabelText('已了解风险'))
  await user.click(screen.getByRole('button', { name: '下一步：选择安全级别' }))
  await user.click(screen.getByRole('radio', { name: /严格/ }))
  await user.click(screen.getByRole('button', { name: '确认启用' }))
  await waitFor(() => expect(updateConfig).toHaveBeenCalledWith({
    enabled: true, securityLevel: 'strict', allowCritical: false, armMinutes: 30,
  }))
  expect(await screen.findByText('电脑控制已启用')).toBeInTheDocument()
  expect(screen.getByText('已启用')).toBeInTheDocument()
})

it('adds a process blocklist entry and shows the updated chip', async () => {
  const current = cfg({ enabled: true, processBlocklist: ['cmd.exe'] })
  const updateConfig = vi.fn().mockImplementation(async payload => ({ ...current, ...payload, processBlocklist: payload.processBlocklist ?? current.processBlocklist }))
  const user = userEvent.setup()
  render(<ComputerPanel bridge={api({ getConfig: vi.fn().mockResolvedValue(current), updateConfig })} />)
  expect(await screen.findByText('cmd.exe')).toBeInTheDocument()
  await user.type(screen.getByLabelText('黑名单新条目'), 'taskmgr.exe')
  await user.click(screen.getByRole('button', { name: '添加' }))
  await waitFor(() => expect(updateConfig).toHaveBeenCalledWith({ processBlocklist: ['cmd.exe', 'taskmgr.exe'] }))
  expect(await screen.findByText('taskmgr.exe')).toBeInTheDocument()
  expect(screen.getByText(/已添加 taskmgr.exe/)).toBeInTheDocument()
})

it('rejects a blocklist path and keeps the previous list', async () => {
  const current = cfg({ enabled: true, processBlocklist: ['cmd.exe'] })
  const updateConfig = vi.fn()
  const user = userEvent.setup()
  render(<ComputerPanel bridge={api({ getConfig: vi.fn().mockResolvedValue(current), updateConfig })} />)
  await screen.findByText('cmd.exe')
  await user.type(screen.getByLabelText('黑名单新条目'), 'C:\\Windows\\cmd.exe')
  await user.click(screen.getByRole('button', { name: '添加' }))
  expect(screen.getByText(/不能含路径分隔符/)).toBeInTheDocument()
  expect(updateConfig).not.toHaveBeenCalled()
})
