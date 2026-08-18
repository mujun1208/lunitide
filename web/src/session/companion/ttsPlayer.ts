// ttsPlayer.ts drives the M9.5 speech pipeline (T-9.5.3.2): serial
// segment playback with segment-start prefetch of the next two syntheses
// (P0-2 optimization — prefetch kicks off immediately when the current
// segment starts, and a second prefetch is queued for the segment after
// that, maximizing synthesis/playback overlap),
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
export type SynthExtras = Pick<CompanionSettings, 'engine' | 'refEndpoint'>

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
    refEndpoint: extras.engine === 'ref' && extras.refEndpoint ? extras.refEndpoint : undefined,
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
  /** P0-2: Double prefetch queue — up to 2 segments ahead. */
  private prefetchQueue: Array<{ index: number; promise: Promise<string> }> = []
  /** Cleanup of the currently playing segment, fired on interrupt. */
  private activeCleanup: (() => void) | null = null
  /** P0-1: Streaming enqueue — segments queued while current playback is
   * ongoing, processed FIFO without interrupting the active segment. */
  private pendingSegments: Array<{ text: string; index: number }> = []
  private queueProcessing = false
  private queueGeneration = 0
  private nextEnqueueIndex = 0

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
      // Check prefetch queue for a ready result
      const queued = this.prefetchQueue.find(q => q.index === index)
      if (queued) {
        this.prefetchQueue = this.prefetchQueue.filter(q => q !== queued)
        wavBase64 = await queued.promise
        if (generation !== this.generation) return
        if (!wavBase64) {
          consecutiveFailures++
          callbacks.onSegmentFailed?.(index, consecutiveFailures)
          if (consecutiveFailures >= 3) {
            callbacks.onFinished?.('circuit-broken')
            return
          }
          continue
        }
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
      // P0-2: prefetch the next TWO segments immediately when the current
      // one starts — synthesis runs in parallel with playback, so
      // segment-to-segment pauses are nearly eliminated.
      this.schedulePrefetch(generation, segments[index + 1] ?? '', index + 1)
      this.schedulePrefetch(generation, segments[index + 2] ?? '', index + 2)
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

      const cleanup = () => {
        audio.removeEventListener('ended', onEnded)
        audio.removeEventListener('error', onError)
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
      audio.src = this.blobUrl
      this.activeCleanup = cleanup
      this.startGainLoop(callbacks)
      void audio.play().catch(() => cleanup())
    })
  }

  private schedulePrefetch(generation: number, text: string, index: number): void {
    if (!text || index < 0) return
    // Don't queue duplicates — only prefetch if not already queued
    if (this.prefetchQueue.some(q => q.index === index)) return
    // Limit to 2 pending prefetches
    if (this.prefetchQueue.length >= 2) return
    this.prefetchQueue.push({
      index,
      promise: getTtsBridge()
        .synthesize(
          buildSynthPayload(text, this.currentVoiceId, this.currentRate, this.currentVolume, this.currentExtras),
        )
        .then(result => {
          if (generation !== this.generation || result.discarded) return ''
          return result.wav_base64
        })
        .catch(() => ''),
    })
  }

  private currentVoiceId = ''
  private currentRate = 0
  private currentVolume = 80
  private currentExtras: SynthExtras = { engine: 'natural', refEndpoint: '' }

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

  /** P0-1: Enqueue segments for streaming playback. New segments are
   * appended to the pending queue and played in order without interrupting
   * the currently active segment. Call `flush()` after the stream ends. */
  enqueue(segments: string[], settings: CompanionSettings, callbacks: TtsPlayerCallbacks): void {
    if (!segments.length) return
    unlockTtsAudio()
    this.configure(settings.voiceId || '', settings.rate, settings.volume, settings)
    const startIndex = this.nextEnqueueIndex
    this.nextEnqueueIndex += segments.length
    this.pendingSegments.push(...segments.map((text, i) => ({ text, index: startIndex + i })))
    if (!this.queueProcessing) {
      this.processQueue(callbacks)
    }
  }

  /** P0-1: Flush remaining queue and resolve when all queued segments are done. */
  async flush(callbacks: TtsPlayerCallbacks): Promise<void> {
    if (!this.queueProcessing) {
      callbacks.onFinished?.('completed')
      return
    }
    // Wait for the queue to drain naturally — processQueue will call onFinished
    // when the last segment is done and the queue is empty.
    return new Promise(resolve => {
      const check = () => {
        if (!this.queueProcessing) {
          resolve()
        } else {
          setTimeout(check, 50)
        }
      }
      check()
    })
  }

  private async processQueue(callbacks: TtsPlayerCallbacks): Promise<void> {
    this.queueProcessing = true
    const gen = ++this.queueGeneration
    const bridge = getTtsBridge()
    let consecutiveFailures = 0

    while (this.pendingSegments.length > 0) {
      if (gen !== this.queueGeneration) return
      const { text, index } = this.pendingSegments.shift()!
      let wavBase64: string
      try {
        const result = await bridge.synthesize(
          buildSynthPayload(text, this.currentVoiceId, this.currentRate, this.currentVolume, this.currentExtras),
        )
        if (gen !== this.queueGeneration) return
        if (result.discarded) continue
        if (!result.wav_base64) continue
        wavBase64 = result.wav_base64
      } catch (error) {
        if (gen !== this.queueGeneration) return
        if (isEngineUnavailable(error)) {
          callbacks.onEngineUnavailable?.()
          callbacks.onFinished?.('engine-unavailable')
          this.queueProcessing = false
          return
        }
        consecutiveFailures++
        callbacks.onSegmentFailed?.(index, consecutiveFailures)
        if (consecutiveFailures >= 3) {
          callbacks.onFinished?.('circuit-broken')
          this.queueProcessing = false
          return
        }
        continue
      }
      if (gen !== this.queueGeneration) return
      consecutiveFailures = 0
      callbacks.onSegmentStart?.(index, this.nextEnqueueIndex)
      // P0-2: prefetch the next segment from the queue for seamless transitions
      this.schedulePrefetchFromQueue(gen)
      try {
        await this.playSegment(wavBase64, gen, callbacks)
      } catch {
        if (gen !== this.queueGeneration) return
        consecutiveFailures++
        callbacks.onSegmentFailed?.(index, consecutiveFailures)
        if (consecutiveFailures >= 3) {
          callbacks.onFinished?.('circuit-broken')
          this.queueProcessing = false
          return
        }
      }
    }
    if (gen === this.queueGeneration) {
      this.queueProcessing = false
      callbacks.onFinished?.('completed')
    }
  }

  /** Prefetch the next pending segment for synthesis/playback overlap. */
  private schedulePrefetchFromQueue(generation: number): void {
    if (this.pendingSegments.length === 0) return
    if (this.prefetchQueue.length >= 2) return
    const next = this.pendingSegments[0]
    if (this.prefetchQueue.some(q => q.index === next.index)) return
    this.prefetchQueue.push({
      index: next.index,
      promise: getTtsBridge()
        .synthesize(
          buildSynthPayload(next.text, this.currentVoiceId, this.currentRate, this.currentVolume, this.currentExtras),
        )
        .then(result => {
          if (generation !== this.queueGeneration || result.discarded) return ''
          return result.wav_base64
        })
        .catch(() => ''),
    })
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
    this.queueGeneration++
    this.prefetchQueue = []
    this.pendingSegments = []
    this.queueProcessing = false
    this.nextEnqueueIndex = 0
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
