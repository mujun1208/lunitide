import { afterEach, expect, test, vi } from 'vitest'
import { scrollOfficeNodeIntoView } from './officePreviewScroll'

afterEach(() => {
  vi.restoreAllMocks()
})

test('moves only the paper scroller and never calls scrollIntoView', () => {
  const scrollIntoView = vi.fn()
  Element.prototype.scrollIntoView = scrollIntoView
  const root = document.createElement('div')
  const node = document.createElement('div')
  root.appendChild(node)
  Object.defineProperty(root, 'scrollTop', { value: 10, writable: true })
  root.getBoundingClientRect = () => ({ top: 100 } as DOMRect)
  node.getBoundingClientRect = () => ({ top: 250 } as DOMRect)

  expect(scrollOfficeNodeIntoView(root, node)).toBe(true)
  expect(root.scrollTop).toBe(148)
  expect(scrollIntoView).not.toHaveBeenCalled()
})

test('leaves foreign scrollers alone when the node is outside the paper root', () => {
  const root = document.createElement('div')
  const node = document.createElement('div')
  Object.defineProperty(root, 'scrollTop', { value: 40, writable: true })

  expect(scrollOfficeNodeIntoView(root, node)).toBe(false)
  expect(root.scrollTop).toBe(40)
})
