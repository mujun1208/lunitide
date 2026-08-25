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
import type { CompanionEngine, CompanionSettings } from './companionSettings'
import { companionEngineProbeOrder } from './companionSettings'

export interface TtsPlayerCallbacks {
  onSegmentStart?: (index: number, total: number) => void
  onGain?: (gain: number) => void
  onFinished?: (reason: 'completed' | 'interrupted' | 'circuit-broken' | 'engine-unavailable') => void
  /** M95-001: the machine has no SAPI engine — degrade to subtitles. */
  onEngineUnavailable?: () => void
  /** Primary engine failed — playback continued on a fallback engine. */
  onEngineFallback?: (engine: CompanionEngine) => void
  onSegmentFailed?: (index: number, consecutiveFailures: number) => void
}

let sharedAudioContext: AudioContext | null = null

/**
 * Hard cap of the tts.synthesize bridge schema (tts.MaxSegmentChars = 500).
 * The renderer segments for subtitles at a much larger size, so a long
 * reply used to reach the engine as one over-long request, fail schema
 * validation, and trip the 3-failure circuit breaker — a whole answer
 * lost. The player owns the engine contract, so it splits here.
 */
export const ENGINE_MAX_CHARS = 480

/**
 * How much of the sounding clip must remain before the tail is handed to the
 * engine. Synthesis is a network round trip of a few hundred milliseconds, so
 * a lead shorter than that guarantees the speaker runs dry waiting for it.
 */
const SYNTH_LEAD_SECONDS = 1.5

/** Split on clause boundaries so an over-long reply still sounds natural. */
export function splitForEngine(text: string): string[] {
  if (Array.from(text).length <= ENGINE_MAX_CHARS) return text.trim() ? [text] : []
  const out: string[] = []
  let current = ''
  const push = (value: string) => {
    if (value.trim()) out.push(value)
  }
  for (const clause of text.split(/(?<=[。？！!?；;，,\n])/)) {
    if (Array.from(clause).length > ENGINE_MAX_CHARS) {
      push(current)
      current = ''
      const runes = Array.from(clause)
      for (let i = 0; i < runes.length; i += ENGINE_MAX_CHARS) {
        push(runes.slice(i, i + ENGINE_MAX_CHARS).join(''))
      }
      continue
    }
    if (Array.from(current + clause).length > ENGINE_MAX_CHARS && current) {
      push(current)
      current = clause
    } else {
      current += clause
    }
  }
  push(current)
  return out
}

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
  /**
   * A synthesis request for the streaming queue is in flight. Tracked
   * separately from queueProcessing, which stays true for the whole reply:
   * text arriving while the engine is busy should join the next clip, but
   * text arriving while a clip is merely *playing* must not wait.
   */
  private inFlightSynths = 0
  private queueGeneration = 0
  private nextEnqueueIndex = 0
  private currentVoiceId = ''
  private currentRate = 0
  private currentVolume = 80
  private currentExtras: SynthExtras = { engine: 'natural', refEndpoint: '' }
  private activeQueueCallbacks: TtsPlayerCallbacks | null = null
  /** Streaming text not yet sent to the engine — later sentences join this
   *  so a turn is one (or two) clips, not one synth per period. */
  private holdTail = ''
  private lastEnqueueAt = 0

  /** True while a clip is synthesizing or playing — used to pause mic commit. */
  isBusy(): boolean {
    return this.queueProcessing || this.pendingSegments.length > 0 || this.holdTail.length > 0 || this.activeSources.size > 0
  }

  /** Join streaming sentences into one synth instead of one Edge request per period. */
  private maybePromoteTail(force = false): void {
    if (!this.holdTail.trim()) return
    const last = this.pendingSegments[this.pendingSegments.length - 1]
    if (last && !this.prefetchQueue.some(q => q.index === last.index)) {
      // Merging is what keeps a reply one continuous reading, but it must
      // not grow a segment past what the engine will accept.
      if (Array.from(last.text + this.holdTail).length <= ENGINE_MAX_CHARS) {
        last.text += this.holdTail
        this.holdTail = ''
        return
      }
    }
    if (!force) {
      // Exactly one clip in preparation at a time. Allowing more chops a
      // paragraph into separate readings; allowing none starves the timeline,
      // which is what used to happen — playback alone held the tail back, so
      // nothing was synthesized until the model stopped writing and the whole
      // wait for the engine landed as silence after her first sentence.
      if (this.inFlightSynths > 0 || this.prefetchQueue.length > 0 || this.pendingSegments.length > 0) return
      const remaining =
        this.ctx && this.timelineEnd > 0 ? Math.max(0, this.timelineEnd - this.ctx.currentTime) : 0
      const stalled = performance.now() - this.lastEnqueueAt >= 400
      // Hand the tail over while the sounding clip still has enough left to
      // cover the round trip. Everything arriving before that moment merges
      // into it, so waiting longer buys continuity — but waiting past this
      // point buys a gap instead.
      if (!stalled && remaining > SYNTH_LEAD_SECONDS) return
    }
    for (const part of splitForEngine(this.holdTail)) {
      this.pendingSegments.push({ text: part, index: this.nextEnqueueIndex++ })
    }
    this.holdTail = ''
    this.fillPrefetch(this.queueProcessing ? this.queueGeneration : this.queueGeneration + 1)
  }

  /**
   * Counts the request in flight so the tail merges into it instead of
   * queueing a second one behind it. Every path that reaches the engine —
   * the queue, its prefetches, and speak() — goes through here, which is why
   * the count lives at this level rather than at each call site.
   */
  private async synthesizeReadySegment(text: string, callbacks: TtsPlayerCallbacks): Promise<ReadySegment | null> {
    this.inFlightSynths++
    try {
      return await this.requestSegment(text, callbacks)
    } finally {
      this.inFlightSynths--
    }
  }

  private async requestSegment(text: string, callbacks: TtsPlayerCallbacks): Promise<ReadySegment | null> {
    const bridge = getTtsBridge()
    const engines = companionEngineProbeOrder(this.currentExtras.engine)
    let lastError: unknown
    for (let attempt = 0; attempt < engines.length; attempt++) {
      const engine = engines[attempt]!
      const voiceId = attempt === 0 ? this.currentVoiceId : ''
      try {
        const result = await bridge.synthesize(
          buildSynthPayload(text, voiceId, this.currentRate, this.currentVolume, {
            engine,
            refEndpoint: engine === 'ref' ? this.currentExtras.refEndpoint : '',
          }),
        )
        if (result.discarded || !result.wav_base64) continue
        if (engine !== this.currentExtras.engine) {
          this.currentExtras = {
            engine,
            refEndpoint: engine === 'ref' ? this.currentExtras.refEndpoint : '',
          }
          if (attempt > 0) this.currentVoiceId = voiceId
          callbacks.onEngineFallback?.(engine)
        }
        return { wavBase64: result.wav_base64, buffer: await this.decodeWav(result.wav_base64) }
      } catch (error) {
        if (isRefEngineStarting(error)) throw error
        lastError = error
      }
    }
    if (lastError) throw lastError
    return null
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
    const overlap = this.timelineEnd > ctx.currentTime + 0.08 ? 0.14 : 0
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
      let lastAudioTime = this.ctx?.currentTime ?? 0
      let frozenChecks = 0
      const check = () => {
        const stale =
          queueGeneration !== undefined
            ? queueGeneration !== this.queueGeneration
            : generation !== this.generation
        if (stale) return resolve()
        this.maybePromoteTail()
        if (this.pendingSegments.length > 0) return resolve()
        if (this.holdTail.trim()) {
          setTimeout(check, 20)
          return
        }
        if (!this.ctx || !this.timelineEnd || this.ctx.currentTime >= this.timelineEnd - 0.03) return resolve()
        if (this.ctx.state !== 'running') return resolve()
        const audioTime = this.ctx.currentTime
        if (audioTime <= lastAudioTime + 1e-4) frozenChecks++
        else frozenChecks = 0
        lastAudioTime = audioTime
        // Suspended/frozen clocks never advance: do not hold the queue.
        if (frozenChecks >= 8) return resolve()
        setTimeout(check, 20)
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
      let watchdog = 0
      const finish = (ok: boolean) => {
        if (settled) return
        settled = true
        window.clearTimeout(watchdog)
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
      watchdog = window.setTimeout(() => finish(false), 12_000)
      void (async () => {
        try {
          await audio.play()
        } catch {
          void unlockTtsAudio()
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
    void unlockTtsAudio()
    const ctx = this.ensureGraph()
    if (ctx?.state === 'suspended') void ctx.resume().catch(() => {})
    if (seg.buffer && ctx?.state === 'running' && this.scheduleBuffer(seg.buffer, callbacks)) return true
    try {
      return await this.playSegmentFallback(seg.wavBase64, generation, callbacks)
    } catch {
      return false
    }
  }

  /** Serially speak the segments; resolves through onFinished. */
  async speak(rawSegments: string[], settings: CompanionSettings, callbacks: TtsPlayerCallbacks): Promise<void> {
    const segments = rawSegments.flatMap(splitForEngine)
    if (!segments.length) {
      callbacks.onFinished?.('completed')
      return
    }
    unlockTtsAudio()
    this.interruptLocal()
    const generation = ++this.generation
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
          seg = await this.synthesizeReadySegment(segments[index], callbacks)
          if (generation !== this.generation) return
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
      this.schedulePrefetch(generation, segments[index + 1] ?? '', index + 1, callbacks)
      this.schedulePrefetch(generation, segments[index + 2] ?? '', index + 2, callbacks)
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

  private schedulePrefetch(generation: number, text: string, index: number, callbacks: TtsPlayerCallbacks): void {
    if (!text || index < 0) return
    if (this.prefetchQueue.some(q => q.index === index)) return
    if (this.prefetchQueue.length >= 2) return
    const prepare = async (): Promise<ReadySegment | null> => {
      try {
        return await this.synthesizeReadySegment(text, callbacks)
      } catch {
        return null
      }
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
    const text = segments.filter(s => s.trim()).join('')
    if (!text) return
    unlockTtsAudio()
    this.configure(settings.voiceId || '', settings.rate, settings.volume, settings)
    this.activeQueueCallbacks = callbacks
    this.lastEnqueueAt = performance.now()
    this.holdTail += text
    this.maybePromoteTail()
    if (!this.queueProcessing) {
      this.maybePromoteTail(true)
      void this.processQueue(callbacks)
    }
  }

  /** P0-1: Flush remaining queue and resolve when all queued segments are done. */
  async flush(callbacks: TtsPlayerCallbacks): Promise<void> {
    this.maybePromoteTail(true)
    if (!this.queueProcessing && this.pendingSegments.length > 0) {
      void this.processQueue(callbacks)
    }
    return new Promise(resolve => {
      const finish = () => {
        callbacks.onFinished?.('completed')
        resolve()
      }
      const check = () => {
        if (!this.isBusy()) finish()
        else setTimeout(check, 40)
      }
      check()
    })
  }

  private async processQueue(callbacks: TtsPlayerCallbacks): Promise<void> {
    this.queueProcessing = true
    this.activeQueueCallbacks = callbacks
    const gen = ++this.queueGeneration
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
            seg = await this.synthesizeReadySegment(text, callbacks)
            if (gen !== this.queueGeneration) return
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
      this.maybePromoteTail()
      if (this.pendingSegments.length > 0) continue
      if (this.holdTail.trim()) {
        await this.waitForTimeline(this.generation, gen)
        if (gen !== this.queueGeneration) return
        this.maybePromoteTail()
        if (this.pendingSegments.length > 0) continue
      }
      this.queueProcessing = false
      if (this.pendingSegments.length > 0 || this.holdTail.trim()) {
        void this.processQueue(callbacks)
        return
      }
      callbacks.onFinished?.('completed')
      return
    }
  }

  /** Prefetch up to 3 pending segments so synthesis overlaps playback. */
  private fillPrefetch(generation: number): void {
    const callbacks = this.activeQueueCallbacks
    if (!callbacks) return
    for (const item of this.pendingSegments) {
      if (this.prefetchQueue.length >= 1) return
      if (this.prefetchQueue.some(q => q.index === item.index)) continue
      const text = item.text
      const prepare = async (): Promise<ReadySegment | null> => {
        try {
          return await this.synthesizeReadySegment(text, callbacks)
        } catch {
          return null
        }
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
    this.holdTail = ''
    this.lastEnqueueAt = 0
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
    if (sharedAudioContext.state === 'suspended') {
      // Never await resume(): without a user gesture it can hang forever,
      // which used to freeze the TTS queue (subtitles moving, no voice).
      void sharedAudioContext.resume().catch(() => {})
    }
  } catch {
    sharedAudioContext = null
  }
  return Promise.resolve()
}

/** The Web Audio graph unlocked by a user gesture — reuse it for the mic meter. */
export function sharedTtsAudioContext(): AudioContext | null {
  try {
    if (!sharedAudioContext || sharedAudioContext.state === 'closed') return null
    return sharedAudioContext
  } catch {
    return null
  }
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

/** Find the audible span of a mono/first channel, keeping 18ms of pad so
 *  concatenated clips do not click. Used to strip SAPI/GPT-SoVITS
 *  leading and trailing silence that otherwise becomes a pause between
 *  sentences. */
export function speechAudioBounds(channel: Float32Array, sampleRate: number): { start: number; length: number } {
  const threshold = 0.012
  const pad = Math.max(1, Math.floor(sampleRate * 0.018))
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
