import { cleanup, render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, expect, it, vi } from 'vitest'
import { LanguageProvider } from '../i18n/language'
import { MroAskButton, openMroChat } from './MroAskButton'
import type { MroSessionContext } from './mroContext'

afterEach(cleanup)

const ctx: MroSessionContext = {
  tailNo: 'B-0000',
  asOf: '2026-09-03',
  manualIds: [],
  pack: 'mro.v1',
  scenario: 'manual',
}

const project = {
  id: '01ARZ3NDEKTSV4RRFFQ69G5FAV',
  name: 'personal',
  projectCode: 'ITM00000',
  type: 'implementation' as const,
  status: 'active' as const,
  createdAt: '2026-01-01T00:00:00Z',
  updatedAt: '2026-01-01T00:00:00Z',
  version: 1,
}

const session = {
  id: '01ARZ3NDEKTSV4RRFFQ69G5FAW',
  projectId: project.id,
  title: '机务手册',
  status: 'active' as const,
  createdAt: '2026-01-01T00:00:00Z',
  updatedAt: '2026-01-01T00:00:00Z',
  version: 1,
  pinned: false,
}

it('disables Ask when the MRO expert is missing', () => {
  render(
    <LanguageProvider value="zh-CN">
      <MroAskButton mroExpertId="" context={ctx} onOpened={vi.fn()} />
    </LanguageProvider>,
  )
  const btn = screen.getByRole('button', { name: '问月汐' })
  expect(btn).toBeDisabled()
  expect(btn).toHaveAttribute('title', '先启用对应机务专家')
})

it('creates one session, writes mroContext, and mounts the MRO ULID', async () => {
  const create = vi.fn().mockResolvedValue(session)
  const metadataSet = vi.fn().mockResolvedValue({ mroContext: ctx })
  const sessionMountSet = vi.fn().mockResolvedValue({ expertIds: ['01ARZ3NDEKTSV4RRFFQ69G5FAX'] })
  const opened = await openMroChat({
    ensureProject: async () => project,
    projects: { list: vi.fn(), create: vi.fn() } as never,
    sessions: { create, metadataSet, update: vi.fn(), list: vi.fn(), delete: vi.fn() },
    experts: { sessionMountSet, mount: vi.fn() } as never,
    mroExpertId: '01ARZ3NDEKTSV4RRFFQ69G5FAX',
    context: ctx,
  })
  expect(opened.sessionId).toBe(session.id)
  expect(create).toHaveBeenCalledOnce()
  expect(metadataSet).toHaveBeenCalledWith(
    expect.objectContaining({ sessionId: session.id, mroContext: expect.objectContaining({ tailNo: 'B-0000' }) }),
    expect.anything(),
  )
  expect(sessionMountSet).toHaveBeenCalledWith(
    { sessionId: session.id, expertIds: ['01ARZ3NDEKTSV4RRFFQ69G5FAX'] },
    expect.anything(),
  )
})

it('mounts at most two experts for a bulletin Ask', async () => {
  const sessionMountSet = vi.fn().mockResolvedValue({ expertIds: [] })
  await openMroChat({
    ensureProject: async () => project,
    projects: { list: vi.fn(), create: vi.fn() } as never,
    sessions: { create: vi.fn().mockResolvedValue(session), metadataSet: vi.fn(), update: vi.fn(), list: vi.fn(), delete: vi.fn() },
    experts: { sessionMountSet, mount: vi.fn() } as never,
    mroExpertId: '01ARZ3NDEKTSV4RRFFQ69G5FAX',
    extraExpertIds: ['01ARZ3NDEKTSV4RRFFQ69G5FAY', '01ARZ3NDEKTSV4RRFFQ69G5FAZ'],
    context: { ...ctx, lot: 'M-1', scenario: 'tools' },
    prompt: '质量通报串查 批次 M-1',
  })
  expect(sessionMountSet).toHaveBeenCalledWith(
    { sessionId: session.id, expertIds: ['01ARZ3NDEKTSV4RRFFQ69G5FAX', '01ARZ3NDEKTSV4RRFFQ69G5FAY'] },
    expect.anything(),
  )
})

it('asks Lunitide from the button when the expert is enabled', async () => {
  const onOpened = vi.fn()
  const user = userEvent.setup()
  render(
    <LanguageProvider value="zh-CN">
      <MroAskButton
        mroExpertId="01ARZ3NDEKTSV4RRFFQ69G5FAX"
        context={ctx}
        onOpened={onOpened}
        openChat={async () => ({ sessionId: session.id, project, session })}
      />
    </LanguageProvider>,
  )
  const btn = screen.getByRole('button', { name: '问月汐' })
  expect(btn).toBeEnabled()
  expect(btn).toHaveClass('primary')
  await user.click(btn)
  expect(onOpened).toHaveBeenCalledWith({ sessionId: session.id, project, session })
})
