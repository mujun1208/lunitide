/** Loopback polls contain consecutive audio, not simultaneous audio tracks.
 * Preserve their order and consume exactly one microphone frame at a time. */
export class MeetingLoopbackQueue {
  private samples = new Int16Array(0)
  clear(): void { this.samples = new Int16Array(0) }
  append(next: Int16Array): void {
    const joined = new Int16Array(this.samples.length + next.length)
    joined.set(this.samples)
    joined.set(next, this.samples.length)
    // Same eight-second live buffer as the native capture; the durable audio
    // path has its own independent buffer in the engine.
    this.samples = joined.slice(-16000 * 8)
  }
  take(count: number): Int16Array {
    const next = this.samples.slice(0, count)
    this.samples = this.samples.slice(next.length)
    return next
  }
}
