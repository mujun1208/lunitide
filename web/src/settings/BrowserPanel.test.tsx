import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterEach, expect, it, vi } from 'vitest'
import type { BrBridge } from '../bridge/client'
import { BrowserPanel } from './SettingsPage'

afterEach(cleanup)

const now = '2026-01-01T00:00:00Z'

const settingsResult = {
  mode: 'builtin' as const, chromePath: '', edgePath: '', extensionPort: 9222,
  allowlist: ['https://docs.example.com'], dataRetentionDays: 30,
  blockPrivateNetworks: true, updatedAt: now, revision: 1, applyStatus: 'applied' as const, applyError: '',
}

const brApi = (o: Partial<BrBridge> = {}): BrBridge => ({
  getSettings: vi.fn().mockResolvedValue({ ...settingsResult }),
  updateSettings: vi.fn().mockResolvedValue({ ...settingsResult }),
  detectModes: vi.fn().mockResolvedValue({
    builtin: true,
    chrome: { available: true, path: 'C:\\chrome.exe' },
    edge: { available: false },
    extension: { available: false, port: 9222 },
  }),
  connect: vi.fn().mockResolvedValue({ sessionId: 'br-01ARZ3NDEKTSV4RRFFQ6', mode: 'builtin' as const, state: 'connected' as const, updatedAt: now }),
  listSessions: vi.fn().mockResolvedValue({ sessions: [] }),
  disconnect: vi.fn().mockResolvedValue({ sessionId: 'br-01ARZ3NDEKTSV4RRFFQ6', mode: 'builtin' as const, state: 'disconnected' as const, updatedAt: now }),
  navigate: vi.fn(),
  dataUsage: vi.fn().mockResolvedValue({ usage: [] }),
  clearData: vi.fn().mockResolvedValue({ clearedSessions: [], freedBytes: 0 }),
  listPermissions: vi.fn().mockResolvedValue({ permissions: [] }),
  requestPermission: vi.fn(),
  decidePermission: vi.fn().mockResolvedValue({ permissionId: 'brp-1', origin: 'https://x.example.com', permission: 'camera' as const, policy: 'ask' as const, state: 'granted' as const, createdAt: now, decidedAt: now }),
  setPermissionPolicy: vi.fn().mockResolvedValue({ permissionId: 'brp-1', origin: 'https://x.example.com', permission: 'camera' as const, policy: 'allow' as const, state: 'granted' as const, createdAt: now, decidedAt: now }),
  ...o,
})

it('does not show raw English load failures', async () => {
  render(<BrowserPanel bridge={brApi({ getSettings: vi.fn().mockRejectedValue(new Error('Failed to fetch')) })} />)
  expect(await screen.findByText('浏览器设置加载失败')).toBeInTheDocument()
  expect(screen.queryByText('Failed to fetch')).toBeNull()
})

it('renders the five mode cards with the active mode checked', async () => {
  render(<BrowserPanel bridge={brApi()} />)
  const group = await screen.findByRole('radiogroup', { name: '浏览器连接模式' })
  const cards = group.querySelectorAll('.br-mode-card')
  expect(cards).toHaveLength(5)
  // The cards render before the settings request resolves, every one of them
  // unchecked, so waiting for the group only proves the shell mounted. The
  // checked state is the thing that arrives late and the thing to wait for.
  await waitFor(() =>
    expect(screen.getByRole('radio', { name: /内置 WebView2/ })).toHaveAttribute('aria-checked', 'true'),
  )
  expect(screen.getByRole('radio', { name: /每次询问/ })).toHaveAttribute('aria-checked', 'false')
})

it('switches the connection mode through a card', async () => {
  const updateSettings = vi.fn().mockResolvedValue({ ...settingsResult, mode: 'edge' as const })
  render(<BrowserPanel bridge={brApi({ updateSettings })} />)
  await screen.findByRole('radiogroup', { name: '浏览器连接模式' })
  fireEvent.click(screen.getByRole('radio', { name: /Edge/ }))
  await waitFor(() => expect(updateSettings).toHaveBeenCalledWith({ mode: 'edge', expectedRevision: 1 }))
})

it('surfaces mode detection availability', async () => {
  render(<BrowserPanel bridge={brApi()} />)
  expect(await screen.findByText('就绪检查')).toBeInTheDocument()
  expect(screen.getByText(/BROWSER_MCP_NOT_READY/)).toBeInTheDocument()
  expect(screen.getByText(/不是默认电脑控制/)).toBeInTheDocument()
  expect(await screen.findByText(/Chrome 可用/)).toBeInTheDocument()
  fireEvent.click(screen.getByRole('button', { name: '重新探测' }))
  expect(await screen.findByText(/Chrome ✓/)).toBeInTheDocument()
  expect(screen.getByText(/Edge ✗/)).toBeInTheDocument()
})

it('adds and removes allowlist entries', async () => {
  const updateSettings = vi.fn().mockResolvedValue({ ...settingsResult })
  render(<BrowserPanel bridge={brApi({ updateSettings })} />)
  expect(await screen.findByText('https://docs.example.com')).toBeInTheDocument()
  fireEvent.change(screen.getByLabelText('白名单新条目'), { target: { value: 'https://api.example.com' } })
  fireEvent.click(screen.getByRole('button', { name: '添加' }))
  await waitFor(() => expect(updateSettings).toHaveBeenCalledWith({ allowlist: ['https://docs.example.com', 'https://api.example.com'], expectedRevision: 1 }))
  fireEvent.click(screen.getByRole('button', { name: '移除' }))
  await waitFor(() => expect(updateSettings).toHaveBeenCalledWith({ allowlist: [], expectedRevision: 1 }))
})

it('rejects malformed allowlist entries client-side', async () => {
  const updateSettings = vi.fn().mockResolvedValue({ ...settingsResult })
  render(<BrowserPanel bridge={brApi({ updateSettings })} />)
  await screen.findByText('https://docs.example.com')
  fireEvent.change(screen.getByLabelText('白名单新条目'), { target: { value: 'ftp://bad' } })
  fireEvent.click(screen.getByRole('button', { name: '添加' }))
  expect(await screen.findByText(/白名单条目需为/)).toBeInTheDocument()
  expect(updateSettings).not.toHaveBeenCalled()
})

it('lists sessions and disconnects a connected one', async () => {
  const disconnect = vi.fn().mockResolvedValue({ sessionId: 'br-01ARZ3NDEKTSV4RRFFQ6', mode: 'builtin' as const, state: 'disconnected' as const, updatedAt: now })
  const listSessions = vi.fn()
    .mockResolvedValueOnce({ sessions: [{ sessionId: 'br-01ARZ3NDEKTSV4RRFFQ6', mode: 'builtin' as const, state: 'connected' as const, connectedAt: now, updatedAt: now }] })
    .mockResolvedValue({ sessions: [] })
  render(<BrowserPanel bridge={brApi({ listSessions, disconnect })} />)
  expect(await screen.findByText(/br-01ARZ3NDEKT/)).toBeInTheDocument()
  fireEvent.click(screen.getByRole('button', { name: '断开' }))
  await waitFor(() => expect(disconnect).toHaveBeenCalledWith({ sessionId: 'br-01ARZ3NDEKTSV4RRFFQ6' }))
})

it('grants a pending site permission', async () => {
  const decidePermission = vi.fn().mockResolvedValue({ permissionId: 'brp-1', origin: 'https://maps.example.com', permission: 'geolocation' as const, policy: 'ask' as const, state: 'granted' as const, createdAt: now, decidedAt: now })
  const listPermissions = vi.fn()
    .mockResolvedValueOnce({ permissions: [{ permissionId: 'brp-1', origin: 'https://maps.example.com', permission: 'geolocation' as const, policy: 'ask' as const, state: 'pending' as const, createdAt: now }] })
    .mockResolvedValue({ permissions: [] })
  render(<BrowserPanel bridge={brApi({ listPermissions, decidePermission })} />)
  expect(await screen.findByText(/地理位置/)).toBeInTheDocument()
  fireEvent.click(screen.getByRole('button', { name: '允许' }))
  await waitFor(() => expect(decidePermission).toHaveBeenCalledWith({ permissionId: 'brp-1', decision: 'grant' }))
})

it('sets an always-allow policy from the approval queue', async () => {
  const setPermissionPolicy = vi.fn().mockResolvedValue({ permissionId: 'brp-2', origin: 'https://meet.example.com', permission: 'camera' as const, policy: 'allow' as const, state: 'granted' as const, createdAt: now, decidedAt: now })
  const listPermissions = vi.fn()
    .mockResolvedValueOnce({ permissions: [{ permissionId: 'brp-2', origin: 'https://meet.example.com', permission: 'camera' as const, policy: 'ask' as const, state: 'pending' as const, createdAt: now }] })
    .mockResolvedValue({ permissions: [] })
  render(<BrowserPanel bridge={brApi({ listPermissions, setPermissionPolicy })} />)
  expect(await screen.findByText(/摄像头/)).toBeInTheDocument()
  fireEvent.click(screen.getByRole('button', { name: '始终允许' }))
  await waitFor(() => expect(setPermissionPolicy).toHaveBeenCalledWith({ origin: 'https://meet.example.com', permission: 'camera', policy: 'allow' }))
})

it('shows saved but unapplied settings and retries the current revision', async () => {
  const updateSettings = vi.fn()
    .mockResolvedValueOnce({ ...settingsResult, mode: 'edge', revision: 2, applyStatus: 'failed', applyError: '旧浏览器停止失败' })
    .mockResolvedValueOnce({ ...settingsResult, mode: 'edge', revision: 3 })
  render(<BrowserPanel bridge={brApi({ updateSettings })} />)
  await waitFor(() => expect(screen.getByRole('radio', { name: /内置 WebView2/ })).toHaveAttribute('aria-checked', 'true'))
  fireEvent.click(screen.getByRole('radio', { name: /Edge/ }))
  expect(await screen.findByRole('alert')).toHaveTextContent('旧浏览器停止失败')
  await waitFor(() => expect(screen.getByRole('button', { name: '重试应用' })).toBeEnabled())
  fireEvent.click(screen.getByRole('button', { name: '重试应用' }))
  await waitFor(() => expect(updateSettings).toHaveBeenLastCalledWith({ expectedRevision: 2 }))
  await waitFor(() => expect(screen.queryByRole('alert')).not.toBeInTheDocument())
  expect(await screen.findByText('浏览器设置已生效')).toBeInTheDocument()
})

it('keeps edited path after a conflict and uses the authoritative revision on explicit retry', async () => {
  const getSettings = vi.fn().mockResolvedValueOnce({ ...settingsResult }).mockResolvedValue({ ...settingsResult, revision: 4, chromePath: 'C:/other.exe' })
  const updateSettings = vi.fn().mockRejectedValueOnce(new Error('设置已变化，请重试')).mockResolvedValue({ ...settingsResult, revision: 5, chromePath: 'C:/mine.exe' })
  render(<BrowserPanel bridge={brApi({ getSettings, updateSettings })} />)
  await waitFor(() => expect(screen.getByRole('radio', { name: /内置 WebView2/ })).toHaveAttribute('aria-checked', 'true'))
  const input = screen.getByLabelText('Chrome 可执行路径')
  fireEvent.change(input, { target: { value: 'C:/mine.exe' } })
  const save = input.parentElement!.querySelector('button')!
  fireEvent.click(save)
  expect(await screen.findByText('设置已变化，请重试')).toBeInTheDocument()
  expect(input).toHaveValue('C:/mine.exe')
  fireEvent.click(save)
  await waitFor(() => expect(updateSettings).toHaveBeenLastCalledWith({ chromePath: 'C:/mine.exe', expectedRevision: 4 }))
})

it('ignores the old panel bridge load after switching its data source', async () => {
  let resolveOld!: (value: typeof settingsResult) => void
  const old = brApi({ getSettings: vi.fn(() => new Promise<typeof settingsResult>(resolve => { resolveOld = resolve })) })
  const fresh = brApi({ getSettings: vi.fn().mockResolvedValue({ ...settingsResult, revision: 9, mode: 'edge' }) })
  const view = render(<BrowserPanel bridge={old} />)
  view.rerender(<BrowserPanel bridge={fresh} />)
  await waitFor(() => expect(screen.getByRole('radio', { name: /Edge/ })).toHaveAttribute('aria-checked', 'true'))
  resolveOld({ ...settingsResult })
  await waitFor(() => expect(screen.getByRole('radio', { name: /Edge/ })).toHaveAttribute('aria-checked', 'true'))
})

it('rejects path-shaped allowlist entries and keeps rejected input', async () => {
  const updateSettings = vi.fn()
  render(<BrowserPanel bridge={brApi({ updateSettings })} />)
  await screen.findByText('https://docs.example.com')
  const input = screen.getByLabelText('白名单新条目')
  fireEvent.change(input, { target: { value: 'https://docs.example.com/path' } })
  fireEvent.click(screen.getByRole('button', { name: '添加' }))
  expect(await screen.findByText(/不能包含路径/)).toBeInTheDocument()
  expect(input).toHaveValue('https://docs.example.com/path')
  expect(updateSettings).not.toHaveBeenCalled()
})
