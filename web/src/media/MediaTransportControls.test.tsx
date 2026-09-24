import { cleanup, fireEvent, render, screen } from '@testing-library/react'
import { afterEach, expect, it, vi } from 'vitest'
import { levelFromY, MediaTransportControls } from './MediaTransportControls'

afterEach(cleanup)

function renderControls(onSeek = vi.fn(), durationMs = 2000) {
  render(
    <MediaTransportControls
      zh
      busy={false}
      playing
      positionMs={0}
      durationMs={durationMs}
      volume={100}
      onPlayPause={() => {}}
      onPrevious={() => {}}
      onNext={() => {}}
      onQueue={() => {}}
      onSeek={onSeek}
      onVolume={() => {}}
    />,
  )
  return onSeek
}

it('seeks when the progress range is clicked or dragged', () => {
  const onSeek = renderControls()
  const slider = screen.getByRole('slider', { name: '进度' })
  fireEvent.change(slider, { target: { value: '400' } })
  fireEvent.change(slider, { target: { value: '800' } })
  expect(onSeek).toHaveBeenNthCalledWith(1, 400)
  expect(onSeek).toHaveBeenNthCalledWith(2, 800)
})

it('opens the volume track from its button, then drag and click set the level', () => {
  const onVolume = vi.fn()
  render(
    <MediaTransportControls
      zh
      busy={false}
      playing
      positionMs={0}
      durationMs={2000}
      volume={100}
      onPlayPause={() => {}}
      onPrevious={() => {}}
      onNext={() => {}}
      onQueue={() => {}}
      onSeek={() => {}}
      onVolume={onVolume}
    />,
  )
  expect(screen.queryByRole('slider', { name: '音量' })).toBeNull()
  fireEvent.click(screen.getByRole('button', { name: '音量' }))
  const track = screen.getByRole('slider', { name: '音量' })
  expect(track).toHaveAttribute('aria-orientation', 'vertical')
  const rect = { x: 0, y: 0, top: 0, left: 0, right: 20, bottom: 100, width: 20, height: 100, toJSON() { return {} } }
  vi.spyOn(track, 'getBoundingClientRect').mockReturnValue(rect as DOMRect)
  expect(levelFromY(30, track)).toBe(70)
  expect(levelFromY(80, track)).toBe(20)
  track.dispatchEvent(new MouseEvent('pointerdown', { bubbles: true, cancelable: true, clientY: 30 }))
  track.dispatchEvent(new MouseEvent('pointermove', { bubbles: true, cancelable: true, clientY: 80, buttons: 1 }))
  expect(onVolume).toHaveBeenNthCalledWith(1, 70)
  expect(onVolume).toHaveBeenNthCalledWith(2, 20)
})
