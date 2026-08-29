import {expect, it} from 'vitest'
import {
  CONVERSATION_EXPERTS,
  CONVERSATION_EXPERT_PREFERRED_SKILLS,
  conversationExpertDivision,
  mergeComposeSkills,
  preferredSkillsForExperts,
  skillMatchesPreferred,
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
  expect(CONVERSATION_EXPERTS.some(item => item.id === 'architect-expert' && item.name === '系统架构师专家')).toBe(true)
  expect(CONVERSATION_EXPERTS.some(item => item.id === 'repo-expert' && item.name === '系统项目结构规范专家')).toBe(true)
  expect(CONVERSATION_EXPERTS.some(item => item.id === 'standards-expert' && item.name === '开发规范专家')).toBe(true)
  expect(CONVERSATION_EXPERTS.some(item => item.id === 'ui-designer' && item.name === 'UI专家')).toBe(true)
  expect(CONVERSATION_EXPERTS.some(item => item.id === 'test-expert' && item.name === '系统测试专家')).toBe(true)
  expect(CONVERSATION_EXPERTS.some(item => item.id === 'hardware-expert' && item.name === '硬件配置专家')).toBe(true)
  expect(CONVERSATION_EXPERTS.some(item => item.id === 'dev-expert' && item.name === '开发专家')).toBe(true)
  expect(conversationExpertDivision('test-expert')).toBe('testing')
  expect(conversationExpertDivision('hardware-expert')).toBe('engineering')
  expect(conversationExpertDivision('dev-expert')).toBe('engineering')
  expect(conversationExpertDivision('standards-expert')).toBe('engineering')
})

it('auto-attaches a landable preferredSkills list for each of the 13 specialists', () => {
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
    'test-expert': ['test-writer', 'e2e-browser', 'browser-automation'],
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

it('merges published compose skills onto 选专家 and drops them when unmounted', () => {
  const published = [
    {id: '01ARZ3NDEKTSV4RRFFQ69G5F01', name: 'tpl-slide-builder', entryPoint: 'builtin://slide-builder'},
    {id: '01ARZ3NDEKTSV4RRFFQ69G5F02', name: 'tpl-web-researcher', entryPoint: 'builtin://web-researcher'},
    {id: '01ARZ3NDEKTSV4RRFFQ69G5F03', name: 'tpl-mermaid-diagrams', entryPoint: 'builtin://mermaid-diagrams'},
    {id: '01ARZ3NDEKTSV4RRFFQ69G5F04', name: 'user-note', entryPoint: 'builtin://user-note'},
  ]
  expect(skillMatchesPreferred(published[0], ['slide-builder'])).toBe(true)
  const user = [{id: '01ARZ3NDEKTSV4RRFFQ69G5F04', name: 'user-note', entryPoint: 'builtin://user-note'}]
  const attached = mergeComposeSkills(user, published, [{name: 'PPT专家'}])
  expect(attached.map(item => item.name)).toEqual(['user-note', 'tpl-slide-builder', 'tpl-web-researcher', 'tpl-mermaid-diagrams'])
  const cleared = mergeComposeSkills(attached, published, [])
  expect(cleared.map(item => item.name)).toEqual(['user-note'])
})
