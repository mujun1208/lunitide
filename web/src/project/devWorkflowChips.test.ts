import { expect, it } from 'vitest'
import { chipsForPhase, chipsWithBrief, DEV_WORKFLOW_CHIPS, PHASE1_TREE_CHIP } from './devWorkflowChips'

it('prefixes the current task brief and otherwise asks the user to pick a row', () => {
  expect(chipsWithBrief('任务 F001：登录').some(chip => chip.prompt.startsWith('任务 F001：登录'))).toBe(true)
  expect(chipsWithBrief().every(chip => chip.prompt.includes('请先点清单里的一条'))).toBe(true)
  expect(chipsWithBrief()[0]?.label).toBe(DEV_WORKFLOW_CHIPS[0]?.label)
})

it('injects the phase-1 tree review chip without auto-sending', () => {
  const chips = chipsForPhase(1, '需求架构规范')
  expect(chips.some(chip => chip.label === PHASE1_TREE_CHIP.label && chip.prompt.includes('请先审树'))).toBe(true)
  expect(chips.some(chip => chip.label === '/grill-me')).toBe(true)
  expect(chipsForPhase(3, '方案和UI设计')).toEqual([])
})

it('offers phase-2 design chips without auto-sending', () => {
  expect(chipsForPhase(2, '方案和UI设计').map(chip => chip.label)).toEqual(['/grill-me', '/to-spec', '/to-tickets'])
})
