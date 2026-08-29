import { describe, expect, it, beforeEach } from 'vitest'
import {
  BUILTIN_SUBAGENT_IDS,
  DEFAULT_CAP_PACK,
  READ_CAP_OPTIONS,
  SUBAGENT_CAP_PACK_IDS,
  SUBAGENT_CAP_PACKS,
  buildSubagentChatPolicy,
  capsForPack,
  defaultSubagentSettings,
  loadSubagentSettings,
  normalizeReadCaps,
  packForCaps,
  saveSubagentSettings,
} from './subagentSettings'

describe('subagentSettings', () => {
  beforeEach(() => {
    localStorage.clear()
  })

  it('defaults to proactive delegation with all built-ins enabled', () => {
    const s = defaultSubagentSettings()
    expect(s.delegationMode).toBe('proactive')
    for (const id of BUILTIN_SUBAGENT_IDS) {
      expect(s.overrides[id]?.enabled).not.toBe(false)
    }
  })

  it('persists and reloads settings', () => {
    const next = { ...defaultSubagentSettings(), delegationMode: 'explicit' as const }
    saveSubagentSettings(next)
    expect(loadSubagentSettings().delegationMode).toBe('explicit')
  })

  it('builds chat.start subagentPolicy payload', () => {
    const s = defaultSubagentSettings()
    s.customProfiles.push({
      id: 'docs',
      displayName: 'Docs',
      systemPrompt: 'Summarize docs read-only.',
      readCaps: ['web.fetch'],
    })
    const policy = buildSubagentChatPolicy(s)
    expect(policy.delegationMode).toBe('proactive')
    expect(policy.customProfiles[0]?.id).toBe('docs')
    expect(policy.overrides.explore?.enabled).not.toBe(false)
  })

  it('maps named cap packs onto the product readCaps whitelist', () => {
    expect(SUBAGENT_CAP_PACKS.all.label).toBe('全部权限')
    expect(SUBAGENT_CAP_PACKS.read.label).toBe('只读权限')
    expect(SUBAGENT_CAP_PACKS.web.label).toBe('网络检索')
    expect(SUBAGENT_CAP_PACKS.browser.label).toBe('浏览器操作')
    expect(capsForPack('all')).toEqual([...READ_CAP_OPTIONS])
    expect(capsForPack('read')).toEqual([
      'fs.read', 'fs.tree', 'fs.grep', 'fs.glob', 'fs.stat', 'fs.readMany',
      'web.search', 'web.fetch', 'evidence.list',
    ])
    expect(capsForPack('web')).toEqual(['web.search', 'web.fetch'])
    expect(capsForPack('browser')).toEqual([
      'web.search', 'web.fetch',
      'browser.act:navigate', 'browser.act:read', 'browser.act:snapshot',
    ])
    expect(capsForPack('read')).not.toContain('browser.act:navigate')
    for (const id of SUBAGENT_CAP_PACK_IDS) {
      expect(packForCaps(capsForPack(id))).toBe(id)
      for (const cap of SUBAGENT_CAP_PACKS[id].caps) {
        expect(READ_CAP_OPTIONS).toContain(cap)
      }
    }
    expect(DEFAULT_CAP_PACK).toBe('all')
    expect(packForCaps(['web.fetch'])).toBe('custom')
    expect(normalizeReadCaps([])).toEqual(capsForPack('all'))
    expect(normalizeReadCaps(['fs.write', 'web.search'])).toEqual(['web.search'])
  })
})
