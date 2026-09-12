import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterEach, expect, it, vi } from 'vitest'
import { LanguageProvider } from '../i18n/language'
import { AgentHubHome } from './AgentHubHome'
import { agentHubApi } from './agentHubApi'

vi.mock('./agentHubApi', () => ({
  agentHubApi: {
    detect: vi.fn(),
    pickDir: vi.fn(),
    threadCreate: vi.fn(),
    threadPrompt: vi.fn(),
  },
}))

afterEach(() => {
  cleanup()
  localStorage.removeItem('lunitide:agent-hub-scene')
  localStorage.removeItem('lunitide:agent-hub-workdir:write')
  localStorage.removeItem('lunitide:agent-hub-workdir:fix')
  localStorage.removeItem('lunitide:agent-hub-workdir:ppt')
  localStorage.removeItem('lunitide:agent-hub-workdir:free')
  vi.clearAllMocks()
})

function stubHome() {
  vi.mocked(agentHubApi.detect).mockResolvedValue({ agents: [] })
}

it('shows 请先选择项目目录 and does not create a 写项目 thread without a folder', async () => {
  stubHome()
  render(<LanguageProvider value="zh-CN"><AgentHubHome /></LanguageProvider>)
  fireEvent.click(await screen.findByRole('button', { name: '写项目' }))
  fireEvent.click(screen.getByRole('button', { name: '执行' }))
  expect(screen.getByRole('alert')).toHaveTextContent('请先选择项目目录')
  expect(agentHubApi.threadCreate).not.toHaveBeenCalled()
})

it('maps 写项目 to write_project after a folder is chosen', async () => {
  stubHome()
  vi.mocked(agentHubApi.pickDir).mockResolvedValue({ canceled: false, path: 'E:/proj' })
  vi.mocked(agentHubApi.threadCreate).mockResolvedValue({
    thread: {
      threadId: '01ARZ3NDEKTSV4RRFFQ69G5FAE',
      harnessId: 'cursor',
      nativeSessionId: '',
      title: '新会话',
      pinned: false,
      workspaceRoot: 'E:/proj',
      exportDir: '',
      scene: 'write_project',
      status: 'idle',
      accessMode: 'approval',
      createdAt: '2026-09-13T00:00:00Z',
      updatedAt: '2026-09-13T00:00:00Z',
    },
    messages: [],
    events: [],
    files: [],
  })
  render(<LanguageProvider value="zh-CN"><AgentHubHome /></LanguageProvider>)
  fireEvent.click(await screen.findByRole('button', { name: '写项目' }))
  expect(screen.getByText('在你选的文件夹里按你的规则创建子目录并写文件。不要把已有文件挪到别处。')).toBeInTheDocument()
  fireEvent.click(screen.getByRole('button', { name: '选择文件夹' }))
  expect(await screen.findByRole('button', { name: 'E:/proj' })).toBeInTheDocument()
  fireEvent.click(screen.getByRole('button', { name: '执行' }))
  await waitFor(() => expect(agentHubApi.threadCreate).toHaveBeenCalledWith(expect.objectContaining({
    harnessId: 'cursor',
    scene: 'write_project',
    workspaceRoot: 'E:/proj',
  })))
})
