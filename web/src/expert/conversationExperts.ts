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
