// useCompanionMachine.ts is the M9.5 Moon Companion state machine
// (T-9.5.2.1): idle / listening / thinking / speaking with a single
// dispatch entry. Guards reject every transition outside the frozen
// matrix (M95-005 COMPANION_STATE_INVALID) while keeping the current
// state and leaving an audit trail in the console.
import { useCallback, useReducer, useRef } from 'react'

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

interface CompanionSnapshot {
  state: CompanionState
  rejected: number // guard rejection counter (M95-005 audit)
}

type CompanionAction = CompanionEvent | { type: 'GUARD_REJECT'; from: CompanionState; event: string }

const transitionTable: Record<CompanionState, Partial<Record<CompanionEvent['type'], CompanionState>>> = {
  idle: { MIC_ACTIVATE: 'listening' },
  listening: { MIC_CANCEL: 'idle', RECOGNIZED_FINAL: 'thinking', MIC_ACTIVATE: 'listening' },
  thinking: { REPLY_COMPLETED: 'speaking', REPLY_TERMINAL: 'idle' },
  // PLAYBACK_ENDED / INTERRUPT in thinking is invalid; speaking handles them.
  speaking: {
    PLAYBACK_ENDED: 'idle',
    INTERRUPT: 'idle',
    // Composite transition: internally speaking -> idle -> listening.
    MIC_CLICK_WHILE_SPEAKING: 'listening',
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

export interface CompanionMachine {
  state: CompanionState
  rejected: number
  dispatch: (event: CompanionEvent) => void
  /** True when event would be a guard rejection — used by tests. */
  wouldReject: (event: CompanionEvent) => boolean
}

export function useCompanionMachine(): CompanionMachine {
  const [snapshot, rawDispatch] = useReducer(reducer, { state: 'idle', rejected: 0 })
  const snapshotRef = useRef(snapshot)
  snapshotRef.current = snapshot

  const dispatch = useCallback((event: CompanionEvent) => {
    rawDispatch(event)
  }, [])

  const wouldReject = useCallback((event: CompanionEvent) => {
    return transitionTable[snapshotRef.current.state][event.type] === undefined
  }, [])

  return { state: snapshot.state, rejected: snapshot.rejected, dispatch, wouldReject }
}
