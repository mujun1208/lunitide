import {describe, expect, it, vi} from 'vitest'
import {
  applyConversationUserScroll,
  conversationNearBottom,
  conversationPinTop,
  pauseFollowOnUserIntent,
  pinConversationScroll,
  STREAM_PIN_EPSILON_PX,
} from './streamScroll'

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

  it('pauses follow when the user scrolls up away from the bottom', () => {
    const stillNear = {scrollHeight: 900, clientHeight: 300, scrollTop: 552}
    expect(conversationNearBottom(stillNear)).toBe(true)
    expect(applyConversationUserScroll({userFollowPaused: false, lastScrollTop: 600}, stillNear).userFollowPaused).toBe(true)
    const box = {scrollHeight: 900, clientHeight: 300, scrollTop: 200}
    expect(conversationNearBottom(box)).toBe(false)
    expect(applyConversationUserScroll({userFollowPaused: false, lastScrollTop: 600}, box).userFollowPaused).toBe(true)
    expect(pauseFollowOnUserIntent({userFollowPaused: false, nearBottom: true, deltaY: -40})).toBe(true)
  })

  it('stays paused after manual scroll until an explicit resume', () => {
    const mid = applyConversationUserScroll({userFollowPaused: true, lastScrollTop: 200}, {scrollHeight: 900, clientHeight: 300, scrollTop: 360})
    expect(mid.userFollowPaused).toBe(true)
    const bottom = applyConversationUserScroll(mid, {scrollHeight: 900, clientHeight: 300, scrollTop: 600})
    expect(bottom.userFollowPaused).toBe(true)
    expect(conversationNearBottom({scrollHeight: 900, clientHeight: 300, scrollTop: 600})).toBe(true)
  })

  it('pauses on any user scroll movement including scroll-down', () => {
    expect(applyConversationUserScroll({userFollowPaused: false, lastScrollTop: 200}, {scrollHeight: 900, clientHeight: 300, scrollTop: 360}).userFollowPaused).toBe(true)
  })

  it('does not call scrollTo when userFollowPaused, including mermaid layout complete', () => {
    const scrollTo = vi.fn()
    const box = {scrollHeight: 900, clientHeight: 300, scrollTop: 100, scrollTo}
    expect(pinConversationScroll(box, {userFollowPaused: true})).toBe(false)
    expect(scrollTo).not.toHaveBeenCalled()
  })

  it('never pins html or body (page-level jump)', () => {
    const scrollTo = vi.fn()
    const html = document.documentElement
    const body = document.body
    const htmlTo = Object.getOwnPropertyDescriptor(html, 'scrollTo')
    const bodyTo = Object.getOwnPropertyDescriptor(body, 'scrollTo')
    Object.defineProperty(html, 'scrollTo', {configurable: true, value: scrollTo})
    Object.defineProperty(body, 'scrollTo', {configurable: true, value: scrollTo})
    try {
      expect(pinConversationScroll(html as unknown as {scrollHeight: number; scrollTop: number; clientHeight: number; scrollTo: typeof scrollTo})).toBe(false)
      expect(pinConversationScroll(body as unknown as {scrollHeight: number; scrollTop: number; clientHeight: number; scrollTo: typeof scrollTo})).toBe(false)
      expect(scrollTo).not.toHaveBeenCalled()
    } finally {
      if (htmlTo) Object.defineProperty(html, 'scrollTo', htmlTo)
      else delete (html as {scrollTo?: unknown}).scrollTo
      if (bodyTo) Object.defineProperty(body, 'scrollTo', bodyTo)
      else delete (body as {scrollTo?: unknown}).scrollTo
    }
  })
})
