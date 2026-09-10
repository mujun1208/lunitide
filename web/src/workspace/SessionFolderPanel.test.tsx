import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterEach, expect, it, vi } from 'vitest'
import { artifactReviewBridge } from '../bridge/client'
import { SessionFolderPanel } from './SessionFolderPanel'

vi.mock('../bridge/client', () => ({
  artifactReviewBridge: { preview: vi.fn() },
  sessionFolderBridge: { get: vi.fn(), list: vi.fn(), open: vi.fn() },
}))

afterEach(() => {
  cleanup()
  vi.clearAllMocks()
})

it('does not show raw English folder load or open failures', async () => {
  const { sessionFolderBridge } = await import('../bridge/client')
  vi.mocked(sessionFolderBridge.get).mockRejectedValue(new Error('Failed to fetch'))
  render(<SessionFolderPanel sessionId="01ARZ3NDEKTSV4RRFFQ69G5FAV" />)
  expect(await screen.findByRole('alert')).toHaveTextContent('会话目录载入失败')
  expect(screen.queryByText('Failed to fetch')).toBeNull()
  cleanup()
  vi.mocked(sessionFolderBridge.get).mockResolvedValue({ path: 'E:/sessions/demo' })
  vi.mocked(sessionFolderBridge.list).mockResolvedValue({ items: [] })
  vi.mocked(sessionFolderBridge.open).mockRejectedValue(new Error('Failed to fetch'))
  render(<SessionFolderPanel sessionId="01ARZ3NDEKTSV4RRFFQ69G5FAV" />)
  await screen.findByText('此对话还没有产物文件。')
  fireEvent.click(screen.getByRole('button', { name: '在资源管理器中打开' }))
  expect(await screen.findByRole('alert')).toHaveTextContent('打开失败')
  expect(screen.queryByText('Failed to fetch')).toBeNull()
})

it('lists directories and files, then previews a file in the workspace instead of opening Office', async () => {
  const { sessionFolderBridge } = await import('../bridge/client')
  vi.mocked(sessionFolderBridge.get).mockResolvedValue({ path: 'E:/sessions/demo' })
  vi.mocked(sessionFolderBridge.list).mockImplementation(async ({ relativePath }) => {
    if (!relativePath) {
      return { items: [{ name: '周报', path: '周报', directory: true }, { name: 'notes.txt', path: 'notes.txt', directory: false }] }
    }
    return { items: [{ name: '周报_2026-W37.md', path: '周报/周报_2026-W37.md', directory: false }] }
  })
  vi.mocked(artifactReviewBridge.preview).mockResolvedValue({
    kind: 'text', path: '周报/周报_2026-W37.md', content: '# 本周进展', size: 12,
  })
  const onPreview = vi.fn()
  render(<SessionFolderPanel sessionId="01ARZ3NDEKTSV4RRFFQ69G5FAV" onPreview={onPreview} />)
  expect(await screen.findByRole('treeitem', { name: /周报$/ })).toBeInTheDocument()
  expect(screen.getByRole('treeitem', { name: /notes.txt/ })).toBeInTheDocument()
  fireEvent.click(screen.getByRole('treeitem', { name: /周报$/ }))
  expect(await screen.findByRole('treeitem', { name: /周报_2026-W37.md/ })).toBeInTheDocument()
  fireEvent.click(screen.getByRole('treeitem', { name: /周报_2026-W37.md/ }))
  await waitFor(() => expect(onPreview).toHaveBeenCalledWith({ path: '周报/周报_2026-W37.md', content: '# 本周进展', size: 12 }))
  expect(sessionFolderBridge.open).not.toHaveBeenCalled()
})
