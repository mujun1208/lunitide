export type ConversationScrollBox = {
  scrollHeight: number
  scrollTop: number
  clientHeight: number
  scrollTo?: (opts: {top: number; behavior?: ScrollBehavior}) => void
}

/** Skip a no-op pin so streaming tokens do not fight overflow-anchor / SetBounds. */
export const STREAM_PIN_EPSILON_PX = 2

export function conversationPinTop(box: Pick<ConversationScrollBox, 'scrollHeight' | 'clientHeight'>): number {
  return Math.max(0, box.scrollHeight - box.clientHeight)
}

/** Returns true when scrollTop actually moved. Thinking tokens must not call this. */
export function pinConversationScroll(box: ConversationScrollBox): boolean {
  const top = conversationPinTop(box)
  if (Math.abs(box.scrollTop - top) < STREAM_PIN_EPSILON_PX) return false
  if (typeof box.scrollTo === 'function') box.scrollTo({top, behavior: 'auto'})
  else box.scrollTop = top
  return true
}
