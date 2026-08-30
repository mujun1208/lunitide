import type { SpeechRecognizer } from './companionSettings'

/** What the stage is actually using this turn, not what settings advertise. */
export type AsrRoute = 'local' | 'cloud' | 'volc'

/**
 * Honest caption for the live recognizer. 'auto' used to look like 本机
 * even when the sidecar was missing and Web Speech had taken over.
 */
export function companionAsrPathLabel(route: AsrRoute, preference: SpeechRecognizer): string {
  if (route === 'local') return '本机识别 · 音频留在这台电脑'
  if (route === 'volc') return '火山听写 · seed-asr'
  if (preference === 'cloud') return '系统识别'
  return '系统识别 · 本机模型未就绪，语音会离开本机'
}
