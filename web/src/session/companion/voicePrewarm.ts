// V3 (opt-in, default off): prewarm the cold-start synthesis engines.
//
// GPT-SoVITS (ref) launches a host on first use (up to ~8s) and the volc
// seed-tts connection has a handshake cost. Edge/SAPI already keep a warm
// socket pool, so they gain nothing here. When the flag is on, entering the
// companion stage fires one discardable synth so the first *real* reply is not
// the one paying the cold start. Audio is never played — this only spins the
// engine up.

import { getTtsBridge } from '../../bridge/client'
import type { CompanionSettings } from './companionSettings'

const FLAG_KEY = 'lunitide:voicePrewarm'

/** Shortest neutral utterance; enough to force the engine/host to initialize. */
export const PREWARM_TEXT = '。'

export function voicePrewarmEnabled(): boolean {
  try {
    return localStorage.getItem(FLAG_KEY) === 'on'
  } catch {
    return false
  }
}

export function setVoicePrewarmEnabled(on: boolean): void {
  try {
    localStorage.setItem(FLAG_KEY, on ? 'on' : 'off')
  } catch {
    /* private-mode / storage-disabled: prewarm simply stays off */
  }
}

/** Only the cold-start engines benefit; edge/sapi are already warm-pooled. */
export function shouldPrewarmEngine(engine: CompanionSettings['engine']): boolean {
  return engine === 'ref' || engine === 'volc'
}

export type PrewarmInput = Pick<CompanionSettings, 'engine' | 'voiceId' | 'refEndpoint' | 'rate' | 'volume'>

export function buildPrewarmPayload(settings: PrewarmInput): Parameters<ReturnType<typeof getTtsBridge>['synthesize']>[0] {
  return {
    text: PREWARM_TEXT,
    voiceId: settings.voiceId || undefined,
    rate: settings.rate,
    // Discarded anyway, but silence keeps a stray play() from being audible.
    volume: 0,
    engine: settings.engine,
    refEndpoint: settings.engine === 'ref' && settings.refEndpoint ? settings.refEndpoint : undefined,
  }
}

/** Whether a prewarm should be attempted for these settings right now. */
export function shouldPrewarm(settings: PrewarmInput): boolean {
  return voicePrewarmEnabled() && shouldPrewarmEngine(settings.engine)
}

/** Fire-and-forget warm of the selected cold-start engine. Returns whether a
 *  warm request was actually issued. Never throws: a failed warm is harmless,
 *  the real turn will just pay the cold start as before. */
export async function prewarmVoice(settings: PrewarmInput): Promise<boolean> {
  if (!shouldPrewarm(settings)) return false
  try {
    await getTtsBridge().synthesize(buildPrewarmPayload(settings))
    return true
  } catch {
    return false
  }
}
