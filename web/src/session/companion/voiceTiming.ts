// Local voice-turn timing. No product thresholds change from these numbers.
// Flush is a console table plus an in-memory ring so 20 idle chats can be
// drawn without leaving the machine.

export type VoiceTimingField =
  | 'endpoint'
  | 'append'
  | 'startReturn'
  | 'ttfb'
  | 'firstSynth'
  | 'firstAudio'
  | 'echo'
  | 'tools'

export type VoiceTurnOutcome = 'ok' | 'stall'

export type VoiceTurnRecord = {
  path: string
  endpointMs?: number
  appendMs?: number
  startReturnMs?: number
  ttfbMs?: number
  firstSynthMs?: number
  firstAudioMs?: number
  echoMs?: number
  toolsMs?: number
  outcome: VoiceTurnOutcome
}

type OpenTurn = VoiceTurnRecord & { t0: number }

const RING = 40
const ring: VoiceTurnRecord[] = []
let current: OpenTurn | undefined

const now = () => (typeof performance !== 'undefined' ? performance.now() : Date.now())

export function startVoiceTurn(path: string): void {
  current = { path, outcome: 'ok', t0: now() }
}

export function currentVoiceTiming(field: VoiceTimingField): number | undefined {
  if (!current) return undefined
  return current[`${field}Ms`]
}

export function markVoiceTiming(field: VoiceTimingField, at = now()): void {
  const turn = current
  if (!turn) return
  const key = `${field}Ms` as const
  if (turn[key] != null) return
  turn[key] = Math.round(at - turn.t0)
}

export function finishVoiceTurn(outcome: VoiceTurnOutcome = 'ok'): VoiceTurnRecord | undefined {
  const turn = current
  if (!turn) return undefined
  current = undefined
  const { t0: _t0, ...record } = turn
  record.outcome = outcome
  ring.push(record)
  if (ring.length > RING) ring.shift()
  if (typeof console !== 'undefined' && console.debug) {
    console.debug(
      `[voiceTiming] path=${record.path} endpoint=${record.endpointMs ?? '-'} append=${record.appendMs ?? '-'} start=${record.startReturnMs ?? '-'} ttfb=${record.ttfbMs ?? '-'} synth=${record.firstSynthMs ?? '-'} audio=${record.firstAudioMs ?? '-'} echo=${record.echoMs ?? '-'} tools=${record.toolsMs ?? '-'} ${record.outcome}`,
    )
  }
  return record
}

export function peekVoiceTimings(): readonly VoiceTurnRecord[] {
  return ring
}

export function resetVoiceTimings(): void {
  ring.length = 0
  current = undefined
}
