// ttsPlayer.ts drives the M9.5 speech pipeline with a gapless Web Audio
// scheduler: every synthesized wav is decoded to an AudioBuffer the
// moment it arrives (decode overlaps playback), silence-padded engine
// tails are trimmed, then clips are queued on one AudioContext timeline
// with a ~28ms overlap so speech sounds like one take. A GainNode
// applies volume; the Analyser keeps feeding the moon glow.
// Environments without AudioContext (jsdom) fall back to the legacy
// HTMLAudioElement path so tests keep running unchanged.
// Also: streaming enqueue (P0-1), triple prefetch that processQueue
// actually consumes (P0-2), immediate interruption, and the
// 3-consecutive-failure circuit breaker.
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
 *  prefetch synthesizer replays the exact same engine payload. */
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

/** A fully-prepared segment: base64 wav plus (when Web Audio is up) the
 *  pre-decoded buffer ready for sample-accurate scheduling. */
interface ReadySegment {
  wavBase64: string
  buffer: AudioBuffer | null
}

export class TtsPlayer {
  // ---- Web Audio graph (gapless path) ----
  private ctx: AudioContext | null = null
  private gainNode: GainNode | null = null
  private analyser: AnalyserNode | null = null
  private samples: Uint8Array<ArrayBuffer> | null = null
  /** Absolute AudioContext time where the next segment should start. */
  private timelineEnd = 0
  private activeSources = new Set<AudioBufferSourceNode>()
  // ---- Legacy fallback (no AudioContext) ----
  private audio: HTMLAudioElement | null = null
  private blobUrl: string | null = null
  private activeCleanup: (() => void) | null = null
  // ---- Shared pipeline state ----
  private generation = 0
  private rafId = 0
  private lastGainAt = 0
  /** P0-2: Prefetch queue — up to 3 segments ahead, decoded. */
  private prefetchQueue: Array<{ index: number; promise: Promise<ReadySegment | null> }> = []
  /** P0-1: Streaming enqueue — segments queued while current playback is
   *  ongoing, processed FIFO without interrupting the active segment. */
  private pendingSegments: Array<{ text: string; index: number }> = []
  private queueProcessing = false
  private queueGeneration = 0
  private nextEnqueueIndex = 0
  private currentVoiceId = ''
  private currentRate = 0
  private currentVolume = 80
  private currentExtras: SynthExtras = { engine: 'edge', refEndpoint: '' }

  /** True while a clip is synthesizing or playing — used to pause mic commit. */
  isBusy(): boolean {
    return this.queueProcessing || this.pendingSegments.length > 0 || this.activeSources.size > 0
  }

  /** Remember the synthesis parameters so prefetches reuse them. */
  configure(voiceId: string, rate: number, volume: number, extras?: SynthExtras): void {
    this.currentVoiceId = voiceId
    this.currentRate = rate
    this.currentVolume = volume
    if (extras) this.currentExtras = extras
    if (this.gainNode) this.gainNode.gain.value = Math.min(1, Math.max(0, volume / 100))
  }

  // ------------------------------------------------------------------
  // Web Audio graph
  // ------------------------------------------------------------------

  /** Build (once) the gain → analyser → destination chain on the shared
   *  context. Returns null when Web Audio is unavailable (jsdom) — the
   *  caller then takes the legacy HTMLAudioElement fallback. */
  private ensureGraph(): AudioContext | null {
    if (this.ctx) {
      if (this.ctx.state === 'suspended') void this.ctx.resume().catch(() => {})
      return this.ctx
    }
    try {
      unlockTtsAudio()
      const ctx = sharedAudioContext
      if (!ctx) return null
      this.gainNode = ctx.createGain()
      this.gainNode.gain.value = Math.min(1, Math.max(0, this.currentVolume / 100))
      this.analyser = ctx.createAnalyser()
      this.analyser.fftSize = 256
      this.gainNode.connect(this.analyser)
      this.analyser.connect(ctx.destination)
      this.samples = new Uint8Array(new ArrayBuffer(this.analyser.frequencyBinCount))
      this.ctx = ctx
      return ctx
    } catch {
      this.ctx = null
      return null
    }
  }

  /** Decode a base64 wav into an AudioBuffer (null when the graph is
   *  unavailable or the bytes are not decodable). */
  private decodeWav(wavBase64: string): Promise<AudioBuffer | null> {
    const ctx = this.ensureGraph()
    if (!ctx) return Promise.resolve(null)
    const bin = atob(wavBase64)
    const bytes = new Uint8Array(new ArrayBuffer(bin.length))
    for (let i = 0; i < bin.length; i++) bytes[i] = bin.charCodeAt(i)
    try {
      return ctx.decodeAudioData(bytes.buffer).catch(() => null)
    } catch {
      return Promise.resolve(null)
    }
  }

  /** Schedule a decoded buffer on the gapless timeline. Leading/trailing
   *  engine padding is trimmed, then the next clip overlaps 28ms so the
   *  join sounds like one take rather than two readings. */
  private scheduleBuffer(buffer: AudioBuffer, callbacks: TtsPlayerCallbacks): boolean {
    const ctx = this.ensureGraph()
    // A suspended context "plays" without sound. Voice wake never grants a
    // click gesture, so skip Web Audio until resume() actually succeeds.
    if (!ctx || ctx.state !== 'running' || !this.gainNode) return false
    const playable = trimSilence(ctx, buffer) ?? buffer
    const source = ctx.createBufferSource()
    source.buffer = playable
    source.connect(this.gainNode)
    const overlap = this.timelineEnd > ctx.currentTime + 0.08 ? 0.028 : 0
    const startAt = this.timelineEnd > ctx.currentTime ? this.timelineEnd - overlap : ctx.currentTime
    this.timelineEnd = startAt + playable.duration
    this.activeSources.add(source)
    source.onended = () => {
      this.activeSources.delete(source)
      if (this.activeSources.size === 0) this.stopGainLoop(callbacks)
    }
    try {
      source.start(startAt)
    } catch {
      this.activeSources.delete(source)
      return false
    }
    this.startGainLoop(callbacks)
    return true
  }

  /** Wait until the scheduled timeline has fully sounded, a newer
   *  generation took over, or more segments were enqueued (so the next
   *  clip can be scheduled onto the still-running timeline). */
  private waitForTimeline(generation: number, queueGeneration?: number): Promise<void> {
    return new Promise(resolve => {
      const check = () => {
        const stale =
          queueGeneration !== undefined
            ? queueGeneration !== this.queueGeneration
            : generation !== this.generation
        if (stale) return resolve()
        if (this.pendingSegments.length > 0) return resolve()
        if (!this.ctx || !this.timelineEnd || this.ctx.currentTime >= this.timelineEnd - 0.03) return resolve()
        setTimeout(check, 40)
      }
      check()
    })
  }

  // ------------------------------------------------------------------
  // Legacy fallback path (HTMLAudioElement, jsdom / no Web Audio)
  // ------------------------------------------------------------------

  private ensureAudio(): HTMLAudioElement {
    if (this.audio) return this.audio
    const audio = new Audio()
    this.audio = audio
    try {
      // Only route the element through Web Audio when the shared
      // context is actually running: a suspended context would swallow
      // the sound entirely.
      if (sharedAudioContext && sharedAudioContext.state === 'running') {
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

  private async playSegmentFallback(wavBase64: string, generation: number, callbacks: TtsPlayerCallbacks): Promise<boolean> {
    return new Promise<boolean>(resolve => {
      const audio = this.ensureAudio()
      const byteCharacters = atob(wavBase64)
      const bytes = new Uint8Array(new ArrayBuffer(byteCharacters.length))
      for (let i = 0; i < byteCharacters.length; i++) bytes[i] = byteCharacters.charCodeAt(i)
      if (this.blobUrl) URL.revokeObjectURL(this.blobUrl)
      this.blobUrl = URL.createObjectURL(new Blob([bytes], { type: bytes[0] === 0xff || (bytes[0] === 0x49 && bytes[1] === 0x44) ? 'audio/mpeg' : 'audio/wav' }))

      let settled = false
      const finish = (ok: boolean) => {
        if (settled) return
        settled = true
        audio.removeEventListener('ended', onEnded)
        audio.removeEventListener('error', onError)
        this.stopGainLoop(callbacks)
        if (this.blobUrl) {
          URL.revokeObjectURL(this.blobUrl)
          this.blobUrl = null
        }
        if (this.activeCleanup === cleanup) this.activeCleanup = null
        resolve(ok)
      }
      const cleanup = () => finish(true)
      const onEnded = () => finish(true)
      const onError = () => finish(false)
      audio.addEventListener('ended', onEnded)
      audio.addEventListener('error', onError)
      audio.src = this.blobUrl
      this.activeCleanup = cleanup
      this.startGainLoop(callbacks)
      void (async () => {
        try {
          await audio.play()
        } catch {
          await unlockTtsAudio()
          try {
            await audio.play()
          } catch {
            finish(false)
          }
        }
      })()
      void generation
    })
  }

  // ------------------------------------------------------------------
  // Segment playback: gapless schedule when possible, else fallback
  // ------------------------------------------------------------------

  /** Play one prepared segment. Returns false when both paths fail. */
  private async playSegment(seg: ReadySegment, generation: number, queueGeneration: number | undefined, callbacks: TtsPlayerCallbacks): Promise<boolean> {
    await unlockTtsAudio()
    const ctx = this.ensureGraph()
    if (ctx?.state === 'suspended') await ctx.resume().catch(() => {})
    if (seg.buffer && ctx?.state === 'running' && this.scheduleBuffer(seg.buffer, callbacks)) return true
    try {
      return await this.playSegmentFallback(seg.wavBase64, generation, callbacks)
    } catch {
      return false
    }
  }

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
    let startingRetries = 0
    let scheduledAny = false

    for (let index = 0; index < segments.length; index++) {
      if (generation !== this.generation) return
      let seg: ReadySegment | null
      const queued = this.prefetchQueue.find(q => q.index === index)
      if (queued) {
        this.prefetchQueue = this.prefetchQueue.filter(q => q !== queued)
        seg = await queued.promise
        if (generation !== this.generation) return
      } else {
        try {
          const result = await bridge.synthesize(
            buildSynthPayload(segments[index], settings.voiceId || '', settings.rate, settings.volume, settings),
          )
          if (result.discarded || generation !== this.generation) return
          if (!result.wav_base64) return
          seg = { wavBase64: result.wav_base64, buffer: await this.decodeWav(result.wav_base64) }
        } catch (error) {
          if (generation !== this.generation) return
          if (isRefEngineStarting(error) && startingRetries < 24) {
            // Auto-hosted model still loading: wait and retry THIS
            // segment (no failure count, no circuit break) for up to
            // ~2 minutes while the backend brings the service up.
            startingRetries++
            await sleep(5000)
            if (generation !== this.generation) return
            index--
            continue
          }
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
      if (!seg) {
        // Synthesis/prefetch produced nothing: treat as a segment failure.
        consecutiveFailures++
        callbacks.onSegmentFailed?.(index, consecutiveFailures)
        if (consecutiveFailures >= 3) {
          callbacks.onFinished?.('circuit-broken')
          return
        }
        continue
      }
      consecutiveFailures = 0
      callbacks.onSegmentStart?.(index, segments.length)
      // P0-2: prefetch the next TWO segments immediately — synthesis AND
      // decoding overlap playback, so the timeline never starves.
      this.schedulePrefetch(generation, segments[index + 1] ?? '', index + 1)
      this.schedulePrefetch(generation, segments[index + 2] ?? '', index + 2)
      // Gapless core: schedule on the timeline and move on — the next
      // segment's synthesis/decode runs while this one is still sounding.
      const played = await this.playSegment(seg, generation, undefined, callbacks)
      if (!played) {
        if (generation !== this.generation) return
        consecutiveFailures++
        callbacks.onSegmentFailed?.(index, consecutiveFailures)
        if (consecutiveFailures >= 3) {
          callbacks.onFinished?.('circuit-broken')
          return
        }
        continue
      }
      scheduledAny = true
    }
    if (generation !== this.generation) return
    if (scheduledAny) await this.waitForTimeline(generation)
    if (generation === this.generation) callbacks.onFinished?.('completed')
  }

  private schedulePrefetch(generation: number, text: string, index: number): void {
    if (!text || index < 0) return
    if (this.prefetchQueue.some(q => q.index === index)) return
    if (this.prefetchQueue.length >= 2) return
    const synth = () =>
      getTtsBridge().synthesize(
        buildSynthPayload(text, this.currentVoiceId, this.currentRate, this.currentVolume, this.currentExtras),
      )
    // Decode as soon as the wav arrives so playback never waits on it.
    const prepare = async (): Promise<ReadySegment | null> => {
      const result = await synth()
      if (result.discarded || !result.wav_base64) return null
      return { wavBase64: result.wav_base64, buffer: await this.decodeWav(result.wav_base64) }
    }
    this.prefetchQueue.push({
      index,
      promise: prepare().catch(async (error: unknown) => {
        // One slow retry while the hosted model server is loading.
        if (!isRefEngineStarting(error)) return null
        await sleep(5000)
        if (generation !== this.generation) return null
        try {
          return await prepare()
        } catch {
          return null
        }
      }),
    })
  }

  /** P0-1: Enqueue segments for streaming playback. New segments are
   *  appended to the pending queue and played in order without interrupting
   *  the currently active segment. Call `flush()` after the stream ends. */
  enqueue(segments: string[], settings: CompanionSettings, callbacks: TtsPlayerCallbacks): void {
    if (!segments.length) return
    unlockTtsAudio()
    this.configure(settings.voiceId || '', settings.rate, settings.volume, settings)
    const startIndex = this.nextEnqueueIndex
    this.nextEnqueueIndex += segments.length
    this.pendingSegments.push(...segments.map((text, i) => ({ text, index: startIndex + i })))
    this.fillPrefetch(this.queueProcessing ? this.queueGeneration : this.queueGeneration + 1)
    if (!this.queueProcessing) {
      void this.processQueue(callbacks)
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
    let startingRetries = 0
    let scheduledAny = false

    while (true) {
      while (this.pendingSegments.length > 0) {
        if (gen !== this.queueGeneration) return
        this.fillPrefetch(gen)
        const { text, index } = this.pendingSegments.shift()!
        let seg: ReadySegment | null = null
        const queued = this.prefetchQueue.find(q => q.index === index)
        if (queued) {
          this.prefetchQueue = this.prefetchQueue.filter(q => q !== queued)
          seg = await queued.promise
          if (gen !== this.queueGeneration) return
        }
        if (!seg) {
          try {
            const result = await bridge.synthesize(
              buildSynthPayload(text, this.currentVoiceId, this.currentRate, this.currentVolume, this.currentExtras),
            )
            if (gen !== this.queueGeneration) return
            if (result.discarded || !result.wav_base64) continue
            seg = { wavBase64: result.wav_base64, buffer: await this.decodeWav(result.wav_base64) }
          } catch (error) {
            if (gen !== this.queueGeneration) return
            if (isRefEngineStarting(error) && startingRetries < 24) {
              startingRetries++
              this.pendingSegments.unshift({ text, index })
              await sleep(5000)
              if (gen !== this.queueGeneration) return
              continue
            }
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
        }
        if (gen !== this.queueGeneration) return
        if (!seg) {
          consecutiveFailures++
          callbacks.onSegmentFailed?.(index, consecutiveFailures)
          if (consecutiveFailures >= 3) {
            callbacks.onFinished?.('circuit-broken')
            this.queueProcessing = false
            return
          }
          continue
        }
        consecutiveFailures = 0
        callbacks.onSegmentStart?.(index, this.nextEnqueueIndex)
        this.fillPrefetch(gen)
        const played = await this.playSegment(seg, this.generation, gen, callbacks)
        if (!played) {
          if (gen !== this.queueGeneration) return
          consecutiveFailures++
          callbacks.onSegmentFailed?.(index, consecutiveFailures)
          if (consecutiveFailures >= 3) {
            callbacks.onFinished?.('circuit-broken')
            this.queueProcessing = false
            return
          }
          continue
        }
        scheduledAny = true
      }
      if (gen !== this.queueGeneration) return
      // Drain or accept more streaming text: wake early when enqueue
      // adds a clip so it can join the still-running timeline.
      if (scheduledAny) await this.waitForTimeline(this.generation, gen)
      if (gen !== this.queueGeneration) return
      if (this.pendingSegments.length > 0) continue
      this.queueProcessing = false
      if (this.pendingSegments.length > 0) {
        void this.processQueue(callbacks)
        return
      }
      callbacks.onFinished?.('completed')
      return
    }
  }

  /** Prefetch up to 3 pending segments so synthesis overlaps playback. */
  private fillPrefetch(generation: number): void {
    for (const item of this.pendingSegments) {
      if (this.prefetchQueue.length >= 3) return
      if (this.prefetchQueue.some(q => q.index === item.index)) continue
      const text = item.text
      const synth = () =>
        getTtsBridge().synthesize(
          buildSynthPayload(text, this.currentVoiceId, this.currentRate, this.currentVolume, this.currentExtras),
        )
      const prepare = async (): Promise<ReadySegment | null> => {
        const result = await synth()
        if (result.discarded || !result.wav_base64) return null
        return { wavBase64: result.wav_base64, buffer: await this.decodeWav(result.wav_base64) }
      }
      this.prefetchQueue.push({
        index: item.index,
        promise: prepare().catch(async (error: unknown) => {
          if (!isRefEngineStarting(error)) return null
          await sleep(5000)
          if (generation !== this.queueGeneration) return null
          try {
            return await prepare()
          } catch {
            return null
          }
        }),
      })
    }
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
    // Stop every scheduled source at once: both the sounding segment
    // and the already-scheduled (silent) followers on the timeline.
    for (const source of this.activeSources) {
      try {
        source.onended = null
        source.stop()
      } catch {
        // already finished
      }
    }
    this.activeSources.clear()
    this.timelineEnd = 0
    this.stopGainLoop({ onGain: () => {} })
    // Release the legacy fallback element, if any.
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

  private startGainLoop(callbacks: TtsPlayerCallbacks): void {
    if (!this.analyser || !this.samples) return
    if (this.rafId) return // already running
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

  dispose(): void {
    this.interruptLocal()
    this.stopGainLoop({ onGain: () => {} })
    this.audio = null
    this.analyser = null
    this.samples = null
    this.ctx = null
    this.gainNode = null
  }
}

/** Returns whether the shared Web Audio context can audibly play TTS. */
export function getTtsAudioState(): 'running' | 'suspended' | 'unsupported' {
  try {
    if (!sharedAudioContext) return 'suspended'
    if (sharedAudioContext.state === 'running') return 'running'
    return 'suspended'
  } catch {
    return 'unsupported'
  }
}

/**
 * Autoplay-policy unlock: create/resume the shared AudioContext ahead
 * of playback. Called on user gestures (pointerdown/keydown, moon
 * click, Space) and at speak() time so the analyser path is only taken
 * when the context is actually running — a suspended context would
 * swallow the sound entirely.
 */
export function unlockTtsAudio(): Promise<void> {
  try {
    sharedAudioContext = sharedAudioContext ?? new AudioContext()
    if (sharedAudioContext.state === 'suspended') return sharedAudioContext.resume().then(() => undefined).catch(() => {})
  } catch {
    sharedAudioContext = null
  }
  return Promise.resolve()
}

function isEngineUnavailable(error: unknown): boolean {
  if (isRefEngineStarting(error)) return false // starting is a wait-state, not a permanent loss
  return (
    error instanceof Error &&
    (('code' in error && (error as { code?: unknown }).code === 'M95-001') ||
      /语音合成引擎/.test(error.message))
  )
}

/** The hosted GPT-SoVITS service is loading (M95-001 + 启动中): wait and
 *  retry instead of degrading or circuit-breaking. */
function isRefEngineStarting(error: unknown): boolean {
  return (
    error instanceof Error &&
    'code' in error &&
    (error as { code?: unknown }).code === 'M95-001' &&
    /启动中/.test(error.message)
  )
}

function sleep(ms: number): Promise<void> {
  return new Promise(resolve => setTimeout(resolve, ms))
}

/** Find the audible span of a mono/first channel, keeping 8ms of pad so
 *  concatenated clips do not click. Used to strip SAPI/GPT-SoVITS
 *  leading and trailing silence that otherwise becomes a pause between
 *  sentences. */
export function speechAudioBounds(channel: Float32Array, sampleRate: number): { start: number; length: number } {
  const threshold = 0.012
  const pad = Math.max(1, Math.floor(sampleRate * 0.008))
  let start = 0
  let end = channel.length - 1
  while (start < channel.length && Math.abs(channel[start]) < threshold) start++
  while (end > start && Math.abs(channel[end]) < threshold) end--
  if (end - start < sampleRate * 0.04) return { start: 0, length: channel.length }
  start = Math.max(0, start - pad)
  end = Math.min(channel.length - 1, end + pad)
  return { start, length: end - start + 1 }
}

function trimSilence(ctx: AudioContext, buffer: AudioBuffer): AudioBuffer | null {
  try {
    const channel = buffer.getChannelData(0)
    const { start, length } = speechAudioBounds(channel, buffer.sampleRate)
    if (start === 0 && length === buffer.length) return buffer
    const trimmed = ctx.createBuffer(buffer.numberOfChannels, length, buffer.sampleRate)
    for (let c = 0; c < buffer.numberOfChannels; c++) {
      trimmed.getChannelData(c).set(buffer.getChannelData(c).subarray(start, start + length))
    }
    return trimmed
  } catch {
    return null
  }
}
