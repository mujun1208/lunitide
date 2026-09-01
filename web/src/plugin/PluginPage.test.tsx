import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterEach, expect, it, vi } from 'vitest'
import type { McpBridge, PluginBridge, SkillBridge } from '../bridge/client'
import type { PluginListResult } from '../generated/bridge'
import { PluginPage } from './PluginPage'
import { PACK_LEDGER_KEY } from './capabilityPacks'

afterEach(() => {
  cleanup()
  localStorage.removeItem(PACK_LEDGER_KEY)
})

type Plugin = PluginListResult['plugins'][number]
const now = '2026-01-01T00:00:00Z'
const webSearch: Plugin = {
  installId: '01ARZ3NDEKTSV4RRFFQ69G5FAA',
  pluginId: 'web-search',
  semver: '1.0.0',
  publisher: 'lunitide',
  kind: 'tool',
  origin: 'local',
  state: 'disabled',
  bindingCount: 0,
  installedAt: now,
}
const filler: Plugin = {
  ...webSearch,
  installId: '01ARZ3NDEKTSV4RRFFQ69G5FAB',
  pluginId: 'tool-1',
  state: 'enabled',
}
const failed: Plugin = {
  ...webSearch,
  installId: '01ARZ3NDEKTSV4RRFFQ69G5FAC',
  pluginId: 'git',
  kind: 'tool',
  state: 'quarantined',
}

function api(overrides: Partial<PluginBridge> = {}): PluginBridge {
  return {
    list: vi.fn().mockResolvedValue({ plugins: [webSearch, filler, failed] }),
    install: vi.fn(),
    toggle: vi.fn().mockResolvedValue({ installId: webSearch.installId, state: 'enabled', bindings: [] }),
    uninstall: vi.fn().mockResolvedValue({ installId: webSearch.installId, state: 'uninstalled', revokedBindings: 0, tombstoneId: '01ARZ3NDEKTSV4RRFFQ69G5FAD' }),
    confirmToken: vi.fn().mockResolvedValue({ confirmToken: 'a'.repeat(64), expiresAt: now }),
    upgrade: vi.fn(),
    marketSearch: vi.fn(),
    marketDetail: vi.fn(),
    devCreate: vi.fn().mockResolvedValue({ bundleId: '01ARZ3NDEKTSV4RRFFQ69G5FAE', state: 'verified' }),
    ...overrides,
  }
}

it('renders the plugin market and enables a catalog card', async () => {
  const bridge = api()
  render(<PluginPage bridge={bridge} />)
  expect(await screen.findByRole('heading', { name: '能力包' })).toBeInTheDocument()
  expect(await screen.findByText('网页搜索')).toBeInTheDocument()
  expect(screen.getAllByText(/不会执行外部脚本/).length).toBeGreaterThan(0)
  expect(screen.getByText(/本机捆绑目录/)).toBeInTheDocument()
  expect(screen.getByText(/不是在线商店/)).toBeInTheDocument()
  expect(screen.getByText(/要连服务器去 MCP/)).toBeInTheDocument()
  expect(screen.getByRole('button', { name: '安装 浏览器工作包' })).toBeInTheDocument()
  fireEvent.click(screen.getByRole('button', { name: '启用 网页搜索' }))
  await waitFor(() => expect(bridge.toggle).toHaveBeenCalledWith({ installId: webSearch.installId, enabled: true }))
  expect(await screen.findByRole('status')).toHaveTextContent('已启用「网页搜索」')
})

it('marks enabled roster cards as built-in tools', async () => {
  const bridge = api({
    list: vi.fn().mockResolvedValue({ plugins: [{ ...webSearch, state: 'enabled' }, filler, failed] }),
  })
  render(<PluginPage bridge={bridge} />)
  expect(await screen.findByText('已是内置工具')).toBeInTheDocument()
})

it('lists installed plugins with status and hides filler roster rows', async () => {
  const bridge = api()
  render(<PluginPage bridge={bridge} />)
  fireEvent.click(await screen.findByRole('tab', { name: /已安装/ }))
  expect(await screen.findByText('网页搜索')).toBeInTheDocument()
  expect(screen.getByText('未启用')).toBeInTheDocument()
  expect(screen.getByText('Git')).toBeInTheDocument()
  expect(screen.getByText('安装失败')).toBeInTheDocument()
  expect(screen.getAllByText(/内置开关/).length).toBeGreaterThan(0)
  expect(screen.queryByText('tool-1')).not.toBeInTheDocument()
})

it('deletes an installed plugin after confirm', async () => {
  const bridge = api()
  render(<PluginPage bridge={bridge} />)
  fireEvent.click(await screen.findByRole('tab', { name: /已安装/ }))
  fireEvent.click((await screen.findAllByRole('button', { name: '删除' }))[0])
  fireEvent.click(await screen.findByRole('button', { name: '确认删除' }))
  await waitFor(() => expect(bridge.uninstall).toHaveBeenCalledOnce())
  const payload = vi.mocked(bridge.uninstall).mock.calls[0][0]
  expect(payload.installId).toBe(webSearch.installId)
  expect(payload.confirmToken).toMatch(/^[0-9a-f]{64}$/)
})

it('creates a plugin from a pasted Harness manifest', async () => {
  const bridge = api()
  render(<PluginPage bridge={bridge} />)
  fireEvent.click(await screen.findByRole('button', { name: '手动填写' }))
  expect(await screen.findByRole('dialog', { name: '手动创建能力包' })).toBeInTheDocument()
  fireEvent.click(screen.getByRole('button', { name: '保存' }))
  await waitFor(() => expect(bridge.devCreate).toHaveBeenCalledOnce())
  expect(vi.mocked(bridge.devCreate).mock.calls[0][0]).toMatchObject({
    workspaceId: 'chat',
    entrypoint: 'pack://manifest',
  })
})

it('lists a chat-created pack from plugin.list without localStorage', async () => {
  const packRow: Plugin = {
    ...webSearch,
    installId: '01ARZ3NDEKTSV4RRFFQ69G5FAH',
    pluginId: 'pack-ppt',
    kind: 'workflow',
    state: 'enabled',
  }
  render(<PluginPage bridge={api({ list: vi.fn().mockResolvedValue({ plugins: [packRow] }) })} />)
  fireEvent.click(await screen.findByRole('tab', { name: /已安装/ }))
  expect(await screen.findByText('演示文稿包')).toBeInTheDocument()
})

it('removes a capability pack without uninstalling skills', async () => {
  localStorage.setItem(PACK_LEDGER_KEY, JSON.stringify([{
    packId: 'pack-browser',
    addedMcpEndpointIds: ['01ARZ3NDEKTSV4RRFFQ69G5FAE'],
    enabledGateInstallIds: ['01ARZ3NDEKTSV4RRFFQ69G5FAF'],
  }]))
  const mcp = { presets: vi.fn(), list: vi.fn(), add: vi.fn(), toggle: vi.fn().mockResolvedValue({}), health: vi.fn(), marketSearch: vi.fn() } as unknown as McpBridge
  const skills = { delete: vi.fn() } as unknown as SkillBridge
  render(<PluginPage bridge={api()} mcp={mcp} skills={skills} />)
  fireEvent.click(await screen.findByRole('tab', { name: /已安装/ }))
  expect(await screen.findByText('浏览器工作包')).toBeInTheDocument()
  fireEvent.click(screen.getAllByRole('button', { name: '删除' })[0])
  expect(await screen.findByRole('dialog', { name: '撤下「浏览器工作包」' })).toBeInTheDocument()
  fireEvent.click(screen.getByRole('button', { name: '确认删除' }))
  await waitFor(() => expect(mcp.toggle).toHaveBeenCalledWith({ endpointId: '01ARZ3NDEKTSV4RRFFQ69G5FAE', enabled: false }))
  expect(skills.delete).not.toHaveBeenCalled()
})

it('starts chat-based plugin creation', async () => {
  const onCreateInChat = vi.fn()
  render(<PluginPage bridge={api()} onCreateInChat={onCreateInChat} />)
  fireEvent.click(await screen.findByRole('button', { name: '＋ 创建能力包' }))
  expect(onCreateInChat).toHaveBeenCalledOnce()
})
