import { cleanup, fireEvent, render, screen } from '@testing-library/react'
import { afterEach, expect, it, vi } from 'vitest'
import { MediaTransportControls } from './MediaTransportControls'

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

it('does not send seek when the progress range updates without a pointer', () => {
  const onSeek = renderControls()
  fireEvent.change(screen.getByRole('slider', { name: '进度' }), { target: { value: '0' } })
  expect(onSeek).not.toHaveBeenCalled()
})

it('sends seek when the pointer releases on the progress range', () => {
  const onSeek = renderControls()
  const slider = screen.getByRole('slider', { name: '进度' })
  fireEvent.pointerDown(slider)
  ;(slider as HTMLInputElement).value = '800'
  fireEvent.pointerUp(slider)
  expect(onSeek).toHaveBeenCalledWith(800)
})
