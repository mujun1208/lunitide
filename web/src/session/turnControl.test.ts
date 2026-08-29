import { expect, it } from 'vitest'
import {
  composerPrimaryAction,
  composerPrimaryLabel,
  showComposerStopButton,
  showSegmentStopControl,
} from './turnControl'

it('morphs primary button to stop for a single in-flight turn with empty composer', () => {
  expect(composerPrimaryAction({ streaming: true, activeTurnCount: 1, composerHasText: false })).toBe('stop')
  expect(composerPrimaryLabel('stop')).toBe('停止')
})

it('keeps primary as send when idle', () => {
  expect(composerPrimaryAction({ streaming: false, activeTurnCount: 0, composerHasText: false })).toBe('send')
  expect(composerPrimaryLabel('send')).toBe('↑ 发送并对话')
})

it('sends follow-ups while streaming instead of stopping', () => {
  expect(composerPrimaryAction({ streaming: true, activeTurnCount: 1, composerHasText: true })).toBe('follow-up-send')
  expect(composerPrimaryLabel('follow-up-send')).toBe('↑ 发送')
})

it('shows composer stop only when multiple turns are active', () => {
  expect(showComposerStopButton(true, 1)).toBe(false)
  expect(showComposerStopButton(true, 2)).toBe(true)
  expect(showComposerStopButton(false, 2)).toBe(false)
})

it('shows per-segment stop for any active turn', () => {
  expect(showSegmentStopControl(0)).toBe(false)
  expect(showSegmentStopControl(1)).toBe(true)
  expect(showSegmentStopControl(2)).toBe(true)
})

it('keeps composer send when multiple actives and empty composer', () => {
  expect(composerPrimaryAction({ streaming: true, activeTurnCount: 2, composerHasText: false })).toBe('follow-up-send')
})
