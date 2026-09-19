import { expect, it } from 'vitest'
import { mcpNeedsRepair, recommendedPreset, repairPresetFor } from './mcpRepair'

const youtube = {
  id: 'youtube-transcript',
  name: 'YouTube Transcript',
  description: '字幕',
  transport: 'stdio' as const,
  command: 'npx' as const,
  args: ['-y', '@sinco-lab/mcp-youtube-transcript'],
  needsArgs: false,
  category: '内容',
}

it('maps the zhsama duckduckgo install onto the Hermes package', () => {
  const preset = repairPresetFor({
    endpointId: 'mcp-2',
    transport: 'stdio',
    state: 'quarantined',
    enabled: true,
    origin: 'manual',
    command: 'npx',
    args: ['-y', 'duckduckgo-mcp-server'],
    diagnosticCode: 'MCP_PROTOCOL_FAILED',
  }, [{
    id: 'duckduckgo',
    name: 'DuckDuckGo',
    description: '搜索',
    transport: 'stdio',
    command: 'npx',
    args: ['-y', '@nickclyde/duckduckgo-mcp-server'],
    needsArgs: false,
    category: '网络',
  }])
  expect(preset?.id).toBe('duckduckgo')
  expect(preset?.args).toContain('@nickclyde/duckduckgo-mcp-server')
})

it('maps the broken youtube-transcript-mcp install onto the curated handshake package', () => {
  const preset = repairPresetFor({
    endpointId: 'mcp-1',
    transport: 'stdio',
    state: 'quarantined',
    enabled: true,
    origin: 'manual',
    command: 'npx',
    args: ['-y', 'youtube-transcript-mcp'],
    diagnosticCode: 'MCP_PROTOCOL_FAILED',
  }, [youtube])
  expect(preset?.id).toBe('youtube-transcript')
  expect(preset?.args).toContain('@sinco-lab/mcp-youtube-transcript')
})

it('marks Hermes/OpenClaw style free servers as recommended', () => {
  expect(recommendedPreset({ id: 'duckduckgo' })).toBe(true)
  expect(recommendedPreset({ id: 'filesystem' })).toBe(true)
  expect(recommendedPreset({ id: 'everything' })).toBe(true)
  expect(recommendedPreset({ id: 'chrome-devtools' })).toBe(false)
})

it('treats handshake failures as repairable', () => {
  expect(mcpNeedsRepair({
    endpointId: 'mcp-1',
    transport: 'stdio',
    state: 'degraded',
    enabled: true,
    origin: 'manual',
    diagnosticCode: 'MCP_PROTOCOL_FAILED',
  })).toBe(true)
})
