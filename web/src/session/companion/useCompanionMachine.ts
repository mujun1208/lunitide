// useCompanionMachine.ts is the M9.5 Moon Companion state machine
// (T-9.5.2.1): idle / listening / thinking / speaking with a single
// dispatch entry. Guards reject every transition outside the frozen
// matrix (M95-005 COMPANION_STATE_INVALID) while keeping the current
// state and leaving an audit trail in the console.
import { useCallback, useMemo, useReducer, useRef } from 'react'

export type CompanionState = 'idle' | 'listening' | 'thinking' | 'speaking'

export type CompanionEvent =
  | { type: 'MIC_ACTIVATE' } // user activates the mic
  | { type: 'MIC_CANCEL' } // recognition cancelled before final
  | { type: 'RECOGNIZED_FINAL' } // final transcript sent to ChatBridge
  | { type: 'REPLY_COMPLETED'; speakable: boolean }
  | { type: 'REPLY_TERMINAL' } // failed / cancelled / TTS off
  | { type: 'PLAYBACK_ENDED' } // all segments finished
  | { type: 'INTERRUPT' } // Esc or moon click during speaking
  | { type: 'MIC_CLICK_WHILE_SPEAKING' } // composite: cancel TTS then listen
  | { type: 'AWAIT_MORE' } // TTS drained mid-stream; keep listening while the model continues

interface CompanionSnapshot {
  state: CompanionState
  rejected: number // guard rejection counter (M95-005 audit)
}

type CompanionAction = CompanionEvent | { type: 'GUARD_REJECT'; from: CompanionState; event: string }

const transitionTable: Record<CompanionState, Partial<Record<CompanionEvent['type'], CompanionState>>> = {
  idle: { MIC_ACTIVATE: 'listening' },
  listening: { MIC_CANCEL: 'idle', RECOGNIZED_FINAL: 'thinking', MIC_ACTIVATE: 'listening' },
  thinking: { REPLY_COMPLETED: 'speaking', REPLY_TERMINAL: 'idle', INTERRUPT: 'idle', MIC_CLICK_WHILE_SPEAKING: 'listening' },
  // Her turn ends by the 打断 button (INTERRUPT / MIC_CLICK_WHILE_SPEAKING) or
  // by her finishing (PLAYBACK_ENDED). There is deliberately no event here for
  // the microphone: what it hears mid-reply is her own voice more often than
  // the user's, and nothing in the text tells them apart.
  speaking: {
    PLAYBACK_ENDED: 'idle',
    INTERRUPT: 'idle',
    // Composite transition: internally speaking -> idle -> listening.
    MIC_CLICK_WHILE_SPEAKING: 'listening',
    AWAIT_MORE: 'thinking',
  },
}

function reducer(snapshot: CompanionSnapshot, action: CompanionAction): CompanionSnapshot {
  if (action.type === 'GUARD_REJECT') {
    // M95-005: illegal transition refused, current state kept, audited.
    console.warn(`[companion] M95-005 COMPANION_STATE_INVALID: ${action.from} x ${action.event}`)
    return { ...snapshot, rejected: snapshot.rejected + 1 }
  }
  const next = transitionTable[snapshot.state][action.type]
  if (!next) {
    return reducer(snapshot, { type: 'GUARD_REJECT', from: snapshot.state, event: action.type })
  }
  // speaking x REPLY_COMPLETED re-arms speaking for a follow-up reply
  // only after playback ended; every other legal target is accepted.
  return { state: next, rejected: snapshot.rejected }
}

export function companionNextState(from: CompanionState, type: CompanionEvent['type']): CompanionState | undefined {
  return transitionTable[from][type]
}

/**
 * Late chat-terminal events arrive after the stage has already moved on.
 * The frozen table has no speaking × REPLY_TERMINAL edge — map that pair
 * onto PLAYBACK_ENDED. Idle/listening leftovers are swallowed so applyEvent
 * does not increment M95-005. Do not add those edges to the matrix.
 */
export function companionEventForDispatch(state: CompanionState, event: CompanionEvent): CompanionEvent | null {
  if (event.type === 'REPLY_TERMINAL' && state === 'speaking') return { type: 'PLAYBACK_ENDED' }
  const lateReply =
    event.type === 'REPLY_TERMINAL' || event.type === 'REPLY_COMPLETED' || event.type === 'PLAYBACK_ENDED'
  if (lateReply && (state === 'idle' || state === 'listening')) return null
  if (event.type === 'REPLY_COMPLETED' && state === 'speaking') return null
  if (event.type === 'PLAYBACK_ENDED' && state === 'thinking') return null
  return event
}

export interface CompanionMachine {
  state: CompanionState
  rejected: number
  /** Next state when accepted; `null` when the guard rejects. Updates the live snapshot immediately so a same-tick follow-up sees the new state. */
  dispatch: (event: CompanionEvent) => CompanionState | null
  /** True when event would be a guard rejection — used by tests. */
  wouldReject: (event: CompanionEvent) => boolean
}

export function useCompanionMachine(): CompanionMachine {
  const [snapshot, rawDispatch] = useReducer(reducer, { state: 'idle', rejected: 0 })
  const snapshotRef = useRef(snapshot)
  snapshotRef.current = snapshot

  const dispatch = useCallback((event: CompanionEvent): CompanionState | null => {
    const next = companionNextState(snapshotRef.current.state, event.type)
    if (!next) {
      rawDispatch(event)
      return null
    }
    snapshotRef.current = { state: next, rejected: snapshotRef.current.rejected }
    rawDispatch(event)
    return next
  }, [])

  const wouldReject = useCallback((event: CompanionEvent) => {
    return companionNextState(snapshotRef.current.state, event.type) === undefined
  }, [])

  return useMemo(
    () => ({ state: snapshot.state, rejected: snapshot.rejected, dispatch, wouldReject }),
    [snapshot.state, snapshot.rejected, dispatch, wouldReject],
  )
}

export const COMPANION_STATE_LABELS: Record<CompanionState, string> = {
  idle: '待机',
  listening: '聆听中',
  thinking: '对答中',
  speaking: '说话中',
}

export const COMPANION_EXECUTING_LABEL = '执行中'

/** Status pill must never say 聆听中 while she is actually talking. */
export function companionSurfaceState(
  state: CompanionState,
  assistantAloud: boolean,
  executing = false,
): CompanionState {
  if (executing && state !== 'listening') return 'thinking'
  if (assistantAloud && state !== 'speaking') return 'speaking'
  return state
}

export function companionStatusLabel(state: CompanionState, executing = false): string {
  if (executing) return COMPANION_EXECUTING_LABEL
  return COMPANION_STATE_LABELS[state]
}
