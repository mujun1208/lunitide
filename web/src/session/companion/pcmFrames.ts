// Turning microphone audio into what a speech recognizer will accept.
//
// sherpa-onnx, like every recognizer this app is likely to meet, wants
// single-channel 16-bit samples at 16 kHz. A microphone gives none of those
// directly: WebAudio hands out 32-bit floats, at whatever rate the device and
// the AudioContext settled on, in blocks of 128 frames. These functions cover
// the gap, and they are separated from the capture graph so the arithmetic can
// be tested without a browser.

/** Every recognizer this feeds expects 16 kHz. */
export const TARGET_SAMPLE_RATE = 16000

/**
 * How much audio each message to the backend carries. Small enough that a
 * partial transcript can appear while the sentence is still being spoken,
 * large enough that the round trip is not paid per handful of samples.
 */
export const FRAME_MS = 100

/** Samples in one frame at the target rate. */
export const FRAME_SAMPLES = (TARGET_SAMPLE_RATE * FRAME_MS) / 1000

/**
 * Resample to 16 kHz by linear interpolation.
 *
 * Deliberately not a windowed-sinc filter. The input is speech from a
 * microphone that has already been low-passed by the capture chain, the ratio
 * is a downward one from 44.1 or 48 kHz, and the consumer is an acoustic model
 * rather than a listener. Interpolation costs a multiply per sample and the
 * recognizers score the same on its output; a better filter would cost real
 * main-thread time on every frame to defend against aliasing that the models
 * do not notice.
 *
 * Returns the input untouched when it already arrives at the target rate,
 * which is the normal case once the AudioContext honours its sampleRate hint.
 */
export function resampleTo16k(input: Float32Array, inputRate: number): Float32Array {
  if (!Number.isFinite(inputRate) || inputRate <= 0) return new Float32Array(0)
  if (inputRate === TARGET_SAMPLE_RATE || input.length === 0) return input
  const ratio = inputRate / TARGET_SAMPLE_RATE
  const outputLength = Math.floor(input.length / ratio)
  if (outputLength <= 0) return new Float32Array(0)
  const out = new Float32Array(outputLength)
  for (let i = 0; i < outputLength; i++) {
    const position = i * ratio
    const left = Math.floor(position)
    const right = Math.min(left + 1, input.length - 1)
    const weight = position - left
    out[i] = input[left]! * (1 - weight) + input[right]! * weight
  }
  return out
}

/**
 * Convert normalized floats to signed 16-bit samples.
 *
 * Clamping before scaling matters: WebAudio does not promise the [-1, 1] range
 * — automatic gain control and summed channels both overshoot it — and letting
 * a value past the bound wraps it to the opposite polarity, which a recognizer
 * hears as a click rather than as loudness.
 */
export function floatToInt16(input: Float32Array): Int16Array {
  const out = new Int16Array(input.length)
  for (let i = 0; i < input.length; i++) {
    const sample = Math.max(-1, Math.min(1, input[i]!))
    // Asymmetric on purpose: two's complement holds one more negative value
    // than positive, so the two directions scale by different maxima.
    out[i] = sample < 0 ? sample * 0x8000 : sample * 0x7fff
  }
  return out
}

/**
 * Collects however WebAudio chooses to deliver samples into frames of one
 * fixed size, because a recognizer wants a steady cadence and WebAudio
 * delivers 128 frames at a time at the context's rate.
 *
 * Whatever does not fill a frame is held for the next call rather than padded
 * with silence: padding would insert a gap into the middle of a word every
 * time the block size and the frame size failed to divide evenly.
 */
export function createFrameAccumulator(frameSamples = FRAME_SAMPLES) {
  let pending = new Float32Array(0)
  return {
    /** Append samples and return every whole frame they completed. */
    push(samples: Float32Array): Float32Array[] {
      if (samples.length > 0) {
        const merged = new Float32Array(pending.length + samples.length)
        merged.set(pending, 0)
        merged.set(samples, pending.length)
        pending = merged
      }
      const frames: Float32Array[] = []
      let offset = 0
      while (pending.length - offset >= frameSamples) {
        frames.push(pending.slice(offset, offset + frameSamples))
        offset += frameSamples
      }
      if (offset > 0) pending = pending.slice(offset)
      return frames
    },
    /**
     * The tail held back by push, zero-padded to a whole frame. For the end of
     * a turn only: the padding is silence the speaker did not utter, harmless
     * once they have stopped and misleading in the middle of a sentence.
     */
    flush(): Float32Array | undefined {
      if (pending.length === 0) return undefined
      const frame = new Float32Array(frameSamples)
      frame.set(pending.subarray(0, Math.min(pending.length, frameSamples)), 0)
      pending = new Float32Array(0)
      return frame
    },
    /** Samples held back, waiting for the rest of their frame. */
    pendingSamples(): number {
      return pending.length
    },
    reset(): void {
      pending = new Float32Array(0)
    },
  }
}

/**
 * Base64 for the bridge, which carries JSON and cannot hold raw bytes.
 *
 * Chunked rather than spread across one apply call: a frame is 3200 bytes
 * today, but the argument list is a fixed-size stack allocation and a caller
 * that raised the frame size would get a RangeError instead of a bigger frame.
 */
export function int16ToBase64(samples: Int16Array): string {
  const bytes = new Uint8Array(samples.buffer, samples.byteOffset, samples.byteLength)
  let binary = ''
  const chunk = 0x8000
  for (let i = 0; i < bytes.length; i += chunk) {
    binary += String.fromCharCode(...bytes.subarray(i, i + chunk))
  }
  return btoa(binary)
}

/** Peak amplitude of a frame, 0–1. Lets a caller meter without a second graph. */
export function framePeak(input: Float32Array): number {
  let peak = 0
  for (let i = 0; i < input.length; i++) {
    const magnitude = Math.abs(input[i]!)
    if (magnitude > peak) peak = magnitude
  }
  return Math.min(1, peak)
}
