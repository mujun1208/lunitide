import { expect, it } from 'vitest'
import { isHubOwnedPage } from './appTypes'

it('keeps project tools on AgentHub chrome', () => {
  for (const page of ['agentHub', 'projects', 'skill', 'expert', 'mcp', 'plugins', 'assets'] as const) {
    expect(isHubOwnedPage(page)).toBe(true)
  }
  for (const page of ['home', 'office', 'media', 'automation', 'settings'] as const) {
    expect(isHubOwnedPage(page)).toBe(false)
  }
})
