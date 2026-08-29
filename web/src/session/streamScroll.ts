export type ConversationScrollBox = {
  scrollHeight: number
  scrollTop: number
  clientHeight: number
  scrollTo?: (opts: {top: number; behavior?: ScrollBehavior}) => void
}

/** Skip a no-op pin so streaming tokens do not fight overflow-anchor / SetBounds. */
export const STREAM_PIN_EPSILON_PX = 2
export const STREAM_NEAR_BOTTOM_PX = 80
export const USER_SCROLL_AWAY_PX = 4

export function conversationPinTop(box: Pick<ConversationScrollBox, 'scrollHeight' | 'clientHeight'>): number {
  return Math.max(0, box.scrollHeight - box.clientHeight)
}

export function conversationNearBottom(
  box: Pick<ConversationScrollBox, 'scrollHeight' | 'scrollTop' | 'clientHeight'>,
  slack = STREAM_NEAR_BOTTOM_PX,
): boolean {
  return box.scrollHeight - box.scrollTop - box.clientHeight < slack
}

export type UserFollowState = {
  userFollowPaused: boolean
  lastScrollTop: number
}

/** Wheel / drag / touch: pause as soon as the user leaves the live tail. */
export function pauseFollowOnUserIntent(opts: {
  userFollowPaused: boolean
  nearBottom: boolean
  /** Negative = toward older content (away from the live tail). */
  deltaY?: number
}): boolean {
  if (opts.deltaY !== undefined && opts.deltaY < 0) return true
  if (!opts.nearBottom) return true
  return false
}

/** After a scroll position change: pause on scroll-up, resume only at the live tail. */
export function applyConversationUserScroll(state: UserFollowState, box: ConversationScrollBox): UserFollowState {
  const nearBottom = conversationNearBottom(box)
  const movedUp = box.scrollTop < state.lastScrollTop - USER_SCROLL_AWAY_PX
  let paused = state.userFollowPaused
  if (movedUp) paused = true
  else if (nearBottom) paused = false
  else if (box.scrollTop !== state.lastScrollTop) paused = true
  return {userFollowPaused: paused, lastScrollTop: box.scrollTop}
}

function isDocumentShell(box: ConversationScrollBox): boolean {
  if (typeof document === 'undefined') return false
  return box === document.documentElement || box === document.body
}

/** Returns true when scrollTop actually moved. Thinking tokens must not call this.
 *  Never pin html/body — that is the whole-window jump on 项目管理. */
export function pinConversationScroll(box: ConversationScrollBox, opts?: {userFollowPaused?: boolean}): boolean {
  if (opts?.userFollowPaused) return false
  if (isDocumentShell(box)) return false
  const top = conversationPinTop(box)
  if (Math.abs(box.scrollTop - top) < STREAM_PIN_EPSILON_PX) return false
  if (typeof box.scrollTo === 'function') box.scrollTo({top, behavior: 'auto'})
  else box.scrollTop = top
  return true
}
