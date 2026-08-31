import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterEach, expect, it, vi } from 'vitest'
import { mcBridge, type McpBridge } from '../bridge/client'
import { McpPage, __parseManualJsonForTest } from './McpPage'

afterEach(cleanup)

const catalog = [
  { id: 'everything', name: 'Everything', description: '官方测试服务器', transport: 'stdio' as const, command: 'npx' as const, args: ['-y', '@modelcontextprotocol/server-everything'], needsArgs: false, category: '测试' },
  { id: 'filesystem', name: 'Filesystem', description: '读写指定目录内的文件', transport: 'stdio' as const, command: 'npx' as const, args: ['-y', '@modelcontextprotocol/server-filesystem', '{{dir}}'], needsArgs: true, argPlaceholder: '{{dir}}', argHint: '要挂载的目录绝对路径', category: '文件' },
]

const memoryEndpoint = {
  endpointId: 'mcp-01ARZ3NDEKTSV4RRFFQ69G5FAA',
  transport: 'stdio' as const,
  state: 'ready' as const,
  enabled: true,
  origin: 'manual' as const,
  displayName: 'Memory',
  command: 'npx',
  args: ['-y', '@modelcontextprotocol/server-memory'],
}

function api(overrides: Partial<McpBridge> = {}): McpBridge {
  return {
    list: vi.fn().mockResolvedValue({ endpoints: [] }),
    add: vi.fn().mockResolvedValue({ endpointId: 'mcp-1', state: 'probe' }),
    toggle: vi.fn().mockResolvedValue({ endpointId: 'mcp-1', enabled: true, state: 'probe' }),
    health: vi.fn().mockResolvedValue({ state: 'ready', driftDetected: false, checkedAt: '2026-01-01T00:00:00Z', latencyMs: 12 }),
    marketSearch: vi.fn(),
    presets: vi.fn().mockResolvedValue({ items: catalog }),
    ...overrides,
  }
}

it('renders MCP market cards and installs with plus', async () => {
  const bridge = api()
  render(<McpPage bridge={bridge} />)
  expect(await screen.findByRole('heading', { name: 'MCP' })).toBeInTheDocument()
  expect(await screen.findByText('Everything')).toBeInTheDocument()
  fireEvent.click(screen.getByRole('button', { name: '安装 Everything' }))
  await waitFor(() => expect(bridge.add).toHaveBeenCalledOnce())
  expect(vi.mocked(bridge.add).mock.calls[0][0]).toMatchObject({
    origin: 'manual',
    transport: 'stdio',
    command: 'npx',
    args: ['-y', '@modelcontextprotocol/server-everything'],
    riskConfirmed: true,
  })
  await waitFor(() => expect(bridge.toggle).toHaveBeenCalledWith({ endpointId: 'mcp-1', enabled: true }))
  expect(await screen.findByRole('status')).toHaveTextContent('已安装「Everything」')
})

it('installs needsArgs presets with argDefault without a path picker', async () => {
  const bridge = api({
    presets: vi.fn().mockResolvedValue({
      items: [{
        ...catalog[1],
        argDefault: 'C:/Users/demo/AppData/Local/Lunitide/mcp/filesystem',
        argHint: '月汐会使用本机数据目录，无需手动填写',
      }],
    }),
  })
  render(<McpPage bridge={bridge} />)
  await screen.findByText('Filesystem')
  fireEvent.click(screen.getByRole('button', { name: '安装 Filesystem' }))
  await waitFor(() => expect(bridge.add).toHaveBeenCalledOnce())
  expect(vi.mocked(bridge.add).mock.calls[0][0].args).toEqual(['-y', '@modelcontextprotocol/server-filesystem', 'C:/Users/demo/AppData/Local/Lunitide/mcp/filesystem'])
  expect(screen.queryByLabelText('Filesystem 参数')).not.toBeInTheDocument()
})

it('expands placeholder input before installing a needsArgs preset', async () => {
  const bridge = api()
  render(<McpPage bridge={bridge} />)
  await screen.findByText('Filesystem')
  fireEvent.click(screen.getByRole('button', { name: '安装 Filesystem' }))
  expect(bridge.add).not.toHaveBeenCalled()
  fireEvent.change(await screen.findByLabelText('Filesystem 参数'), { target: { value: 'E:\\proj\\demo' } })
  fireEvent.click(screen.getByRole('button', { name: '安装' }))
  await waitFor(() => expect(bridge.add).toHaveBeenCalledOnce())
  expect(vi.mocked(bridge.add).mock.calls[0][0].args).toEqual(['-y', '@modelcontextprotocol/server-filesystem', 'E:/proj/demo'])
})

it('labels a curated install with its preset id', async () => {
  const bridge = api({
    presets: vi.fn().mockResolvedValue({
      items: [{
        id: 'playwright', name: 'Playwright', description: '浏览器自动化',
        transport: 'stdio' as const, command: 'npx' as const, args: ['-y', '@playwright/mcp'],
        needsArgs: false, category: '浏览器',
      }],
    }),
    list: vi.fn().mockResolvedValue({
      endpoints: [{
        ...memoryEndpoint,
        displayName: 'Playwright',
        args: ['-y', '@playwright/mcp'],
      }],
    }),
  })
  render(<McpPage bridge={bridge} />)
  fireEvent.click(await screen.findByRole('tab', { name: /已安装/ }))
  expect(await screen.findByText(/策展预置 playwright/)).toBeInTheDocument()
})

it('shows installed display names and connection status', async () => {
  const bridge = api({ list: vi.fn().mockResolvedValue({ endpoints: [memoryEndpoint] }) })
  render(<McpPage bridge={bridge} />)
  fireEvent.click(await screen.findByRole('tab', { name: '已安装（1）' }))
  expect(await screen.findByText('Memory')).toBeInTheDocument()
  expect(screen.getByText('已连接')).toBeInTheDocument()
  expect(screen.queryByText(memoryEndpoint.endpointId)).not.toBeInTheDocument()
})

it('reconnects an installed MCP', async () => {
  const bridge = api({
    list: vi.fn().mockResolvedValue({ endpoints: [{ ...memoryEndpoint, enabled: false, state: 'degraded' }] }),
  })
  render(<McpPage bridge={bridge} />)
  fireEvent.click(await screen.findByRole('tab', { name: /已安装/ }))
  await screen.findByText('Memory')
  fireEvent.click(screen.getByRole('button', { name: '重新连接' }))
  await waitFor(() => expect(bridge.toggle).toHaveBeenCalledWith({ endpointId: memoryEndpoint.endpointId, enabled: true }))
  await waitFor(() => expect(bridge.health).toHaveBeenCalledWith({ endpointId: memoryEndpoint.endpointId }))
})

it('deletes an installed MCP after confirm', async () => {
  const confirmToken = vi.spyOn(mcBridge, 'confirmToken').mockResolvedValue({ confirmToken: 'a'.repeat(64), expiresAt: '2026-01-01T00:00:00Z' })
  const uninstall = vi.spyOn(mcBridge, 'uninstall').mockResolvedValue({ endpointId: memoryEndpoint.endpointId, state: 'revoked' })
  const bridge = api({ list: vi.fn().mockResolvedValue({ endpoints: [memoryEndpoint] }) })
  render(<McpPage bridge={bridge} />)
  fireEvent.click(await screen.findByRole('tab', { name: /已安装/ }))
  fireEvent.click(await screen.findByRole('button', { name: '删除' }))
  fireEvent.click(await screen.findByRole('button', { name: '确认删除' }))
  await waitFor(() => expect(uninstall).toHaveBeenCalledOnce())
  expect(confirmToken).toHaveBeenCalledWith({ method: 'mc.connector.uninstall', target: memoryEndpoint.endpointId })
  confirmToken.mockRestore()
  uninstall.mockRestore()
})

it('saves Cursor-style mcpServers JSON from the create dialog', async () => {
  const bridge = api()
  render(<McpPage bridge={bridge} />)
  fireEvent.click(await screen.findByRole('button', { name: '＋ 创建 MCP' }))
  const dialog = await screen.findByRole('dialog', { name: '创建 MCP' })
  fireEvent.change(screen.getByLabelText('MCP JSON'), { target: { value: '{"mcpServers":{"memory":{"command":"npx","args":["-y","@modelcontextprotocol/server-memory"]}}}' } })
  fireEvent.click(dialog.querySelector('input[type="checkbox"]')!)
  fireEvent.click(screen.getByRole('button', { name: '保存' }))
  await waitFor(() => expect(bridge.add).toHaveBeenCalledOnce())
  expect(vi.mocked(bridge.add).mock.calls[0][0]).toMatchObject({
    origin: 'manual',
    transport: 'stdio',
    command: 'npx',
    args: ['-y', '@modelcontextprotocol/server-memory'],
  })
})

it('warns that Chrome attach presets are not default computer control', async () => {
  const bridge = api({
    presets: vi.fn().mockResolvedValue({
      items: [{
        id: 'chrome-devtools', name: 'Chrome DevTools', description: '官方 Chrome DevTools MCP',
        transport: 'stdio' as const, command: 'npx' as const, args: ['-y', 'chrome-devtools-mcp'],
        needsArgs: false, category: '浏览器',
      }],
    }),
  })
  render(<McpPage bridge={bridge} />)
  expect(await screen.findByText(/不是默认电脑控制/)).toBeInTheDocument()
  expect(screen.getByText(/月伴不会自动安装/)).toBeInTheDocument()
})

it('parses both mcpServers maps and single command entries', () => {
  expect(__parseManualJsonForTest('{"mcpServers":{"a":{"command":"npx","args":["-y","pkg"]}}}')).toEqual([
    { name: 'a', transport: 'stdio', command: 'npx', args: ['-y', 'pkg'], url: undefined },
  ])
  expect(__parseManualJsonForTest('{"url":"https://example.test/mcp"}')).toEqual([
    { name: 'manual', transport: 'https', command: undefined, args: undefined, url: 'https://example.test/mcp' },
  ])
})
