export const OPEN_CHAT_SESSION_EVENT = 'lunitide:open-chat-session'

export function openChatSession(sessionId: string): void {
  if (!sessionId) return
  window.dispatchEvent(new CustomEvent(OPEN_CHAT_SESSION_EVENT, { detail: { sessionId } }))
}
