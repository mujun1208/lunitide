// Home-wake live-voice gate. Zipformer KWS is a separate model download;
// this is the piece we can run on the same getUserMedia stream the wake
// listener already holds: reject a match when the last ~800 ms looks like
// a loud, unmodulated speaker rather than a person addressing the mic.
//
// Analyser missing (tests, old WebView) fails open — wake still works.

export const WAKE_VAD_FRAME_MS = 20
export const WAKE_VAD_WINDOW_FRAMES = 40

export interface WakeVadSnapshot {
  speechLikely: boolean
  playbackLikely: boolean
  tooQuiet: boolean
}

export type WakeMatchKind = 'none' | 'phrase' | 'name'

export function classifyWakeEnergy(peaks: number[]): WakeVadSnapshot {
  if (peaks.length < 8) {
    return { speechLikely: false, playbackLikely: false, tooQuiet: true }
  }
  const recent = peaks.slice(-25)
  let sum = 0
  let max = 0
  for (const peak of recent) {
    sum += peak
    if (peak > max) max = peak
  }
  const mean = sum / recent.length
  let varSum = 0
  for (const peak of recent) {
    const delta = peak - mean
    varSum += delta * delta
  }
  const stdev = Math.sqrt(varSum / recent.length)
  const cv = mean > 1e-6 ? stdev / mean : 0
  const tooQuiet = max < 0.02
  // TV / speaker bleed is loud and relatively flat. Live address has
  // syllable-scale amplitude swings (higher coefficient of variation).
  const playbackLikely = !tooQuiet && mean > 0.08 && cv < 0.18 && max > 0.12
  const speechLikely = !tooQuiet && max > 0.03 && cv > 0.25 && cv < 1.8
  return { speechLikely, playbackLikely, tooQuiet }
}

export function shouldAcceptWake(
  kind: WakeMatchKind,
  snapshot: WakeVadSnapshot | null,
  vadEnabled: boolean,
): boolean {
  if (!vadEnabled || kind === 'none') return kind !== 'none'
  if (!snapshot) return true
  if (snapshot.playbackLikely) return false
  if (kind === 'name' && (snapshot.tooQuiet || !snapshot.speechLikely)) return false
  return true
}

export function createWakeVadMonitor(stream: MediaStream): { read: () => WakeVadSnapshot; stop: () => void } | null {
  const AudioContextClass =
    window.AudioContext ?? (window as typeof window & { webkitAudioContext?: typeof AudioContext }).webkitAudioContext
  if (!AudioContextClass) return null
  let context: AudioContext
  try {
    context = new AudioContextClass()
  } catch {
    return null
  }
  let source: MediaStreamAudioSourceNode
  try {
    source = context.createMediaStreamSource(stream)
  } catch {
    void context.close()
    return null
  }
  const analyser = context.createAnalyser()
  analyser.fftSize = 512
  analyser.smoothingTimeConstant = 0
  source.connect(analyser)
  const time = new Uint8Array(analyser.fftSize)
  const peaks: number[] = []
  const timer = window.setInterval(() => {
    analyser.getByteTimeDomainData(time)
    let energy = 0
    for (let i = 0; i < time.length; i++) {
      const sample = (time[i]! - 128) / 128
      energy += sample * sample
    }
    peaks.push(Math.sqrt(energy / time.length))
    if (peaks.length > WAKE_VAD_WINDOW_FRAMES) peaks.shift()
  }, WAKE_VAD_FRAME_MS)
  return {
    read: () => classifyWakeEnergy(peaks),
    stop: () => {
      window.clearInterval(timer)
      try {
        source.disconnect()
      } catch {
        /* already torn down */
      }
      void context.close()
    },
  }
}
