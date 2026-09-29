export const USER_ASK_TOOL = 'user.ask'
export const USER_ASK_OTHER_ID = '__other__'

export type UserAskOption = {id: string; label: string}
export type UserAskQuestion = {id: string; prompt: string; options: UserAskOption[]}
export type UserAskReason = 'login' | '2fa' | 'captcha' | 'pay' | 'uac' | 'file_picker' | 'decision'
export type UserAskPack = {title: string; questions: UserAskQuestion[]; reason?: UserAskReason}

const ASK_REASON_TITLES: Record<UserAskReason, string> = {
  login: '需要登录',
  '2fa': '需要验证',
  captcha: '需要验证码',
  pay: '需要支付确认',
  uac: '系统提权',
  file_picker: '文件对话框',
  decision: '需要你决策',
}

export function titleForAskReason(reason?: string, fallback = ''): string {
  if (reason && reason in ASK_REASON_TITLES) return ASK_REASON_TITLES[reason as UserAskReason]
  return fallback
}
export type UserAskChoice = {optionId: string; otherText?: string}
export type UserAskAnswers = Record<string, UserAskChoice>

function asRecord(value: unknown): Record<string, unknown> | undefined {
  return value !== null && typeof value === 'object' && !Array.isArray(value) ? (value as Record<string, unknown>) : undefined
}

function parseQuestion(raw: unknown, index: number): UserAskQuestion | undefined {
  const row = asRecord(raw)
  if (!row) return undefined
  const prompt = typeof row.prompt === 'string' ? row.prompt.trim() : ''
  if (!prompt) return undefined
  const id = typeof row.id === 'string' && row.id.trim() ? row.id.trim() : `q${index + 1}`
  const optionsIn = Array.isArray(row.options) ? row.options : []
  const options: UserAskOption[] = []
  for (const [i, item] of optionsIn.entries()) {
    if (options.length >= 5) break
    const opt = asRecord(item)
    if (!opt) continue
    const label = typeof opt.label === 'string' ? opt.label.trim() : ''
    if (!label) continue
    const optId = typeof opt.id === 'string' && opt.id.trim() ? opt.id.trim() : `opt${i + 1}`
    if (optId === USER_ASK_OTHER_ID) continue
    options.push({id: optId, label})
  }
  if (options.length < 2) return undefined
  return {id, prompt, options}
}

/** Recover questions from a tool summary (JSON args or a leading `{...}` blob). */
export function parseUserAskSummary(summary: string | undefined): UserAskPack | undefined {
  const text = summary?.trim()
  if (!text) return undefined
  const start = text.indexOf('{')
  const end = text.lastIndexOf('}')
  if (start < 0 || end <= start) return undefined
  let parsed: unknown
  try {
    parsed = JSON.parse(text.slice(start, end + 1))
  } catch {
    return undefined
  }
  const root = asRecord(parsed)
  if (!root || !Array.isArray(root.questions)) return undefined
  const questions = root.questions.flatMap((item, index) => {
    const q = parseQuestion(item, index)
    return q ? [q] : []
  })
  if (!questions.length) return undefined
  const title = typeof root.title === 'string' ? root.title.trim() : ''
  const reasonRaw = typeof root.reason === 'string' ? root.reason.trim() : ''
  const reason = (reasonRaw in ASK_REASON_TITLES ? reasonRaw : undefined) as UserAskReason | undefined
  return {title: title || titleForAskReason(reason), questions: questions.slice(0, 8), reason}
}

export function userAskChoiceReady(question: UserAskQuestion, choice: UserAskChoice | undefined): boolean {
  if (!choice) return false
  if (choice.optionId === USER_ASK_OTHER_ID) return Boolean(choice.otherText?.trim())
  return question.options.some(option => option.id === choice.optionId)
}

export function formatUserAskFollowUp(pack: UserAskPack, answers: UserAskAnswers): string {
  const lines = pack.questions.map((question, index) => {
    const choice = answers[question.id]
    const n = index + 1
    if (!choice) return `${n}. ${question.prompt}：未选`
    if (choice.optionId === USER_ASK_OTHER_ID) {
      return `${n}. ${question.prompt}：其他 — ${choice.otherText?.trim() || '（空）'}`
    }
    const label = question.options.find(option => option.id === choice.optionId)?.label ?? choice.optionId
    return `${n}. ${question.prompt}：${label}`
  })
  const head = pack.title ? `【决策提交】${pack.title}` : '【决策提交】'
  return [head, ...lines].join('\n')
}

/** Activity-row label: never dump the wizard JSON into the thinking panel. */
export function userAskActivitySummary(summary?: string): string {
  const pack = parseUserAskSummary(summary)
  if (pack?.title) return pack.title
  return titleForAskReason(pack?.reason, '需要你决策')
}

export const FILE_PICKER_ASK: UserAskPack = {
  title: '文件对话框',
  questions: [
    {
      id: 'file-dialog',
      prompt: '屏幕上出现了打开/保存文件对话框。我不能代你选文件，请你在系统对话框里点「保存」「打开」或「取消」。',
      options: [
        {id: 'done', label: '我已经点完了'},
        {id: 'cancel', label: '我点了取消'},
        {id: 'wait', label: '稍等一下'},
      ],
    },
  ],
}

export function looksLikeFilePickerHandoff(summary?: string): boolean {
  const text = summary?.trim() ?? ''
  if (!text) return false
  if (text.includes('needs_user') && (text.includes('file_dialog') || text.includes('选文件') || text.includes('不能代你'))) {
    return true
  }
  return text.includes('请你点') && (text.includes('保存') || text.includes('打开'))
}

export function filePickerHandoffKey(activities: Array<{callId: string; summary?: string}>): string {
  return activities.filter(item => looksLikeFilePickerHandoff(item.summary)).map(item => item.callId).join('|')
}

export const UAC_ASK: UserAskPack = {
  title: '系统提权',
  questions: [
    {
      id: 'uac-dialog',
      prompt: '屏幕上出现了 UAC / 系统提权对话框。我不能代点「是」。请你自己确认或取消。',
      options: [
        {id: 'done', label: '我已经处理完了'},
        {id: 'cancel', label: '我点了取消'},
        {id: 'wait', label: '稍等一下'},
      ],
    },
  ],
}

export function looksLikeUACHandoff(summary?: string): boolean {
  const text = (summary ?? '').trim().toLowerCase()
  if (!text) return false
  if (text.includes('needs_user') && (text.includes('uac') || text.includes('提权'))) {
    return true
  }
  return text.includes('uac dialog') || text.includes('elevation dialog')
}

export function uacHandoffKey(activities: Array<{callId: string; summary?: string}>): string {
  return activities.filter(item => looksLikeUACHandoff(item.summary)).map(item => item.callId).join('|')
}
