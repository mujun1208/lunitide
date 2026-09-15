import type { UserAskPack } from '../session/userAsk'

export type InterviewQuestion = { id: string; prompt: string; options: string[] }

export const PHASE1_QUESTIONS: InterviewQuestion[] = [
  { id: 'core_problem', prompt: '本项目首先要解决什么？', options: ['核心业务闭环', '内部提效', '对外交付', '其他'] },
  { id: 'system_shape', prompt: '系统形态？', options: ['桌面工具', 'Web 应用', '桌面+本地服务', '其他'] },
  { id: 'stack', prompt: '主要技术栈？', options: ['沿用本仓库习惯', 'Go+React', '其他'] },
  { id: 'data_store', prompt: '数据存在哪？', options: ['项目内 SQLite', '稍后再定', '其他'] },
  { id: 'tree_choice', prompt: '目录树？', options: ['用默认树', '我要改树', '其他'] },
  { id: 'rule_strictness', prompt: '开发规范？', options: ['按生成的开发/技术规范严格执行', '先出草稿我再改', '其他'] },
]

export const PHASE2_QUESTIONS: InterviewQuestion[] = [
  { id: 'main_flows', prompt: '有几条必须先设计的主业务流？', options: ['1 条', '2–3 条', '4 条以上', '其他'] },
  { id: 'api_style', prompt: '接口怎么给？', options: ['OpenAPI REST', '仅内部模块调用', '两者都要', '其他'] },
  { id: 'module_cut', prompt: '功能怎么切？', options: ['按业务对象', '按页面', '按角色', '其他'] },
  { id: 'ui_depth', prompt: 'UI 详细设计要做到哪？', options: ['线框+状态', '完整页面说明', '本阶段先清单', '其他'] },
  { id: 'integration_cut', prompt: '集成测试怎么切场景？', options: ['按主业务流', '按角色任务', '本阶段只出清单', '其他'] },
]

export function questionsForPhase(phase: number): InterviewQuestion[] {
  if (phase === 1) return PHASE1_QUESTIONS
  if (phase === 2) return PHASE2_QUESTIONS
  return []
}

export function interviewPack(phase: number, questions = questionsForPhase(phase)): UserAskPack {
  return {
    title: phase === 1 ? '需求架构引导选择' : '方案和UI引导选择',
    reason: 'decision',
    questions: questions.map(q => ({
      id: q.id,
      prompt: q.prompt,
      options: q.options.map((label, i) => ({ id: q.id + '_' + i, label })),
    })),
  }
}

export const COUNCIL_PROMPT = '请就本阶段交付物互辩并给出可落盘的综合稿。讨论结束后由人点「生成本阶段交付物」。'
export const INCOMPLETE_INTERVIEW_BANNER = '本题库未答完，按已答 + 骨架生成，请人审。'

export function parseGuideAnswers(followUp: string, phase: number): Array<{ id: string; prompt: string; value: string }> {
  const lines = followUp.split('\n')
  return questionsForPhase(phase).map(q => {
    const line = lines.find(item => item.includes(q.prompt) && item.includes('：'))
    const value = line ? line.slice(line.indexOf('：') + 1).trim() : ''
    return { id: q.id, prompt: q.prompt, value }
  })
}

export function phaseAnswersComplete(phase: number, answers: Array<{ id: string; value?: string }>): boolean {
  const byId = new Map(answers.map(item => [item.id, (item.value ?? '').trim()]))
  const questions = questionsForPhase(phase)
  return questions.length > 0 && questions.every(q => Boolean(byId.get(q.id)))
}

export function answersFromInterview(interview: unknown, phase: number): Array<{ id: string; value: string }> {
  const doc = interview as { phases?: Record<string, { answers?: Array<{ id?: string; value?: string }> }> } | undefined
  return (doc?.phases?.[String(phase)]?.answers ?? []).map(item => ({
    id: item.id ?? '',
    value: item.value ?? '',
  }))
}
