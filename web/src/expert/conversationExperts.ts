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
] as const

export type ConversationExpertID = typeof CONVERSATION_EXPERTS[number]['id']

export type ConversationExpertDivision = 'product' | 'data' | 'design' | 'engineering' | 'testing'

/** Catalog template IDs auto-attached when the user 选专家. Keep in sync with conversation_experts.json. */
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
  'test-expert': ['test-writer', 'e2e-browser', 'browser-automation'],
  'hardware-expert': ['web-researcher', 'hardware-bom'],
  'dev-expert': ['implement', 'tdd-loop', 'debugger', 'code-reviewer', 'super-coders'],
}

const ALL_COMPOSE_SKILL_IDS = [...new Set(Object.values(CONVERSATION_EXPERT_PREFERRED_SKILLS).flat())]

export function conversationExpertByNameOrID(idOrName: string): (typeof CONVERSATION_EXPERTS)[number] | undefined {
  const key = idOrName.trim()
  return CONVERSATION_EXPERTS.find(item => item.id === key || item.name === key)
}

export function preferredSkillsForExperts(experts: ReadonlyArray<{name?: string; id?: string}>): string[] {
  const seen = new Set<string>()
  const out: string[] = []
  for (const expert of experts) {
    const hit = conversationExpertByNameOrID(expert.name ?? '') ?? conversationExpertByNameOrID(expert.id ?? '')
    if (!hit) continue
    for (const id of CONVERSATION_EXPERT_PREFERRED_SKILLS[hit.id]) {
      if (seen.has(id)) continue
      seen.add(id)
      out.push(id)
    }
  }
  return out
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

export function mergeComposeSkills<T extends {id: string; name: string; entryPoint?: string}>(
  current: readonly T[],
  published: readonly T[],
  experts: ReadonlyArray<{name?: string; id?: string}>,
): T[] {
  const preferred = preferredSkillsForExperts(experts)
  const userKept = current.filter(item => !skillMatchesPreferred(item, ALL_COMPOSE_SKILL_IDS))
  const compose = published.filter(item => skillMatchesPreferred(item, preferred))
  const seen = new Set(userKept.map(item => item.id))
  const next = [...userKept]
  for (const item of compose) {
    if (seen.has(item.id)) continue
    seen.add(item.id)
    next.push(item)
  }
  if (next.length === current.length && next.every((item, i) => item.id === current[i]?.id)) return current as T[]
  return next
}

export function conversationExpertDivision(id: string): ConversationExpertDivision {
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
