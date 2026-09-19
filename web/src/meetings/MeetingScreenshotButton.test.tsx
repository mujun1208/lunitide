import { cleanup, render, screen } from '@testing-library/react'
import { afterEach, expect, it } from 'vitest'
import type { CapabilityRolesBridge } from '../bridge/client'
import { MeetingScreenshotButton } from './MeetingScreenshotButton'

afterEach(() => cleanup())

const roles = (vision: boolean): CapabilityRolesBridge => ({
  get: async () => ({
    roles: (['chat', 'flash', 'vision', 'embed', 'judge', 'gui'] as const).map(role => ({
      role,
      allowJudgeEqChat: false,
      ...(role === 'vision' && vision ? { providerId: '01ARZ3NDEKTSV4RRFFQ69G5FAV', modelId: 'vision' } : {}),
    })),
    revision: 'a'.repeat(64),
    appliedRevision: 'a'.repeat(64),
    state: 'applied',
  }),
  set: async () => ({ roles: [], revision: 'a'.repeat(64), appliedRevision: 'a'.repeat(64), state: 'applied' }),
})

it('keeps 会议截图 disabled when vision is not configured', async () => {
  render(<MeetingScreenshotButton roles={roles(false)} />)
  expect(await screen.findByRole('button', { name: '会议截图' })).toBeDisabled()
  expect(screen.getByRole('button', { name: '会议截图' })).toHaveAttribute('title', '未配置视觉能力')
})

it('enables 会议截图 when vision is bound', async () => {
  render(<MeetingScreenshotButton roles={roles(true)} />)
  expect(await screen.findByRole('button', { name: '会议截图' })).toBeEnabled()
})
