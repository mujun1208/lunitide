import {describe, expect, it} from 'vitest'
import {
  FILE_PICKER_ASK,
  filePickerHandoffKey,
  formatUserAskFollowUp,
  looksLikeFilePickerHandoff,
  looksLikeUACHandoff,
  parseUserAskSummary,
  UAC_ASK,
  uacHandoffKey,
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

  it('detects file-picker handoff and parks as a decision pack', () => {
    expect(looksLikeFilePickerHandoff('{"needs_user":"x","handoff":"file_dialog"}')).toBe(true)
    expect(looksLikeFilePickerHandoff('needs_user: 请你点「保存」「打开」或「取消」，我不能代你选文件')).toBe(true)
    expect(looksLikeFilePickerHandoff('captured desktop 1920x1080')).toBe(false)
    expect(FILE_PICKER_ASK.questions[0].options.length).toBeGreaterThanOrEqual(2)
    expect(filePickerHandoffKey([{callId: 'a', summary: 'captured'}, {callId: 'b', summary: 'needs_user file_dialog'}])).toBe('b')
  })

  it('detects UAC handoff and parks as a decision pack', () => {
    expect(looksLikeUACHandoff('needs_user: 这是系统提权对话框，我不能代点「是」。请你自己确认或取消。')).toBe(true)
    expect(looksLikeUACHandoff('ccapp: operation blocked by risk policy: uac dialog')).toBe(true)
    expect(looksLikeUACHandoff('captured desktop 1920x1080')).toBe(false)
    expect(UAC_ASK.title).toBe('系统提权')
    expect(UAC_ASK.questions[0].options.length).toBeGreaterThanOrEqual(2)
    expect(uacHandoffKey([{callId: 'a', summary: 'clicked'}, {callId: 'b', summary: 'needs_user uac 提权'}])).toBe('b')
  })
})
