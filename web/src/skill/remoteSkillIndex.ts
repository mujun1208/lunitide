export interface RemoteSkillSource {
  id: string
  name: string
  repo: string
  contentsUrl: string
  description: string
  skills: Array<{ slug: string; title: string; summary: string }>
}

export const REMOTE_SKILL_SOURCES: RemoteSkillSource[] = [
  {
    id: 'anthropics-skills',
    name: 'anthropics/skills',
    repo: 'https://github.com/anthropics/skills',
    contentsUrl: 'https://api.github.com/repos/anthropics/skills/contents/skills',
    description: 'Anthropic 公开技能示例，导入仍走 GitHub 审批。',
    skills: [
      { slug: 'docx', title: 'DOCX', summary: 'Word 文档约定' },
      { slug: 'pptx', title: 'PPTX', summary: '演示文稿约定' },
      { slug: 'xlsx', title: 'XLSX', summary: '表格约定' },
      { slug: 'pdf', title: 'PDF', summary: 'PDF 阅读与摘录' },
      { slug: 'webapp-testing', title: 'Webapp testing', summary: '浏览器验收' },
      { slug: 'algorithmic-art', title: 'Algorithmic art', summary: '程序化视觉生成' },
      { slug: 'brand-guidelines', title: 'Brand guidelines', summary: '品牌视觉约定' },
      { slug: 'canvas-design', title: 'Canvas design', summary: '画布与海报' },
      { slug: 'doc-coauthoring', title: 'Doc coauthoring', summary: '协作写文档' },
      { slug: 'frontend-design', title: 'Frontend design', summary: '前端界面约定' },
      { slug: 'mcp-builder', title: 'MCP builder', summary: 'MCP 服务器脚手架' },
      { slug: 'skill-creator', title: 'Skill creator', summary: '编写新技能' },
      { slug: 'slack-gif-creator', title: 'Slack GIF', summary: '动图约定' },
      { slug: 'theme-factory', title: 'Theme factory', summary: '主题与配色' },
      { slug: 'web-artifacts-builder', title: 'Web artifacts', summary: '网页产物' },
    ],
  },
  {
    id: 'mattpocock-skills',
    name: 'mattpocock/skills',
    repo: 'https://github.com/mattpocock/skills',
    contentsUrl: 'https://api.github.com/repos/mattpocock/skills/contents',
    description: 'TypeScript / 工程向技能，导入仍走审批。',
    skills: [
      { slug: 'typescript', title: 'TypeScript', summary: 'TS 工程约定' },
      { slug: 'review', title: 'Review', summary: '代码审查清单' },
      { slug: 'tdd', title: 'TDD', summary: '测试先行' },
      { slug: 'refactor', title: 'Refactor', summary: '安全重构清单' },
    ],
  },
  {
    id: 'anbeime-skill',
    name: 'anbeime/skill',
    repo: 'https://github.com/anbeime/skill',
    contentsUrl: 'https://api.github.com/repos/anbeime/skill/contents',
    description: '中文社区技能仓，命中后打开导入向导，不会静默下载。',
    skills: [
      { slug: 'writing', title: '写作', summary: '中文写作约定' },
      { slug: 'research', title: '调研', summary: '资料检索约定' },
      { slug: 'meeting-notes', title: '会议纪要', summary: '纪要整理约定' },
      { slug: 'weekly-report', title: '周报', summary: '周报结构约定' },
    ],
  },
]

export interface RemoteSkillHit {
  sourceId: string
  sourceName: string
  repo: string
  slug: string
  title: string
  summary: string
}

export function searchLocalRemoteSkills(query: string): RemoteSkillHit[] {
  const q = query.trim().toLowerCase()
  if (!q) return []
  const hits: RemoteSkillHit[] = []
  for (const source of REMOTE_SKILL_SOURCES) {
    const sourceHit = source.name.toLowerCase().includes(q) || source.description.toLowerCase().includes(q)
    for (const skill of source.skills) {
      if (sourceHit || skill.slug.toLowerCase().includes(q) || skill.title.toLowerCase().includes(q) || skill.summary.toLowerCase().includes(q)) {
        hits.push({ sourceId: source.id, sourceName: source.name, repo: source.repo, slug: skill.slug, title: skill.title, summary: skill.summary })
      }
    }
  }
  return hits
}

export function bundledRemoteSkillCount(): number {
  return REMOTE_SKILL_SOURCES.reduce((n, source) => n + source.skills.length, 0)
}

/** @deprecated use searchRemoteSkillsLive; kept as the bundled-catalog fallback. */
export function searchRemoteSkills(query: string): RemoteSkillHit[] {
  return searchLocalRemoteSkills(query)
}

export interface RemoteSkillSearchResult {
  hits: RemoteSkillHit[]
  fallback: boolean
  notice?: string
}

interface GitHubContent {
  name?: string
  type?: string
  path?: string
}

async function listRepoSkills(source: RemoteSkillSource): Promise<Array<{ slug: string; title: string; summary: string }>> {
  const response = await fetch(source.contentsUrl, { headers: { Accept: 'application/vnd.github+json' } })
  if (!response.ok) throw new Error(String(response.status))
  const rows = await response.json() as GitHubContent[]
  if (!Array.isArray(rows)) throw new Error('not a directory listing')
  const skills: Array<{ slug: string; title: string; summary: string }> = []
  for (const row of rows) {
    if (row.type !== 'dir' || !row.name || row.name.startsWith('.')) continue
    skills.push({ slug: row.name, title: row.name, summary: `${source.name} / ${row.path || row.name}` })
  }
  if (skills.length === 0) throw new Error('empty listing')
  return skills
}

const SKILL_INDEX_CACHE = 'lunitide:remote-skill-index'

type CachedSkillListing = { sourceId: string; skills: Array<{ slug: string; title: string; summary: string }> }

function saveSkillIndexCache(listings: CachedSkillListing[]): void {
  try {
    localStorage.setItem(SKILL_INDEX_CACHE, JSON.stringify({ savedAt: Date.now(), listings }))
  } catch { /* ignore */ }
}

export function loadSkillIndexCache(): CachedSkillListing[] {
  try {
    const raw = localStorage.getItem(SKILL_INDEX_CACHE)
    if (!raw) return []
    const parsed = JSON.parse(raw) as { listings?: CachedSkillListing[] }
    return Array.isArray(parsed.listings) ? parsed.listings : []
  } catch {
    return []
  }
}

function searchCachedSkills(query: string): RemoteSkillHit[] {
  const q = query.trim().toLowerCase()
  const cached = loadSkillIndexCache()
  if (!q || cached.length === 0) return []
  const byId = new Map(REMOTE_SKILL_SOURCES.map(source => [source.id, source]))
  const hits: RemoteSkillHit[] = []
  for (const listing of cached) {
    const source = byId.get(listing.sourceId)
    if (!source) continue
    const sourceHit = source.name.toLowerCase().includes(q) || source.description.toLowerCase().includes(q)
    for (const skill of listing.skills) {
      if (sourceHit || skill.slug.toLowerCase().includes(q) || skill.title.toLowerCase().includes(q) || skill.summary.toLowerCase().includes(q)) {
        hits.push({ sourceId: source.id, sourceName: source.name, repo: source.repo, slug: skill.slug, title: skill.title, summary: skill.summary })
      }
    }
  }
  return hits
}

export async function searchRemoteSkillsLive(query: string): Promise<RemoteSkillSearchResult> {
  const q = query.trim().toLowerCase()
  if (!q) return { hits: [], fallback: false }
  try {
    const listings = await Promise.all(REMOTE_SKILL_SOURCES.map(async source => ({ source, skills: await listRepoSkills(source) })))
    saveSkillIndexCache(listings.map(item => ({ sourceId: item.source.id, skills: item.skills })))
    const hits: RemoteSkillHit[] = []
    for (const { source, skills } of listings) {
      const sourceHit = source.name.toLowerCase().includes(q) || source.description.toLowerCase().includes(q)
      for (const skill of skills) {
        if (sourceHit || skill.slug.toLowerCase().includes(q) || skill.title.toLowerCase().includes(q) || skill.summary.toLowerCase().includes(q)) {
          hits.push({ sourceId: source.id, sourceName: source.name, repo: source.repo, slug: skill.slug, title: skill.title, summary: skill.summary })
        }
      }
    }
    return { hits, fallback: false }
  } catch {
    const cached = searchCachedSkills(query)
    if (cached.length > 0) {
      return { hits: cached, fallback: true, notice: '远程索引暂时不可用，已使用上次成功缓存。' }
    }
    return {
      hits: searchLocalRemoteSkills(query),
      fallback: true,
      notice: `远程索引暂时不可用，已使用本地技能目录（${bundledRemoteSkillCount()} 条）。`,
    }
  }
}
