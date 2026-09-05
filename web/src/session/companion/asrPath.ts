import type { SpeechRecognizer } from './companionSettings'
import type { VoicePath } from './voicePersonas'
import { shownVoicePath } from './voicePersonas'

/** What the stage is actually using this turn, not what settings advertise. */
export type AsrRoute = 'local' | 'cloud' | 'volc'

/**
 * Which listen engine the three product cards open.
 *
 * `auto` is only the leftover cloud-path overlay: prefer sherpa when it is
 * already installed. An explicit 本地 / 火山 card must not wait on that probe
 * and silently fall through to Web Speech.
 */
export function companionListenKind(voicePath: VoicePath, recognizer: SpeechRecognizer): AsrRoute | 'auto' {
  const path = shownVoicePath(voicePath)
  if (path === 'volc') return 'volc'
  if (path === 'local') return 'local'
  if (recognizer === 'local') return 'local'
  if (recognizer === 'cloud') return 'cloud'
  return 'auto'
}

/**
 * Next listen engine after the current one heard audio but produced no text.
 * An explicit 本地 card stays on-machine. An explicit 火山 card stays on
 * seed-asr — sherpa is a different product choice, not a quiet repair.
 */
export function companionListenFailover(failed: AsrRoute, preferred: AsrRoute | 'auto', localReady: boolean): AsrRoute {
  if (preferred === 'local') return 'local'
  if (preferred === 'volc' || failed === 'volc') return 'volc'
  if (failed === 'cloud') return localReady ? 'local' : 'cloud'
  if (failed === 'local') return 'cloud'
  return failed
}

/** Stay-on-volc deaf recoveries before VOICE-004 instead of restarting forever. */
export const VOLC_DEAF_RESTART_LIMIT = 2

export function companionVolcDeafGiveUp(preferred: AsrRoute | 'auto', next: AsrRoute, restarts: number): boolean {
  return preferred === 'volc' && next === 'volc' && restarts >= VOLC_DEAF_RESTART_LIMIT
}

/** Reject if `work` has not settled before `ms`. The original promise keeps running. */
export function withDeadline<T>(work: Promise<T>, ms: number): Promise<T> {
  return new Promise((resolve, reject) => {
    const timer = window.setTimeout(() => reject(new Error('LISTEN_DEADLINE')), ms)
    work.then(
      value => {
        window.clearTimeout(timer)
        resolve(value)
      },
      error => {
        window.clearTimeout(timer)
        reject(error)
      },
    )
  })
}

/**
 * Honest caption for the live recognizer. 'auto' used to look like 本机
 * even when the sidecar was missing and Web Speech had taken over.
 */
export function companionAsrPathLabel(route: AsrRoute, preference: SpeechRecognizer): string {
  if (route === 'local') return '本机识别 · 说完再答 · 打断用按钮'
  if (route === 'volc') return '火山听写 · seed-asr · 说完再答 · 可对着麦打断'
  if (preference === 'cloud') return '系统识别 · 说完再答 · 打断用按钮'
  return '系统识别 · 本机模型未就绪，语音会离开本机 · 说完再答 · 打断用按钮'
}

/** Short 听-light copy for the live route, not the card the user saved. */
export function companionListenLightLabel(route: AsrRoute): string {
  if (route === 'local') return '本机 sherpa'
  if (route === 'volc') return '火山 seed-asr'
  return '系统识别'
}
