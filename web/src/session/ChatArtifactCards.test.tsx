import { render, screen } from '@testing-library/react'
import { expect, it, vi } from 'vitest'
import { ChatArtifactCards, filterChatDeliverables, isChatDeliverableArtifact } from './ChatArtifactCards'

vi.mock('../bridge/client', () => ({
  sessionFolderBridge: { open: vi.fn() },
}))

it('labels image artifacts as screenshots the user can open', () => {
  render(<ChatArtifactCards sessionId="01ARZ3NDEKTSV4RRFFQ69G5FAV" artifacts={[{
    kind: 'image', path: 'screen-capture-20260826.png', content: '', callId: 'call-1', toolName: 'cc.screen_capture',
  }]} />)
  expect(screen.getByRole('listitem')).toHaveTextContent('截图 · 点击打开')
  expect(screen.getByText('screen-capture-20260826.png')).toBeInTheDocument()
})

it('hides intermediate web search and fetch HTML from deliverable cards', () => {
  expect(isChatDeliverableArtifact({ toolName: 'web.search', kind: 'html', path: 'search.html' })).toBe(false)
  expect(isChatDeliverableArtifact({ toolName: 'web.fetch', kind: 'html', path: 'fetch.html' })).toBe(false)
  expect(isChatDeliverableArtifact({ toolName: 'pptx.gen', kind: 'pptx', path: 'deck.pptx' })).toBe(true)
  const visible = filterChatDeliverables([
    { kind: 'html', path: 'search.html', content: '', callId: 's1', toolName: 'web.search' },
    { kind: 'html', path: 'fetch.html', content: '', callId: 'f1', toolName: 'web.fetch' },
    { kind: 'pptx', path: 'deck.pptx', content: '', callId: 'p1', toolName: 'pptx.gen' },
  ])
  expect(visible).toHaveLength(1)
  expect(visible[0]?.path).toBe('deck.pptx')
  render(<ChatArtifactCards sessionId="01ARZ3NDEKTSV4RRFFQ69G5FAV" artifacts={[
    { kind: 'html', path: 'search.html', content: '', callId: 's1', toolName: 'web.search' },
    { kind: 'pptx', path: 'deck.pptx', content: '', callId: 'p1', toolName: 'pptx.gen' },
  ]} />)
  expect(screen.queryByText('search.html')).toBeNull()
  expect(screen.getByText('deck.pptx')).toBeInTheDocument()
})
