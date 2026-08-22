import { describe, expect, it, beforeEach } from 'vitest'
import {
  BUILTIN_SUBAGENT_IDS,
  buildSubagentChatPolicy,
  defaultSubagentSettings,
  loadSubagentSettings,
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
})
