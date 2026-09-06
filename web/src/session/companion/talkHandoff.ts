import { BridgeClientError, type MessageBridge } from '../../bridge/client'
import type { MessageDTO } from '../../generated/bridge'

/** Resolve an engine ACK against authoritative history before skipping append.
 * A provider-supplied or stale ID cannot borrow another session's message. */
export async function resolveTalkHandoffMessage(
  bridge: Pick<MessageBridge, 'list'>, sessionId: string, messageId: string, text: string,
): Promise<MessageDTO> {
  const normalized = text.replace(/\r\n?/g, '\n').trim()
  const items: MessageDTO[] = []
  let cursor: string | undefined
  for (let pageNumber = 0; pageNumber < 16; pageNumber++) {
    const page = await bridge.list({ sessionId, direction: 'backward', limit: 64, byteBudget: 131072, ...(cursor ? {cursor} : {}) })
    items.push(...page.items)
    const first = items.find(item => item.id === messageId)
    if (first) {
      let assembled = ''
      let sequence = first.sequence
      for (const item of items.filter(value => value.sequence >= first.sequence).sort((a, b) => a.sequence - b.sequence)) {
        if (item.sequence !== sequence++ || item.sessionId !== sessionId || item.role !== 'user' || item.status !== 'completed') break
        assembled += item.text
        if (assembled === normalized) return first
        if (!normalized.startsWith(assembled)) break
      }
      break
    }
    if (!page.hasMore || !page.nextCursor || page.nextCursor === cursor) break
    cursor = page.nextCursor
  }
  throw new BridgeClientError('通话指令与已保存会话不匹配，请重新发起指令', 'TALK_HANDOFF_SCOPE_INVALID', false, 'renderer')
}

export function validTalkHandoffText(text: string): boolean {
  return text.length > 0 && !text.includes('\0') && new TextEncoder().encode(text).length <= 512 * 1024
}

export function talkHandoffMessages(text: string): Array<{role: 'user'; content: string}> {
  const runes = Array.from(text)
  const messages: Array<{role: 'user'; content: string}> = []
  for (let at = 0; at < runes.length; at += 16384) messages.push({role: 'user', content: runes.slice(at, at + 16384).join('')})
  return messages
}
