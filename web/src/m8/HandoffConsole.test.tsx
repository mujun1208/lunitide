import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterEach, expect, it, vi } from 'vitest'
import type { ContextBridge, ProjectBridge, SessionBridge } from '../bridge/client'
import { HandoffConsole } from './HandoffConsole'

const P = '01ARZ3NDEKTSV4RRFFQ69G5FAV'
const S = '01ARZ3NDEKTSV4RRFFQ69G5FAW'
const C = '01ARZ3NDEKTSV4RRFFQ69G5FAX'
afterEach(cleanup)

const project = { id: P, name: '月汐', projectCode: 'ITM00001', type: 'implementation' as const, status: 'active' as const, createdAt: '2026-01-01T00:00:00Z', updatedAt: '2026-01-01T00:00:00Z', version: 1 }
const session = { id: S, projectId: P, title: '交接会话', pinned: false, status: 'active' as const, createdAt: '2026-01-01T00:00:00Z', updatedAt: '2026-01-01T00:00:00Z', version: 1 }
const capsule = { capsuleId: C, sourceSessionId: S, checkpointId: P, status: 'active' as const, digest: 'abc', createdAt: '2026-08-19T00:00:00Z' }

function harness() {
  const compactPreview = vi.fn().mockResolvedValue({ checkpointId: P, version: 1, sourceStartSeq: 1, sourceEndSeq: 2, sourceDigest: 'd', summaryPreview: '摘要预览', humanSummary: '人类摘要', status: 'succeeded' })
  const handoffCreate = vi.fn().mockResolvedValue({ capsuleId: C, sourceSessionId: S, checkpointId: P, status: 'active', digest: 'abc', createdAt: '2026-08-19T00:00:00Z' })
  const handoffList = vi.fn().mockResolvedValue({ items: [] })
  const handoffListImports = vi.fn().mockResolvedValue({ items: [] })
  const handoffImport = vi.fn().mockResolvedValue({ capsuleId: C, sourceSessionId: S, checkpointId: P, status: 'active', digestValid: true, expiredCheck: false, alreadyImported: false, importedAt: '2026-08-19T00:00:00Z' })
  const handoffInspect = vi.fn().mockResolvedValue({ ...capsule, sourceDeleted: false, humanSummary: '查看摘要' })
  const handoffRevoke = vi.fn().mockResolvedValue({ capsuleId: C, revoked: true })
  const context = { compactPreview, handoffCreate, handoffList, handoffListImports, handoffImport, handoffInspect, handoffRevoke } as unknown as ContextBridge
  const projects = { list: vi.fn().mockResolvedValue({ items: [project] }) } as unknown as ProjectBridge
  const sessions = { list: vi.fn().mockResolvedValue({ items: [session] }) } as unknown as SessionBridge
  return { context, projects, sessions, compactPreview, handoffCreate, handoffList, handoffImport, handoffRevoke }
}

it('previews then confirms capsule export without activating compaction', async () => {
  const h = harness()
  render(<HandoffConsole context={h.context} projects={h.projects} sessions={h.sessions} />)
  expect(await screen.findByRole('option', { name: '交接会话' })).toBeInTheDocument()
  fireEvent.click(screen.getByRole('button', { name: '生成压缩预览' }))
  expect(await screen.findByText('人类摘要')).toBeInTheDocument()
  expect(h.handoffCreate).not.toHaveBeenCalled()
  fireEvent.click(screen.getByRole('button', { name: '确认导出胶囊' }))
  await waitFor(() => expect(h.handoffCreate).toHaveBeenCalledWith({ sourceSessionId: S, checkpointId: P }))
  expect(await screen.findByText(/已导出交接胶囊/)).toBeInTheDocument()
})

it('imports a capsule into the selected session', async () => {
  const h = harness()
  render(<HandoffConsole context={h.context} projects={h.projects} sessions={h.sessions} />)
  await screen.findByRole('option', { name: '交接会话' })
  fireEvent.change(screen.getByLabelText('导入胶囊 ID'), { target: { value: C } })
  fireEvent.click(screen.getByRole('button', { name: '导入' }))
  await waitFor(() => expect(h.handoffImport).toHaveBeenCalledWith({ capsuleId: C, targetSessionId: S }))
  expect(await screen.findByText(/已导入胶囊/)).toBeInTheDocument()
})
