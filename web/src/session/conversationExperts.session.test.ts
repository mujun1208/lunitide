import {describe, expect, it} from 'vitest'
import {CONVERSATION_EXPERTS, conversationExpertDivision} from '../expert/conversationExperts'
import {expertInitial, readMountedExperts, writeMountedExperts} from './sessionExperts'

describe('conversation specialists on a 对话 session', () => {
  it('can mount 系统架构师专家 with the other conversation specialists', () => {
    const sessionId = '01ARZ3NDEKTSV4RRFFQ69G5FAB'
    const mounted = CONVERSATION_EXPERTS.map((item, index) => ({
      expertId: `01ARZ3NDEKTSV4RRFFQ69G5F${String(10 + index).padStart(2, '0')}`,
      name: item.name,
      division: conversationExpertDivision(item.id),
    }))
    writeMountedExperts(sessionId, mounted)
    const names = readMountedExperts(sessionId).map(item => item.name)
    expect(names).toEqual(CONVERSATION_EXPERTS.map(item => item.name))
    expect(names).toContain('系统架构师专家')
    expect(names).toContain('数据库设计专家')
    expect(names).toContain('系统项目结构规范专家')
    expect(names).toContain('开发规范专家')
    expect(names).toContain('UI专家')
    expect(names).toContain('系统测试专家')
    expect(names).toContain('硬件配置专家')
    expect(names).toContain('开发专家')
    expect(expertInitial('系统架构师专家')).toBe('系')
    expect(expertInitial('数据库设计专家')).toBe('数')
    expect(expertInitial('系统项目结构规范专家')).toBe('系')
    expect(expertInitial('开发规范专家')).toBe('开')
    expect(expertInitial('系统测试专家')).toBe('系')
    expect(expertInitial('硬件配置专家')).toBe('硬')
    expect(expertInitial('开发专家')).toBe('开')
  })
})
