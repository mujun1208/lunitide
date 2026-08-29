const SYSTEM_AUDIO_HINT = '当前环境无法收录本机系统声音。只会转写麦克风。请在窗口选择器里点整个屏幕（不要点飞书窗口）并勾选共享音频，以便收录对面说话；取消则不会开这场会。这不会把桌面共享给其他电脑。'

export const NO_SYSTEM_AUDIO_NOTICE = '未获得系统声音（请选择整个屏幕并勾选共享音频，或启用立体声混音）。已仅转写麦克风，仍会继续尝试收录会议播放。'

const LOOPBACK_INPUT = /stereo mix|立体声混音|what u hear|loopback|wave out mix|立体声 混音/i

export type CaptureThisPcSystemAudioOptions = {
  getDisplayMedia?: MediaDevices['getDisplayMedia']
  getUserMedia?: MediaDevices['getUserMedia']
  enumerateDevices?: MediaDevices['enumerateDevices']
  /** When false, only try a loopback recording device — no picker. */
  interactive?: boolean
}

export function isCaptureCanceled(error: unknown): boolean {
  const name = error instanceof DOMException ? error.name : ''
  if (name === 'AbortError' || name === 'NotAllowedError') return true
  return error instanceof Error && /取消/.test(error.message)
}

export function isLoopbackInputLabel(label: string): boolean {
  return LOOPBACK_INPUT.test(label)
}

export function liveAudioTracks(stream: MediaStream | undefined): MediaStreamTrack[] {
  return (stream?.getAudioTracks() ?? []).filter(track => track.readyState !== 'ended')
}

export function hasAudioTrack(stream: MediaStream | undefined): boolean {
  return liveAudioTracks(stream).length > 0
}

export function hasLiveAudioTrack(stream: MediaStream | undefined): boolean {
  return liveAudioTracks(stream).length > 0
}

export function stopMediaStream(stream: MediaStream | undefined): void {
  stream?.getTracks().forEach(track => track.stop())
}

/** Prefer entire-screen WASAPI loopback. Feishu/Electron window share usually has no audio. */
export function displayMediaConstraints(): DisplayMediaStreamOptions {
  return {
    video: {
      displaySurface: 'monitor',
      width: 16,
      height: 16,
      frameRate: 1,
    },
    audio: {
      echoCancellation: false,
      noiseSuppression: false,
      autoGainControl: false,
    },
    systemAudio: 'include',
    selfBrowserSurface: 'exclude',
    monitorTypeSurfaces: 'include',
    preferCurrentTab: false,
  } as DisplayMediaStreamOptions
}

export function fallbackDisplayMediaConstraints(): DisplayMediaStreamOptions {
  return { video: true, audio: true, systemAudio: 'include' } as DisplayMediaStreamOptions
}

export async function captureLoopbackInputDevice(options: CaptureThisPcSystemAudioOptions = {}): Promise<MediaStream | undefined> {
  const enumerate = options.enumerateDevices ?? navigator.mediaDevices?.enumerateDevices?.bind(navigator.mediaDevices)
  const getUserMedia = options.getUserMedia ?? navigator.mediaDevices?.getUserMedia?.bind(navigator.mediaDevices)
  if (!enumerate || !getUserMedia) return undefined
  let devices: MediaDeviceInfo[]
  try {
    devices = await enumerate()
  } catch {
    return undefined
  }
  const match = devices.find(device => device.kind === 'audioinput' && isLoopbackInputLabel(device.label))
  if (!match?.deviceId) return undefined
  try {
    const stream = await getUserMedia({
      audio: {
        deviceId: { exact: match.deviceId },
        echoCancellation: false,
        noiseSuppression: false,
        autoGainControl: false,
      },
    })
    if (hasLiveAudioTrack(stream)) return stream
    stopMediaStream(stream)
  } catch {
    /* Stereo Mix is often present but disabled */
  }
  return undefined
}

async function pickDisplayStream(
  getDisplayMedia: MediaDevices['getDisplayMedia'],
  constraints: DisplayMediaStreamOptions,
): Promise<MediaStream> {
  return getDisplayMedia.call(navigator.mediaDevices ?? {}, constraints)
}

/** Chromium WASAPI loopback on this PC: loopback device first, then entire-screen share. */
export async function captureThisPcSystemAudio(options: CaptureThisPcSystemAudioOptions = {}): Promise<MediaStream> {
  const fromDevice = await captureLoopbackInputDevice(options)
  if (fromDevice) return fromDevice
  if (options.interactive === false) {
    throw new Error(NO_SYSTEM_AUDIO_NOTICE)
  }
  const native = navigator.mediaDevices?.getDisplayMedia
  const getDisplayMedia = options.getDisplayMedia ?? (native ? native.bind(navigator.mediaDevices) : undefined)
  if (!getDisplayMedia) throw new Error(SYSTEM_AUDIO_HINT)
  const attempts = [displayMediaConstraints(), fallbackDisplayMediaConstraints()]
  let lastEmpty: MediaStream | undefined
  for (const constraints of attempts) {
    try {
      const stream = await pickDisplayStream(getDisplayMedia, constraints)
      if (hasLiveAudioTrack(stream)) return stream
      stopMediaStream(lastEmpty)
      lastEmpty = stream
    } catch (error) {
      if (isCaptureCanceled(error)) throw error
    }
  }
  stopMediaStream(lastEmpty)
  throw new Error(NO_SYSTEM_AUDIO_NOTICE)
}

export function watchAudioTrackEnded(stream: MediaStream, onEnded: () => void): () => void {
  const tracks = stream.getAudioTracks()
  const handler = () => onEnded()
  tracks.forEach(track => {
    track.addEventListener('ended', handler)
  })
  return () => {
    tracks.forEach(track => track.removeEventListener('ended', handler))
  }
}

export { SYSTEM_AUDIO_HINT }
