import { describe, expect, test, vi } from 'vitest'
import { canUseCompanionWebgl } from './webglSupport'

describe('canUseCompanionWebgl', () => {
  test('does not open a new WebGL2 context on every call', () => {
    const spy = vi.spyOn(HTMLCanvasElement.prototype, 'getContext')
    canUseCompanionWebgl()
    const afterFirst = spy.mock.calls.filter(call => String(call[0]).toLowerCase().includes('webgl')).length
    canUseCompanionWebgl()
    canUseCompanionWebgl()
    expect(spy.mock.calls.filter(call => String(call[0]).toLowerCase().includes('webgl')).length).toBe(afterFirst)
    spy.mockRestore()
  })
})