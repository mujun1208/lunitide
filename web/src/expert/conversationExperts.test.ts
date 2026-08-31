import {expect, it} from 'vitest'
import {
  CONVERSATION_EXPERTS,
  CONVERSATION_EXPERT_PREFERRED_SKILLS,
  conversationExpertDivision,
  conversationExpertEmoji,
  conversationExpertKind,
  conversationExpertRole,
  expertCatalogKey,
  expertKindOf,
  mcpBindKey,
  missingPreferredSkills,
  preferredMcpForExperts,
  preferredSkillsForExperts,
  shouldOpenExpertAsColleague,
  skillMatchesPreferred,
  splitBoundKeys,
} from './conversationExperts'

it('registers the conversation specialists for Expert Center and 对话 picker', () => {
  expect(CONVERSATION_EXPERTS.map(item => item.id)).toEqual(expect.arrayContaining([
    'ppt-expert', 'report-writer', 'novel-writer', 'excel-maker', 'ui-designer',
    'pm-expert', 'architect-expert', 'db-expert', 'repo-expert', 'standards-expert',
    'test-expert', 'hardware-expert', 'dev-expert',
  ]))
  expect(CONVERSATION_EXPERTS.map(item => item.name)).toEqual(expect.arrayContaining([
    'PPT专家', '报告编写专家', '小说编写专家', 'Excel表格制作专家', 'UI专家',
    '产品经理专家', '系统架构师专家', '数据库设计专家', '系统项目结构规范专家', '开发规范专家',
    '系统测试专家', '硬件配置专家', '开发专家',
  ]))
  expect(conversationExpertDivision('test-expert')).toBe('testing')
  expect(conversationExpertDivision('hardware-expert')).toBe('engineering')
  expect(conversationExpertDivision('dev-expert')).toBe('engineering')
  expect(conversationExpertDivision('standards-expert')).toBe('engineering')
  expect(conversationExpertKind('PPT专家')).toBe('agent')
  expect(conversationExpertKind('ppt-expert')).toBe('agent')
  expect(conversationExpertKind('安全工程师')).toBe('prompt_skill')
  expect(conversationExpertRole('design')).toBe('设计师')
  expect(conversationExpertRole('security')).toBe('工程师')
  expect(conversationExpertEmoji('PPT专家')).toBe('📊')
})

it('keeps a factory kit preferredSkills list for each of the 13 specialists', () => {
  const want: Record<string, string[]> = {
    'ppt-expert': ['slide-builder', 'web-researcher', 'mermaid-diagrams'],
    'report-writer': ['web-researcher', 'docx-writer', 'anti-ai-prose'],
    'novel-writer': ['docx-writer', 'anti-ai-prose', 'fiction-continuity'],
    'excel-maker': ['excel-analyst', 'csv-workbook'],
    'ui-designer': ['frontend-design', 'ui-components', 'design-system'],
    'pm-expert': ['pm-skill', 'grill-me', 'to-spec'],
    'architect-expert': ['improve-architecture', 'mermaid-diagrams'],
    'db-expert': ['mermaid-diagrams', 'pm-phase-3'],
    'repo-expert': ['knowledge-index', 'mermaid-diagrams'],
    'standards-expert': ['code-reviewer', 'grill-me'],
    'test-expert': ['test-writer', 'e2e-browser', 'browser-automation', 'find-bug'],
    'hardware-expert': ['web-researcher', 'hardware-bom'],
    'dev-expert': ['implement', 'tdd-loop', 'debugger', 'code-reviewer'],
  }
  expect(CONVERSATION_EXPERTS).toHaveLength(13)
  for (const expert of CONVERSATION_EXPERTS) {
    const skills = CONVERSATION_EXPERT_PREFERRED_SKILLS[expert.id]
    expect(skills.length, expert.id).toBeGreaterThan(0)
    for (const id of want[expert.id]) {
      expect(skills, expert.id).toContain(id)
    }
    const composed = preferredSkillsForExperts([{name: expert.name}])
    for (const id of want[expert.id]) {
      expect(composed, expert.name).toContain(id)
    }
  }
})

it('lists preferred factory kits that are not yet published', () => {
  expect(missingPreferredSkills(['slide-builder', 'web-researcher'], [
    {name: 'tpl-slide-builder', entryPoint: 'builtin://slide-builder'},
  ])).toEqual(['web-researcher'])
})

it('matches published catalog names without treating them as composer chips', () => {
  const published = {id: '01ARZ3NDEKTSV4RRFFQ69G5F01', name: 'tpl-slide-builder', entryPoint: 'builtin://slide-builder'}
  expect(skillMatchesPreferred(published, ['slide-builder'])).toBe(true)
  expect(preferredSkillsForExperts([{name: 'PPT专家'}])).toEqual(['slide-builder', 'web-researcher', 'mermaid-diagrams'])
})

it('keeps a renamed specialist as an agent when the catalog id is present', () => {
  expect(expertCatalogKey({name: '演示顾问', catalogItemId: 'ppt-expert'})).toBe('ppt-expert')
  expect(expertKindOf({name: '演示顾问', catalogItemId: 'ppt-expert'})).toBe('agent')
  expect(expertKindOf({name: '演示顾问'})).toBe('prompt_skill')
  expect(preferredSkillsForExperts([{name: '演示顾问', id: 'ppt-expert'}])).toEqual([
    'slide-builder', 'web-researcher', 'mermaid-diagrams',
  ])
})

it('opens conversation specialists as colleagues and stores MCP bindings beside skills', () => {
  expect(shouldOpenExpertAsColleague('PPT专家')).toBe(true)
  expect(shouldOpenExpertAsColleague('安全工程师')).toBe(false)
  expect(preferredMcpForExperts([{name: 'PPT专家'}])).toEqual(['playwright'])
  expect(splitBoundKeys(['slide-builder', mcpBindKey('playwright'), '  '])).toEqual({
    skills: ['slide-builder'],
    mcp: ['playwright'],
    brain: 'lunitide',
  })
})
