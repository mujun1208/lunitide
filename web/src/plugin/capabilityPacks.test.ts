import { afterEach, expect, it, vi } from 'vitest'
import { CAPABILITY_PACKS, exportCapabilityPackJSON, installCapabilityPack, isAlreadyInstalledError, mergedPackLedger, PACK_LEDGER_KEY, packLedgerEntry, parseCapabilityPackJSON, uninstallCapabilityPack } from './capabilityPacks'

afterEach(() => localStorage.removeItem(PACK_LEDGER_KEY))

it('installs skills, MCP presets and tool gates and treats already-installed as ok', async () => {
  const skills = { install: vi.fn().mockRejectedValueOnce(new Error('该模板版本已安装')).mockResolvedValue({ name: 'tpl-e2e-browser' }) }
  const mcp = {
    presets: vi.fn().mockResolvedValue({ items: [{ id: 'playwright', name: 'Playwright', command: 'npx', args: ['-y', '@playwright/mcp'], needsArgs: false }] }),
    list: vi.fn().mockResolvedValue({ endpoints: [] }),
    add: vi.fn().mockResolvedValue({ endpointId: '01ARZ3NDEKTSV4RRFFQ69G5FAA' }),
    toggle: vi.fn().mockResolvedValue({}),
  }
  const plugins = { toggle: vi.fn().mockResolvedValue({}) }
  const result = await installCapabilityPack(CAPABILITY_PACKS[0], {
    skills: skills as never,
    mcp: mcp as never,
    plugins: plugins as never,
    installed: [{ pluginId: 'browser', installId: '01ARZ3NDEKTSV4RRFFQ69G5FAB', state: 'disabled' }, { pluginId: 'web-fetch', installId: '01ARZ3NDEKTSV4RRFFQ69G5FAC', state: 'enabled' }],
  })
  expect(result.ok).toBe(true)
  expect(isAlreadyInstalledError(new Error('该模板版本已安装'))).toBe(true)
  expect(mcp.add).toHaveBeenCalledOnce()
  expect(plugins.toggle).toHaveBeenCalledWith({ installId: '01ARZ3NDEKTSV4RRFFQ69G5FAB', enabled: true })
  expect(packLedgerEntry(CAPABILITY_PACKS[0].id)).toEqual({
    packId: 'pack-browser',
    addedMcpEndpointIds: ['01ARZ3NDEKTSV4RRFFQ69G5FAA'],
    enabledGateInstallIds: ['01ARZ3NDEKTSV4RRFFQ69G5FAB'],
  })
})

it('uninstalls only bindings this pack added and keeps skills', async () => {
  localStorage.setItem(PACK_LEDGER_KEY, JSON.stringify([
    { packId: 'pack-browser', addedMcpEndpointIds: ['ep-1'], enabledGateInstallIds: ['gate-1'] },
    { packId: 'pack-research', addedMcpEndpointIds: ['ep-1'], enabledGateInstallIds: ['gate-2'] },
  ]))
  const mcp = { toggle: vi.fn().mockResolvedValue({}) }
  const plugins = { toggle: vi.fn().mockResolvedValue({}) }
  const skills = { delete: vi.fn() }
  const result = await uninstallCapabilityPack(CAPABILITY_PACKS[0], { mcp: mcp as never, plugins: plugins as never })
  expect(result.ok).toBe(true)
  expect(result.notes.some(item => item.includes('技能留在技能中心'))).toBe(true)
  expect(plugins.toggle).toHaveBeenCalledWith({ installId: 'gate-1', enabled: false })
  expect(mcp.toggle).not.toHaveBeenCalled()
  expect(skills.delete).not.toHaveBeenCalled()
  expect(packLedgerEntry('pack-browser')).toBeUndefined()
  expect(packLedgerEntry('pack-research')).toBeDefined()
})

it('treats plugin.list pack-* as installed without localStorage', () => {
  expect(mergedPackLedger([{ pluginId: 'pack-ppt', state: 'enabled' }]).some(item => item.packId === 'pack-ppt')).toBe(true)
  expect(mergedPackLedger([{ pluginId: 'web-search', state: 'enabled' }])).toEqual([])
})

it('exports and imports pack JSON without scripts', () => {
  expect(CAPABILITY_PACKS.length).toBeGreaterThanOrEqual(12)
  const raw = exportCapabilityPackJSON(CAPABILITY_PACKS[0])
  expect(raw).toContain('lunitide-capability-pack')
  expect(raw).not.toContain('plugin/main.ts')
  expect(parseCapabilityPackJSON(raw).id).toBe('pack-browser')
})
