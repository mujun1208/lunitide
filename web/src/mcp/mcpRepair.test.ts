import { expect, it } from 'vitest'
import { mcpExistingMarketInstall, mcpNeedsRepair, mcpRepairArgs, mcpRepairTargets, missingRecommendedPresets, recommendedPreset, repairPresetFor } from './mcpRepair'

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

it('maps the unpublished nickclyde duckduckgo install onto the published package', () => {
  const preset = repairPresetFor({
    endpointId: 'mcp-2',
    transport: 'stdio',
    state: 'quarantined',
    enabled: true,
    origin: 'manual',
    command: 'npx',
    args: ['-y', '@nickclyde/duckduckgo-mcp-server'],
    diagnosticCode: 'MCP_PACKAGE_NOT_FOUND',
  }, [{
    id: 'duckduckgo',
    name: 'DuckDuckGo',
    description: '搜索',
    transport: 'stdio',
    command: 'npx',
    args: ['-y', 'duckduckgo-mcp-server'],
    needsArgs: false,
    category: '网络',
  }])
  expect(preset?.id).toBe('duckduckgo')
  expect(preset?.args).toContain('duckduckgo-mcp-server')
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

it('treats quarantined with no diagnostic code as repairable (lost on restart)', () => {
  expect(mcpNeedsRepair({
    endpointId: 'mcp-1',
    transport: 'stdio',
    state: 'quarantined',
    enabled: true,
    origin: 'manual',
    command: 'npx',
    args: ['-y', '@sinco-lab/mcp-youtube-transcript'],
  })).toBe(true)
})

it('still repairs remappable handshake quarantines', () => {
  expect(mcpNeedsRepair({
    endpointId: 'mcp-1',
    transport: 'stdio',
    state: 'quarantined',
    enabled: true,
    origin: 'manual',
    command: 'npx',
    args: ['-y', 'youtube-transcript-mcp'],
    diagnosticCode: 'MCP_PROTOCOL_FAILED',
  })).toBe(true)
  expect(mcpNeedsRepair({
    endpointId: 'mcp-2',
    transport: 'stdio',
    state: 'quarantined',
    enabled: true,
    origin: 'manual',
    command: 'npx',
    args: ['-y', 'duckduckgo-mcp-server'],
    diagnosticMessage: '服务器握手或工具目录响应不符合支持的 MCP 协议，请检查启动配置及服务器版本。',
  })).toBe(true)
})

it('skips leftover credential servers and keeps remapped recommended installs', () => {
  const live = [
    { endpointId: 'mcp-old', transport: 'stdio' as const, state: 'degraded' as const, enabled: true, origin: 'manual' as const, command: 'npx', args: ['-y', 'youtube-transcript-mcp'] },
    { endpointId: 'mcp-gdrive', transport: 'stdio' as const, state: 'degraded' as const, enabled: true, origin: 'manual' as const, command: 'npx', args: ['-y', '@modelcontextprotocol/server-gdrive'] },
  ]
  expect(mcpRepairTargets(live).map(item => item.endpointId)).toEqual(['mcp-old'])
  expect(missingRecommendedPresets([youtube], live).map(item => item.id)).toEqual([])
})

it('restores a deleted recommended youtube when no live remount remains', () => {
  expect(missingRecommendedPresets([youtube], [
    { endpointId: 'mcp-revoked', transport: 'stdio', state: 'revoked', enabled: false, origin: 'manual', command: 'npx', args: ['-y', '@sinco-lab/mcp-youtube-transcript'] },
  ]).map(item => item.id)).toEqual(['youtube-transcript'])
})

it('reuses an already-installed remapped package instead of adding a second row', () => {
  const current = {
    endpointId: 'mcp-new',
    transport: 'stdio' as const,
    state: 'degraded' as const,
    enabled: true,
    origin: 'manual' as const,
    command: 'npx',
    args: ['-y', '@sinco-lab/mcp-youtube-transcript'],
  }
  expect(mcpExistingMarketInstall({
    endpointId: 'mcp-old',
    transport: 'stdio',
    state: 'degraded',
    enabled: true,
    origin: 'manual',
    command: 'npx',
    args: ['-y', 'youtube-transcript-mcp'],
  }, [current], [youtube])?.endpointId).toBe('mcp-new')
})

it('fills the filesystem sandbox and keeps leftover {{dir}} only when no default exists', () => {
  expect(mcpRepairArgs({
    ...youtube,
    id: 'filesystem',
    args: ['-y', '@modelcontextprotocol/server-filesystem', '{{dir}}'],
    needsArgs: true,
    argPlaceholder: '{{dir}}',
    argDefault: 'C:/Users/demo/AppData/Local/Lunitide/mcp/filesystem',
  })).toEqual(['-y', '@modelcontextprotocol/server-filesystem', 'C:/Users/demo/AppData/Local/Lunitide/mcp/filesystem'])
  expect(mcpRepairArgs({
    ...youtube,
    id: 'filesystem',
    args: ['-y', '@modelcontextprotocol/server-filesystem', '{{dir}}'],
    needsArgs: true,
    argPlaceholder: '{{dir}}',
  })).toEqual(['-y', '@modelcontextprotocol/server-filesystem', '{{dir}}'])
})
