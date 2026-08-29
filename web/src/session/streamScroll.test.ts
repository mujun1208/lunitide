import {describe, expect, it, vi} from 'vitest'
import {conversationPinTop, pinConversationScroll, STREAM_PIN_EPSILON_PX} from './streamScroll'

describe('conversation stream scroll pin', () => {
  it('does not call scrollTo when already pinned within 2px', () => {
    const scrollTo = vi.fn()
    const box = {scrollHeight: 900, clientHeight: 300, scrollTop: 600, scrollTo}
    expect(conversationPinTop(box)).toBe(600)
    expect(pinConversationScroll(box)).toBe(false)
    expect(scrollTo).not.toHaveBeenCalled()
    box.scrollTop = 600 - STREAM_PIN_EPSILON_PX + 1
    expect(pinConversationScroll(box)).toBe(false)
    expect(scrollTo).not.toHaveBeenCalled()
  })

  it('pins once when the log grew, without smooth behavior', () => {
    const scrollTo = vi.fn()
    const box = {scrollHeight: 900, clientHeight: 300, scrollTop: 400, scrollTo}
    expect(pinConversationScroll(box)).toBe(true)
    expect(scrollTo).toHaveBeenCalledOnce()
    expect(scrollTo).toHaveBeenCalledWith({top: 600, behavior: 'auto'})
  })
})
