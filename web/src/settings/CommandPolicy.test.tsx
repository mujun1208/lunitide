import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterEach, expect, it, vi } from 'vitest'
import { BridgeClientError, type ToolsPolicyBridge } from '../bridge/client'
import { CommandPolicyPanel } from './SettingsPage'

afterEach(cleanup)

function api(overrides: Partial<ToolsPolicyBridge> = {}): ToolsPolicyBridge {
  return {
    getCommandPolicy: vi.fn().mockResolvedValue({ commands: [{ prefix: ['node', '--version'], maxArgs: 2, timeoutMs: 15000 }] }),
    setCommandPolicy: vi.fn().mockResolvedValue({ applied: 1 }),
    ...overrides,
  }
}

it('loads the persisted whitelist and renders entries as editable rows', async () => {
  const bridge = api()
  render(<CommandPolicyPanel bridge={bridge} />)
  expect(await screen.findByLabelText('规则 1 命令前缀')).toHaveValue('node --version')
  expect(screen.getByLabelText('规则 1 最大参数数')).toHaveValue(2)
  expect(screen.getByLabelText('规则 1 超时毫秒')).toHaveValue(15000)
  expect(screen.getByText('共 1 条用户规则（不含内置 git/go 只读集）')).toBeInTheDocument()
  expect(bridge.getCommandPolicy).toHaveBeenCalledOnce()
})

it('normalizes whitespace prefixes and posts the exact fail-closed document', async () => {
  const bridge = api({ getCommandPolicy: vi.fn().mockResolvedValue({ commands: [] }) })
  render(<CommandPolicyPanel bridge={bridge} />)
  await screen.findByText('共 0 条用户规则（不含内置 git/go 只读集）')
  fireEvent.click(screen.getByRole('button', { name: '添加规则' }))
  fireEvent.change(screen.getByLabelText('规则 1 命令前缀'), { target: { value: '  python   --version  ' } })
  fireEvent.change(screen.getByLabelText('规则 1 最大参数数'), { target: { value: '3' } })
  fireEvent.change(screen.getByLabelText('规则 1 超时毫秒'), { target: { value: '20000' } })
  fireEvent.click(screen.getByRole('button', { name: '保存并热生效' }))
  await waitFor(() => expect(bridge.setCommandPolicy).toHaveBeenCalledOnce())
  expect(vi.mocked(bridge.setCommandPolicy).mock.calls[0][0]).toEqual({
    commands: [{ prefix: ['python', '--version'], maxArgs: 3, timeoutMs: 20000 }],
  })
  expect(await screen.findByRole('status')).toHaveTextContent('已保存并热生效：1 条用户规则')
})

it('drops blank rows instead of sending empty prefixes', async () => {
  const bridge = api({ getCommandPolicy: vi.fn().mockResolvedValue({ commands: [] }) })
  render(<CommandPolicyPanel bridge={bridge} />)
  await screen.findByText('共 0 条用户规则（不含内置 git/go 只读集）')
  fireEvent.click(screen.getByRole('button', { name: '添加规则' }))
  fireEvent.click(screen.getByRole('button', { name: '添加规则' }))
  fireEvent.change(screen.getByLabelText('规则 1 命令前缀'), { target: { value: 'node --version' } })
  fireEvent.click(screen.getByRole('button', { name: '保存并热生效' }))
  await waitFor(() => expect(bridge.setCommandPolicy).toHaveBeenCalledOnce())
  expect(vi.mocked(bridge.setCommandPolicy).mock.calls[0][0]).toEqual({
    commands: [{ prefix: ['node', '--version'], timeoutMs: 10000 }],
  })
})

it('keeps rows editable and surfaces the rejection reason on rejected documents', async () => {
  const bridge = api({ setCommandPolicy: vi.fn().mockRejectedValue(new BridgeClientError('command-policy.json: invalid prefix item', 'COMMAND_POLICY_INVALID', false, 'trace')) })
  render(<CommandPolicyPanel bridge={bridge} />)
  await screen.findByLabelText('规则 1 命令前缀')
  fireEvent.click(screen.getByRole('button', { name: '保存并热生效' }))
  expect(await screen.findByRole('status')).toHaveTextContent('command-policy.json: invalid prefix item')
  expect(screen.getByLabelText('规则 1 命令前缀')).toHaveValue('node --version')
})

it('surfaces load failures without unlocking save', async () => {
  const bridge = api({ getCommandPolicy: vi.fn().mockRejectedValue(new BridgeClientError('读盘失败', 'STORAGE_UNAVAILABLE', true, 'trace')) })
  render(<CommandPolicyPanel bridge={bridge} />)
  expect(await screen.findByRole('status')).toHaveTextContent('读盘失败')
  expect(screen.getByRole('button', { name: '保存并热生效' })).toBeDisabled()
})
