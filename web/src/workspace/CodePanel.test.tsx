import { render, waitFor } from '@testing-library/react'
import { expect, it, vi } from 'vitest'
import { CodePanel } from './CodePanel'

it('binds the open code folder for this session', async () => {
  const code = vi.fn().mockResolvedValue({ ok: true })
  render(
    <CodePanel
      bridge={{
        root: vi.fn(),
        select: vi.fn(),
        clear: vi.fn(),
        open: vi.fn(),
        list: vi.fn().mockResolvedValue({ items: [] }),
        read: vi.fn(),
        code,
      }}
      sessionId="01ARZ3NDEKTSV4RRFFQ69G5FAV"
      projectRoot="D:/work/mall"
    />,
  )
  await waitFor(() => expect(code).toHaveBeenCalledWith(expect.objectContaining({
    action: 'diff',
    root: 'D:/work/mall',
    sessionId: '01ARZ3NDEKTSV4RRFFQ69G5FAV',
  })))
})
