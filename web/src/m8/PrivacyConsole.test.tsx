import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterEach, expect, it, vi } from 'vitest'
import type { DiagnosticsBridge, MemoryOpsBridge } from '../bridge/client'
import { PrivacyConsole } from './PrivacyConsole'

afterEach(cleanup)

it('does not show raw English export failures', async () => {
  const diagnostics = { exportDiagnostics: vi.fn().mockRejectedValue(new Error('Failed to fetch')) } as unknown as DiagnosticsBridge
  const memory = { export: vi.fn(), purge: vi.fn() } as unknown as MemoryOpsBridge
  render(<PrivacyConsole diagnostics={diagnostics} memory={memory} />)
  fireEvent.click(screen.getByRole('button', { name: '导出诊断包' }))
  expect(await screen.findByRole('alert')).toHaveTextContent('诊断包导出失败')
  expect(screen.queryByText('Failed to fetch')).toBeNull()
})

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
  const purgePrepare = vi.fn().mockResolvedValue({
    counts: { facts: 3, candidates: 1, searchDocuments: 0, embeddings: 0 },
    snapshotDigest: 'b'.repeat(64),
    confirmationToken: 'a'.repeat(64),
    expiresAt: '2026-08-19T00:05:00Z',
    operationId: '01ARZ3NDEKTSV4RRFFQ69G5FAV',
  })
  const diagnostics = { exportDiagnostics: vi.fn() } as unknown as DiagnosticsBridge
  const memory = { export: vi.fn(), purge, purgePrepare } as unknown as MemoryOpsBridge
  render(<PrivacyConsole diagnostics={diagnostics} memory={memory} />)
  fireEvent.click(screen.getByRole('button', { name: '清除记忆…' }))
  expect(purge).not.toHaveBeenCalled()
  await waitFor(() => expect(purgePrepare).toHaveBeenCalledOnce())
  fireEvent.click(screen.getByRole('button', { name: '确认清除' }))
  await waitFor(() => expect(purge).toHaveBeenCalledOnce())
  expect(purge.mock.calls[0][0]).toEqual(expect.objectContaining({
    confirmationToken: 'a'.repeat(64),
    snapshotDigest: 'b'.repeat(64),
  }))
  expect(await screen.findByText(/已清除本机记忆/)).toBeInTheDocument()
})
