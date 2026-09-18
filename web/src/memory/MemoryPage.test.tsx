import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterEach, expect, it, vi } from 'vitest'
import type { MemoryBridge, MemoryOpsBridge } from '../bridge/client'
import { MemoryPage } from './MemoryPage'

afterEach(cleanup)

const now = '2026-01-01T00:00:00Z'
const version = 'a'.repeat(64)
const item = {
  factId: '01ARZ3NDEKTSV4RRFFQ69G5FAV',
  version: 1,
  revision: 1,
  scopeKind: 'user' as const,
  kind: 'preference' as const,
  text: '我喜欢简洁的回答',
  forgotten: false,
  updatedAt: now,
}

function page(overrides: { memory?: Partial<MemoryBridge>; ops?: Partial<MemoryOpsBridge> } = {}) {
  const itemList = vi.fn().mockResolvedValue({ items: [item], databaseRevision: 1 })
  const itemCreate = vi.fn().mockResolvedValue({ item, databaseRevision: 1, undoOperationId: item.factId, undoExpiresAt: now })
  const itemForget = vi.fn().mockResolvedValue({ factId: item.factId, forgotten: true, databaseRevision: 2 })
  const importPreview = vi.fn().mockResolvedValue({
    previewId: '01ARZ3NDEKTSV4RRFFQ69G5FAW',
    sourceArtifactId: item.factId,
    archiveDigest: 'a'.repeat(64),
    manifestDigest: 'b'.repeat(64),
    databaseRevision: 1,
    expiresAt: now,
    counts: { total: 1, accepted: 1, review: 0, conflicts: 0, sensitive: 0, tombstones: 0 },
    warnings: [],
    operationId: '01ARZ3NDEKTSV4RRFFQ69G5FAX',
  })
  const importCommit = vi.fn().mockResolvedValue({
    previewId: '01ARZ3NDEKTSV4RRFFQ69G5FAW',
    state: 'committed',
    importedCount: 1,
    reviewCount: 0,
    skippedCount: 0,
    conflictCount: 0,
    databaseRevision: 2,
    operationId: '01ARZ3NDEKTSV4RRFFQ69G5FAY',
  })
  const getSettings = vi.fn().mockResolvedValue({
    subjectId: 'local-user', memoryEnabled: true, autoNominate: false, growthDays: 14,
    updatedAt: now, version, captureMode: 'auto', revision: 1,
    personalMemoryEnabled: true, projectMemoryEnabled: true,
  })
  const updateSettings = vi.fn().mockResolvedValue({
    subjectId: 'local-user', memoryEnabled: true, autoNominate: false, growthDays: 14,
    updatedAt: now, version, captureMode: 'manual', revision: 2,
    personalMemoryEnabled: true, projectMemoryEnabled: false,
  })
  const exportMemory = vi.fn().mockImplementation(async (payload: { format?: 'fabric_v2' } = {}) => {
    if (payload.format === 'fabric_v2') {
      return {
        format: 'fabric_v2' as const,
        artifactId: item.factId,
        archiveDigest: 'a'.repeat(64),
        manifestDigest: 'b'.repeat(64),
        databaseRevision: 1,
        counts: { records: 1, current: 1, tombstones: 0 },
      }
    }
    return { facts: [], leaves: [], candidates: [], traces: [], growth: [], flags: [], settings: [] }
  })
  const captureUndo = vi.fn().mockResolvedValue({ undone: true, forgottenFactIds: [item.factId], databaseRevision: 2 })
  const generationList = vi.fn().mockResolvedValue({ items: [], nextCursor: null, databaseRevision: 1 })
  const memory = {
    get: vi.fn(), list: vi.fn(), create: vi.fn(), search: vi.fn(), update: vi.fn(), delete: vi.fn(),
    itemList, itemCreate, itemForget, importPreview, importCommit, captureUndo, generationList,
    reviewList: vi.fn().mockResolvedValue({ items: [], databaseRevision: 1 }),
    ...overrides.memory,
  } as MemoryBridge
  const ops = {
    getSettings, updateSettings,
    stats: vi.fn(), listFacts: vi.fn(), flagFact: vi.fn(), listTraces: vi.fn(),
    listGrowth: vi.fn(), decideGrowth: vi.fn(), export: exportMemory,
    purgePrepare: vi.fn(), purge: vi.fn(),
    ...overrides.ops,
  } as MemoryOpsBridge
  render(<MemoryPage bridge={memory} ops={ops} />)
  return { itemList, itemCreate, itemForget, captureUndo, generationList, getSettings, updateSettings, exportMemory, importPreview, importCommit }
}

it('TestMemoryCenterContract: shows status, search, a single list and advanced entry without tabs or radios', async () => {
  page()
  expect(await screen.findByText(/自动记忆 · 个人开 · 项目开/)).toBeInTheDocument()
  expect(screen.getByRole('button', { name: '记忆设置' })).toBeInTheDocument()
  expect(screen.getByRole('searchbox', { name: '搜索记忆' })).toBeInTheDocument()
  expect(await screen.findByText('我喜欢简洁的回答')).toBeInTheDocument()
  expect(screen.getByText('个人')).toBeInTheDocument()
  expect(screen.getByText('2026-01-01')).toBeInTheDocument()
  expect(screen.getByRole('button', { name: '更多 我喜欢简洁的回答' })).toBeInTheDocument()
  expect(screen.getByRole('button', { name: '高级管理' })).toBeInTheDocument()
  expect(screen.queryByRole('radio')).toBeNull()
  expect(screen.queryByRole('switch')).toBeNull()
  expect(screen.queryByRole('tab')).toBeNull()
  expect(screen.queryByText('当前任务')).toBeNull()
  expect(screen.queryByText('记忆运营')).toBeNull()
})

it('TestSingleMemoryDrawer: opens one settings drawer with three modes and two scope switches', async () => {
  const { updateSettings } = page()
  fireEvent.click(await screen.findByRole('button', { name: '记忆设置' }))
  expect(screen.getAllByRole('complementary')).toHaveLength(1)
  expect(screen.getAllByRole('radio')).toHaveLength(3)
  expect(screen.getByRole('switch', { name: '个人记忆' })).toBeInTheDocument()
  expect(screen.getByRole('switch', { name: '项目记忆' })).toBeInTheDocument()
  fireEvent.click(screen.getByRole('radio', { name: /手动/ }))
  fireEvent.click(screen.getByRole('button', { name: '保存设置' }))
  await waitFor(() => expect(updateSettings).toHaveBeenCalled())
  expect(updateSettings.mock.calls[0][0]).toEqual(expect.objectContaining({
    captureMode: 'manual',
    expectedRevision: 1,
  }))
  expect(updateSettings.mock.calls[0][0]).not.toHaveProperty('memoryEnabled')
  expect(updateSettings.mock.calls[0][0]).not.toHaveProperty('autoNominate')
})

it('forgets a canonical item from the same drawer', async () => {
  const { itemForget } = page()
  fireEvent.click(await screen.findByText('我喜欢简洁的回答'))
  expect(screen.getAllByRole('complementary')).toHaveLength(1)
  fireEvent.click(screen.getByRole('button', { name: '忘记' }))
  await waitFor(() => expect(itemForget).toHaveBeenCalled())
  expect(itemForget.mock.calls[0][0]).toEqual(expect.objectContaining({
    factId: item.factId,
    mode: 'fact_history',
    expectedRevision: 1,
  }))
  expect(await screen.findByRole('status')).toHaveTextContent('已忘记这条记忆')
})

it('forgets from the row more menu without opening another control surface', async () => {
  const { itemForget } = page()
  fireEvent.click(await screen.findByRole('button', { name: '更多 我喜欢简洁的回答' }))
  fireEvent.click(screen.getByRole('menuitem', { name: '忘记' }))
  await waitFor(() => expect(itemForget).toHaveBeenCalled())
  expect(itemForget.mock.calls[0][0]).toEqual(expect.objectContaining({
    factId: item.factId,
    mode: 'fact_history',
    expectedRevision: 1,
  }))
})

it('creates an explicit memory from advanced management', async () => {
  const { itemCreate, captureUndo } = page()
  fireEvent.click(await screen.findByRole('button', { name: '高级管理' }))
  fireEvent.change(screen.getByRole('textbox', { name: '记忆正文' }), { target: { value: '我喜欢简洁的回答' } })
  fireEvent.click(screen.getByRole('button', { name: '保存为记忆' }))
  await waitFor(() => expect(itemCreate).toHaveBeenCalled())
  expect(itemCreate.mock.calls[0][0]).toEqual(expect.objectContaining({
    scopeKind: 'user',
    text: '我喜欢简洁的回答',
  }))
  fireEvent.click(await screen.findByRole('button', { name: '撤销' }))
  await waitFor(() => expect(captureUndo).toHaveBeenCalled())
  expect(captureUndo.mock.calls[0][0]).toEqual(expect.objectContaining({
    undoOperationId: item.factId,
  }))
})

it('lists memory generations and previews then activates a ready generation', async () => {
  const generation = {
    generationId: item.factId,
    parentGenerationId: null,
    scopeKind: 'user' as const,
    scopeId: null,
    state: 'ready' as const,
    sourceCutoffSeq: 1,
    builderVersion: 'v1',
    memberCount: 1,
    revision: 1,
    createdAt: now,
    readyAt: now,
    activatedAt: null,
    errorCode: null,
  }
  const generationList = vi.fn().mockResolvedValue({ items: [generation], nextCursor: null, databaseRevision: 1 })
  const generationPreview = vi.fn().mockResolvedValue({
    generation,
    changes: [{ change: 'add' as const, factId: item.factId, fromVersion: null, toVersion: 1, beforeText: null, afterText: '我喜欢简洁的回答', reasonCodes: ['index'] }],
  })
  const generationActivate = vi.fn().mockResolvedValue({ generation: { ...generation, state: 'active' as const }, databaseRevision: 2, operationId: item.factId })
  page({ memory: { generationList, generationPreview, generationActivate } })
  fireEvent.click(await screen.findByRole('button', { name: '高级管理' }))
  fireEvent.click(screen.getByRole('button', { name: '记忆世代' }))
  expect(await screen.findByText(/可启用 · 1 条/)).toBeInTheDocument()
  fireEvent.click(screen.getByRole('button', { name: '预览' }))
  await waitFor(() => expect(generationPreview).toHaveBeenCalledWith({ generationId: item.factId }))
  expect(await screen.findByText(/新增 · 我喜欢简洁的回答/)).toBeInTheDocument()
  fireEvent.click(screen.getByRole('button', { name: '启用' }))
  await waitFor(() => expect(generationActivate).toHaveBeenCalled())
  expect(generationActivate.mock.calls[0][0]).toEqual(expect.objectContaining({
    generationId: item.factId,
    expectedRevision: 1,
  }))
})

it('TestMemoryReviewQueue: lists a pending review and resolves it from advanced', async () => {
  const reviewResolve = vi.fn().mockResolvedValue({ reviewId: item.factId, decision: 'accept', databaseRevision: 2 })
  page({
    memory: {
      reviewList: vi.fn().mockResolvedValue({
        items: [{ reviewId: item.factId, kind: 'preference', novelty: 'new', reasonCodes: ['manual'], text: '待审：我喜欢简洁的回答', createdAt: now }],
        databaseRevision: 1,
      }),
      reviewResolve,
    },
  })
  fireEvent.click(await screen.findByRole('button', { name: '高级管理' }))
  fireEvent.click(screen.getByRole('button', { name: '待审' }))
  expect(await screen.findByText('待审：我喜欢简洁的回答')).toBeInTheDocument()
  fireEvent.click(screen.getByRole('button', { name: '接受' }))
  await waitFor(() => expect(reviewResolve).toHaveBeenCalled())
  expect(reviewResolve.mock.calls[0][0]).toEqual(expect.objectContaining({
    reviewId: item.factId,
    decision: 'accept',
  }))
})

it('loads item history when the drawer opens', async () => {
  const itemGet = vi.fn().mockResolvedValue(item)
  const itemHistory = vi.fn().mockResolvedValue({
    items: [{ ...item, version: 1, text: '我喜欢简洁的回答' }],
    nextCursor: null,
  })
  page({ memory: { itemGet, itemHistory } })
  fireEvent.click(await screen.findByText('我喜欢简洁的回答'))
  await waitFor(() => expect(itemGet).toHaveBeenCalledWith({ factId: item.factId }))
  await waitFor(() => expect(itemHistory).toHaveBeenCalledWith(expect.objectContaining({ factId: item.factId })))
  expect(await screen.findByRole('heading', { name: '版本历史' })).toBeInTheDocument()
})

it('corrects a canonical item from the same drawer', async () => {
  const itemCorrect = vi.fn().mockResolvedValue({ item: { ...item, text: '我喜欢更准确的回答', revision: 2 }, databaseRevision: 2 })
  page({ memory: { itemCorrect } })
  fireEvent.click(await screen.findByText('我喜欢简洁的回答'))
  fireEvent.change(screen.getByRole('textbox', { name: '更正正文' }), { target: { value: '我喜欢更准确的回答' } })
  fireEvent.click(screen.getByRole('button', { name: '更正' }))
  await waitFor(() => expect(itemCorrect).toHaveBeenCalled())
  expect(itemCorrect.mock.calls[0][0]).toEqual(expect.objectContaining({
    factId: item.factId,
    replacementText: '我喜欢更准确的回答',
    expectedRevision: 1,
  }))
})

it('exports fabric metadata and previews import without writing until commit', async () => {
  const { exportMemory, importPreview, importCommit } = page()
  fireEvent.click(await screen.findByRole('button', { name: '高级管理' }))
  fireEvent.click(screen.getByRole('button', { name: '导入/导出' }))
  fireEvent.click(screen.getByRole('button', { name: '导出完整档案' }))
  await waitFor(() => expect(exportMemory).toHaveBeenCalledWith({ format: 'fabric_v2' }))
  expect(screen.getByText(/当前 1 条 · 墓碑 0 条/)).toBeInTheDocument()
  fireEvent.click(screen.getByRole('button', { name: '预览导入' }))
  await waitFor(() => expect(importPreview).toHaveBeenCalled())
  expect(screen.getByText(/可写入 1 条/)).toBeInTheDocument()
  expect(importCommit).not.toHaveBeenCalled()
  fireEvent.click(screen.getByRole('button', { name: '确认导入' }))
  await waitFor(() => expect(importCommit).toHaveBeenCalled())
})
