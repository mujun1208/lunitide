import { expect, it } from 'vitest'
import { OPEN_CHAT_SESSION_EVENT, openChatSession } from './openChatSession'

it('dispatches the session id for App to open', () => {
  const seen: string[] = []
  const onOpen = (event: Event) => {
    seen.push((event as CustomEvent<{ sessionId?: string }>).detail.sessionId ?? '')
  }
  window.addEventListener(OPEN_CHAT_SESSION_EVENT, onOpen)
  openChatSession('01ARZ3NDEKTSV4RRFFQ69G5FAV')
  window.removeEventListener(OPEN_CHAT_SESSION_EVENT, onOpen)
  expect(seen).toEqual(['01ARZ3NDEKTSV4RRFFQ69G5FAV'])
})
