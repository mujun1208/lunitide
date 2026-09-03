export const CONVERSATION_EXPERTS = [
  {id: 'ppt-expert', name: 'PPT专家'},
  {id: 'report-writer', name: '报告编写专家'},
  {id: 'novel-writer', name: '小说编写专家'},
  {id: 'excel-maker', name: 'Excel表格制作专家'},
  {id: 'ui-designer', name: 'UI专家'},
  {id: 'pm-expert', name: '产品经理专家'},
  {id: 'architect-expert', name: '系统架构师专家'},
  {id: 'db-expert', name: '数据库设计专家'},
  {id: 'repo-expert', name: '系统项目结构规范专家'},
  {id: 'standards-expert', name: '开发规范专家'},
  {id: 'test-expert', name: '系统测试专家'},
  {id: 'hardware-expert', name: '硬件配置专家'},
  {id: 'dev-expert', name: '开发专家'},
  {id: 'mro-expert', name: '航空机务维修专家'},
] as const

export type ConversationExpertID = typeof CONVERSATION_EXPERTS[number]['id']

export type ConversationExpertDivision = 'product' | 'data' | 'design' | 'engineering' | 'testing' | 'operations'

export type ExpertKind = 'agent' | 'prompt_skill'

/** Factory kit catalog IDs owned by the specialist. Hang on the expert, never on the composer. */
export const CONVERSATION_EXPERT_PREFERRED_SKILLS: Record<ConversationExpertID, readonly string[]> = {
  'ppt-expert': ['slide-builder', 'web-researcher', 'mermaid-diagrams'],
  'report-writer': ['web-researcher', 'docx-writer', 'anti-ai-prose', 'mermaid-diagrams'],
  'novel-writer': ['docx-writer', 'anti-ai-prose', 'content-brief', 'fiction-continuity'],
  'excel-maker': ['excel-analyst', 'csv-workbook'],
  'ui-designer': ['frontend-design', 'ui-components', 'design-system'],
  'pm-expert': ['pm-skill', 'brainstorming', 'grill-me', 'to-spec'],
  'architect-expert': ['improve-architecture', 'mermaid-diagrams', 'grill-me', 'security-review'],
  'db-expert': ['mermaid-diagrams', 'pm-phase-3'],
  'repo-expert': ['knowledge-index', 'mermaid-diagrams'],
  'standards-expert': ['code-reviewer', 'grill-me', 'git-status'],
  'test-expert': ['test-writer', 'e2e-browser', 'browser-automation', 'find-bug'],
  'hardware-expert': ['web-researcher', 'hardware-bom'],
  'dev-expert': ['implement', 'tdd-loop', 'debugger', 'code-reviewer', 'super-coders'],
  'mro-expert': ['aircraft-maintenance-engineer', 'mro-manual-rag', 'mro-fault-tree', 'mro-checklist'],
}

export function conversationExpertByNameOrID(idOrName: string): (typeof CONVERSATION_EXPERTS)[number] | undefined {
  const key = idOrName.trim()
  if (key === '航空机务专家') {
    return CONVERSATION_EXPERTS.find(item => item.id === 'mro-expert')
  }
  return CONVERSATION_EXPERTS.find(item => item.id === key || item.name === key)
}

export function conversationExpertKind(idOrName: string): ExpertKind {
  return conversationExpertByNameOrID(idOrName) ? 'agent' : 'prompt_skill'
}

export function expertCatalogKey(item: {name?: string; id?: string; catalogItemId?: string}): string {
  return (item.catalogItemId ?? item.id ?? item.name ?? '').trim()
}

export function expertKindOf(item: {name?: string; id?: string; catalogItemId?: string; kind?: string}): ExpertKind {
  if (item.kind === 'agent' || item.kind === 'prompt_skill') return item.kind
  return conversationExpertKind(expertCatalogKey(item) || item.name || '')
}

export const MCP_BIND_PREFIX = 'mcp:'
export const BRAIN_BIND_PREFIX = 'brain:'
export type ExpertBrain = 'lunitide' | 'codex' | 'claude'

export function brainBindKey(kind: ExpertBrain): string {
  return `${BRAIN_BIND_PREFIX}${kind}`
}

export function boundBrainFromKeys(keys: readonly string[]): ExpertBrain {
  for (const raw of keys) {
    if (!raw.startsWith(BRAIN_BIND_PREFIX)) continue
    const id = raw.slice(BRAIN_BIND_PREFIX.length).trim().toLowerCase()
    if (id === 'codex' || id === 'claude') return id
  }
  return 'lunitide'
}

export const CONVERSATION_EXPERT_REQUIRED_TOOLS: Record<ConversationExpertID, readonly string[]> = {
  'ppt-expert': ['web.search', 'web.fetch', 'pptx.gen', 'skill.invoke', 'image.generate', 'todo.write'],
  'report-writer': ['web.search', 'web.fetch', 'docx.gen', 'skill.invoke', 'todo.write'],
  'novel-writer': ['docx.gen', 'skill.invoke', 'web.search', 'todo.write'],
  'excel-maker': ['excel.gen', 'excel.parse', 'skill.invoke', 'todo.write'],
  'ui-designer': ['workspace.write', 'skill.invoke', 'todo.write'],
  'pm-expert': ['web.search', 'skill.invoke', 'docx.gen', 'todo.write'],
  'architect-expert': ['skill.invoke', 'workspace.read', 'workspace.search', 'todo.write'],
  'db-expert': ['skill.invoke', 'workspace.read', 'todo.write', 'workspace.write'],
  'repo-expert': ['workspace.list', 'workspace.read', 'skill.invoke', 'todo.write'],
  'standards-expert': ['skill.invoke', 'workspace.read', 'todo.write'],
  'test-expert': ['skill.invoke', 'browser.act', 'todo.write'],
  'hardware-expert': ['web.search', 'excel.gen', 'skill.invoke', 'todo.write'],
  'dev-expert': ['workspace.read', 'workspace.edit', 'command.run', 'skill.invoke', 'todo.write'],
  'mro-expert': ['kb.search', 'graph.expand', 'workspace.write', 'docx.gen', 'excel.gen', 'todo.write', 'datasource.query'],
}

export const CONVERSATION_EXPERT_PREFERRED_MCP: Record<ConversationExpertID, readonly string[]> = {
  'ppt-expert': ['playwright'],
  'report-writer': ['fetch'],
  'novel-writer': [],
  'excel-maker': [],
  'ui-designer': [],
  'pm-expert': [],
  'architect-expert': [],
  'db-expert': [],
  'repo-expert': ['filesystem'],
  'standards-expert': [],
  'test-expert': ['playwright'],
  'hardware-expert': [],
  'dev-expert': ['filesystem'],
  'mro-expert': [],
}

export const CONVERSATION_EXPERT_MCP_FALLBACK: Record<ConversationExpertID, string> = {
  'ppt-expert': '未连接 Playwright MCP 时用 browser.act；素材用 web.search / web.fetch。',
  'report-writer': '未连接 Fetch MCP 时用 web.fetch；检索用 web.search。',
  'novel-writer': '时代/设定核对用 web.search / web.fetch。',
  'excel-maker': 'CSV 与表格走 excel.parse / excel.gen。',
  'ui-designer': '不另开 UI 专家卡，不装 shadcn MCP。',
  'pm-expert': '市场调研用 web.search。',
  'architect-expert': 'C4 用对话内 mermaid。证据缺口标 MISSING。',
  'db-expert': '没有 SQLite MCP。用 mermaid erDiagram + workspace.write 写 DDL 草稿。',
  'repo-expert': '未连接 Filesystem MCP 时用 workspace.list/read。',
  'standards-expert': 'format/lint/test 走白名单 command.run。',
  'test-expert': '未连接 Playwright MCP 时用 browser.act。',
  'hardware-expert': '价格/SKU 必须 web.search，标待确认。',
  'dev-expert': '未连接 Filesystem MCP 时用 workspace.*。Git 走 command.run 白名单。',
  'mro-expert': '手册走 kb.search。库存走已探测的 datasource.query。不另装 MRO 云 MCP。',
}

export function shouldOpenExpertAsColleague(idOrName: string): boolean {
  return conversationExpertKind(idOrName) === 'agent'
}

export function splitBoundKeys(keys: readonly string[]): {skills: string[]; mcp: string[]; brain: ExpertBrain} {
  const skills: string[] = []
  const mcp: string[] = []
  for (const raw of keys) {
    const key = raw.trim()
    if (!key) continue
    if (key.startsWith(MCP_BIND_PREFIX)) {
      const id = key.slice(MCP_BIND_PREFIX.length).trim()
      if (id) mcp.push(id)
      continue
    }
    if (key.startsWith(BRAIN_BIND_PREFIX)) continue
    skills.push(key)
  }
  return {skills, mcp, brain: boundBrainFromKeys(keys)}
}

export function mcpBindKey(id: string): string {
  return `${MCP_BIND_PREFIX}${id.trim()}`
}

export function preferredMcpForExperts(experts: ReadonlyArray<{name?: string; id?: string}>): string[] {
  return collectPreferred(experts, hit => CONVERSATION_EXPERT_PREFERRED_MCP[hit.id])
}

export function requiredToolsForExperts(experts: ReadonlyArray<{name?: string; id?: string}>): string[] {
  return collectPreferred(experts, hit => CONVERSATION_EXPERT_REQUIRED_TOOLS[hit.id])
}

export function mcpFallbackForExpert(idOrName: string): string {
  const hit = conversationExpertByNameOrID(idOrName)
  return hit ? CONVERSATION_EXPERT_MCP_FALLBACK[hit.id] : ''
}

function collectPreferred(experts: ReadonlyArray<{name?: string; id?: string}>, pick: (hit: (typeof CONVERSATION_EXPERTS)[number]) => readonly string[]): string[] {
  const seen = new Set<string>()
  const out: string[] = []
  for (const expert of experts) {
    const hit = conversationExpertByNameOrID(expert.name ?? '') ?? conversationExpertByNameOrID(expert.id ?? '')
    if (!hit) continue
    for (const id of pick(hit)) {
      if (seen.has(id)) continue
      seen.add(id)
      out.push(id)
    }
  }
  return out
}

export function preferredSkillsForExperts(experts: ReadonlyArray<{name?: string; id?: string}>): string[] {
  return collectPreferred(experts, hit => CONVERSATION_EXPERT_PREFERRED_SKILLS[hit.id])
}

export function missingPreferredSkills(preferred: readonly string[], published: ReadonlyArray<{name: string; entryPoint?: string}>): string[] {
  return preferred.filter(key => !published.some(item => skillMatchesPreferred(item, [key])))
}

export function skillMatchesPreferred(skill: {name: string; entryPoint?: string}, preferred: readonly string[]): boolean {
  const name = skill.name.trim()
  const entry = (skill.entryPoint ?? '').trim()
  for (const raw of preferred) {
    const p = raw.trim()
    if (!p) continue
    if (name === p || name === `tpl-${p}` || name.replace(/^tpl-/, '') === p) return true
    if (entry.endsWith(`://${p}`) || entry.endsWith(`://tpl-${p}`)) return true
  }
  return false
}

export const CONVERSATION_EXPERT_EMOJI: Record<ConversationExpertID, string> = {
  'ppt-expert': '📊',
  'report-writer': '📝',
  'novel-writer': '📖',
  'excel-maker': '▦',
  'ui-designer': '🎨',
  'pm-expert': '📋',
  'architect-expert': '🏗',
  'db-expert': '🗄',
  'repo-expert': '📁',
  'standards-expert': '📐',
  'test-expert': '☑',
  'hardware-expert': '⊞',
  'dev-expert': '⌨',
  'mro-expert': '✈',
}

export function conversationExpertRole(division: string): string {
  switch (division) {
    case 'design': return '设计师'
    case 'engineering':
    case 'operations':
    case 'security': return '工程师'
    case 'product':
    case 'project-management': return '产品'
    default: return '研究员'
  }
}

export function conversationExpertEmoji(idOrName: string): string {
  const hit = conversationExpertByNameOrID(idOrName)
  return hit ? CONVERSATION_EXPERT_EMOJI[hit.id] : '🌙'
}

export function conversationExpertDivision(id: string): ConversationExpertDivision {
  if (id === 'mro-expert') return 'operations'
  if (id === 'ui-designer') return 'design'
  if (id === 'excel-maker' || id === 'db-expert') return 'data'
  if (id === 'test-expert') return 'testing'
  if (
    id === 'architect-expert' ||
    id === 'repo-expert' ||
    id === 'standards-expert' ||
    id === 'hardware-expert' ||
    id === 'dev-expert'
  ) {
    return 'engineering'
  }
  return 'product'
}
