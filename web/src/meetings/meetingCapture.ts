const SYSTEM_AUDIO_HINT = '当前环境无法收录本机系统声音。只会转写麦克风。请在窗口选择器里点腾讯会议、飞书或浏览器标签页以收录对面说话；取消则不会开这场会。这不会把桌面共享给其他电脑。'

export type CaptureThisPcSystemAudioOptions = {
  getDisplayMedia?: MediaDevices['getDisplayMedia']
}

export function isCaptureCanceled(error: unknown): boolean {
  const name = error instanceof DOMException ? error.name : ''
  if (name === 'AbortError' || name === 'NotAllowedError') return true
  return error instanceof Error && /取消/.test(error.message)
}

export function hasAudioTrack(stream: MediaStream | undefined): boolean {
  return (stream?.getAudioTracks().length ?? 0) > 0
}

export function stopMediaStream(stream: MediaStream | undefined): void {
  stream?.getTracks().forEach(track => track.stop())
}

/** Chromium WASAPI loopback on this PC via the display picker. Video is required by the picker and is not saved or shared. */
export async function captureThisPcSystemAudio(options: CaptureThisPcSystemAudioOptions = {}): Promise<MediaStream> {
  const native = navigator.mediaDevices?.getDisplayMedia
  const getDisplayMedia = options.getDisplayMedia ?? (native ? native.bind(navigator.mediaDevices) : undefined)
  if (!getDisplayMedia) throw new Error(SYSTEM_AUDIO_HINT)
  const constraints = {
    video: true,
    audio: true,
    systemAudio: 'include',
  } as DisplayMediaStreamOptions
  return getDisplayMedia.call(navigator.mediaDevices ?? {}, constraints)
}

export { SYSTEM_AUDIO_HINT }
