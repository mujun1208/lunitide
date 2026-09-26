import { fireEvent, render, screen, waitFor } from '@testing-library/react'
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

it('opens the definition file and shows both diffs and the language-server problem', async () => {
  const read = vi.fn()
    .mockResolvedValueOnce({ path: 'use.go', content: 'package bound\n\nfunc Use() string {\n\treturn Answer()\n}\n', size: 40 })
    .mockResolvedValueOnce({ path: 'answer.go', content: 'package bound\n\nfunc Answer() string { return "right" }\n', size: 40 })
  let finishDiagnostics: (value: { ok: boolean; diagnostics: { line: number; message: string }[] }) => void = () => {}
  const diagnostics = new Promise<{ ok: boolean; diagnostics: { line: number; message: string }[] }>((resolve) => {
    finishDiagnostics = resolve
  })
  const code = vi.fn(async (payload: { action: string }) => {
    if (payload.action === 'diagnostics') return diagnostics
    if (payload.action === 'definition') return { ok: true, path: 'answer.go', line: 3 }
    if (payload.action === 'diff') return { ok: true, diff: '--- a.txt\n+++ a.txt\n-alpha\n+ALPHA\n--- b.txt\n+++ b.txt\n-beta\n+BETA\n' }
    return { ok: true }
  })
  render(
    <CodePanel
      bridge={{
        root: vi.fn(),
        select: vi.fn(),
        clear: vi.fn(),
        open: vi.fn(),
        list: vi.fn().mockResolvedValue({ items: [] }),
        read,
        code,
      }}
      sessionId="01ARZ3NDEKTSV4RRFFQ69G5FAV"
      projectRoot="D:/work/mall"
      targetPath="use.go"
    />,
  )
  await waitFor(() => expect(screen.getByText('未检查')).toBeInTheDocument())
  finishDiagnostics({ ok: true, diagnostics: [{ line: 4, message: 'undefined: fmt' }] })
  await waitFor(() => expect(screen.getByText('4: undefined: fmt')).toBeInTheDocument())
  expect(screen.getByText('1 问题')).toBeInTheDocument()
  expect(screen.getByText(/b\.txt/)).toBeInTheDocument()
  fireEvent.click(screen.getByRole('button', { name: '转到定义' }))
  await waitFor(() => {
    expect(read).toHaveBeenCalledWith('answer.go')
    expect(screen.getByText('断点停在第 3 行')).toBeInTheDocument()
  })
  expect(screen.getAllByText('answer.go').length).toBeGreaterThan(0)
})
