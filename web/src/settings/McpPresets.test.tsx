import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterEach, expect, it, vi } from 'vitest'
import type { McpBridge } from '../bridge/client'
import { McpPresetsSection } from './SettingsPage'

afterEach(cleanup)

function api(overrides: Partial<McpBridge> = {}): McpBridge {
  return {
    list: vi.fn().mockResolvedValue({ endpoints: [] }),
    add: vi.fn(),
    toggle: vi.fn(),
    health: vi.fn(),
    marketSearch: vi.fn(),
    presets: vi.fn().mockResolvedValue({ items: [] }),
    ...overrides,
  }
}

it('points to the MCP page instead of registering presets in settings', async () => {
  const onOpenMcp = vi.fn()
  const bridge = api()
  render(<McpPresetsSection bridge={bridge} onOpenMcp={onOpenMcp} />)
  expect(await screen.findByText(/设置里不再安装第二套/)).toBeInTheDocument()
  expect(screen.queryByRole('button', { name: '注册 Everything' })).not.toBeInTheDocument()
  expect(screen.queryByRole('button', { name: '一键启用推荐套件' })).not.toBeInTheDocument()
  fireEvent.click(screen.getByRole('button', { name: '去 MCP 页' }))
  expect(onOpenMcp).toHaveBeenCalledOnce()
  expect(bridge.add).not.toHaveBeenCalled()
  expect(bridge.presets).not.toHaveBeenCalled()
})

it('marks leftover archived endpoints and still links to the MCP page', async () => {
  const onOpenMcp = vi.fn()
  const bridge = api({
    list: vi.fn().mockResolvedValue({
      endpoints: [{
        endpointId: 'mcp-old',
        transport: 'stdio' as const,
        state: 'ready' as const,
        enabled: true,
        args: ['-y', '@modelcontextprotocol/server-github'],
      }],
    }),
  })
  render(<McpPresetsSection bridge={bridge} onOpenMcp={onOpenMcp} />)
  expect(await screen.findByRole('status')).toHaveTextContent(/已下架 MCP/)
  expect(screen.getByRole('status')).toHaveTextContent(/GitHub/)
  fireEvent.click(screen.getByRole('button', { name: '去 MCP 页' }))
  expect(onOpenMcp).toHaveBeenCalledOnce()
})

it('hides leftover notice after MCP-page uninstall leaves only revoked rows', async () => {
  const bridge = api({
    list: vi.fn().mockResolvedValue({
      endpoints: [{
        endpointId: 'mcp-old',
        transport: 'stdio' as const,
        state: 'revoked' as const,
        enabled: false,
        args: ['-y', '@modelcontextprotocol/server-github'],
      }],
    }),
  })
  render(<McpPresetsSection bridge={bridge} onOpenMcp={vi.fn()} />)
  expect(await screen.findByText(/设置里不再安装第二套/)).toBeInTheDocument()
  await waitFor(() => expect(bridge.list).toHaveBeenCalled())
  expect(screen.queryByText(/已下架 MCP/)).not.toBeInTheDocument()
})
