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
    inbox: vi.fn(),
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
  const first = vi.mocked(agentHubApi.threadCreate).mock.calls[0]?.[0]
  expect(first?.accessMode === undefined || first?.accessMode === 'approval').toBe(true)
})

it('explains auto-edit and full-access chips', async () => {
  stubHome()
  render(<LanguageProvider value="zh-CN"><AgentHubHome /></LanguageProvider>)
  fireEvent.click(await screen.findByRole('button', { name: '写项目' }))
  fireEvent.click(screen.getByRole('button', { name: '自动' }))
  expect(screen.getByText('自动会放过改文件权限，执行和联网仍要你点。业务选项永远要人点。')).toBeInTheDocument()
  fireEvent.click(screen.getByRole('button', { name: '完全访问' }))
  expect(screen.getByText('完全访问会自动放过该 CLI 的工具权限，并可能使用你本机已配的 MCP。业务选项仍要你点。')).toBeInTheDocument()
})

it('passes title, exportDir, and accessMode from Home', async () => {
  stubHome()
  vi.mocked(agentHubApi.pickDir)
    .mockResolvedValueOnce({ canceled: false, path: 'E:/proj' })
    .mockResolvedValueOnce({ canceled: false, path: 'E:/export' })
  vi.mocked(agentHubApi.threadCreate).mockResolvedValue({
    thread: {
      threadId: '01ARZ3NDEKTSV4RRFFQ69G5FAE',
      harnessId: 'cursor',
      nativeSessionId: '',
      title: '写一个 CLI',
      pinned: false,
      workspaceRoot: 'E:/proj',
      exportDir: 'E:/export',
      scene: 'write_project',
      status: 'idle',
      accessMode: 'full-access',
      createdAt: '2026-09-13T00:00:00Z',
      updatedAt: '2026-09-13T00:00:00Z',
    },
    messages: [],
    events: [],
    files: [],
  })
  render(<LanguageProvider value="zh-CN"><AgentHubHome /></LanguageProvider>)
  fireEvent.click(await screen.findByRole('button', { name: '写项目' }))
  fireEvent.click(screen.getByRole('button', { name: '选择文件夹' }))
  expect(await screen.findByRole('button', { name: 'E:/proj' })).toBeInTheDocument()
  fireEvent.click(screen.getByRole('button', { name: '导出目录' }))
  expect(await screen.findByRole('button', { name: 'E:/export' })).toBeInTheDocument()
  fireEvent.click(screen.getByRole('button', { name: '完全访问' }))
  fireEvent.change(screen.getByLabelText('任务说明'), { target: { value: '写一个 CLI' } })
  fireEvent.click(screen.getByRole('button', { name: '执行' }))
  await waitFor(() => expect(agentHubApi.threadCreate).toHaveBeenCalledWith(expect.objectContaining({
    harnessId: 'cursor',
    scene: 'write_project',
    workspaceRoot: 'E:/proj',
    exportDir: 'E:/export',
    title: '写一个 CLI',
    accessMode: 'full-access',
  })))
})

it('lets the user override the default harness and shows the Codex one-shot hint', async () => {
  vi.mocked(agentHubApi.detect).mockResolvedValue({
    agents: [
      { name: 'cursor', state: 'available', version: '1', nonInteractive: true, streamJSON: true, hint: '可用', interactive: true, protocol: 'acp' },
      { name: 'codex', state: 'available', version: '1', nonInteractive: true, streamJSON: true, hint: '当前只能一把跑完，不能中途提问', interactive: false, protocol: 'exec' },
    ],
  })
  vi.mocked(agentHubApi.pickDir).mockResolvedValue({ canceled: false, path: 'E:/proj' })
  vi.mocked(agentHubApi.threadCreate).mockResolvedValue({
    thread: {
      threadId: '01ARZ3NDEKTSV4RRFFQ69G5FAE',
      harnessId: 'codex',
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
  expect(await screen.findByRole('button', { name: 'cursor' })).toBeEnabled()
  expect(screen.queryByText('可用')).toBeNull()
  fireEvent.click(screen.getByRole('button', { name: 'codex' }))
  expect(screen.getByText('当前只能一把跑完，不能中途提问')).toBeInTheDocument()
  fireEvent.click(screen.getByRole('button', { name: '选择文件夹' }))
  expect(await screen.findByRole('button', { name: 'E:/proj' })).toBeInTheDocument()
  fireEvent.click(screen.getByRole('button', { name: '执行' }))
  await waitFor(() => expect(agentHubApi.threadCreate).toHaveBeenCalledWith(expect.objectContaining({
    harnessId: 'codex',
    scene: 'write_project',
  })))
})

it('copies inbox files into the chosen workspace and clips the title', async () => {
  stubHome()
  vi.mocked(agentHubApi.pickDir).mockResolvedValue({ canceled: false, path: 'E:/proj' })
  vi.mocked(agentHubApi.inbox).mockResolvedValue({ canceled: false, workDir: 'E:/proj', files: [{ name: 'a.md', path: 'a.md', size: 1 }] })
  vi.mocked(agentHubApi.threadCreate).mockResolvedValue({
    thread: {
      threadId: '01ARZ3NDEKTSV4RRFFQ69G5FAE',
      harnessId: 'cursor',
      nativeSessionId: '',
      title: '第一行',
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
  fireEvent.click(screen.getByRole('button', { name: '选择文件夹' }))
  fireEvent.click(await screen.findByRole('button', { name: '添加文件' }))
  await waitFor(() => expect(agentHubApi.inbox).toHaveBeenCalledWith({ action: 'files', workDir: 'E:/proj' }))
  fireEvent.change(screen.getByLabelText('任务说明'), { target: { value: '第一行\n第二行' } })
  fireEvent.click(screen.getByRole('button', { name: '执行' }))
  await waitFor(() => expect(agentHubApi.threadCreate).toHaveBeenCalledWith(expect.objectContaining({
    title: '第一行',
  })))
})

it('does not start a thread when the default harness is not available', async () => {
  vi.mocked(agentHubApi.detect).mockResolvedValue({
    agents: [
      { name: 'cursor', state: 'not_installed', version: '', nonInteractive: true, streamJSON: true, hint: '未安装 Cursor CLI', interactive: true, protocol: 'acp' },
      { name: 'codex', state: 'available', version: '1', nonInteractive: true, streamJSON: true, hint: '可用', interactive: false, protocol: 'exec' },
    ],
  })
  vi.mocked(agentHubApi.pickDir).mockResolvedValue({ canceled: false, path: 'E:/proj' })
  render(<LanguageProvider value="zh-CN"><AgentHubHome /></LanguageProvider>)
  fireEvent.click(await screen.findByRole('button', { name: '写项目' }))
  expect(await screen.findByRole('button', { name: 'cursor' })).toBeDisabled()
  expect(screen.getByText('未安装 Cursor CLI')).toBeInTheDocument()
  fireEvent.click(screen.getByRole('button', { name: '选择文件夹' }))
  expect(await screen.findByRole('button', { name: 'E:/proj' })).toBeInTheDocument()
  fireEvent.click(screen.getByRole('button', { name: '执行' }))
  expect(screen.getByRole('alert')).toHaveTextContent('当前 Agent 不可用')
  expect(agentHubApi.threadCreate).not.toHaveBeenCalled()
})
