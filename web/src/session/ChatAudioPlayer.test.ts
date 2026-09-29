import { expect, it, vi } from 'vitest'
import { audioMimeFromPath, audioObjectUrlFromPreview } from './ChatAudioPlayer'

it('turns preview bytes into a playable blob URL', () => {
  const create = vi.fn(() => 'blob:clip')
  vi.stubGlobal('URL', { ...URL, createObjectURL: create, revokeObjectURL: vi.fn() })
  expect(audioMimeFromPath('song.wav')).toBe('audio/wav')
  expect(audioMimeFromPath('clip.mp3')).toBe('audio/mpeg')
  expect(audioObjectUrlFromPreview(btoa('RIFF____WAVE'), 'song.wav')).toBe('blob:clip')
  expect(create).toHaveBeenCalled()
  expect(audioObjectUrlFromPreview('', 'song.wav')).toBe('')
  vi.unstubAllGlobals()
})
