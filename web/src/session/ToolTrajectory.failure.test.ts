import { describe, expect, it } from 'vitest'
import { toolTrajectoryStatus } from './ToolTrajectory'
import { companionToolPhaseCaption } from './companion/companionText'

describe('desktop result status', () => {
  it('does not show a rejected input as complete', () => {
    const summary = 'ok:false M10-CC-008: COMPUTER_STALE_FRAME'
    expect(toolTrajectoryStatus('tool_completed', summary)).toBe('失败')
    expect(companionToolPhaseCaption('succeeded', summary)).toMatch(/^执行失败/)
  })
  it('preserves successful and running results', () => {
    expect(toolTrajectoryStatus('tool_completed', 'verified playing')).toBe('已返回')
    expect(companionToolPhaseCaption('running', 'ok:false')).toBe('执行中…')
    expect(companionToolPhaseCaption('succeeded', 'verified playing')).toMatch(/^执行完成/)
  })
  it('does not infer the whole task outcome from the last tool event', () => {
    for (const summary of ['sent next track', 'captured foreground window', 'ok:false old attempt']) {
      expect(companionToolPhaseCaption('returned', summary)).toBe('操作已返回')
    }
  })
})
