import { cleanup, fireEvent, render, screen } from '@testing-library/react'
import { afterEach, expect, it, vi } from 'vitest'
import { MemoryStatusHeader } from './MemoryStatusHeader'

afterEach(cleanup)

it('shows a read-only mode and scope summary with a settings button', () => {
  const onOpenSettings = vi.fn()
  render(<MemoryStatusHeader draft={{ captureMode: 'manual', personalMemoryEnabled: true, projectMemoryEnabled: false, revision: 2 }} onOpenSettings={onOpenSettings} />)
  expect(screen.getByText('仅手动 · 个人开 · 项目关')).toBeInTheDocument()
  fireEvent.click(screen.getByRole('button', { name: '记忆设置' }))
  expect(onOpenSettings).toHaveBeenCalledOnce()
  expect(screen.queryByRole('radio')).toBeNull()
  expect(screen.queryByRole('switch')).toBeNull()
})
