// liveChat.ts keeps chat streams alive across session switches: the
// stream handle, its accumulated reply state and the App activity
// callback live in a module-level registry keyed by session id, so
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
  stream?: ChatStream
  state: LiveChatState
  terminal: boolean
  activity?: (active: boolean) => void
  listeners: Set<(event: StreamEvent) => void>
}

const entries = new Map<string, LiveChatEntry>()

export function liveChatEntry(sessionId: string): LiveChatEntry | undefined {
  return entries.get(sessionId)
}

/** Open the per-session live slot for a new round. A stale non-terminal
 *  entry is explicitly cancelled so it cannot leak a backend generation. */
export function startLiveChat(sessionId: string, activity?: (active: boolean) => void): LiveChatEntry {
  const previous = entries.get(sessionId)
  if (previous && !previous.terminal) void previous.stream?.cancel().catch(() => {})
  const entry: LiveChatEntry = {
    sessionId,
    state: { chatStatus: 'streaming', assistantText: '', thinkingText: '', toolActivities: [] },
    terminal: false,
    activity,
    listeners: new Set(),
  }
  entries.set(sessionId, entry)
  return entry
}

export function subscribeLiveChat(sessionId: string, listener: (event: StreamEvent) => void): () => void {
  const entry = entries.get(sessionId)
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
      entries.delete(entry.sessionId)
      try { entry.activity?.(false) } catch { /* spinner must not kill the host */ }
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
  entries.delete(entry.sessionId)
  entry.activity?.(false)
}

/** Test-only: drop every registry entry. Production never calls this —
 * entries retire through terminal stream events — but vitest suites
 * reuse one session id across cases, and a case whose mock stream never
 * emits a terminal event must not poison the next mount. */
export function resetLiveChatForTests(): void {
  for (const entry of entries.values()) {
    entry.terminal = true
    entry.activity?.(false)
  }
  entries.clear()
}
