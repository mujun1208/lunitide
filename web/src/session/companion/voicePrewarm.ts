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

/** Tri-state so an engine-aware default can apply when the user has not chosen.
 * 'default' = follow the per-engine default; 'on'/'off' = explicit override. */
export type PrewarmPref = 'default' | 'on' | 'off'

export function voicePrewarmPref(): PrewarmPref {
  try {
    const v = localStorage.getItem(FLAG_KEY)
    return v === 'on' ? 'on' : v === 'off' ? 'off' : 'default'
  } catch {
    return 'default'
  }
}

export function setVoicePrewarmPref(pref: PrewarmPref): void {
  try {
    if (pref === 'default') localStorage.removeItem(FLAG_KEY)
    else localStorage.setItem(FLAG_KEY, pref)
  } catch {
    /* private-mode / storage-disabled: prewarm simply follows the default */
  }
}

/** Only the cold-start engines benefit; edge/sapi are already warm-pooled. */
export function shouldPrewarmEngine(engine: CompanionSettings['engine']): boolean {
  return engine === 'ref' || engine === 'volc'
}

/**
 * Per-engine default when the user has not chosen. Local GPT-SoVITS (ref)
 * prewarms by default — it is free and removes the ~8s first-reply cold start
 * (validated by the seeded latency simulation). Volc prewarm stays opt-in
 * because a warmup synth spends Agent Plan quota.
 */
export function prewarmDefaultForEngine(engine: CompanionSettings['engine']): boolean {
  return engine === 'ref'
}

/** Effective on/off for one engine — what the settings toggle should show. */
export function voicePrewarmEffective(engine: CompanionSettings['engine']): boolean {
  const pref = voicePrewarmPref()
  if (pref === 'on') return true
  if (pref === 'off') return false
  return prewarmDefaultForEngine(engine)
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
  return shouldPrewarmEngine(settings.engine) && voicePrewarmEffective(settings.engine)
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
