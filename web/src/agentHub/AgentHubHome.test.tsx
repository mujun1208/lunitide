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
  localStorage.removeItem('lunitide:agent-hub-workdir:docs')
  localStorage.removeItem('lunitide:agent-hub-workdir:free')
  vi.clearAllMocks()
})

function availableAgents() {
  return [
    { name: 'codex', state: 'available', version: '1', nonInteractive: true, streamJSON: true, hint: '可用', interactive: true, protocol: 'app-server' },
    { name: 'cursor', state: 'available', version: '1', nonInteractive: true, streamJSON: true, hint: '可用', interactive: true, protocol: 'acp' },
    { name: 'kimi', state: 'available', version: '1', nonInteractive: true, streamJSON: true, hint: '可用', interactive: true, protocol: 'acp' },
  ] as const
}

function stubHome() {
  vi.mocked(agentHubApi.detect).mockResolvedValue({ agents: [...availableAgents()] })
}

function chooseProject() {
  fireEvent.change(screen.getByLabelText('任务类型'), { target: { value: 'write' } })
}

function openPlus() {
  fireEvent.click(screen.getByRole('button', { name: '添加上下文' }))
}

it('shows 请先选择项目目录 and does not create a 项目 thread without a folder', async () => {
  stubHome()
  render(<LanguageProvider value="zh-CN"><AgentHubHome selectedAgent="cursor" /></LanguageProvider>)
  await screen.findByLabelText('任务类型')
  chooseProject()
  fireEvent.change(screen.getByLabelText('任务说明'), { target: { value: '开始' } })
  fireEvent.click(screen.getByRole('button', { name: '发送' }))
  expect(screen.getByRole('alert')).toHaveTextContent('请先选择项目目录')
  expect(agentHubApi.threadCreate).not.toHaveBeenCalled()
})

it('does not create a thread before detect finishes', async () => {
  vi.mocked(agentHubApi.detect).mockReturnValue(new Promise(() => undefined))
  vi.mocked(agentHubApi.pickDir).mockResolvedValue({ canceled: false, path: 'E:/proj' })
  render(<LanguageProvider value="zh-CN"><AgentHubHome selectedAgent="cursor" /></LanguageProvider>)
  chooseProject()
  openPlus()
  fireEvent.click(screen.getByRole('button', { name: /项目目录/ }))
  expect(await screen.findByText(/E:\/proj/)).toBeInTheDocument()
  fireEvent.change(screen.getByLabelText('任务说明'), { target: { value: '开始' } })
  expect(screen.getByRole('button', { name: '发送' })).toBeDisabled()
  expect(agentHubApi.threadCreate).not.toHaveBeenCalled()
})

it('offers 写周报 Markdown and never 生成周报', async () => {
  stubHome()
  render(<LanguageProvider value="zh-CN"><AgentHubHome selectedAgent="cursor" /></LanguageProvider>)
  await screen.findByLabelText('任务类型')
  expect(screen.getByRole('button', { name: '写周报 Markdown' })).toBeInTheDocument()
  expect(screen.queryByRole('button', { name: '生成周报' })).toBeNull()
  fireEvent.click(screen.getByRole('button', { name: '写周报 Markdown' }))
  expect(screen.getByLabelText('任务说明')).toHaveValue('根据本工作目录材料写一份周报 Markdown，不要生成 Office 文档。')
})

it('publishes the composer default agent when the header has not chosen one', async () => {
  stubHome()
  const onSelectAgent = vi.fn()
  render(<LanguageProvider value="zh-CN"><AgentHubHome onSelectAgent={onSelectAgent} /></LanguageProvider>)
  await waitFor(() => expect(onSelectAgent).toHaveBeenCalledWith('cursor'))
})

it('hides the weekly Markdown chip on the PPT scene', async () => {
  stubHome()
  render(<LanguageProvider value="zh-CN"><AgentHubHome selectedAgent="kimi" /></LanguageProvider>)
  await screen.findByLabelText('任务类型')
  fireEvent.change(screen.getByLabelText('任务类型'), { target: { value: 'ppt' } })
  expect(screen.queryByRole('button', { name: '写周报 Markdown' })).toBeNull()
})

it('says the thread page is 月汐 UI and later CLIs stay off the list', async () => {
  stubHome()
  render(<LanguageProvider value="zh-CN"><AgentHubHome selectedAgent="cursor" /></LanguageProvider>)
  expect(await screen.findByText('对话页就是月汐自己的界面，不嵌官方窗口。未接入的 CLI 不会出现。')).toBeInTheDocument()
  expect(screen.getByLabelText('任务类型')).toBeInTheDocument()
  expect(screen.queryByRole('button', { name: 'pi' })).toBeNull()
  expect(screen.queryByRole('button', { name: 'claude' })).toBeNull()
})

it('maps 项目 to write_project after a folder is chosen', async () => {
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
  render(<LanguageProvider value="zh-CN"><AgentHubHome selectedAgent="cursor" /></LanguageProvider>)
  await screen.findByLabelText('任务类型')
  chooseProject()
  expect(screen.getByText('在你选的文件夹里按你的规则创建子目录并写文件。不要把已有文件挪到别处。')).toBeInTheDocument()
  openPlus()
  fireEvent.click(screen.getByRole('button', { name: /项目目录/ }))
  expect(await screen.findByText(/E:\/proj/)).toBeInTheDocument()
  fireEvent.change(screen.getByLabelText('任务说明'), { target: { value: '开始' } })
  fireEvent.click(screen.getByRole('button', { name: '发送' }))
  await waitFor(() => expect(agentHubApi.threadCreate).toHaveBeenCalledWith(expect.objectContaining({
    harnessId: 'cursor',
    scene: 'write_project',
    workspaceRoot: 'E:/proj',
  })))
  const first = vi.mocked(agentHubApi.threadCreate).mock.calls[0]?.[0]
  expect(first?.accessMode === undefined || first?.accessMode === 'approval').toBe(true)
})

it('explains auto-edit and full-access from the permission dropdown', async () => {
  stubHome()
  render(<LanguageProvider value="zh-CN"><AgentHubHome /></LanguageProvider>)
  await screen.findByLabelText('权限')
  fireEvent.change(screen.getByLabelText('权限'), { target: { value: 'auto-edit' } })
  expect(screen.getByText('自动会放过改文件权限，执行和联网仍要你点。业务选项永远要人点。')).toBeInTheDocument()
  fireEvent.change(screen.getByLabelText('权限'), { target: { value: 'full-access' } })
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
  render(<LanguageProvider value="zh-CN"><AgentHubHome selectedAgent="cursor" /></LanguageProvider>)
  await screen.findByLabelText('任务类型')
  chooseProject()
  openPlus()
  fireEvent.click(screen.getByRole('button', { name: /项目目录/ }))
  expect(await screen.findByText(/E:\/proj/)).toBeInTheDocument()
  openPlus()
  fireEvent.click(screen.getByRole('button', { name: /产物目录/ }))
  expect(await screen.findByText(/E:\/export/)).toBeInTheDocument()
  fireEvent.change(screen.getByLabelText('权限'), { target: { value: 'full-access' } })
  fireEvent.change(screen.getByLabelText('任务说明'), { target: { value: '写一个 CLI' } })
  fireEvent.click(screen.getByRole('button', { name: '发送' }))
  await waitFor(() => expect(agentHubApi.threadCreate).toHaveBeenCalledWith(expect.objectContaining({
    harnessId: 'cursor',
    scene: 'write_project',
    workspaceRoot: 'E:/proj',
    exportDir: 'E:/export',
    title: '写一个 CLI',
    accessMode: 'full-access',
  })))
})

it('uses the selected harness and shows the Codex one-shot hint', async () => {
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
  render(<LanguageProvider value="zh-CN"><AgentHubHome selectedAgent="codex" /></LanguageProvider>)
  expect(await screen.findByText('当前只能一把跑完，不能中途提问')).toBeInTheDocument()
  chooseProject()
  openPlus()
  fireEvent.click(screen.getByRole('button', { name: /项目目录/ }))
  expect(await screen.findByText(/E:\/proj/)).toBeInTheDocument()
  fireEvent.change(screen.getByLabelText('任务说明'), { target: { value: '开始' } })
  fireEvent.click(screen.getByRole('button', { name: '发送' }))
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
  render(<LanguageProvider value="zh-CN"><AgentHubHome selectedAgent="cursor" /></LanguageProvider>)
  await screen.findByLabelText('任务类型')
  chooseProject()
  openPlus()
  fireEvent.click(screen.getByRole('button', { name: /项目目录/ }))
  expect(await screen.findByText(/E:\/proj/)).toBeInTheDocument()
  openPlus()
  fireEvent.click(screen.getByRole('button', { name: /附件/ }))
  await waitFor(() => expect(agentHubApi.inbox).toHaveBeenCalledWith({ action: 'files', workDir: 'E:/proj' }))
  expect(await screen.findByText(/a\.md/)).toBeInTheDocument()
  fireEvent.change(screen.getByLabelText('任务说明'), { target: { value: '第一行\n第二行' } })
  fireEvent.click(screen.getByRole('button', { name: '发送' }))
  await waitFor(() => expect(agentHubApi.threadCreate).toHaveBeenCalledWith(expect.objectContaining({
    title: '第一行',
  })))
  await waitFor(() => expect(agentHubApi.threadPrompt).toHaveBeenCalledWith(expect.objectContaining({
    text: expect.stringContaining('.agenthub-inbox'),
  })))
})

it('does not start a thread when the selected harness is not available', async () => {
  vi.mocked(agentHubApi.detect).mockResolvedValue({
    agents: [
      { name: 'cursor', state: 'not_installed', version: '', nonInteractive: true, streamJSON: true, hint: '未安装 Cursor CLI', interactive: true, protocol: 'acp' },
      { name: 'codex', state: 'available', version: '1', nonInteractive: true, streamJSON: true, hint: '可用', interactive: false, protocol: 'exec' },
    ],
  })
  vi.mocked(agentHubApi.pickDir).mockResolvedValue({ canceled: false, path: 'E:/proj' })
  render(<LanguageProvider value="zh-CN"><AgentHubHome selectedAgent="cursor" /></LanguageProvider>)
  expect(await screen.findByText('未安装 Cursor CLI')).toBeInTheDocument()
  chooseProject()
  openPlus()
  fireEvent.click(screen.getByRole('button', { name: /项目目录/ }))
  expect(await screen.findByText(/E:\/proj/)).toBeInTheDocument()
  fireEvent.change(screen.getByLabelText('任务说明'), { target: { value: '开始' } })
  fireEvent.click(screen.getByRole('button', { name: '发送' }))
  expect(screen.getByRole('alert')).toHaveTextContent('当前 Agent 不可用')
  expect(agentHubApi.threadCreate).not.toHaveBeenCalled()
})
