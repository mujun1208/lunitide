export type MediaCenterPlay = { url: string; kind: 'audio' | 'video'; title: string; rate?: number }

export const MEDIA_CENTER_PLAY_EVENT = 'lunitide:media-center-play'

const MEDIA_FILE = /\.(mp4|webm|m4v|mp3|m4a|aac|flac|wav|ogg|oga)$/i

let activeBlob = ''

function privateHost(host: string): boolean {
  const name = host.toLowerCase()
  if (name === 'localhost' || name.endsWith('.localhost') || name.endsWith('.local')) return true
  if (name === '::1' || name.startsWith('fe80:') || name.startsWith('fc') || name.startsWith('fd')) return true
  const parts = /^(\d{1,3})\.(\d{1,3})\.(\d{1,3})\.(\d{1,3})$/.exec(name)
  if (!parts) return false
  const nums = parts.slice(1).map(Number)
  if (nums.some(part => part > 255)) return true
  const [a, b] = nums
  if (a === 0 || a === 10 || a === 127) return true
  if (a === 169 && b === 254) return true
  if (a === 192 && b === 168) return true
  return a === 172 && b >= 16 && b <= 31
}

export function pageCanPlay(url: string): boolean {
  if (url.startsWith('blob:')) return true
  let parsed: URL
  try {
    parsed = new URL(url)
  } catch {
    return false
  }
  if (parsed.protocol !== 'https:' || parsed.username || parsed.password) return false
  if (parsed.hostname.toLowerCase() === 'media.lunitide.local') return true
  if (privateHost(parsed.hostname)) return false
  return MEDIA_FILE.test(decodeURIComponent(parsed.pathname))
}

export function parseMediaCenterPlay(summary: string): MediaCenterPlay | null {
  if (!summary.includes('MEDIA_CENTER')) return null
  const url = summary.match(/^url:\s*(https:\/\/\S+)/m)?.[1]
  if (!url || !pageCanPlay(url)) return null
  const kindLine = summary.match(/^kind:\s*(audio|video)\b/m)?.[1]
  const title = summary.match(/^title:\s*(.+)$/m)?.[1]?.trim() || '媒体中心'
  const kind = kindLine === 'audio' || kindLine === 'video'
    ? kindLine
    : /\.(mp3|wav|flac|m4a|aac|ogg|oga)$/i.test(url) ? 'audio' : 'video'
  return { url, kind, title }
}

function publish(play: MediaCenterPlay): void {
  if (activeBlob && activeBlob !== play.url) {
    URL.revokeObjectURL(activeBlob)
    activeBlob = ''
  }
  if (play.url.startsWith('blob:')) activeBlob = play.url
  window.dispatchEvent(new CustomEvent<MediaCenterPlay>(MEDIA_CENTER_PLAY_EVENT, { detail: play }))
}

export function mediaCenterOwnsUrl(url: string): boolean {
  return url !== '' && url === activeBlob
}

export function releaseMediaCenterPlay(): void {
  if (!activeBlob) return
  const url = activeBlob
  activeBlob = ''
  URL.revokeObjectURL(url)
}

export function handoffPageMedia(play: MediaCenterPlay): boolean {
  if (!pageCanPlay(play.url) || typeof window === 'undefined') return false
  publish(play)
  return true
}

export function noteMediaCenterPlay(summary?: string): void {
  const play = parseMediaCenterPlay(summary ?? '')
  if (!play || typeof window === 'undefined') return
  publish(play)
}

export function pageMediaFromElement(node: HTMLMediaElement): MediaCenterPlay | null {
  if (node.closest('.media-center, .owned-media-player, .chat-audio-player')) return null
  const url = node.currentSrc || node.src
  if (!pageCanPlay(url)) return null
  const title = node.getAttribute('aria-label') || node.getAttribute('title') || '网页媒体'
  return { url, kind: node instanceof HTMLVideoElement ? 'video' : 'audio', title }
}
