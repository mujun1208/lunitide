import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterEach, expect, it, vi } from 'vitest'
import { LanguageProvider } from '../i18n/language'
import { AgentHubAskBar } from './AgentHubAskBar'
import { agentHubApi } from './agentHubApi'

vi.mock('./agentHubApi', () => ({
  agentHubApi: { threadRespond: vi.fn() },
}))

afterEach(() => {
  cleanup()
  vi.clearAllMocks()
})

it('calls threadRespond with the clicked option id and call id', async () => {
  vi.mocked(agentHubApi.threadRespond).mockResolvedValue({
    thread: {
      threadId: '01ARZ3NDEKTSV4RRFFQ69G5FAE',
      harnessId: 'loopback',
      nativeSessionId: '',
      title: '新会话',
      pinned: false,
      workspaceRoot: 'C:/tmp',
      exportDir: '',
      scene: 'free',
      status: 'running',
      accessMode: 'approval',
      createdAt: '2026-09-13T00:00:00Z',
      updatedAt: '2026-09-13T00:00:00Z',
    },
    messages: [],
    events: [],
    files: [],
  })
  render(
    <LanguageProvider value="zh-CN">
      <AgentHubAskBar
        threadId="01ARZ3NDEKTSV4RRFFQ69G5FAE"
        prompt={{
          callId: '01ARZ3NDEKTSV4RRFFQ69G5FAF',
          prompt: '选哪个?',
          options: [{ id: '是', label: '是' }, { id: '否', label: '否' }],
          status: 'open',
        }}
      />
    </LanguageProvider>,
  )
  fireEvent.click(screen.getByRole('button', { name: '是' }))
  await waitFor(() => expect(agentHubApi.threadRespond).toHaveBeenCalledWith({
    threadId: '01ARZ3NDEKTSV4RRFFQ69G5FAE',
    callId: '01ARZ3NDEKTSV4RRFFQ69G5FAF',
    optionId: '是',
  }))
})

it('sends optional AskBar text only when filled', async () => {
  vi.mocked(agentHubApi.threadRespond).mockResolvedValue({
    thread: {
      threadId: '01ARZ3NDEKTSV4RRFFQ69G5FAE',
      harnessId: 'loopback',
      nativeSessionId: '',
      title: '新会话',
      pinned: false,
      workspaceRoot: 'C:/tmp',
      exportDir: '',
      scene: 'free',
      status: 'running',
      accessMode: 'approval',
      createdAt: '2026-09-13T00:00:00Z',
      updatedAt: '2026-09-13T00:00:00Z',
    },
    messages: [],
    events: [],
    files: [],
  })
  render(
    <LanguageProvider value="zh-CN">
      <AgentHubAskBar
        threadId="01ARZ3NDEKTSV4RRFFQ69G5FAE"
        prompt={{
          callId: '01ARZ3NDEKTSV4RRFFQ69G5FAF',
          prompt: '选哪个?',
          options: [{ id: '是', label: '是' }, { id: '否', label: '否' }],
          status: 'open',
        }}
      />
    </LanguageProvider>,
  )
  fireEvent.change(screen.getByLabelText('补充说明'), { target: { value: '补充一句' } })
  fireEvent.click(screen.getByRole('button', { name: '是' }))
  await waitFor(() => expect(agentHubApi.threadRespond).toHaveBeenCalledWith({
    threadId: '01ARZ3NDEKTSV4RRFFQ69G5FAE',
    callId: '01ARZ3NDEKTSV4RRFFQ69G5FAF',
    optionId: '是',
    text: '补充一句',
  }))
})
