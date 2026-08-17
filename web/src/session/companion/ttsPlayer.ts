// ttsPlayer.ts drives the M9.5 speech pipeline (T-9.5.3.2): serial
// segment playback with 80%-progress prefetch of the next synthesis,
// subtitle highlight callbacks, immediate interruption (audio stop +
// tts.cancel, unplayed segments discarded), an AudioContext singleton
// feeding one Analyser for the moon glow, and the 3-consecutive-
// failure circuit breaker from the degradation matrix.
import { getTtsBridge } from '../../bridge/client'
import type { CompanionSettings } from './companionSettings'

export interface TtsPlayerCallbacks {
  onSegmentStart?: (index: number, total: number) => void
  onGain?: (gain: number) => void
  onFinished?: (reason: 'completed' | 'interrupted' | 'circuit-broken' | 'engine-unavailable') => void
  /** M95-001: the machine has no SAPI engine — degrade to subtitles. */
  onEngineUnavailable?: () => void
  onSegmentFailed?: (index: number, consecutiveFailures: number) => void
}

let sharedAudioContext: AudioContext | null = null

/** Engine routing extras carried alongside voiceId/rate/volume so the
 * prefetch synthesizer replays the exact same engine payload. */
export type SynthExtras = Pick<CompanionSettings, 'engine'>

function buildSynthPayload(
  text: string,
  voiceId: string,
  rate: number,
  volume: number,
  extras: SynthExtras,
): Parameters<ReturnType<typeof getTtsBridge>['synthesize']>[0] {
  return {
    text,
    voiceId: voiceId || undefined,
    rate,
    volume,
    engine: extras.engine,
  }
}

export class TtsPlayer {
  private audio: HTMLAudioElement | null = null
  private analyser: AnalyserNode | null = null
  private samples: Uint8Array<ArrayBuffer> | null = null
  private generation = 0
  private blobUrl: string | null = null
  private rafId = 0
  private lastGainAt = 0
  private prefetch: Promise<string> | null = null
  private prefetchIndex = -1
  /** Cleanup of the currently playing segment, fired on interrupt. */
  private activeCleanup: (() => void) | null = null

  /** Serially speak the segments; resolves through onFinished. */
  async speak(segments: string[], settings: CompanionSettings, callbacks: TtsPlayerCallbacks): Promise<void> {
    if (!segments.length) {
      callbacks.onFinished?.('completed')
      return
    }
    unlockTtsAudio()
    this.interruptLocal()
    const generation = ++this.generation
    const bridge = getTtsBridge()
    let consecutiveFailures = 0

    for (let index = 0; index < segments.length; index++) {
      if (generation !== this.generation) return
      let wavBase64: string
      if (this.prefetch && this.prefetchIndex === index) {
        wavBase64 = await this.prefetch
        this.prefetch = null
      } else {
        try {
          const result = await bridge.synthesize(
            buildSynthPayload(segments[index], settings.voiceId || '', settings.rate, settings.volume, settings),
          )
          if (result.discarded || generation !== this.generation) return
          if (!result.wav_base64) return
          wavBase64 = result.wav_base64
        } catch (error) {
          if (generation !== this.generation) return
          if (isEngineUnavailable(error)) {
            callbacks.onEngineUnavailable?.()
            callbacks.onFinished?.('engine-unavailable')
            return
          }
          consecutiveFailures++
          callbacks.onSegmentFailed?.(index, consecutiveFailures)
          if (consecutiveFailures >= 3) {
            callbacks.onFinished?.('circuit-broken')
            return
          }
          continue // skip the segment, keep the subtitle
        }
      }
      if (generation !== this.generation) return
      callbacks.onSegmentStart?.(index, segments.length)
      // Stage the next segment so the 80%-progress timeupdate can prefetch it.
      this.stageNextSegment(segments[index + 1] ?? '', index + 1)
      try {
        await this.playSegment(wavBase64, generation, callbacks)
      } catch {
        if (generation !== this.generation) return
        consecutiveFailures++
        callbacks.onSegmentFailed?.(index, consecutiveFailures)
        if (consecutiveFailures >= 3) {
          callbacks.onFinished?.('circuit-broken')
          return
        }
      }
    }
    if (generation === this.generation) callbacks.onFinished?.('completed')
  }

  private playSegment(wavBase64: string, generation: number, callbacks: TtsPlayerCallbacks): Promise<void> {
    return new Promise(resolve => {
      const audio = this.ensureAudio()
      const byteCharacters = atob(wavBase64)
      const bytes = new Uint8Array(new ArrayBuffer(byteCharacters.length))
      for (let i = 0; i < byteCharacters.length; i++) bytes[i] = byteCharacters.charCodeAt(i)
      if (this.blobUrl) URL.revokeObjectURL(this.blobUrl)
      this.blobUrl = URL.createObjectURL(new Blob([bytes], { type: 'audio/wav' }))

      let prefetched = false
      const onTimeUpdate = () => {
        if (prefetched || !audio.duration || Number.isNaN(audio.duration)) return
        if (audio.currentTime / audio.duration >= 0.8) {
          prefetched = true
          this.schedulePrefetch(generation, audio.dataset.nextText ?? '', Number(audio.dataset.nextIndex ?? -1))
        }
      }
      const cleanup = () => {
        audio.removeEventListener('ended', onEnded)
        audio.removeEventListener('error', onError)
        audio.removeEventListener('timeupdate', onTimeUpdate)
        this.stopGainLoop(callbacks)
        if (this.blobUrl) {
          URL.revokeObjectURL(this.blobUrl)
          this.blobUrl = null
        }
        if (this.activeCleanup === cleanup) this.activeCleanup = null
        resolve()
      }
      const onEnded = () => cleanup()
      const onError = () => cleanup()
      audio.addEventListener('ended', onEnded)
      audio.addEventListener('error', onError)
      audio.addEventListener('timeupdate', onTimeUpdate)
      audio.src = this.blobUrl
      this.activeCleanup = cleanup
      this.startGainLoop(callbacks)
      void audio.play().catch(() => cleanup())
    })
  }

  /** The renderer pre-stages the next segment for the 80% prefetch. */
  stageNextSegment(text: string, index: number): void {
    const audio = this.audio
    if (!audio) return
    audio.dataset.nextText = text
    audio.dataset.nextIndex = String(index)
  }

  private schedulePrefetch(generation: number, text: string, index: number): void {
    if (!text || index < 0 || this.prefetch) return
    this.prefetchIndex = index
    this.prefetch = getTtsBridge()
      .synthesize(
        buildSynthPayload(text, this.currentVoiceId, this.currentRate, this.currentVolume, this.currentExtras),
      )
      .then(result => {
        if (generation !== this.generation || result.discarded) return ''
        return result.wav_base64
      })
      .catch(() => '')
  }

  private currentVoiceId = ''
  private currentRate = 0
  private currentVolume = 80
  private currentExtras: SynthExtras = { engine: 'natural' }

  /** Remember the synthesis parameters so prefetches reuse them. */
  configure(voiceId: string, rate: number, volume: number, extras?: SynthExtras): void {
    this.currentVoiceId = voiceId
    this.currentRate = rate
    this.currentVolume = volume
    if (extras) this.currentExtras = extras
  }

  private ensureAudio(): HTMLAudioElement {
    if (this.audio) return this.audio
    const audio = new Audio()
    this.audio = audio
    try {
      // Only route the element through Web Audio when the shared
      // context is actually running: a suspended context would swallow
      // the sound entirely. unlockTtsAudio() runs on user gestures so
      // the context is running before the first segment; otherwise we
      // skip the analyser (visual-only loss) and keep native playback.
      if (sharedAudioContext && sharedAudioContext.state === 'running') {
        // One createMediaElementSource per element for the whole lifetime.
        const source = sharedAudioContext.createMediaElementSource(audio)
        const analyser = sharedAudioContext.createAnalyser()
        analyser.fftSize = 256
        source.connect(analyser)
        analyser.connect(sharedAudioContext.destination)
        this.analyser = analyser
        this.samples = new Uint8Array(new ArrayBuffer(analyser.frequencyBinCount))
      } else {
        void sharedAudioContext?.resume().catch(() => {})
      }
    } catch {
      this.analyser = null // Visual-only loss: playback still works.
    }
    return audio
  }

  private startGainLoop(callbacks: TtsPlayerCallbacks): void {
    if (!this.analyser || !this.samples) return
    this.lastGainAt = 0
    const loop = () => {
      if (!this.analyser || !this.samples) return
      const now = performance.now()
      if (now - this.lastGainAt >= 33) {
        // ~30fps: throttled reads, low-frequency band average in [0,1].
        this.lastGainAt = now
        this.analyser.getByteFrequencyData(this.samples)
        const band = Math.max(1, Math.floor(this.samples.length / 3))
        let sum = 0
        for (let i = 0; i < band; i++) sum += this.samples[i]
        callbacks.onGain?.(Math.min(1, sum / band / 255))
      }
      this.rafId = requestAnimationFrame(loop)
    }
    this.rafId = requestAnimationFrame(loop)
  }

  private stopGainLoop(callbacks: TtsPlayerCallbacks): void {
    if (this.rafId) cancelAnimationFrame(this.rafId)
    this.rafId = 0
    callbacks.onGain?.(0)
  }

  /** Immediate interruption: silence now, cancel the engine, drop the rest. */
  interrupt(): void {
    this.interruptLocal()
    // Fire-and-forget per the design: no retry, no error surface when
    // the receipt times out — the renderer is already muted.
    getTtsBridge().cancel().catch(() => {})
  }

  private interruptLocal(): void {
    this.generation++
    this.prefetch = null
    this.prefetchIndex = -1
    // Release the in-flight playback first: detaching its listeners and
    // resolving its promise lets the speak() loop exit cleanly instead of
    // awaiting an 'ended' that an interrupted element never fires.
    this.activeCleanup?.()
    this.activeCleanup = null
    if (this.audio) {
      this.audio.pause()
      this.audio.removeAttribute('src')
    }
    if (this.blobUrl) {
      URL.revokeObjectURL(this.blobUrl)
      this.blobUrl = null
    }
  }

  dispose(): void {
    this.interruptLocal()
    this.stopGainLoop({ onGain: () => {} })
    this.audio = null
    this.analyser = null
    this.samples = null
  }
}

/**
 * Autoplay-policy unlock: create/resume the shared AudioContext ahead
 * of playback. Called on user gestures (pointerdown/keydown, moon
 * click, Space) and at speak() time so the analyser path is only taken
 * when the context is actually running — a suspended context would
 * swallow the sound entirely.
 */
export function unlockTtsAudio(): void {
  try {
    sharedAudioContext = sharedAudioContext ?? new AudioContext()
    if (sharedAudioContext.state === 'suspended') void sharedAudioContext.resume().catch(() => {})
  } catch {
    sharedAudioContext = null
  }
}

function isEngineUnavailable(error: unknown): boolean {
  return (
    error instanceof Error &&
    (('code' in error && (error as { code?: unknown }).code === 'M95-001') ||
      /语音合成引擎/.test(error.message))
  )
}
