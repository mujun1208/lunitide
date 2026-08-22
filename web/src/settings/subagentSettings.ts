import type { ProviderDTO } from '../generated/bridge'

export type SubagentDelegationMode = 'disabled' | 'explicit' | 'proactive'

export type SubagentProfileOverride = {
  enabled?: boolean
  providerId?: string
  modelId?: string
}

export type CustomSubagentProfile = {
  id: string
  displayName: string
  description?: string
  systemPrompt: string
  readCaps: string[]
  maxSteps?: number
  budgetTokens?: number
}

export type SubagentSettings = {
  rev: number
  delegationMode: SubagentDelegationMode
  overrides: Record<string, SubagentProfileOverride>
  customProfiles: CustomSubagentProfile[]
}

const STORAGE_KEY = 'lunitide:subagents'
const SETTINGS_REV = 1

export const BUILTIN_SUBAGENT_IDS = [
  'explore',
  'research',
  'general-purpose',
  'review',
  'browser',
  'shell',
  'writer',
  'test',
] as const

export type BuiltinSubagentId = (typeof BUILTIN_SUBAGENT_IDS)[number]

export const BUILTIN_SUBAGENT_META: Record<BuiltinSubagentId, { label: string; desc: string; tools: string }> = {
  explore: { label: 'Explore', desc: '只读代码库广搜，适合并行查文件与引用。', tools: '6 个工具' },
  research: { label: 'Research', desc: '网页搜索与抓取，产出带链接的调研摘要。', tools: '2 个工具' },
  'general-purpose': { label: 'General purpose', desc: '通用只读多步调查，与 Zed 内置同名。', tools: '全部只读' },
  review: { label: 'Review', desc: '结构化代码/文档审查，引用 path:line。', tools: '5 个工具' },
  browser: { label: 'Browser', desc: '公开页 navigate/read，过滤 DOM 噪声。', tools: '4 个工具' },
  shell: { label: 'Shell', desc: '只读命令输出隔离在子上下文，主对话更干净。', tools: '4 个工具' },
  writer: { label: 'Writer', desc: '只读上下文后在报告中起草文稿（不写盘）。', tools: '4 个工具' },
  test: { label: 'Test', desc: '分析测试缺口并建议用例。', tools: '4 个工具' },
}

export const READ_CAP_OPTIONS = [
  'fs.read', 'fs.tree', 'fs.grep', 'fs.glob', 'fs.stat', 'fs.readMany',
  'web.search', 'web.fetch',
  'browser.act:navigate', 'browser.act:read', 'browser.act:snapshot',
  'evidence.list',
] as const

export function defaultSubagentSettings(): SubagentSettings {
  const overrides: Record<string, SubagentProfileOverride> = {}
  for (const id of BUILTIN_SUBAGENT_IDS) overrides[id] = { enabled: true }
  return { rev: SETTINGS_REV, delegationMode: 'proactive', overrides, customProfiles: [] }
}

export function loadSubagentSettings(): SubagentSettings {
  const fallback = defaultSubagentSettings()
  try {
    const raw = localStorage.getItem(STORAGE_KEY)
    if (!raw) return fallback
    const parsed = JSON.parse(raw) as Partial<SubagentSettings>
    const mode = parsed.delegationMode
    const delegationMode: SubagentDelegationMode =
      mode === 'disabled' || mode === 'explicit' || mode === 'proactive' ? mode : fallback.delegationMode
    const overrides = { ...fallback.overrides, ...(parsed.overrides ?? {}) }
    for (const id of BUILTIN_SUBAGENT_IDS) {
      overrides[id] = { enabled: overrides[id]?.enabled !== false, ...overrides[id] }
    }
    const customProfiles = Array.isArray(parsed.customProfiles)
      ? parsed.customProfiles.filter(p => p && typeof p.id === 'string' && typeof p.systemPrompt === 'string').slice(0, 16)
      : []
    return { rev: SETTINGS_REV, delegationMode, overrides, customProfiles }
  } catch {
    return fallback
  }
}

export function saveSubagentSettings(settings: SubagentSettings): void {
  try {
    localStorage.setItem(STORAGE_KEY, JSON.stringify({ ...settings, rev: SETTINGS_REV }))
  } catch { /* ignore */ }
}

export function buildSubagentChatPolicy(settings: SubagentSettings): {
  delegationMode: SubagentDelegationMode
  overrides: Record<string, SubagentProfileOverride>
  customProfiles: CustomSubagentProfile[]
} {
  return {
    delegationMode: settings.delegationMode,
    overrides: settings.overrides,
    customProfiles: settings.customProfiles.map(p => ({
      ...p,
      id: p.id.trim(),
      displayName: p.displayName.trim() || p.id.trim(),
      systemPrompt: p.systemPrompt.trim(),
      readCaps: p.readCaps?.length ? [...p.readCaps] : ['web.search', 'web.fetch'],
      maxSteps: p.maxSteps ?? 4,
      budgetTokens: p.budgetTokens ?? 8192,
    })),
  }
}

export function configuredModelOptions(providers: readonly ProviderDTO[]): Array<{ value: string; label: string; providerId: string; modelId: string }> {
  const out: Array<{ value: string; label: string; providerId: string; modelId: string }> = []
  for (const p of providers) {
    if (p.status !== 'enabled' || p.credentialState !== 'configured' || !p.models.length) continue
    for (const m of p.models) {
      out.push({
        value: `${p.id}\u0000${m.modelId}`,
        label: `${m.displayName} · ${p.name}`,
        providerId: p.id,
        modelId: m.modelId,
      })
    }
  }
  return out
}

export function inheritModelLabel(): string {
  return '继承主对话模型'
}
