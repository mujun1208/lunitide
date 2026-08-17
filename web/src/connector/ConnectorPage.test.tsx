import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterEach, expect, it, vi } from 'vitest'
import type { McBridge } from '../bridge/client'
import { ConnectorPage } from './ConnectorPage'

afterEach(cleanup)
const digest = '0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef'
const now = '2026-01-01T00:00:00Z'
const marketItem = {
  id: '01ARZ3NDEKTSV4RRFFQ69G5FA1', name: '示例 MCP 服务器', publisher: 'lunitide',
  description: '演示用市场条目', transportHint: 'https' as const, catalogDigest: digest, fetchedAt: now,
}
const usageStat = {
  endpointId: 'mcp-01ARZ3NDEKTSV4RRFFQ69G5FA2', installs: 1, updates: 0, uninstalls: 0,
  transport: 'https' as const, state: 'ready' as const, origin: 'market' as const, enabled: true,
}
const passingChecks = [
  { rule: 'MC-VR-01' as const, passed: true }, { rule: 'MC-VR-04' as const, passed: true },
  { rule: 'MC-VR-05' as const, passed: true }, { rule: 'MC-VR-07' as const, passed: true },
  { rule: 'MC-VR-08' as const, passed: true },
]
const mcApi = (o: Partial<McBridge> = {}): McBridge => ({
  marketList: vi.fn().mockResolvedValue({ items: [marketItem], fresh: true }),
  marketDetail: vi.fn().mockResolvedValue({
    item: marketItem,
    config: { transport: 'https', url: 'https://mcp.example.com/sse' },
    checks: passingChecks,
  }),
  configValidate: vi.fn().mockResolvedValue({ valid: true, checks: passingChecks }),
  confirmToken: vi.fn().mockResolvedValue({ confirmToken: digest, expiresAt: now }),
  install: vi.fn().mockResolvedValue({ endpointId: 'mcp-01ARZ3NDEKTSV4RRFFQ69G5FA3', state: 'ready' as const, checks: passingChecks }),
  uninstall: vi.fn().mockResolvedValue({ endpointId: usageStat.endpointId, state: 'revoked' as const }),
  update: vi.fn(),
  usage: vi.fn().mockResolvedValue({ stats: [usageStat] }),
  tombstoneCheck: vi.fn().mockResolvedValue({ fresh: true, revoked: [], drifted: [] }),
  ...o,
})

it('renders the market catalog and opens the validation-chain detail dialog', async () => {
  const bridge = mcApi()
  render(<ConnectorPage bridge={bridge} />)
  expect(await screen.findByText('示例 MCP 服务器')).toBeInTheDocument()
  expect(bridge.marketList).toHaveBeenCalled()
  fireEvent.click(screen.getByRole('button', { name: '查看并安装' }))
  expect(await screen.findByRole('dialog', { name: '市场条目 示例 MCP 服务器' })).toBeInTheDocument()
  expect(screen.getByText('服务端校验链')).toBeInTheDocument()
  expect(screen.getAllByText(/✓/).length).toBeGreaterThan(0)
})

it('installs through the two-step confirmation-token flow', async () => {
  const bridge = mcApi()
  render(<ConnectorPage bridge={bridge} />)
  await screen.findByText('示例 MCP 服务器')
  fireEvent.click(screen.getByRole('button', { name: '查看并安装' }))
  await screen.findByRole('dialog', { name: '市场条目 示例 MCP 服务器' })
  fireEvent.click(screen.getByRole('button', { name: '申请令牌并安装' }))
  const confirm = await screen.findByRole('dialog', { name: '确认安装连接器' })
  expect(confirm).toBeInTheDocument()
  expect(bridge.confirmToken).toHaveBeenCalledWith({ method: 'mc.connector.install', target: marketItem.id, digest: expect.any(String) })
  fireEvent.click(screen.getByRole('button', { name: '确认安装' }))
  await waitFor(() => expect(bridge.install).toHaveBeenCalledOnce())
  expect(vi.mocked(bridge.install).mock.calls[0][0]).toMatchObject({
    origin: 'market', marketItemId: marketItem.id, confirmToken: digest, requestId: expect.any(String),
  })
})

it('runs the manual-config validation chain before enabling install', async () => {
  const bridge = mcApi({ configValidate: vi.fn().mockResolvedValue({ valid: false, checks: [{ rule: 'MC-VR-05' as const, passed: false, reason: 'private host' }] }) })
  render(<ConnectorPage bridge={bridge} />)
  await screen.findByText('示例 MCP 服务器')
  fireEvent.click(screen.getByRole('tab', { name: /手动配置/ }))
  fireEvent.click(screen.getByRole('button', { name: '运行校验链' }))
  expect(await screen.findByText(/配置未通过校验链/)).toBeInTheDocument()
  expect(screen.getByText(/SSRF 防护/)).toBeInTheDocument()
  expect(screen.queryByRole('button', { name: '申请令牌并安装' })).toBeNull()
  expect(bridge.confirmToken).not.toHaveBeenCalled()
})

it('lists installed endpoints and uninstalls after confirmation', async () => {
  const bridge = mcApi()
  render(<ConnectorPage bridge={bridge} />)
  await screen.findByText('示例 MCP 服务器')
  fireEvent.click(screen.getByRole('tab', { name: /已安装/ }))
  expect(await screen.findByText(usageStat.endpointId)).toBeInTheDocument()
  fireEvent.click(screen.getByRole('button', { name: '卸载…' }))
  await screen.findByRole('dialog', { name: '确认卸载连接器' })
  fireEvent.click(screen.getByRole('button', { name: '签发令牌并卸载' }))
  await waitFor(() => expect(bridge.uninstall).toHaveBeenCalledOnce())
  expect(vi.mocked(bridge.uninstall).mock.calls[0][0]).toMatchObject({ endpointId: usageStat.endpointId, confirmToken: digest })
})

it('surfaces revoked market items from the tombstone report', async () => {
  const bridge = mcApi({ tombstoneCheck: vi.fn().mockResolvedValue({ fresh: false, revoked: [{ marketItemId: marketItem.id, name: marketItem.name, endpointIds: [usageStat.endpointId] }], drifted: [] }) })
  render(<ConnectorPage bridge={bridge} />)
  await screen.findByText('示例 MCP 服务器')
  fireEvent.click(screen.getByRole('button', { name: '墓碑检测' }))
  fireEvent.click(screen.getByRole('tab', { name: /已安装/ }))
  expect(await screen.findByText(/发现 1 个已撤销条目/)).toBeInTheDocument()
})
