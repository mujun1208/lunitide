import type { Mcp6PresetsListResult, McpListResult } from '../generated/bridge'
import { leftoverArchivedMcp } from '../settings/leftoverMcp'

export type McpPreset = Mcp6PresetsListResult['items'][number]
export type McpEndpoint = McpListResult['endpoints'][number]

export const RECOMMENDED_PRESET_IDS = new Set([
  'everything',
  'filesystem',
  'fetch',
  'memory',
  'sequentialthinking',
  'playwright',
  'time',
  'context7',
  'calculator',
  'duckduckgo',
  'youtube-transcript',
])

const PACKAGE_REMAP: Record<string, string> = {
  'youtube-transcript-mcp': '@sinco-lab/mcp-youtube-transcript',
  '@nickclyde/duckduckgo-mcp-server': 'duckduckgo-mcp-server',
}

export function mcpPackageName(args?: string[]): string {
  return args?.find(item => item.startsWith('@') || item.includes('mcp')) ?? ''
}

export function recommendedPreset(preset: Pick<McpPreset, 'id'>): boolean {
  return RECOMMENDED_PRESET_IDS.has(preset.id)
}

export function mcpUsesUv(item: Pick<McpEndpoint, 'command'> | Pick<McpPreset, 'command'>): boolean {
  return item.command === 'uvx'
}

export function repairPresetFor(item: McpEndpoint, presets: readonly McpPreset[]): McpPreset | undefined {
  const pkg = mcpPackageName(item.args)
  const remapped = pkg ? PACKAGE_REMAP[pkg] : ''
  if (remapped) {
    const next = presets.find(preset => mcpPackageName(preset.args) === remapped)
    if (next) return next
  }
  return presets.find(preset => {
    const presetPkg = mcpPackageName(preset.args)
    return Boolean(presetPkg) && (presetPkg === pkg || presetPkg === remapped)
  })
}

const HANDSHAKE_REPAIR_CODES = new Set([
  'MCP_PACKAGE_NOT_FOUND',
  'MCP_PROTOCOL_FAILED',
  'MCP_CONNECT_FAILED',
])

export function mcpDriftQuarantined(item: McpEndpoint): boolean {
  if (item.state !== 'quarantined') return false
  const pkg = mcpPackageName(item.args)
  if (pkg && PACKAGE_REMAP[pkg]) return false
  if (HANDSHAKE_REPAIR_CODES.has(item.diagnosticCode ?? '')) return false
  const message = `${item.diagnosticMessage ?? ''} ${item.diagnosticCode ?? ''}`
  if (/握手|协议|handshake|connect failed|package not found/i.test(message)) return false
  return true
}

export function mcpNeedsRepair(item: McpEndpoint): boolean {
  if (mcpDriftQuarantined(item)) return false
  return item.state === 'quarantined' || item.state === 'degraded' || Boolean(item.diagnosticCode)
}

export function mcpRepairTargets(items: readonly McpEndpoint[]): McpEndpoint[] {
  return items.filter(item =>
    item.state !== 'revoked' &&
    leftoverArchivedMcp(item.args, item.url).length === 0 &&
    mcpNeedsRepair(item),
  )
}

export function mcpRepairArgs(preset: McpPreset): string[] {
  const value = (preset.argDefault ?? '').trim().replaceAll('\\', '/')
  if (!preset.needsArgs) return [...preset.args]
  return preset.args.map(item => item === preset.argPlaceholder ? (value || item) : item)
}

function installedPackageKey(item: { command?: string; args?: string[]; url?: string; transport?: string }): string {
  if (item.transport === 'https') return `https|${item.url ?? ''}`
  return `${item.command ?? ''}|${mcpPackageName(item.args) || item.args?.[0] || ''}`
}

function presetPackageKey(preset: McpPreset): string {
  if (preset.transport === 'https') return `https|${preset.url ?? ''}`
  return `${preset.command}|${mcpPackageName(preset.args) || preset.args[0] || ''}`
}

export function mcpLiveForPreset(preset: McpPreset, live: readonly McpEndpoint[]): McpEndpoint | undefined {
  const key = presetPackageKey(preset)
  return live.find(item => item.state !== 'revoked' && installedPackageKey(item) === key)
}

export function mcpExistingMarketInstall(item: McpEndpoint, live: readonly McpEndpoint[], presets: readonly McpPreset[]): McpEndpoint | undefined {
  const preset = repairPresetFor(item, presets)
  if (!preset) return undefined
  const found = mcpLiveForPreset(preset, live)
  if (found && found.endpointId !== item.endpointId) return found
  return undefined
}

export function missingRecommendedPresets(presets: readonly McpPreset[], live: readonly McpEndpoint[]): McpPreset[] {
  return presets.filter(preset => {
    if (!recommendedPreset(preset)) return false
    if (mcpLiveForPreset(preset, live)) return false
    return !live.some(item => item.state !== 'revoked' && repairPresetFor(item, [preset])?.id === preset.id)
  })
}
