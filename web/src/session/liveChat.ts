// liveChat.ts keeps chat streams alive across session switches: the
// stream handle, its accumulated reply state and the App activity
// callback live in a module-level registry keyed by session+turn, so
// leaving a session (or opening another one) never cancels a running
// generation. The reply keeps streaming in the background, the backend
// persists it on completion, and a returning MessagePanel rehydrates
// from the registry and continues rendering the live reply.
import type { ChatStream, StreamArtifact, StreamEvent } from '../bridge/client'

export interface LiveToolActivity { callId: string; name: string; argsDigest: string; status: string; summary?: string; artifact?: StreamArtifact }

export interface LiveChatState {
  chatStatus: 'streaming' | 'done' | 'failed' | 'cancelled'
  assistantText: string
  thinkingText: string
  toolActivities: LiveToolActivity[]
  usage?: { inputTokens: number; outputTokens: number; totalTokens: number }
  error?: { message: string; code: string; retryable: boolean }
}

export interface LiveChatEntry {
  sessionId: string
  turnId: string
  stream?: ChatStream
  state: LiveChatState
  terminal: boolean
  activity?: (active: boolean) => void
  listeners: Set<(event: StreamEvent) => void>
}

const entries = new Map<string, LiveChatEntry>()
const cancellingStreams = new WeakSet<ChatStream>()

export function liveTurnKey(sessionId: string, turnId: string): string {
  return `${sessionId}\0${turnId}`
}

export function listActiveTurns(sessionId: string): LiveChatEntry[] {
  const out: LiveChatEntry[] = []
  for (const entry of entries.values()) {
    if (entry.sessionId === sessionId && !entry.terminal) out.push(entry)
  }
  return out.sort((a, b) => a.turnId.localeCompare(b.turnId))
}

export function liveChatEntry(sessionId: string, turnId?: string): LiveChatEntry | undefined {
  if (turnId) return entries.get(liveTurnKey(sessionId, turnId))
  const active = listActiveTurns(sessionId)
  return active[active.length - 1]
}

export function liveChatTurnEntry(sessionId: string, turnId: string): LiveChatEntry | undefined {
  return entries.get(liveTurnKey(sessionId, turnId))
}

/** Open the per-session live slot for a new round. A stale non-terminal
 *  entry for the same turn is explicitly cancelled so it cannot leak. */
export function startLiveChat(sessionId: string, turnId = '_current', activity?: (active: boolean) => void): LiveChatEntry {
  const key = liveTurnKey(sessionId, turnId)
  const previous = entries.get(key)
  if (previous && !previous.terminal) void previous.stream?.cancel().catch(() => {})
  const entry: LiveChatEntry = {
    sessionId,
    turnId,
    state: { chatStatus: 'streaming', assistantText: '', thinkingText: '', toolActivities: [] },
    terminal: false,
    activity,
    listeners: new Set(),
  }
  entries.set(key, entry)
  return entry
}

export async function cancelLiveChatTurn(sessionId: string, turnId?: string): Promise<void> {
  const cancelEntry = async (entry: LiveChatEntry) => {
    const stream = entry.stream
    if (!stream || cancellingStreams.has(stream)) return
    cancellingStreams.add(stream)
    try {
      await stream.cancel()
    } catch { /* best effort */ }
    finally {
      cancellingStreams.delete(stream)
    }
  }
  if (turnId) {
    const entry = entries.get(liveTurnKey(sessionId, turnId))
    if (entry) await cancelEntry(entry)
    return
  }
  const seen = new WeakSet<ChatStream>()
  for (const entry of listActiveTurns(sessionId)) {
    const stream = entry.stream
    if (!stream || seen.has(stream)) continue
    seen.add(stream)
    await cancelEntry(entry)
  }
}

export function subscribeLiveChat(sessionId: string, listener: (event: StreamEvent) => void, turnId?: string): () => void {
  const entry = liveChatEntry(sessionId, turnId)
  if (!entry) return () => {}
  entry.listeners.add(listener)
  return () => { entry.listeners.delete(listener) }
}

/** Single reducer applied to every stream event whether or not a panel
 *  is mounted: the entry accumulates the reply; terminal events retire
 *  the entry (the backend has persisted the completed reply) and clear
 *  the App activity spinner through the captured callback. */
export function applyLiveChatEvent(entry: LiveChatEntry, event: StreamEvent): void {
  if (entry.terminal) return
  try {
    const state = entry.state
    switch (event.type) {
      case 'delta':
        state.assistantText += event.delta?.text ?? ''
        break
      case 'thinking':
        state.thinkingText += event.thinking?.text ?? ''
        break
      case 'usage':
        if (event.usage) state.usage = { inputTokens: event.usage.inputTokens, outputTokens: event.usage.outputTokens, totalTokens: event.usage.totalTokens }
        break
      case 'tool_started':
      case 'tool_completed':
      case 'approval_required': {
        const next = { ...event.tool, status: event.type }
        state.toolActivities = [...state.toolActivities.filter(x => x.callId !== next.callId), next]
        break
      }
      case 'tool_output': {
        const existing = state.toolActivities.find(x => x.callId === event.tool.callId)
        const next = { callId: event.tool.callId, name: event.tool.name, argsDigest: event.tool.argsDigest, status: existing?.status ?? 'tool_started', summary: event.tool.summary, artifact: existing?.artifact }
        state.toolActivities = [...state.toolActivities.filter(x => x.callId !== next.callId), next]
        break
      }
      case 'completed':
        state.chatStatus = 'done'
        entry.terminal = true
        break
      case 'cancelled':
        state.chatStatus = 'cancelled'
        entry.terminal = true
        break
      case 'failed':
        state.chatStatus = 'failed'
        state.error = { message: event.error?.message ?? 'stream failed', code: event.error?.code ?? 'STREAM_FAILED', retryable: event.error?.retryable ?? false }
        entry.terminal = true
        break
    }
    if (entry.terminal) {
      entries.delete(liveTurnKey(entry.sessionId, entry.turnId))
      if (!listActiveTurns(entry.sessionId).length) {
        try { entry.activity?.(false) } catch { /* spinner must not kill the host */ }
      }
    }
    for (const listener of [...entry.listeners]) {
      try { listener(event) } catch (err) { console.error('[lunitide] live chat listener', err) }
    }
  } catch (err) {
    console.error('[lunitide] live chat event', err)
  }
}

/** Retire an entry without a terminal stream event (chat.start itself
 * failed): stop the activity spinner so no phantom spinner remains. */
export function failLiveChat(entry: LiveChatEntry): void {
  if (entry.terminal) return
  entry.terminal = true
  entries.delete(liveTurnKey(entry.sessionId, entry.turnId))
  if (!listActiveTurns(entry.sessionId).length) entry.activity?.(false)
}

/** Test-only: drop every registry entry. Production never calls this —
 * entries retire through terminal stream events — but vitest suites
 * reuse one session id across cases, and a case whose mock stream never
 * emits a terminal event must not poison the next mount. */
export function resetLiveChatForTests(): void {
  for (const entry of entries.values()) {
    entry.terminal = true
    if (!listActiveTurns(entry.sessionId).length) entry.activity?.(false)
  }
  entries.clear()
}
