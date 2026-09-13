export type DevWorkflowChip = { label: string; prompt: string }

/** Matt Pocock-style shortcuts for project workbench dev phase. */
export const DEV_WORKFLOW_CHIPS: DevWorkflowChip[] = [
  { label: '/grill-me', prompt: '请 skill.invoke grill-me：帮我梳理需求边界、遗漏与风险。' },
  { label: '/to-spec', prompt: '请 skill.invoke to-spec：把当前需求整理成可执行规格。' },
  { label: '/to-tickets', prompt: '请 skill.invoke to-tickets：拆成可执行的开发任务清单。' },
  { label: '/implement', prompt: '请 skill.invoke implement：按规格实现代码并运行验证。' },
  { label: '/code-review', prompt: '请 skill.invoke code-reviewer：审查本轮改动并给出分级建议。' },
  { label: '/improve-architecture', prompt: '请 skill.invoke improve-architecture：审视架构并提出改进方案。' },
]

export function isDevWorkflowPhase(phase?: number, label?: string): boolean {
  return label === '开发' && (phase === 4 || phase === 5)
}

export function isTestWorkflowPhase(phase?: number, label?: string): boolean {
  return label === '测试' && (phase === 5 || phase === 6)
}

export function workspaceTabForPhase(phase?: number, label?: string): 'code' | 'plan' | undefined {
  if (isDevWorkflowPhase(phase, label)) return 'code'
  if (isTestWorkflowPhase(phase, label)) return 'plan'
  return undefined
}

export function chipsWithBrief(brief?: string): DevWorkflowChip[] {
  const prefix = brief?.trim()
  return DEV_WORKFLOW_CHIPS.map(chip => ({
    ...chip,
    prompt: prefix ? `${prefix}\n\n${chip.prompt}` : `${chip.prompt}\n\n（请先点清单里的一条）`,
  }))
}

export const PHASE1_TREE_CHIP: DevWorkflowChip = {
  label: '/审树',
  prompt: '确认本阶段会按结构规范在项目根生成目录，请先审树。可改默认树；不改则确认时按默认生成。',
}

export function isPhase1Workflow(phase?: number): boolean {
  return phase === 1
}

export function isPhase2Workflow(phase?: number, label?: string): boolean {
  return phase === 2 && (label === undefined || label === '方案和UI设计')
}

export function chipsForPhase(phase?: number, label?: string, brief?: string): DevWorkflowChip[] {
  if (isDevWorkflowPhase(phase, label)) return chipsWithBrief(brief)
  if (isPhase1Workflow(phase)) {
    return [DEV_WORKFLOW_CHIPS[0]!, DEV_WORKFLOW_CHIPS[1]!, PHASE1_TREE_CHIP]
  }
  if (isPhase2Workflow(phase, label)) {
    return [DEV_WORKFLOW_CHIPS[0]!, DEV_WORKFLOW_CHIPS[1]!, DEV_WORKFLOW_CHIPS[2]!]
  }
  return []
}
