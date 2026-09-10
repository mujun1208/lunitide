import type { PluginBridge } from '../bridge/client'
import type { PluginPackInstallResult } from '../generated/bridge'
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
    description: '安装浏览器技能、Playwright MCP，并打开浏览器/抓取门闸。MCP 启用时会启动其本地服务进程。',
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
    description: '给 PPT 专家用的幻灯片与素材技能。MCP 启用时会启动其本地服务进程。',
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

// Kept only as a legacy storage name for migration tests. Ownership never
// reads renderer storage; authoritative references live in SQLite.
export const PACK_LEDGER_KEY = 'lunitide:capability-pack-ledger'
export type PackLedgerEntry = PluginPackInstallResult & {packId:string;failed?:string}
export function localizePackUserError(msg?: string): string {
  const text = (msg ?? '').trim()
  if (!text) return ''
  if (/is not installed/i.test(text)) return '权限开关尚未安装'
  if (/is not built in/i.test(text)) return '权限开关不是内置能力'
  if (/template unknown/i.test(text)) return '技能模板不存在'
  if (/unknown MCP preset/i.test(text)) return '未知的 MCP 预设'
  if (/^MCP .+ needs /i.test(text)) return 'MCP 需要先配置参数'
  if (/probe failed/i.test(text)) return '能力包组件探测失败'
  if (/capability pack not found/i.test(text)) return '能力包不存在'
  if (/manifest or operation changed/i.test(text)) return '能力包清单或操作已变化，请刷新后再试'
  if (/service unavailable/i.test(text)) return '能力包服务暂时不可用'
  if (/[\u4e00-\u9fff]/.test(text)) return text
  return '能力包操作失败，可继续或撤下'
}

export function packLedgerRecords(records:PluginPackInstallResult[]):PackLedgerEntry[]{
 return records.filter(row=>row.state!=='uninstalled').map(row=>({...row,packId:row.spec.id,failed:row.state==='installed'?undefined:(localizePackUserError(row.error)||'操作未完成，可继续或撤下')}))
}
export async function loadPackLedger(plugins:PluginBridge):Promise<PackLedgerEntry[]>{
 if(!plugins.packList)throw new Error('能力包服务不可用，请更新客户端')
 return packLedgerRecords((await plugins.packList()).items)
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

export async function installCapabilityPack(pack:CapabilityPackSpec,deps:{plugins?:PluginBridge;repair?:boolean}):Promise<{ok:boolean;notes:string[];record:PackLedgerEntry}>{
 if(!deps.plugins?.packInstall)throw new Error('能力包安装服务不可用')
 const result=await deps.plugins.packInstall({spec:pack,repair:deps.repair??false,confirmed:true})
 const error=localizePackUserError(result.error)
 const record={...result,packId:result.spec.id,error,failed:result.state==='installed'?undefined:error}
 return {ok:result.state==='installed',notes:result.state==='installed'?['已复核技能、MCP 与权限开关']: [error||'操作尚未完成'],record}
}
export async function uninstallCapabilityPack(pack:CapabilityPackSpec,deps:{plugins?:PluginBridge;record?:PackLedgerEntry}):Promise<{ok:boolean;notes:string[]}>{
 if(!deps.plugins?.packUninstall||!deps.record)throw new Error('缺少服务端安装记录，请刷新后重试')
 const result=await deps.plugins.packUninstall({packId:pack.id,expectedVersion:deps.record.version,confirmed:true})
 if(result.state!=='uninstalled')throw new Error(result.error||'能力包尚未撤下，可以重试')
 return {ok:true,notes:['已撤下本包独占的组件；共享或手动接管的组件保留','技能留在技能中心，不会卸载']}
}
