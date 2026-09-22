import { expect, it, vi } from 'vitest'
import { handoffPageMedia, pageCanPlay, pageMediaFromElement, parseMediaCenterPlay } from './mediaCenterPlay'

it('reads the owned-player handoff and ignores ordinary media receipts', () => {
  expect(parseMediaCenterPlay('sent play to the active media app')).toBeNull()
  expect(parseMediaCenterPlay('已交给媒体中心播放。\nMEDIA_CENTER\nurl: https://archive.org/download/night/Night.mp4\nkind: video\ntitle: Night of the Living Dead\n')).toEqual({
    url: 'https://archive.org/download/night/Night.mp4',
    kind: 'video',
    title: 'Night of the Living Dead',
  })
  expect(parseMediaCenterPlay('MEDIA_CENTER\nurl: https://10.0.0.5/a.mp4\nkind: video\n')).toBeNull()
  expect(parseMediaCenterPlay('MEDIA_CENTER\nurl: http://archive.org/download/night/Night.mp4\nkind: video\n')).toBeNull()
})

it('accepts page-playable media and refuses private or non-media links', () => {
  expect(pageCanPlay('blob:https://app.local/clip')).toBe(true)
  expect(pageCanPlay('https://media.lunitide.local/v1/assets/ticket')).toBe(true)
  expect(pageCanPlay('https://archive.org/download/night/Night.mp4')).toBe(true)
  expect(pageCanPlay('https://archive.org/details/night')).toBe(false)
  expect(pageCanPlay('https://192.168.1.8/song.mp3')).toBe(false)
  expect(pageCanPlay('file:///C:/song.mp3')).toBe(false)
})

it('keeps in-chat speech in the chat player and takes a page clip', () => {
  const host = document.createElement('article')
  host.className = 'chat-audio-player'
  const speech = document.createElement('audio')
  speech.src = 'blob:speech'
  host.append(speech)
  document.body.append(host)
  expect(pageMediaFromElement(speech)).toBeNull()
  host.remove()

  const page = document.createElement('video')
  page.src = 'https://archive.org/download/night/Night.mp4'
  document.body.append(page)
  expect(pageMediaFromElement(page)?.kind).toBe('video')
  page.remove()
})

it('hands a page clip to the media center', () => {
  const seen: unknown[] = []
  const onPlay = (event: Event) => seen.push((event as CustomEvent).detail)
  window.addEventListener('lunitide:media-center-play', onPlay)
  expect(handoffPageMedia({ url: 'blob:clip', kind: 'audio', title: '朗读', rate: 1.25 })).toBe(true)
  expect(handoffPageMedia({ url: 'https://example.test/page', kind: 'audio', title: 'nope' })).toBe(false)
  window.removeEventListener('lunitide:media-center-play', onPlay)
  expect(seen).toEqual([{ url: 'blob:clip', kind: 'audio', title: '朗读', rate: 1.25 }])
  vi.unstubAllGlobals()
})
