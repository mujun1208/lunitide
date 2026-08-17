import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterEach, expect, it, vi } from 'vitest'
import type { BrBridge } from '../bridge/client'
import { BrowserPanel } from './SettingsPage'

afterEach(cleanup)

const now = '2026-01-01T00:00:00Z'

const settingsResult = {
  mode: 'builtin' as const, chromePath: '', edgePath: '', extensionPort: 9222,
  allowlist: ['https://docs.example.com'], dataRetentionDays: 30,
  blockPrivateNetworks: true, updatedAt: now,
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

it('renders the five mode cards with the active mode checked', async () => {
  render(<BrowserPanel bridge={brApi()} />)
  const group = await screen.findByRole('radiogroup', { name: '浏览器连接模式' })
  const cards = group.querySelectorAll('.br-mode-card')
  expect(cards).toHaveLength(5)
  expect(screen.getByRole('radio', { name: /内置 WebView2/ })).toHaveAttribute('aria-checked', 'true')
  expect(screen.getByRole('radio', { name: /每次询问/ })).toHaveAttribute('aria-checked', 'false')
})

it('switches the connection mode through a card', async () => {
  const updateSettings = vi.fn().mockResolvedValue({ ...settingsResult, mode: 'edge' as const })
  render(<BrowserPanel bridge={brApi({ updateSettings })} />)
  await screen.findByRole('radiogroup', { name: '浏览器连接模式' })
  fireEvent.click(screen.getByRole('radio', { name: /Edge/ }))
  await waitFor(() => expect(updateSettings).toHaveBeenCalledWith({ mode: 'edge' }))
})

it('surfaces mode detection availability', async () => {
  render(<BrowserPanel bridge={brApi()} />)
  await screen.findByText('https://docs.example.com')
  fireEvent.click(screen.getByRole('button', { name: '探测本机浏览器' }))
  expect(await screen.findByText(/Chrome ✓/)).toBeInTheDocument()
  expect(screen.getByText(/Edge ✗/)).toBeInTheDocument()
})

it('adds and removes allowlist entries', async () => {
  const updateSettings = vi.fn().mockResolvedValue({ ...settingsResult })
  render(<BrowserPanel bridge={brApi({ updateSettings })} />)
  expect(await screen.findByText('https://docs.example.com')).toBeInTheDocument()
  fireEvent.change(screen.getByLabelText('白名单新条目'), { target: { value: 'https://api.example.com' } })
  fireEvent.click(screen.getByRole('button', { name: '添加' }))
  await waitFor(() => expect(updateSettings).toHaveBeenCalledWith({ allowlist: ['https://docs.example.com', 'https://api.example.com'] }))
  fireEvent.click(screen.getByRole('button', { name: '移除' }))
  await waitFor(() => expect(updateSettings).toHaveBeenCalledWith({ allowlist: [] }))
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
