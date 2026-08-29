import {describe, expect, it} from 'vitest'
import {
  formatUserAskFollowUp,
  parseUserAskSummary,
  USER_ASK_OTHER_ID,
  userAskActivitySummary,
  userAskChoiceReady,
} from './userAsk'

const sample = {
  title: '需求边界',
  questions: [
    {
      id: 'deploy',
      prompt: '部署方式',
      options: [
        {id: 'k8s', label: '容器化'},
        {id: 'vm', label: '虚拟机'},
      ],
    },
    {
      id: 'db',
      prompt: '数据库',
      options: [
        {id: 'pg', label: 'PostgreSQL'},
        {id: 'mysql', label: 'MySQL'},
      ],
    },
  ],
}

describe('user.ask pack', () => {
  it('parses questions from a tool summary JSON blob', () => {
    const pack = parseUserAskSummary(JSON.stringify(sample))
    expect(pack?.title).toBe('需求边界')
    expect(pack?.questions).toHaveLength(2)
    expect(pack?.questions[0].options.map(o => o.label)).toEqual(['容器化', '虚拟机'])
  })

  it('rejects summaries without at least two options', () => {
    expect(parseUserAskSummary('{"questions":[{"prompt":"x","options":[{"label":"only"}]}]}')).toBeUndefined()
  })

  it('requires other-text before a question is ready', () => {
    const q = sample.questions[0]
    expect(userAskChoiceReady(q, {optionId: 'k8s'})).toBe(true)
    expect(userAskChoiceReady(q, {optionId: USER_ASK_OTHER_ID})).toBe(false)
    expect(userAskChoiceReady(q, {optionId: USER_ASK_OTHER_ID, otherText: '  混合云  '})).toBe(true)
  })

  it('formats a follow-up the model can continue from', () => {
    const pack = parseUserAskSummary(JSON.stringify(sample))!
    const text = formatUserAskFollowUp(pack, {
      deploy: {optionId: 'k8s'},
      db: {optionId: USER_ASK_OTHER_ID, otherText: '已有 TiDB'},
    })
    expect(text).toContain('【决策提交】需求边界')
    expect(text).toContain('1. 部署方式：容器化')
    expect(text).toContain('2. 数据库：其他 — 已有 TiDB')
  })

  it('hides wizard JSON from the activity line', () => {
    expect(userAskActivitySummary(JSON.stringify(sample))).toBe('需求边界')
    expect(userAskActivitySummary('approval required')).toBe('需要你决策')
  })
})
