import { expect, it } from 'vitest'
import { companionMayAutoApprove, isDangerousCompanionTool, isFullDiskCompanionWrite } from './approvalProfile'

it('keeps low-risk companion tools auto and dangerous ones confirmed', () => {
  expect(isDangerousCompanionTool('command.run')).toBe(true)
  expect(isDangerousCompanionTool('run_terminal_cmd')).toBe(true)
  expect(companionMayAutoApprove('run_terminal_cmd')).toBe(false)
  expect(isDangerousCompanionTool('cc.mouse_click')).toBe(true)
  expect(isDangerousCompanionTool('workspace.read')).toBe(false)
  expect(companionMayAutoApprove('workspace.read')).toBe(true)
  expect(companionMayAutoApprove('command.run')).toBe(false)
  expect(companionMayAutoApprove('user.ask')).toBe(false)
  expect(isFullDiskCompanionWrite('workspace.write')).toBe(true)
  expect(companionMayAutoApprove('workspace.write', false)).toBe(true)
  expect(companionMayAutoApprove('workspace.write', true)).toBe(false)
  expect(companionMayAutoApprove('workspace.edit')).toBe(false)
})
