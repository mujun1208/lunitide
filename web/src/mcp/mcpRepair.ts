import type { Mcp6PresetsListResult, McpListResult } from '../generated/bridge'

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
  'duckduckgo-mcp-server': '@nickclyde/duckduckgo-mcp-server',
}

export function mcpPackageName(args?: string[]): string {
  return args?.find(item => item.startsWith('@') || item.includes('mcp')) ?? ''
}

export function recommendedPreset(preset: Pick<McpPreset, 'id'>): boolean {
  return RECOMMENDED_PRESET_IDS.has(preset.id)
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

export function mcpNeedsRepair(item: McpEndpoint): boolean {
  return item.state === 'quarantined' || item.state === 'degraded' || Boolean(item.diagnosticCode)
}
