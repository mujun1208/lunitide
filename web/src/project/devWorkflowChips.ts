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
