// liveChat.ts keeps chat streams alive across session switches: the
// stream handle, its accumulated reply state and the App activity
// callback live in a module-level registry keyed by session+turn, so
// leaving a session (or opening another one) never cancels a running
// generation. The reply keeps streaming in the background, the backend
// persists it on completion, and a returning MessagePanel rehydrates
// from the registry and continues rendering the live reply.
import type { ChatStream, StreamArtifact, StreamEvent } from '../bridge/client'
import { coerceTaskOutcome, type TaskOutcome } from './taskOutcome'

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
  startedAtMs: number
  usage?: Extract<StreamEvent, {type: 'usage'}>['usage']
  error?: { message: string; code: string; retryable: boolean }
  guidance?: { labels: string[]; digest: string }
  equip?: { experts: string[]; skills?: string[]; missingMcp?: string[] }
  taskOutcome?: TaskOutcome
}

export interface LiveChatEntry {
  sessionId: string
  turnId: string
  stream?: ChatStream
  state: LiveChatState
  terminal: boolean
  lastEventAt: number
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
  const now = Date.now()
  const entry: LiveChatEntry = {
    sessionId,
    turnId,
    state: { chatStatus: 'streaming', assistantText: '', thinkingText: '', toolActivities: [], startedAtMs: now },
    terminal: false,
    lastEventAt: now,
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
  entry.lastEventAt = Date.now()
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
        if (event.usage) state.usage = {...event.usage}
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
        const nextOutcome = coerceTaskOutcome(event.completed?.taskOutcome)
        if (nextOutcome) state.taskOutcome = nextOutcome
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

/** Retire an entry without emitting any terminal stream event. For turns
 *  whose server side will never emit another event (a user.ask approval
 *  consumes the parked stream inside chat.tool.approve), a lost terminal
 *  event would leave a zombie entry that keeps swallowing follow-up sends
 *  into the durable input queue — retire silently instead. */
export function retireLiveChatTurn(entry: LiveChatEntry): void {
  if (entry.terminal) return
  entry.terminal = true
  entries.delete(liveTurnKey(entry.sessionId, entry.turnId))
  notifyLiveChatRegistry()
  if (!listActiveTurns(entry.sessionId).length) {
    try { entry.activity?.(false) } catch { /* spinner must not kill the host */ }
  }
}

/** Mark an approval_required activity as resolved in the registry. The
 *  watchdog exempts turns with a pending decision; once chat.approve has
 *  consumed that decision the exemption must lapse or a turn whose resumed
 *  events were all dropped would never be retired. */
export function resolveLiveApproval(sessionId: string, callId: string, status: string): void {
  for (const entry of listActiveTurns(sessionId)) {
    if (!entry.state.toolActivities.some(t => t.callId === callId && t.status === 'approval_required')) continue
    entry.state.toolActivities = entry.state.toolActivities.map(t => t.callId === callId ? { ...t, status } : t)
  }
}

const LIVE_WATCHDOG_INTERVAL_MS = 30_000
const LIVE_WATCHDOG_SILENCE_MS = 300_000
let watchdogTimer: ReturnType<typeof setInterval> | undefined

/** A turn waiting for a user decision or with a running tool can stay
 *  quiet for a long time by design; only truly silent turns are zombies. */
function staleLiveTurnSilent(entry: LiveChatEntry, now: number, silenceMs: number): boolean {
  if (now - entry.lastEventAt < silenceMs) return false
  return !entry.state.toolActivities.some(t => t.status === 'approval_required' || t.status === 'tool_started')
}

/** Background sweep for zombie turns: a terminal event dropped while the
 *  WebView was hidden (server-side emit failures are logged and dropped)
 *  leaves an entry that never settles, which blocks the durable input
 *  queue forever. The watchdog retires such entries with a synthetic
 *  cancelled event so history settles, the queue flushes and the resume
 *  banner appears. Idempotent: panels may call it on every mount. */
export function startLiveChatWatchdog(options: { intervalMs?: number; silenceMs?: number } = {}): void {
  if (watchdogTimer !== undefined) return
  const intervalMs = options.intervalMs ?? LIVE_WATCHDOG_INTERVAL_MS
  const silenceMs = options.silenceMs ?? LIVE_WATCHDOG_SILENCE_MS
  watchdogTimer = setInterval(() => {
    if (watchdogTimer === undefined) return
    const now = Date.now()
    for (const entry of [...entries.values()]) {
      if (!entry.terminal && staleLiveTurnSilent(entry, now, silenceMs)) retireAsCancelled(entry)
    }
  }, intervalMs)
}

/** Test-only: drop every registry entry. Production never calls this —
 * entries retire through terminal stream events — but vitest suites
 * reuse one session id across cases, and a case whose mock stream never
 * emits a terminal event must not poison the next mount. */
export function resetLiveChatForTests(): void {
  if (watchdogTimer !== undefined) {
    clearInterval(watchdogTimer)
    watchdogTimer = undefined
  }
  for (const entry of entries.values()) {
    entry.terminal = true
    if (!listActiveTurns(entry.sessionId).length) entry.activity?.(false)
  }
  entries.clear()
  notifyLiveChatRegistry()
}
