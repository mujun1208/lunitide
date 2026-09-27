import { expect, it } from 'vitest'
import { workspaceCoversChat, workspaceDragLimit, workspaceWidthAfterCover } from './workspaceSplit'

it('lets the side panel drag past the old 800px cap', () => {
  expect(workspaceDragLimit(1600)).toBeGreaterThan(800)
  expect(workspaceDragLimit(1600)).toBe(1552)
})

it('hides the chat once the panel is pulled near the window edge', () => {
  expect(workspaceCoversChat(1280, 1600)).toBe(true)
  expect(workspaceCoversChat(800, 1600)).toBe(false)
  expect(workspaceCoversChat(520, 1600)).toBe(false)
})

it('keeps a normal width after covering so restoring the chat does not cover it again', () => {
  const restored = workspaceWidthAfterCover(1600)
  expect(restored).toBeGreaterThanOrEqual(360)
  expect(restored).toBeLessThanOrEqual(720)
  expect(workspaceCoversChat(restored, 1600)).toBe(false)
})

it('does not cover the chat when the splitter is hidden on a narrow window', () => {
  expect(workspaceCoversChat(700, 700)).toBe(false)
})
