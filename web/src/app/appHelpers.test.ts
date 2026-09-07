import { afterEach, expect, it, vi } from 'vitest'
import type { ProjectBridge } from '../bridge/client'
import type { ProjectDTO } from '../generated/bridge'
import personalChatRequest from '../../../internal/app/testdata/personal_chat_project.json'
import { ensurePersonalProject, PERSONAL_CHAT_PROJECT_ID_KEY } from './appHelpers'

const personal: ProjectDTO = {
  id: '01ARZ3NDEKTSV4RRFFQ69G5FAV', name: personalChatRequest.name,
  projectCode: 'ITM00001', type: 'implementation', status: 'created',
  createdAt: '2026-01-01T00:00:00Z', updatedAt: '2026-01-01T00:00:00Z', version: 1,
}

function projects(items: ProjectDTO[]): ProjectBridge {
  return { list: vi.fn().mockResolvedValue({ items }), create: vi.fn().mockResolvedValue(personal),
    update: vi.fn(), publish: vi.fn(), close: vi.fn(), reopen: vi.fn(), advanceStatus: vi.fn(), delete: vi.fn() }
}

afterEach(() => localStorage.clear())

it('sends the name-only request exercised against the real Engine and SQLite', async () => {
  const bridge = projects([])
  await expect(ensurePersonalProject(bridge)).resolves.toEqual(personal)
  expect(bridge.create).toHaveBeenCalledWith(personalChatRequest, expect.objectContaining({
    attempt: expect.objectContaining({ payload: personalChatRequest }),
  }))
  expect(localStorage.getItem(PERSONAL_CHAT_PROJECT_ID_KEY)).toBe(personal.id)
})

it('rediscovers existing history after the browser pointer is lost without creating or deleting data', async () => {
  localStorage.setItem(PERSONAL_CHAT_PROJECT_ID_KEY, 'stale-browser-pointer')
  const bridge = projects([personal])
  await expect(ensurePersonalProject(bridge)).resolves.toEqual(personal)
  expect(bridge.create).not.toHaveBeenCalled()
  expect(bridge.delete).not.toHaveBeenCalled()
  expect(localStorage.getItem(PERSONAL_CHAT_PROJECT_ID_KEY)).toBe(personal.id)
})

it('preserves the browser pointer when creation fails', async () => {
  localStorage.setItem(PERSONAL_CHAT_PROJECT_ID_KEY, 'existing-pointer')
  const bridge = projects([])
  vi.mocked(bridge.create).mockRejectedValue(new Error('project type is required'))
  await expect(ensurePersonalProject(bridge)).rejects.toThrow('project type is required')
  expect(localStorage.getItem(PERSONAL_CHAT_PROJECT_ID_KEY)).toBe('existing-pointer')
  expect(bridge.delete).not.toHaveBeenCalled()
})
