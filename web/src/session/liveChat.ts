// liveChat.ts keeps chat streams alive across session switches: the
// stream handle, its accumulated reply state and the App activity
// callback live in a module-level registry keyed by session+turn, so
// leaving a session (or opening another one) never cancels a running
// generation. The reply keeps streaming in the background, the backend
// persists it on completion, and a returning MessagePanel rehydrates
// from the registry and continues rendering the live reply.
import type { ChatStream, StreamArtifact, StreamEvent } from '../bridge/client'

const LOCAL_CANCEL_ID = '01ARZ3NDEKTSV4RRFFQ69G5FAZ'
const CANCEL_WAIT_MS = 800

function localCancelledEvent(streamId: string): StreamEvent {
  return {
    v: '1.0',
    kind: 'event',
    id: LOCAL_CANCEL_ID,
    streamId: streamId || LOCAL_CANCEL_ID,
    sequence: 1,
    type: 'cancelled',
  }
}

function retireAsCancelled(entry: LiveChatEntry): void {
  if (entry.terminal) return
  applyLiveChatEvent(entry, localCancelledEvent(entry.stream?.streamId ?? entry.turnId))
}

export interface LiveToolActivity { callId: string; name: string; argsDigest: string; status: string; summary?: string; artifact?: StreamArtifact }

export interface LiveChatState {
  chatStatus: 'streaming' | 'done' | 'failed' | 'cancelled'
  assistantText: string
  thinkingText: string
  toolActivities: LiveToolActivity[]
  usage?: { inputTokens: number; outputTokens: number; totalTokens: number }
  error?: { message: string; code: string; retryable: boolean }
  guidance?: { labels: string[]; digest: string }
  equip?: { experts: string[]; skills?: string[]; missingMcp?: string[] }
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
const registryListeners = new Set<() => void>()

function notifyLiveChatRegistry(): void {
  for (const listener of [...registryListeners]) {
    try { listener() } catch (err) { console.error('[lunitide] live chat registry', err) }
  }
}

/** Session ids that currently have a non-terminal live turn (sidebar spinners). */
export function listActiveSessionIds(): string[] {
  const ids = new Set<string>()
  for (const entry of entries.values()) {
    if (!entry.terminal) ids.add(entry.sessionId)
  }
  return [...ids]
}

export function subscribeLiveChatRegistry(listener: () => void): () => void {
  registryListeners.add(listener)
  return () => { registryListeners.delete(listener) }
}

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
  notifyLiveChatRegistry()
  return entry
}

export async function cancelLiveChatTurn(sessionId: string, turnId?: string, spokenText?: string): Promise<void> {
  const spoken = typeof spokenText === 'string' ? spokenText.slice(0, 8000) : undefined
  const cancelEntry = async (entry: LiveChatEntry) => {
    const stream = entry.stream
    if (stream && !cancellingStreams.has(stream)) {
      cancellingStreams.add(stream)
      try {
        await Promise.race([
          stream.cancel(spoken ? { spokenText: spoken } : undefined).then(() => undefined),
          new Promise<void>(resolve => setTimeout(resolve, CANCEL_WAIT_MS)),
        ])
      } catch { /* best effort */ }
      finally {
        cancellingStreams.delete(stream)
      }
    }
    retireAsCancelled(entry)
  }
  if (turnId) {
    const entry = entries.get(liveTurnKey(sessionId, turnId))
    if (entry) await cancelEntry(entry)
    return
  }
  const seen = new WeakSet<ChatStream>()
  for (const entry of listActiveTurns(sessionId)) {
    const stream = entry.stream
    if (stream && seen.has(stream)) {
      retireAsCancelled(entry)
      continue
    }
    if (stream) seen.add(stream)
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
      case 'guidance':
        if (event.guidance) state.guidance = { labels: event.guidance.labels, digest: event.guidance.digest }
        break
      case 'equip':
        if (event.equip) state.equip = { experts: event.equip.experts, skills: event.equip.skills, missingMcp: event.equip.missingMcp }
        break
    }
    if (entry.terminal) {
      entries.delete(liveTurnKey(entry.sessionId, entry.turnId))
      notifyLiveChatRegistry()
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
  notifyLiveChatRegistry()
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
  notifyLiveChatRegistry()
}
