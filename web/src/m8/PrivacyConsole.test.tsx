import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterEach, expect, it, vi } from 'vitest'
import type { DiagnosticsBridge, MemoryOpsBridge } from '../bridge/client'
import { PrivacyConsole } from './PrivacyConsole'

afterEach(cleanup)

it('exports a diagnostics pack through the live bridge', async () => {
  const exportDiagnostics = vi.fn().mockResolvedValue({ path: 'C:\\diag.zip', createdAt: '2026-08-19T00:00:00Z', redacted: true })
  const diagnostics = { exportDiagnostics } as unknown as DiagnosticsBridge
  const memory = { export: vi.fn(), purge: vi.fn() } as unknown as MemoryOpsBridge
  render(<PrivacyConsole diagnostics={diagnostics} memory={memory} />)
  fireEvent.click(screen.getByRole('button', { name: '导出诊断包' }))
  await waitFor(() => expect(exportDiagnostics).toHaveBeenCalledWith({ includeLogs: false, redactPaths: true }))
  expect(await screen.findByText(/诊断包已导出/)).toBeInTheDocument()
})

it('requires a second click before purging memory', async () => {
  const purge = vi.fn().mockResolvedValue({ factsTombstoned: 3, candidates: 1, growthRows: 0, flags: 0, traces: 0, memories: 2 })
  const diagnostics = { exportDiagnostics: vi.fn() } as unknown as DiagnosticsBridge
  const memory = { export: vi.fn(), purge } as unknown as MemoryOpsBridge
  render(<PrivacyConsole diagnostics={diagnostics} memory={memory} />)
  fireEvent.click(screen.getByRole('button', { name: '清除记忆…' }))
  expect(purge).not.toHaveBeenCalled()
  fireEvent.click(screen.getByRole('button', { name: '确认清除' }))
  await waitFor(() => expect(purge).toHaveBeenCalledWith({}))
  expect(await screen.findByText(/已清除本机记忆/)).toBeInTheDocument()
})
