import type { McpBridge, PluginBridge, SkillBridge } from '../bridge/client'
import { PLUGIN_MARKET, type PluginMarketEntry } from './pluginMarket'

export interface CapabilityPackSpec {
  id: string
  name: string
  description: string
  skills: string[]
  mcpPresetIds: string[]
  toolGates: string[]
}

export const CAPABILITY_PACKS: CapabilityPackSpec[] = [
  {
    id: 'pack-browser',
    name: '浏览器工作包',
    description: '安装浏览器技能、Playwright MCP，并打开浏览器/抓取门闸。不会执行外部脚本。',
    skills: ['browser-automation', 'e2e-browser'],
    mcpPresetIds: ['playwright'],
    toolGates: ['browser', 'web-fetch'],
  },
  {
    id: 'pack-research',
    name: '调研工作包',
    description: '安装联网调研技能、Fetch/Time MCP，并打开搜索与抓取门闸。',
    skills: ['web-researcher'],
    mcpPresetIds: ['fetch', 'time'],
    toolGates: ['web-search', 'web-fetch'],
  },
  {
    id: 'pack-docs',
    name: '文档产出包',
    description: '安装文档/演示/表格技能。门闸保持内置生成工具可用。',
    skills: ['docx-writer', 'slide-builder', 'excel-analyst'],
    mcpPresetIds: [],
    toolGates: ['workspace'],
  },
  {
    id: 'pack-dev',
    name: '开发工作包',
    description: '安装审查/排障/测试技能、Filesystem MCP，并打开工作区与 Git 门闸。',
    skills: ['code-reviewer', 'debugger', 'test-writer'],
    mcpPresetIds: ['filesystem'],
    toolGates: ['workspace', 'git', 'filesystem'],
  },
  {
    id: 'pack-ppt',
    name: '演示文稿包',
    description: '给 PPT 专家用的幻灯片与素材技能。不会执行外部脚本。',
    skills: ['slide-builder', 'web-researcher', 'mermaid-diagrams'],
    mcpPresetIds: ['playwright'],
    toolGates: ['browser'],
  },
  {
    id: 'pack-report',
    name: '报告写作包',
    description: '调研、长文和去 AI 味。成文走内置 docx.gen。',
    skills: ['docx-writer', 'web-researcher', 'anti-ai-prose'],
    mcpPresetIds: ['fetch'],
    toolGates: ['web-search', 'web-fetch'],
  },
  {
    id: 'pack-novel',
    name: '小说连续包',
    description: '长篇正文、去 AI 味和连续性约定。',
    skills: ['docx-writer', 'fiction-continuity', 'anti-ai-prose', 'content-brief'],
    mcpPresetIds: [],
    toolGates: ['workspace'],
  },
  {
    id: 'pack-excel',
    name: '表格分析包',
    description: '表格分析与 CSV 工作簿。',
    skills: ['excel-analyst', 'csv-workbook'],
    mcpPresetIds: [],
    toolGates: ['workspace'],
  },
  {
    id: 'pack-meeting',
    name: '会议纪要包',
    description: '纪要、周报和每日简报。',
    skills: ['meeting-minutes', 'weekly-report', 'daily-brief'],
    mcpPresetIds: [],
    toolGates: ['workspace'],
  },
  {
    id: 'pack-pm',
    name: '产品规划包',
    description: '产品经理技能：追问、规格和拆票。',
    skills: ['pm-skill', 'brainstorming', 'grill-me', 'to-spec'],
    mcpPresetIds: [],
    toolGates: ['workspace'],
  },
  {
    id: 'pack-test',
    name: '测试验收包',
    description: '测试补全、E2E 和浏览器验收。',
    skills: ['test-writer', 'e2e-browser', 'browser-automation', 'find-bug'],
    mcpPresetIds: ['playwright'],
    toolGates: ['browser'],
  },
  {
    id: 'pack-security',
    name: '安全快审包',
    description: '安全审查与找缺陷。',
    skills: ['security-review', 'find-bug', 'code-reviewer'],
    mcpPresetIds: [],
    toolGates: ['workspace'],
  },
]

export const capabilityPack = (id: string) => CAPABILITY_PACKS.find(item => item.id === id)

export function packMarketEntries(): PluginMarketEntry[] {
  return CAPABILITY_PACKS.map(pack => ({
    id: pack.id,
    name: pack.name,
    description: pack.description,
    kind: 'workflow',
    category: '效率提升',
    publisher: 'lunitide',
    semver: '1.0.0',
    glyph: pack.name.slice(0, 1),
    tint: '#5ee0ff',
    honesty: 'builtin-toggle',
  }))
}

export function combinedPluginMarket(): PluginMarketEntry[] {
  return [...packMarketEntries(), ...PLUGIN_MARKET]
}

export function isAlreadyInstalledError(error: unknown): boolean {
  const message = error instanceof Error ? error.message : String(error ?? '')
  return /已安装|already installed|TEMPLATE_INSTALLED|duplicate/i.test(message)
}

export const PACK_LEDGER_KEY = 'lunitide:capability-pack-ledger'

export interface PackLedgerEntry {
  packId: string
  addedMcpEndpointIds: string[]
  enabledGateInstallIds: string[]
  failed?: string
}

function isLedgerEntry(value: unknown): value is PackLedgerEntry {
  if (!value || typeof value !== 'object') return false
  const row = value as PackLedgerEntry
  return typeof row.packId === 'string' && row.packId.length > 0 && Array.isArray(row.addedMcpEndpointIds) && Array.isArray(row.enabledGateInstallIds)
}

export function readPackLedger(): PackLedgerEntry[] {
  try {
    const raw = localStorage.getItem(PACK_LEDGER_KEY)
    if (!raw) return []
    const parsed = JSON.parse(raw) as unknown
    return Array.isArray(parsed) ? parsed.filter(isLedgerEntry) : []
  } catch {
    return []
  }
}

export function writePackLedger(entries: readonly PackLedgerEntry[]): void {
  try {
    localStorage.setItem(PACK_LEDGER_KEY, JSON.stringify(entries))
  } catch { /* quota / private mode */ }
}

export function upsertPackLedger(entry: PackLedgerEntry): void {
  writePackLedger([...readPackLedger().filter(item => item.packId !== entry.packId), entry])
}

export function removePackLedger(packId: string): void {
  writePackLedger(readPackLedger().filter(item => item.packId !== packId))
}

export function packLedgerEntry(packId: string): PackLedgerEntry | undefined {
  return readPackLedger().find(item => item.packId === packId)
}

export function mergedPackLedger(plugins: Array<{ pluginId: string; state: string }>): PackLedgerEntry[] {
  const byId = new Map(readPackLedger().map(entry => [entry.packId, entry]))
  for (const plugin of plugins) {
    if (!isPackPluginId(plugin.pluginId) || plugin.state === 'uninstalled') continue
    if (!byId.has(plugin.pluginId)) {
      byId.set(plugin.pluginId, { packId: plugin.pluginId, addedMcpEndpointIds: [], enabledGateInstallIds: [] })
    }
  }
  return [...byId.values()]
}

function ownedByOtherPack(packId: string, pick: (entry: PackLedgerEntry) => string[]): Set<string> {
  const owned = new Set<string>()
  for (const entry of readPackLedger()) {
    if (entry.packId === packId) continue
    for (const id of pick(entry)) owned.add(id)
  }
  return owned
}

export function exportCapabilityPackJSON(pack: CapabilityPackSpec): string {
  return JSON.stringify({ kind: 'lunitide-capability-pack', version: 1, ...pack }, null, 2)
}

export function parseCapabilityPackJSON(raw: string): CapabilityPackSpec {
  const value = JSON.parse(raw) as Partial<CapabilityPackSpec> & { mcp?: string[]; gates?: string[] }
  const id = String(value.id ?? '').trim()
  const name = String(value.name ?? '').trim()
  if (!id || !name || !Array.isArray(value.skills)) {
    throw new Error('不是能力包 JSON')
  }
  return {
    id,
    name,
    description: String(value.description ?? ''),
    skills: value.skills.filter((item): item is string => typeof item === 'string' && item.trim().length > 0),
    mcpPresetIds: (value.mcpPresetIds ?? value.mcp ?? []).filter((item): item is string => typeof item === 'string' && item.trim().length > 0),
    toolGates: (value.toolGates ?? value.gates ?? []).filter((item): item is string => typeof item === 'string' && item.trim().length > 0),
  }
}

export function isPackPluginId(pluginId: string): boolean {
  return pluginId.startsWith('pack-') && !/^pack-\d+$/.test(pluginId)
}

export async function installCapabilityPack(
  pack: CapabilityPackSpec,
  deps: { skills?: SkillBridge; mcp?: McpBridge; plugins?: PluginBridge; installed?: Array<{ pluginId: string; installId: string; state: string }> },
): Promise<{ ok: boolean; notes: string[]; record: PackLedgerEntry }> {
  const notes: string[] = []
  const addedMcpEndpointIds: string[] = []
  const enabledGateInstallIds: string[] = []
  let failed = ''
  for (const templateId of pack.skills) {
    try {
      const result = await deps.skills?.install?.({ templateId })
      notes.push(result?.name ? `技能 ${result.name}` : `技能 ${templateId}`)
    } catch (error) {
      if (isAlreadyInstalledError(error)) {
        notes.push(`技能 ${templateId}（已有）`)
        continue
      }
      failed = `技能 ${templateId}：${error instanceof Error ? error.message : '安装失败'}`
      break
    }
  }
  if (!failed) {
    const presets = deps.mcp?.presets ? (await deps.mcp.presets()).items : []
    const listed = deps.mcp?.list ? (await deps.mcp.list({})).endpoints : []
    const seenPkgs = new Set(listed.filter(item => item.state !== 'revoked').map(item => item.args?.find(arg => arg.startsWith('@') || arg.includes('mcp')) ?? ''))
    for (const presetId of pack.mcpPresetIds) {
      const preset = presets.find(item => item.id === presetId)
      if (!preset) {
        failed = `MCP ${presetId} 不在策展货架`
        break
      }
      const pkg = preset.args.find(item => item.startsWith('@') || item.includes('mcp')) ?? ''
      if (seenPkgs.has(pkg)) {
        notes.push(`MCP ${preset.name}（已有）`)
        continue
      }
      const resolved = preset.needsArgs ? (preset.argDefault ?? '').trim() : ''
      if (preset.needsArgs && !resolved) {
        failed = `MCP ${preset.name} 需要本地参数：${preset.argHint || '请到 MCP 页填写'}`
        break
      }
      try {
        const args = preset.args.map(item => item === preset.argPlaceholder ? resolved.replaceAll('\\', '/') : item)
        const added = await deps.mcp?.add({ origin: 'manual', transport: 'stdio', command: preset.command, args, riskConfirmed: true, requestId: crypto.randomUUID() })
        if (added?.endpointId) {
          addedMcpEndpointIds.push(added.endpointId)
          seenPkgs.add(pkg)
          try { await deps.mcp?.toggle({ endpointId: added.endpointId, enabled: true }) } catch { /* registered */ }
        }
        notes.push(`MCP ${preset.name}`)
      } catch (error) {
        failed = `MCP ${preset.name}：${error instanceof Error ? error.message : '安装失败'}`
        break
      }
    }
  }
  if (!failed) {
    const byId = new Map((deps.installed ?? []).map(item => [item.pluginId, item]))
    for (const gate of pack.toolGates) {
      const hit = byId.get(gate)
      if (!hit) {
        notes.push(`门闸 ${gate}（无安装记录，跳过）`)
        continue
      }
      if (hit.state === 'enabled') {
        notes.push(`门闸 ${gate}（已开）`)
        continue
      }
      try {
        await deps.plugins?.toggle({ installId: hit.installId, enabled: true })
        enabledGateInstallIds.push(hit.installId)
        notes.push(`门闸 ${gate}`)
      } catch (error) {
        failed = `门闸 ${gate}：${error instanceof Error ? error.message : '启用失败'}`
        break
      }
    }
  }
  if (failed) notes.push(`失败：${failed}`)
  const record: PackLedgerEntry = {
    packId: pack.id,
    addedMcpEndpointIds,
    enabledGateInstallIds,
    failed: failed || undefined,
  }
  upsertPackLedger(record)
  if (deps.plugins?.devCreate) {
    try {
      await deps.plugins.devCreate({
        workspaceId: 'chat',
        entrypoint: 'pack://manifest',
        manifest: {
          pluginId: pack.id,
          id: pack.id,
          kind: 'workflow',
          semver: '1.0.0',
          publisher: 'lunitide',
          skills: pack.skills,
          mcpPresetIds: pack.mcpPresetIds,
          toolGates: pack.toolGates,
          addedMcpEndpointIds,
          enabledGateInstallIds,
          failed: failed || undefined,
        },
      })
    } catch {
      /* already mounted or validation skipped */
    }
  }
  return { ok: !failed, notes, record }
}

async function pluginUninstallToken(installId: string): Promise<string> {
  const digest = await crypto.subtle.digest('SHA-256', new TextEncoder().encode(`plugin.uninstall|${installId}`))
  return Array.from(new Uint8Array(digest)).map(byte => byte.toString(16).padStart(2, '0')).join('')
}

export async function uninstallCapabilityPack(
  pack: CapabilityPackSpec,
  deps: { mcp?: McpBridge; plugins?: PluginBridge; listedPlugins?: Array<{ pluginId: string; installId: string; state: string }> },
): Promise<{ ok: boolean; notes: string[] }> {
  const entry = packLedgerEntry(pack.id)
  const row = (deps.listedPlugins ?? []).find(item => item.pluginId === pack.id && item.state !== 'uninstalled')
  const notes: string[] = []
  if (!entry && !row) {
    return { ok: true, notes: ['没有本包安装记录'] }
  }
  if (entry) {
    const otherGates = ownedByOtherPack(pack.id, item => item.enabledGateInstallIds)
    const otherMcp = ownedByOtherPack(pack.id, item => item.addedMcpEndpointIds)
    for (const installId of entry.enabledGateInstallIds) {
      if (otherGates.has(installId)) {
        notes.push('门闸保留（其他包仍在用）')
        continue
      }
      try {
        await deps.plugins?.toggle({ installId, enabled: false })
        notes.push('已关闭本包打开的门闸')
      } catch (error) {
        notes.push(`门闸未关：${error instanceof Error ? error.message : '失败'}`)
      }
    }
    for (const endpointId of entry.addedMcpEndpointIds) {
      if (otherMcp.has(endpointId)) {
        notes.push('MCP 保留（其他包仍在用）')
        continue
      }
      try {
        await deps.mcp?.toggle({ endpointId, enabled: false })
        notes.push('已停用本包新加的 MCP')
      } catch (error) {
        notes.push(`MCP 未停：${error instanceof Error ? error.message : '失败'}`)
      }
    }
  }
  if (row && deps.plugins?.uninstall) {
    try {
      await deps.plugins.uninstall({ installId: row.installId, confirmToken: await pluginUninstallToken(row.installId) })
      notes.push('已从插件行撤下本包')
    } catch (error) {
      notes.push(`插件行未撤：${error instanceof Error ? error.message : '失败'}`)
    }
  }
  notes.push('技能留在技能中心，不会卸载')
  removePackLedger(pack.id)
  return { ok: true, notes }
}
