// Microphone capture for recognizers that want the audio itself.
//
// The Web Speech path never needed this: Windows captured, recognized and
// returned text without the renderer ever seeing a sample. A local engine or a
// speech-to-speech model needs the samples, and the renderer is the only place
// that can get them — the Content-Security-Policy sets connect-src to 'none',
// so audio cannot leave here except through the bridge.
//
// What comes out is a steady 100ms of 16 kHz mono, already base64 for the
// bridge's JSON. What goes in is whatever the device and the browser agreed on.
import { microphoneConstraints, saveMicrophoneId, selectedMicrophoneId } from '../../settings/microphone'
import { BridgeClientError } from '../../bridge/client'
import workletUrl from './pcmWorklet.js?url'
import {
  FRAME_SAMPLES,
  TARGET_SAMPLE_RATE,
  createFrameAccumulator,
  floatToInt16,
  framePeak,
  int16ToBase64,
  resampleTo16k,
} from './pcmFrames'

export interface PcmCaptureOptions {
  /** One frame of 16 kHz mono audio, base64 for the bridge. */
  onFrame: (frame: { base64: string; samples: Int16Array; peak: number }) => void
  /** Capture died after it had started — the device was unplugged or seized. */
  onError?: (error: BridgeClientError) => void
  /**
   * Extra this-PC streams mixed into the same frames. Meeting notes uses
   * Chromium's WASAPI loopback here. Tracks are stopped on teardown.
   */
  extraStreams?: MediaStream[]
}

export interface PcmCaptureHandle {
  /** Release the device and tear down the graph. Safe to call twice. */
  stop: () => Promise<void>
  /**
   * Stop delivering frames without releasing the device. Reacquiring a
   * microphone costs hundreds of milliseconds and can fail outright if
   * something else grabbed it in between, so a pause holds the device.
   */
  setMuted: (muted: boolean) => void
  /** What the AudioContext actually gave us, which may not be 16 kHz. */
  contextSampleRate: () => number
  /** Emit the partial frame held back by the accumulator. End of turn only. */
  flush: () => void
}

/** The processor name registered inside pcmWorklet.js. */
const PROCESSOR_NAME = 'pcm-capture'

/**
 * Ask for one channel at the target rate. Both are hints: a device that cannot
 * do either is resampled and downmixed by the browser, and a device that can
 * saves us doing it. `resampleTo16k` covers the case where the hint is refused.
 */
function captureConstraints(): MediaStreamConstraints {
  const base = microphoneConstraints()
  const audio = base.audio
  if (audio === false) return base
  const shaping = {
    channelCount: 1 as const,
    sampleRate: TARGET_SAMPLE_RATE,
    // Echo cancellation stays on for the same reason the Web Speech path
    // keeps it: the companion's own voice is coming out of the speakers a few
    // centimetres away, and without it every reply is transcribed back.
    echoCancellation: true as const,
    noiseSuppression: false as const,
    autoGainControl: true as const,
  }
  return { audio: audio === true || audio == null ? shaping : { ...audio, ...shaping } }
}

function captureFailure(error: unknown): BridgeClientError {
  const name = error instanceof DOMException ? error.name : ''
  if (name === 'NotAllowedError' || name === 'SecurityError') {
    return new BridgeClientError('麦克风权限被拒绝，请允许桌面应用访问麦克风', 'MICROPHONE_PERMISSION_DENIED', false, 'renderer')
  }
  if (name === 'NotFoundError' || name === 'DevicesNotFoundError' || name === 'OverconstrainedError') {
    return new BridgeClientError('未检测到可用麦克风，请在“设置 → 语音与麦克风”中检查设备', 'MICROPHONE_DEVICE_NOT_FOUND', false, 'renderer')
  }
  if (name === 'NotReadableError' || name === 'TrackStartError') {
    return new BridgeClientError('麦克风被其他应用占用或驱动无法启动', 'MICROPHONE_DEVICE_BUSY', false, 'renderer')
  }
  return new BridgeClientError('无法启动麦克风，请检查设备设置', 'MICROPHONE_START_FAILED', false, 'renderer')
}

export async function startPcmCapture(options: PcmCaptureOptions): Promise<PcmCaptureHandle> {
  const AudioContextClass =
    window.AudioContext ?? (window as typeof window & { webkitAudioContext?: typeof AudioContext }).webkitAudioContext
  if (!AudioContextClass || !navigator.mediaDevices?.getUserMedia) {
    throw new BridgeClientError('当前系统 WebView 不支持音频采集', 'SPEECH_RECOGNITION_UNAVAILABLE', false, 'renderer')
  }

  let stream: MediaStream | undefined
  try {
    stream = await navigator.mediaDevices.getUserMedia(captureConstraints())
  } catch (error) {
    const name = error instanceof DOMException ? error.name : ''
    // A saved device that has since been unplugged should not disable voice
    // outright; forget it and take whatever the system offers now.
    if (selectedMicrophoneId() && (name === 'NotFoundError' || name === 'DevicesNotFoundError' || name === 'OverconstrainedError')) {
      saveMicrophoneId('')
      try {
        stream = await navigator.mediaDevices.getUserMedia({ audio: { channelCount: 1, echoCancellation: true } })
      } catch (retry) {
        throw captureFailure(retry)
      }
    } else {
      throw captureFailure(error)
    }
  }

  const deviceId = stream.getAudioTracks()[0]?.getSettings()?.deviceId
  if (deviceId) saveMicrophoneId(deviceId)

  // A context of its own, at the recognizer's rate. The shared playback
  // context runs at the device rate so synthesized speech is not degraded on
  // the way out; forcing it to 16 kHz to save an object here would be heard.
  const context = new AudioContextClass({ sampleRate: TARGET_SAMPLE_RATE })
  let stopped = false
  let muted = false
  const accumulator = createFrameAccumulator(FRAME_SAMPLES)

  const extraStreams = (options.extraStreams ?? []).filter(item => item.getAudioTracks().length > 0)

  const teardown = async () => {
    stream?.getTracks().forEach(track => track.stop())
    stream = undefined
    extraStreams.forEach(item => item.getTracks().forEach(track => track.stop()))
    accumulator.reset()
    // close() rejects on a context already closed by a prior stop.
    await context.close().catch(() => undefined)
  }

  try {
    if (context.state === 'suspended') await context.resume()
    await context.audioWorklet.addModule(workletUrl)
  } catch (error) {
    await teardown()
    throw error instanceof BridgeClientError
      ? error
      : new BridgeClientError('音频采集模块加载失败', 'MICROPHONE_START_FAILED', false, 'renderer')
  }

  const source = context.createMediaStreamSource(stream)
  const extraSources = extraStreams.map(item => context.createMediaStreamSource(item))
  const worklet = new AudioWorkletNode(context, PROCESSOR_NAME)
  // Zero gain, not a missing connection: a node with no path to the
  // destination is not pulled by the graph at all, so it would never run.
  const sink = context.createGain()
  sink.gain.value = 0

  const emit = (samples: Float32Array) => {
    const pcm = floatToInt16(samples)
    options.onFrame({ base64: int16ToBase64(pcm), samples: pcm, peak: framePeak(samples) })
  }

  worklet.port.onmessage = event => {
    if (stopped || muted) return
    const block = event.data as Float32Array
    if (!(block instanceof Float32Array)) return
    // The sampleRate hint is honoured on every device seen so far, which makes
    // this a no-op copy; it is here for the one that refuses.
    for (const frame of accumulator.push(resampleTo16k(block, context.sampleRate))) emit(frame)
  }

  source.connect(worklet)
  extraSources.forEach(item => item.connect(worklet))
  worklet.connect(sink)
  sink.connect(context.destination)

  // Losing the device mid-turn is not an error anyone catches — the promise
  // that started capture resolved long ago — so it is reported through onError.
  stream.getAudioTracks().forEach(track => {
    track.onended = () => {
      if (stopped) return
      options.onError?.(new BridgeClientError('麦克风已断开', 'MICROPHONE_DEVICE_BUSY', false, 'renderer'))
    }
  })

  return {
    stop: async () => {
      if (stopped) return
      stopped = true
      worklet.port.onmessage = null
      source.disconnect()
      extraSources.forEach(item => item.disconnect())
      worklet.disconnect()
      sink.disconnect()
      await teardown()
    },
    setMuted: (next: boolean) => {
      muted = next
      // Whatever was mid-frame belongs to the audio before the pause; keeping
      // it would splice the two sides of the pause into one word.
      if (next) accumulator.reset()
    },
    contextSampleRate: () => context.sampleRate,
    flush: () => {
      if (stopped || muted) return
      const tail = accumulator.flush()
      if (tail) emit(tail)
    },
  }
}
