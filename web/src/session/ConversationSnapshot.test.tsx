import { cleanup, fireEvent, render, screen } from '@testing-library/react'
import { afterEach, expect, it, vi } from 'vitest'
import { LanguageProvider } from '../i18n/language'
import { ConversationSnapshot } from './ConversationSnapshot'

vi.mock('html-to-image', () => ({
  toBlob: vi.fn(),
}))

afterEach(cleanup)

it('does not show raw English snapshot save or copy failures', async () => {
  const { toBlob } = await import('html-to-image')
  vi.mocked(toBlob).mockRejectedValue(new Error('Failed to fetch'))
  const items = [{
    id: '01ARZ3NDEKTSV4RRFFQ69G5FAV', sessionId: '01ARZ3NDEKTSV4RRFFQ69G5FAW', role: 'user' as const,
    status: 'completed' as const, text: '你好', sequence: 1, createdAt: '2026-01-01T00:00:00Z',
  }]
  render(<LanguageProvider value="zh-CN"><ConversationSnapshot open title="对话" items={items} onClose={vi.fn()} /></LanguageProvider>)
  fireEvent.click(screen.getByRole('button', { name: '下载图片' }))
  expect(await screen.findByText('图片生成失败')).toBeInTheDocument()
  expect(screen.queryByText('Failed to fetch')).toBeNull()
  fireEvent.click(screen.getByRole('button', { name: '复制图片' }))
  expect(await screen.findByText('复制图片失败')).toBeInTheDocument()
  expect(screen.queryByText('Failed to fetch')).toBeNull()
})
