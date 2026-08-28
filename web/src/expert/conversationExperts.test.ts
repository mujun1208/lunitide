import {expect, it} from 'vitest'
import {CONVERSATION_EXPERTS, conversationExpertDivision} from './conversationExperts'

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
