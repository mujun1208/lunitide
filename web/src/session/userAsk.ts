export const USER_ASK_TOOL = 'user.ask'
export const USER_ASK_OTHER_ID = '__other__'

export type UserAskOption = {id: string; label: string}
export type UserAskQuestion = {id: string; prompt: string; options: UserAskOption[]}
export type UserAskPack = {title: string; questions: UserAskQuestion[]}
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
  return {title, questions: questions.slice(0, 8)}
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
  return '需要你决策'
}
