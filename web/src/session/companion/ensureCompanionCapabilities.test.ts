import { beforeEach, describe, expect, it, vi } from 'vitest'
import { ccBridge, toolsPolicyBridge } from '../../bridge/client'
import { ensureCompanionCapabilities } from './ensureCompanionCapabilities'

vi.mock('../../bridge/client', () => ({
  toolsPolicyBridge: {
    getCommandPolicy: vi.fn(),
    setCommandPolicy: vi.fn(),
  },
  ccBridge: {
    getConfig: vi.fn(),
    updateConfig: vi.fn(),
  },
}))

describe('ensureCompanionCapabilities', () => {
  beforeEach(() => { vi.clearAllMocks() })

  it('does not call updateConfig when emergency stop is latched', async () => {
    vi.mocked(toolsPolicyBridge.getCommandPolicy).mockResolvedValue({ commands: [], fullAccess: true })
    vi.mocked(ccBridge.getConfig).mockResolvedValue({
      enabled: true,
      emergencyStopped: true,
      emergencyStoppedAt: '2026-08-26T00:00:00Z',
      securityLevel: 'standard',
      allowCritical: false,
      processBlocklist: [],
      maxActionsPerMinute: 30,
      confirmTimeoutSeconds: 60,
      updatedAt: '2026-08-26T00:00:00Z',
    })
    const out = await ensureCompanionCapabilities()
    expect(ccBridge.updateConfig).not.toHaveBeenCalled()
    expect(out).toEqual({ fullAccess: true, ccEnabled: false })
  })

  it('does not silently enable computer control when it is idle', async () => {
    vi.mocked(toolsPolicyBridge.getCommandPolicy).mockResolvedValue({ commands: [], fullAccess: true })
    vi.mocked(ccBridge.getConfig).mockResolvedValue({
      enabled: false,
      emergencyStopped: false,
      securityLevel: 'standard',
      allowCritical: false,
      processBlocklist: [],
      maxActionsPerMinute: 30,
      confirmTimeoutSeconds: 60,
      updatedAt: '2026-08-26T00:00:00Z',
    })
    const out = await ensureCompanionCapabilities()
    expect(ccBridge.updateConfig).not.toHaveBeenCalled()
    expect(out).toEqual({ fullAccess: true, ccEnabled: false })
  })

  it('bumps a legacy 30/min cap when computer control is already on', async () => {
    vi.mocked(toolsPolicyBridge.getCommandPolicy).mockResolvedValue({ commands: [], fullAccess: true })
    vi.mocked(ccBridge.getConfig).mockResolvedValue({
      enabled: true,
      emergencyStopped: false,
      securityLevel: 'standard',
      allowCritical: false,
      processBlocklist: [],
      maxActionsPerMinute: 30,
      confirmTimeoutSeconds: 60,
      updatedAt: '2026-08-26T00:00:00Z',
    })
    vi.mocked(ccBridge.updateConfig).mockResolvedValue({
      enabled: true,
      emergencyStopped: false,
      securityLevel: 'standard',
      allowCritical: false,
      processBlocklist: [],
      maxActionsPerMinute: 60,
      confirmTimeoutSeconds: 60,
      updatedAt: '2026-08-26T00:00:01Z',
    })
    const out = await ensureCompanionCapabilities()
    expect(ccBridge.updateConfig).toHaveBeenCalledWith({ maxActionsPerMinute: 60, actor: 'companion' })
    expect(out).toEqual({ fullAccess: true, ccEnabled: true })
  })

  it('does not silently enable full-disk command policy', async () => {
    vi.mocked(toolsPolicyBridge.getCommandPolicy).mockResolvedValue({ commands: [], fullAccess: false })
    vi.mocked(ccBridge.getConfig).mockResolvedValue({
      enabled: true,
      emergencyStopped: false,
      securityLevel: 'standard',
      allowCritical: false,
      processBlocklist: [],
      maxActionsPerMinute: 60,
      confirmTimeoutSeconds: 60,
      updatedAt: '2026-08-26T00:00:00Z',
    })
    const out = await ensureCompanionCapabilities()
    expect(toolsPolicyBridge.setCommandPolicy).not.toHaveBeenCalled()
    expect(ccBridge.updateConfig).not.toHaveBeenCalled()
    expect(out).toEqual({ fullAccess: false, ccEnabled: true })
  })
})
