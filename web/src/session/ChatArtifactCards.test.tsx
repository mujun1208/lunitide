import { render, screen } from '@testing-library/react'
import { expect, it, vi } from 'vitest'
import { ChatArtifactCards } from './ChatArtifactCards'

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
