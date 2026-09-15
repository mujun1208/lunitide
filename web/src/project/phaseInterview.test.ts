import { describe, expect, it } from 'vitest'
import {
  INCOMPLETE_INTERVIEW_BANNER,
  answersFromInterview,
  parseGuideAnswers,
  phaseAnswersComplete,
} from './phaseInterview'

describe('parseGuideAnswers', () => {
  it('reads only the matching prompt line and leaves unanswered questions empty', () => {
    const followUp = [
      '【决策提交】需求架构引导选择',
      '1. 本项目首先要解决什么？：商场进销存',
      '2. 系统形态？：桌面工具',
    ].join('\n')
    const answers = parseGuideAnswers(followUp, 1)
    expect(answers).toHaveLength(6)
    expect(answers[0]).toEqual({ id: 'core_problem', prompt: '本项目首先要解决什么？', value: '商场进销存' })
    expect(answers[1].value).toBe('桌面工具')
    expect(answers.slice(2).every(item => item.value === '')).toBe(true)
    expect(answers.some(item => item.value.includes('【决策提交】'))).toBe(false)
  })
})

describe('phaseAnswersComplete', () => {
  it('is false until every locked question has a value', () => {
    expect(phaseAnswersComplete(1, [{ id: 'core_problem', value: '进销存' }])).toBe(false)
    expect(phaseAnswersComplete(1, [
      { id: 'core_problem', value: '进销存' },
      { id: 'system_shape', value: '桌面工具' },
      { id: 'stack', value: 'Go+React' },
      { id: 'data_store', value: '项目内 SQLite' },
      { id: 'tree_choice', value: '用默认树' },
      { id: 'rule_strictness', value: '先出草稿我再改' },
    ])).toBe(true)
  })
})

describe('answersFromInterview', () => {
  it('reads the phase bucket and ignores missing interviews', () => {
    expect(answersFromInterview(undefined, 1)).toEqual([])
    expect(answersFromInterview({
      phases: { '1': { answers: [{ id: 'core_problem', value: '进销存' }] } },
    }, 1)).toEqual([{ id: 'core_problem', value: '进销存' }])
    expect(INCOMPLETE_INTERVIEW_BANNER).toContain('本题库未答完')
  })
})
