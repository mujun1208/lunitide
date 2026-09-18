import { cleanup, fireEvent, render, screen } from '@testing-library/react'
import { afterEach, expect, it, vi } from 'vitest'
import { MemoryItemMenu } from './MemoryItemMenu'

afterEach(cleanup)

it('opens view, correct and forget from the row menu without leaking as a switch', () => {
  const onView = vi.fn()
  const onCorrect = vi.fn()
  const onForget = vi.fn()
  render(<MemoryItemMenu label="我喜欢简洁的回答" onView={onView} onCorrect={onCorrect} onForget={onForget} />)
  expect(screen.queryByRole('menu')).toBeNull()
  fireEvent.click(screen.getByRole('button', { name: '更多 我喜欢简洁的回答' }))
  expect(screen.getByRole('menu')).toBeInTheDocument()
  fireEvent.click(screen.getByRole('menuitem', { name: '更正' }))
  expect(onCorrect).toHaveBeenCalledOnce()
  expect(onView).not.toHaveBeenCalled()
  expect(onForget).not.toHaveBeenCalled()
  expect(screen.queryByRole('menu')).toBeNull()
})
