import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { expect, it, vi } from 'vitest'
import { ChatFollowUps, suggestChatFollowUps } from './ChatFollowUps'

it('turns a closing question and office kinds into a few next-step chips', () => {
  const chips = suggestChatFollowUps(
    'PPT 已写到桌面。需要我调整某页措辞、加一页联系方式，还是换成深浅色主题？',
    [{ kind: 'pptx', path: 'desktop/介绍.pptx', content: '', callId: 'c1', toolName: 'pptx.gen' }],
  )
  expect(chips.some(item => item.includes('联系方式') || item.includes('主题'))).toBe(true)
  expect(chips).toContain('把这一版再精简一页')
  expect(chips.length).toBeGreaterThan(0)
  expect(chips.length).toBeLessThanOrEqual(3)
})

it('sends the tapped chip as the next prompt', async () => {
  const onSelect = vi.fn()
  const user = userEvent.setup()
  render(
    <ChatFollowUps
      text="文件已生成。需要我加一页联系方式吗？"
      artifacts={[{ kind: 'pptx', path: 'deck.pptx', content: '', callId: 'c1', toolName: 'pptx.gen' }]}
      onSelect={onSelect}
    />,
  )
  await user.click(screen.getAllByRole('button')[0])
  expect(onSelect).toHaveBeenCalledOnce()
  expect(String(onSelect.mock.calls[0][0]).length).toBeGreaterThan(3)
})
