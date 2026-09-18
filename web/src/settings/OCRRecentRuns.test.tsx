import { cleanup, render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, expect, it, vi } from 'vitest'
import type { OCRRunBridge } from '../bridge/client'
import { OCRRecentRuns } from './OCRRecentRuns'

afterEach(cleanup)

it('reads bounded artifacts and does not render raw HTML', async () => {
  const user = userEvent.setup()
  const readArtifact = vi.fn()
    .mockResolvedValueOnce({
      base64: btoa('# page one\n<img src=x onerror="window.__ocr=1">'),
      nextOffset: 40,
      eof: false,
      sha256: 'a'.repeat(64),
      totalBytes: 2_000_000,
    })
    .mockResolvedValueOnce({
      base64: btoa('page two'),
      nextOffset: 1_048_576,
      eof: false,
      sha256: 'a'.repeat(64),
      totalBytes: 2_000_000,
    })
  const api = {
    list: vi.fn().mockResolvedValue({
      items: [{ runId: '01ARZ3NDEKTSV4RRFFQ69G5FAV', createdAt: '2026-09-18T00:00:00Z', status: 'succeeded', artifactId: '01ARZ3NDEKTSV4RRFFQ69G5FAW' }],
      nextCursor: null,
    }),
    get: vi.fn(),
    readArtifact,
  } as unknown as OCRRunBridge
  render(<OCRRecentRuns api={api} />)
  await user.click(await screen.findByRole('button', { name: /2026-09-18/ }))
  await waitFor(() => expect(readArtifact).toHaveBeenCalledWith({
    artifactId: '01ARZ3NDEKTSV4RRFFQ69G5FAW',
    offset: 0,
    limit: 65_536,
  }))
  expect(screen.getByText('page one')).toBeInTheDocument()
  expect(document.querySelector('img')).toBeNull()
  expect((window as Window & { __ocr?: number }).__ocr).toBeUndefined()
  await user.click(screen.getByRole('button', { name: '下一页' }))
  await waitFor(() => expect(readArtifact).toHaveBeenCalledTimes(2))
  expect(await screen.findByText(/预览已达 1 MiB/)).toBeInTheDocument()
})
