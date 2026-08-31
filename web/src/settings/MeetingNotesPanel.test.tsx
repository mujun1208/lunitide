import { cleanup, render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, beforeEach, describe, expect, test, vi } from 'vitest'
import { MeetingNotesPanel } from './MeetingNotesPanel'

const now = '2026-08-31T03:00:00.000Z'

vi.mock('../bridge/client', async importOriginal => {
  const actual = await importOriginal<typeof import('../bridge/client')>()
  return {
    ...actual,
    getProviderBridge: () => ({
      list: () => Promise.resolve({
        items: [{
          id: '01ARZ3NDEKTSV4RRFFQ69G5FAW',
          name: 'Chat',
          protocol: 'openai_compatible',
          baseUrl: 'https://example.com',
          status: 'enabled',
          credentialState: 'configured',
          createdAt: now,
          updatedAt: now,
          version: 1,
          models: [{ modelId: 'glm-5.3', displayName: 'glm-5.3', isDefault: true, kind: 'llm', kindDefault: true }],
        }],
      }),
    }),
  }
})

describe('MeetingNotesPanel', () => {
  beforeEach(() => localStorage.clear())
  afterEach(cleanup)

  test('explains listen vs notes model and persists both', async () => {
    const user = userEvent.setup()
    render(<MeetingNotesPanel />)
    expect(await screen.findByRole('radiogroup', { name: '实时听写' })).toBeInTheDocument()
    expect(screen.getByRole('radio', { name: /^系统/ })).toHaveAttribute('aria-checked', 'true')
    expect(screen.getByText(/只负责开会时的实时字幕/)).toBeInTheDocument()
    expect(screen.getByText(/整理成「会议摘要」和「决议\/待办」/)).toBeInTheDocument()
    await user.click(screen.getByRole('radio', { name: /^本机/ }))
    await user.selectOptions(await screen.findByLabelText('纪要模型'), 'glm-5.3')
    expect(JSON.parse(localStorage.getItem('lunitide:meeting') || '{}')).toEqual({ listen: 'local', modelId: 'glm-5.3' })
  })
})
