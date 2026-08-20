import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterEach, expect, it, vi } from 'vitest'
import type { PluginBridge } from '../bridge/client'
import type { PluginListResult } from '../generated/bridge'
import { PluginPage } from './PluginPage'

afterEach(cleanup)

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
  expect(await screen.findByRole('heading', { name: '插件' })).toBeInTheDocument()
  expect(await screen.findByText('网页搜索')).toBeInTheDocument()
  fireEvent.click(screen.getByRole('button', { name: '安装 网页搜索' }))
  await waitFor(() => expect(bridge.toggle).toHaveBeenCalledWith({ installId: webSearch.installId, enabled: true }))
  expect(await screen.findByRole('status')).toHaveTextContent('已安装并启用「网页搜索」')
})

it('lists installed plugins with status and hides filler roster rows', async () => {
  const bridge = api()
  render(<PluginPage bridge={bridge} />)
  fireEvent.click(await screen.findByRole('tab', { name: /已安装/ }))
  expect(await screen.findByText('网页搜索')).toBeInTheDocument()
  expect(screen.getByText('未启用')).toBeInTheDocument()
  expect(screen.getByText('Git')).toBeInTheDocument()
  expect(screen.getByText('安装失败')).toBeInTheDocument()
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
  expect(await screen.findByRole('dialog', { name: '手动创建插件' })).toBeInTheDocument()
  fireEvent.click(screen.getByRole('button', { name: '保存' }))
  await waitFor(() => expect(bridge.devCreate).toHaveBeenCalledOnce())
  expect(vi.mocked(bridge.devCreate).mock.calls[0][0]).toMatchObject({
    workspaceId: 'chat',
    entrypoint: 'plugin/main.ts',
  })
})

it('starts chat-based plugin creation', async () => {
  const onCreateInChat = vi.fn()
  render(<PluginPage bridge={api()} onCreateInChat={onCreateInChat} />)
  fireEvent.click(await screen.findByRole('button', { name: '＋ 创建插件' }))
  expect(onCreateInChat).toHaveBeenCalledOnce()
})
