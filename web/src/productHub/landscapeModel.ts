export const LANDSCAPE_STORAGE_KEY = 'lunitide:ph-landscape:v1'

export const LANDSCAPE_AXES = [
  {
    id: 'local',
    zh: '本机优先',
    en: 'Local-first',
    meaning: '引擎与数据是否主要在本机运行，而不是只开一个云会话窗口。',
    related: ['module.execution.computer', 'capability.computer.control', 'feature.execution.computer.launch'],
  },
  {
    id: 'media',
    zh: '媒体核验',
    en: 'Media verify',
    meaning: '播放成功是否回读 SMTC / 自有播放器相位，而不是「键已发送」。',
    related: ['plugin.media-session', 'feature.dialog.music.play', 'capability.tts.voice'],
  },
  {
    id: 'assets',
    zh: '技能 / MCP',
    en: 'Skills & MCP',
    meaning: '技能、专家、插件、MCP 是否收成同一资产域，并能被知识卡引用。',
    related: ['skillpack.computer-ops', 'mcp.windows-sysmon', 'plugin.computer-control'],
  },
  {
    id: 'hub',
    zh: '知识自描述',
    en: 'Self-describe',
    meaning: '产品能否用本体 + 图谱说明自己，再用诊断驱动修复，而不是静态营销页。',
    related: ['module.foundation.hub', 'landscape.frontier.self-heal-agents'],
  },
] as const

export type LandscapeAxisId = (typeof LANDSCAPE_AXES)[number]['id']
export type LandscapeScore = 'strong' | 'mid' | 'weak' | 'unknown'

export type LandscapeCell = {
  score: LandscapeScore
  note: string
  source: string
  date: string
}

export type LandscapeRow = {
  id: string
  name: string
  kind: 'self' | 'known' | 'custom'
  cells: Record<LandscapeAxisId, LandscapeCell>
}

const DATED = '2026-09-21'
const SRC = '各产品公开桌面端说明与 Lunitide 活源扫描，非营销排名'

function cell(score: LandscapeScore, note: string, source = SRC, date = DATED): LandscapeCell {
  return { score, note, source, date }
}

export const LUNITIDE_ROW: LandscapeRow = {
  id: 'lunitide',
  name: 'Lunitide / 月汐',
  kind: 'self',
  cells: {
    local: cell('strong', '引擎与 SQLite 在本机，电脑控制走本机权限。'),
    media: cell('strong', '以 SMTC / owned runtime 回读 playing，不把键回执当成功。'),
    assets: cell('strong', '技能、专家、MCP、插件收成同一资产域并进入知识卡。'),
    hub: cell('strong', '种子 + 活源生成说明书，诊断报告驱动内部模型/技能修复。'),
  },
}

const KNOWN: Record<string, LandscapeRow> = {
  cursor: {
    id: 'cursor',
    name: 'Cursor',
    kind: 'known',
    cells: {
      local: cell('mid', '本地工作区 + 云端模型；本机控制不是产品主轴。'),
      media: cell('weak', 'IDE 代理，不是桌面媒体控制产品。'),
      assets: cell('strong', '规则 / MCP / skills 是公开主路径。'),
      hub: cell('mid', '有规则与文档，无对等的产品本体中枢。'),
    },
  },
  copilot: {
    id: 'copilot',
    name: 'Copilot',
    kind: 'known',
    cells: {
      local: cell('weak', '桌面端仍以云会话为主。'),
      media: cell('weak', '常见「命令已发出」，少见播放相位回读。'),
      assets: cell('mid', '插件与连接器多，未收成同一产品知识卡。'),
      hub: cell('weak', '无对等的产品自描述中枢。'),
    },
  },
  'chatgpt desktop': {
    id: 'chatgpt-desktop',
    name: 'ChatGPT Desktop',
    kind: 'known',
    cells: {
      local: cell('weak', '桌面壳 + 云会话。'),
      media: cell('weak', '不是本机媒体核验产品。'),
      assets: cell('mid', 'GPTs / 工具调用，资产域与桌面控制分离。'),
      hub: cell('weak', '无对等本体图谱。'),
    },
  },
  'claude desktop': {
    id: 'claude-desktop',
    name: 'Claude Desktop',
    kind: 'known',
    cells: {
      local: cell('weak', '桌面端以云会话为主。'),
      media: cell('weak', '不是本机媒体核验产品。'),
      assets: cell('strong', 'MCP 是公开主入口。'),
      hub: cell('weak', '无对等产品自描述中枢。'),
    },
  },
  windsurf: {
    id: 'windsurf',
    name: 'Windsurf',
    kind: 'known',
    cells: {
      local: cell('mid', '本地工作区 + 云端代理。'),
      media: cell('weak', 'IDE 代理，不做桌面媒体核验。'),
      assets: cell('mid', '规则与工具链，未覆盖桌面技能/MCP 资产域。'),
      hub: cell('weak', '无对等产品本体中枢。'),
    },
  },
  trae: {
    id: 'trae',
    name: 'Trae',
    kind: 'known',
    cells: {
      local: cell('mid', '本地工作区 + 云端模型。'),
      media: cell('weak', '不是桌面媒体控制产品。'),
      assets: cell('mid', '规则与技能入口，未做成产品知识中枢。'),
      hub: cell('weak', '无对等本体 + 诊断闭环。'),
    },
  },
}

export const LANDSCAPE_SUGGESTIONS = ['Cursor', 'Copilot', 'ChatGPT Desktop', 'Claude Desktop', 'Windsurf', 'Trae']

export function normalizeCompetitor(name: string): string {
  return name.trim().toLocaleLowerCase().replace(/\s+/g, ' ')
}

export function parseCompetitorInput(text: string): string[] {
  return text.split(/[,，、;；\n]+/).map(item => item.trim()).filter(Boolean)
}

function unknownRow(name: string): LandscapeRow {
  return {
    id: `custom.${normalizeCompetitor(name).replace(/\s+/g, '-')}`,
    name,
    kind: 'custom',
    cells: {
      local: cell('unknown', '名单外竞品：这一维还没有带日期的出处。', '', ''),
      media: cell('unknown', '名单外竞品：这一维还没有带日期的出处。', '', ''),
      assets: cell('unknown', '名单外竞品：这一维还没有带日期的出处。', '', ''),
      hub: cell('unknown', '名单外竞品：这一维还没有带日期的出处。', '', ''),
    },
  }
}

export function compareLandscape(names: string[]): LandscapeRow[] {
  const seen = new Set<string>(['lunitide', '月汐', 'lunitide / 月汐'])
  const rows: LandscapeRow[] = [LUNITIDE_ROW]
  for (const raw of names) {
    const key = normalizeCompetitor(raw)
    if (!key || seen.has(key)) continue
    seen.add(key)
    const known = KNOWN[key]
    rows.push(known ? { ...known, name: raw.trim() || known.name } : unknownRow(raw.trim()))
  }
  return rows
}

export function loadLandscapeNames(): string[] {
  try {
    const raw = localStorage.getItem(LANDSCAPE_STORAGE_KEY)
    if (!raw) return []
    const parsed = JSON.parse(raw) as { names?: string[] }
    return Array.isArray(parsed.names) ? parsed.names.filter(item => typeof item === 'string') : []
  } catch {
    return []
  }
}

export function saveLandscapeNames(names: string[]): void {
  try {
    localStorage.setItem(LANDSCAPE_STORAGE_KEY, JSON.stringify({ v: 1, names }))
  } catch { /* private mode */ }
}

export function landscapeScoreLabel(score: LandscapeScore, zh: boolean): string {
  if (score === 'strong') return zh ? '强' : 'Strong'
  if (score === 'mid') return zh ? '中' : 'Mid'
  if (score === 'weak') return zh ? '弱' : 'Weak'
  return zh ? '待核验' : 'Unverified'
}
