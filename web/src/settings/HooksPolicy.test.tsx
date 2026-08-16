import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterEach, expect, it, vi } from 'vitest'
import { BridgeClientError, type HooksPolicyBridge } from '../bridge/client'
import { HooksPanel } from './SettingsPage'

afterEach(cleanup)

const sampleEvents = [
  { sessionId: '01ARZ3NDEKTSV4RRFFQ69G5FAV', toolName: 'workspace.write', hookId: 'no-write', event: 'beforeToolCall', decision: 'block', argsDigest: 'a', resultDigest: '', createdAt: '2026-08-16T12:00:00Z' },
]

function api(overrides: Partial<HooksPolicyBridge> = {}): HooksPolicyBridge {
  return {
    getHooksPolicy: vi.fn().mockResolvedValue({ hooks: [{ id: 'no-docx', events: ['beforeToolCall'], tools: ['docx.gen'], decision: 'block', message: '禁止生成 Word 文档' }] }),
    setHooksPolicy: vi.fn().mockResolvedValue({ applied: 1 }),
    listHookEvents: vi.fn().mockResolvedValue({ events: sampleEvents }),
    ...overrides,
  }
}

it('loads persisted rules and recent hook matches', async () => {
  const bridge = api()
  render(<HooksPanel bridge={bridge} />)
  expect(await screen.findByLabelText('规则 1 ID')).toHaveValue('no-docx')
  expect(screen.getByLabelText('规则 1 动作')).toHaveValue('block')
  expect(screen.getByLabelText('规则 1 拒绝原因')).toHaveValue('禁止生成 Word 文档')
  expect(screen.getByLabelText('规则 1 工具 docx.gen')).toBeChecked()
  expect(screen.getByText('共 1 条规则')).toBeInTheDocument()
  expect(screen.getByText(/no-write · workspace.write · beforeToolCall · block/)).toBeInTheDocument()
})

it('adds a rule, selects tools and posts the fail-closed document', async () => {
  const bridge = api({ getHooksPolicy: vi.fn().mockResolvedValue({ hooks: [] }), listHookEvents: vi.fn().mockResolvedValue({ events: [] }) })
  render(<HooksPanel bridge={bridge} />)
  await screen.findByText('共 0 条规则')
  fireEvent.click(screen.getByRole('button', { name: '添加规则' }))
  fireEvent.change(screen.getByLabelText('规则 1 ID'), { target: { value: 'gate-pdf' } })
  fireEvent.change(screen.getByLabelText('规则 1 动作'), { target: { value: 'requireApproval' } })
  fireEvent.click(screen.getByLabelText('规则 1 工具 pdf.gen'))
  fireEvent.click(screen.getByRole('button', { name: '保存并热生效' }))
  await waitFor(() => expect(bridge.setHooksPolicy).toHaveBeenCalledOnce())
  expect(vi.mocked(bridge.setHooksPolicy).mock.calls[0][0]).toEqual({
    hooks: [{ id: 'gate-pdf', events: ['beforeToolCall'], tools: ['pdf.gen'], decision: 'requireApproval', message: '' }],
  })
  expect(await screen.findByRole('status')).toHaveTextContent('已保存并热生效：1 条 Hook 规则')
})

it('drops rows without id or tools instead of sending them', async () => {
  const bridge = api({ getHooksPolicy: vi.fn().mockResolvedValue({ hooks: [] }), listHookEvents: vi.fn().mockResolvedValue({ events: [] }) })
  render(<HooksPanel bridge={bridge} />)
  await screen.findByText('共 0 条规则')
  fireEvent.click(screen.getByRole('button', { name: '添加规则' }))
  fireEvent.change(screen.getByLabelText('规则 1 ID'), { target: { value: 'x' } })
  fireEvent.click(screen.getByRole('button', { name: '添加规则' }))
  fireEvent.click(screen.getByRole('button', { name: '保存并热生效' }))
  await waitFor(() => expect(bridge.setHooksPolicy).toHaveBeenCalledOnce())
  expect(vi.mocked(bridge.setHooksPolicy).mock.calls[0][0]).toEqual({ hooks: [] })
})

it('surfaces the rejection reason and keeps rules editable', async () => {
  const bridge = api({ setHooksPolicy: vi.fn().mockRejectedValue(new BridgeClientError('hooks-policy.json: hook "no-docx" block decision requires a message', 'HOOKS_POLICY_INVALID', false, 'trace')) })
  render(<HooksPanel bridge={bridge} />)
  await screen.findByLabelText('规则 1 ID')
  fireEvent.click(screen.getByRole('button', { name: '保存并热生效' }))
  expect(await screen.findByRole('status')).toHaveTextContent('block decision requires a message')
  expect(screen.getByLabelText('规则 1 ID')).toHaveValue('no-docx')
})

it('surfaces load failures without unlocking save', async () => {
  const bridge = api({ getHooksPolicy: vi.fn().mockRejectedValue(new BridgeClientError('读盘失败', 'STORAGE_UNAVAILABLE', true, 'trace')) })
  render(<HooksPanel bridge={bridge} />)
  expect(await screen.findByRole('status')).toHaveTextContent('读盘失败')
  expect(screen.getByRole('button', { name: '保存并热生效' })).toBeDisabled()
})
