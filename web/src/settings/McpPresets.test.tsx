import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterEach, expect, it, vi } from 'vitest'
import { BridgeClientError, type McpBridge } from '../bridge/client'
import { McpPresetsSection } from './SettingsPage'

afterEach(cleanup)

const catalog = [
  { id: 'everything', name: 'Everything', description: '官方测试服务器', transport: 'stdio' as const, command: 'npx' as const, args: ['-y', '@modelcontextprotocol/server-everything'], needsArgs: false, category: '测试' },
  { id: 'filesystem', name: 'Filesystem', description: '读写指定目录内的文件', transport: 'stdio' as const, command: 'npx' as const, args: ['-y', '@modelcontextprotocol/server-filesystem', '{{dir}}'], needsArgs: true, argPlaceholder: '{{dir}}', argHint: '要挂载的目录绝对路径', category: '文件' },
]

function api(overrides: Partial<McpBridge> = {}): McpBridge {
  return {
    list: vi.fn(),
    add: vi.fn().mockResolvedValue({ endpointId: 'mcp-1', state: 'probe' }),
    toggle: vi.fn().mockResolvedValue({ endpointId: 'mcp-1', enabled: true, state: 'probe' }),
    health: vi.fn(),
    marketSearch: vi.fn(),
    presets: vi.fn().mockResolvedValue({ items: catalog }),
    ...overrides,
  }
}

it('renders the preset catalog cards with name, description and command', async () => {
  const bridge = api()
  render(<McpPresetsSection bridge={bridge} />)
  expect(await screen.findByText('Everything · 测试')).toBeInTheDocument()
  expect(screen.getByText('Filesystem · 文件 · 需补充参数')).toBeInTheDocument()
  expect(screen.getByText(/npx -y @modelcontextprotocol\/server-everything/)).toBeInTheDocument()
  expect(screen.getByRole('button', { name: '注册 Everything' })).toBeInTheDocument()
  expect(bridge.presets).toHaveBeenCalledOnce()
})

it('registers a no-args preset with one click through mcp.add', async () => {
  const bridge = api()
  render(<McpPresetsSection bridge={bridge} />)
  await screen.findByText('Everything · 测试')
  fireEvent.click(screen.getByRole('button', { name: '注册 Everything' }))
  await waitFor(() => expect(bridge.add).toHaveBeenCalledOnce())
  const payload = vi.mocked(bridge.add).mock.calls[0][0]
  expect(payload).toMatchObject({
    origin: 'manual',
    transport: 'stdio',
    command: 'npx',
    args: ['-y', '@modelcontextprotocol/server-everything'],
    riskConfirmed: true,
  })
  expect(typeof payload.requestId).toBe('string')
  await waitFor(() => expect(bridge.toggle).toHaveBeenCalledOnce())
  expect(vi.mocked(bridge.toggle).mock.calls[0][0]).toMatchObject({ endpointId: 'mcp-1', enabled: true })
  expect(await screen.findByRole('status')).toHaveTextContent('已启用 Everything，对话里可直接调用其工具。')
})

it('collects the placeholder value, normalizes windows separators and then registers', async () => {
  const bridge = api()
  render(<McpPresetsSection bridge={bridge} />)
  await screen.findByText('Filesystem · 文件 · 需补充参数')
  // 第一击不注册，先展开参数输入
  fireEvent.click(screen.getByRole('button', { name: '注册 Filesystem' }))
  expect(bridge.add).not.toHaveBeenCalled()
  const input = await screen.findByLabelText('Filesystem 参数')
  expect(screen.getByRole('button', { name: '确认注册 Filesystem' })).toBeDisabled()
  fireEvent.change(input, { target: { value: 'E:\\proj\\demo repo' } })
  fireEvent.click(screen.getByRole('button', { name: '确认注册 Filesystem' }))
  await waitFor(() => expect(bridge.add).toHaveBeenCalledOnce())
  const payload = vi.mocked(bridge.add).mock.calls[0][0]
  // 反斜杠归一为正斜杠：stdio 白名单拒绝元字符 \
  expect(payload.args).toEqual(['-y', '@modelcontextprotocol/server-filesystem', 'E:/proj/demo repo'])
})

it('surfaces registration failures without losing the section', async () => {
  const bridge = api({ add: vi.fn().mockRejectedValue(new BridgeClientError('M7-MCP-001: stdio needs command+args', 'BRIDGE_SCHEMA_INVALID', false, 'trace')) })
  render(<McpPresetsSection bridge={bridge} />)
  await screen.findByText('Everything · 测试')
  fireEvent.click(screen.getByRole('button', { name: '注册 Everything' }))
  expect(await screen.findByRole('status')).toHaveTextContent('M7-MCP-001')
  expect(screen.getByRole('button', { name: '注册 Everything' })).toBeInTheDocument()
})
