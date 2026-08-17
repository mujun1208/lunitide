// M10 wave-4 computer-control session status coverage: the five-state
// derivation (idle/running/paused/stopped/blocked) merges the live cc.*
// tool activity, the emergency-stop latch and the latest audit row; the bar
// hides while CC is idle and exposes a one-click emergency stop.
import { act, cleanup, render, renderHook, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, expect, it, vi } from 'vitest'
import { ccBridge, type CcBridge } from '../bridge/client'
import type { CcGetAuditLogResult, CcGetConfigResult } from '../generated/bridge'
import { CcStatusBar, useCcStatus } from './ccStatus'

vi.mock('../bridge/client', async importOriginal => {
  const actual = await importOriginal<typeof import('../bridge/client')>()
  const bridge: CcBridge = {
    getConfig: vi.fn(),
    updateConfig: vi.fn(),
    getAuditLog: vi.fn().mockResolvedValue({ items: [] }),
    emergencyStop: vi.fn(),
  }
  return { ...actual, ccBridge: bridge, getCcBridge: () => bridge }
})

afterEach(() => { cleanup(); vi.clearAllMocks() })

const bridge = () => vi.mocked(ccBridge)

const config = (overrides: Partial<CcGetConfigResult> = {}) => ({
  enabled: true, securityLevel: 'standard' as const, allowCritical: false,
  processBlocklist: ['cmd.exe'], maxActionsPerMinute: 60, confirmTimeoutSeconds: 120,
  emergencyStopped: false, updatedAt: '2026-08-17T00:00:00Z', ...overrides,
})

const audit = (overrides: Partial<CcGetAuditLogResult['items'][number]> = {}) => ({
  entryId: '01ARZ3NDEKTSV4RRFFQ69G5FAV', sessionId: '01ARZ3NDEKTSV4RRFFQ69G5FAV',
  tool: 'cc.mouse_click' as const, action: 'click', riskLevel: 'medium' as const,
  status: 'blocked' as const, layer: 'process-monitor' as const,
  detail: '目标进程在黑名单', createdAt: '2026-08-17T00:00:00Z', ...overrides,
})

it('projects stopped when the emergency-stop latch is set, even mid-operation', async () => {
  const b = bridge()
  vi.mocked(b.getConfig).mockResolvedValue(config({ emergencyStopped: true }))
  vi.mocked(b.getAuditLog).mockResolvedValue({ items: [audit()] })
  const { result } = renderHook(() => useCcStatus('01ARZ3NDEKTSV4RRFFQ69G5FAV', 'cc.mouse_move', 'tool_started'))
  await waitFor(() => expect(result.current.status).toBe('stopped'))
  expect(result.current.detail).toContain('紧急停止')
})

it('derives paused and running from the live cc tool activity', async () => {
  const b = bridge()
  vi.mocked(b.getConfig).mockResolvedValue(config())
  vi.mocked(b.getAuditLog).mockResolvedValue({ items: [] })
  const paused = renderHook(() => useCcStatus('01ARZ3NDEKTSV4RRFFQ69G5FAV', 'cc.keyboard_shortcut', 'approval_required'))
  await waitFor(() => expect(paused.result.current.status).toBe('paused'))
  const running = renderHook(() => useCcStatus('01ARZ3NDEKTSV4RRFFQ69G5FAV', 'cc.screen_capture', 'tool_started'))
  await waitFor(() => expect(running.result.current.status).toBe('running'))
  // Non-cc tools never project machine actuation states.
  const other = renderHook(() => useCcStatus('01ARZ3NDEKTSV4RRFFQ69G5FAV', 'workspace.read', 'tool_started'))
  await waitFor(() => expect(other.result.current.status).toBe('idle'))
})

it('projects blocked from the latest audit row and labels the interception layer', async () => {
  const b = bridge()
  vi.mocked(b.getConfig).mockResolvedValue(config())
  vi.mocked(b.getAuditLog).mockResolvedValue({ items: [audit()] })
  const { result } = renderHook(() => useCcStatus('01ARZ3NDEKTSV4RRFFQ69G5FAV'))
  await waitFor(() => expect(result.current.status).toBe('blocked'))
  expect(result.current.detail).toContain('进程监控')
  expect(result.current.detail).toContain('目标进程在黑名单')
})

it('stays idle when CC is disabled or nothing happened', async () => {
  const b = bridge()
  vi.mocked(b.getConfig).mockResolvedValue(config({ enabled: false }))
  vi.mocked(b.getAuditLog).mockResolvedValue({ items: [audit({ status: 'executed', layer: '' })] })
  const { result } = renderHook(() => useCcStatus('01ARZ3NDEKTSV4RRFFQ69G5FAV'))
  await waitFor(() => expect(result.current.enabled).toBe(false))
  expect(result.current.status).toBe('idle')
})

it('emergencyStop flips the latch through the bridge', async () => {
  const b = bridge()
  vi.mocked(b.getConfig).mockResolvedValue(config())
  vi.mocked(b.getAuditLog).mockResolvedValue({ items: [] })
  vi.mocked(b.emergencyStop).mockResolvedValue(config({ emergencyStopped: true }))
  const { result } = renderHook(() => useCcStatus('01ARZ3NDEKTSV4RRFFQ69G5FAV'))
  await waitFor(() => expect(result.current.status).toBe('idle'))
  await act(async () => { expect(await result.current.emergencyStop('test')).toBe(true) })
  expect(b.emergencyStop).toHaveBeenCalledWith({ reason: 'test' })
  await waitFor(() => expect(result.current.status).toBe('stopped'))
})

it('renders the floating bar only for active states and wires the stop button', async () => {
  const b = bridge()
  vi.mocked(b.getConfig).mockResolvedValue(config())
  vi.mocked(b.getAuditLog).mockResolvedValue({ items: [] })
  vi.mocked(b.emergencyStop).mockResolvedValue(config({ emergencyStopped: true }))
  const idle = renderHook(() => useCcStatus('01ARZ3NDEKTSV4RRFFQ69G5FAV'))
  await waitFor(() => expect(idle.result.current.status).toBe('idle'))
  const { container, rerender } = render(<CcStatusBar state={idle.result.current} />)
  expect(container.querySelector('.cc-status-bar')).toBeNull()
  const running = renderHook(() => useCcStatus('01ARZ3NDEKTSV4RRFFQ69G5FAV', 'cc.mouse_move', 'tool_started'))
  await waitFor(() => expect(running.result.current.status).toBe('running'))
  rerender(<CcStatusBar state={running.result.current} onStop={() => void running.result.current.emergencyStop('session-status-bar')} />)
  const bar = container.querySelector('.cc-status-bar.cc-running')
  expect(bar).not.toBeNull()
  expect(bar?.getAttribute('role')).toBe('status')
  await userEvent.click(container.querySelector('.cc-status-stop')!)
  expect(b.emergencyStop).toHaveBeenCalledWith({ reason: 'session-status-bar' })
})
