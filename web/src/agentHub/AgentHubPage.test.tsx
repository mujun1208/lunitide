import { act, cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterEach, expect, it, vi } from 'vitest'
import { LanguageProvider } from '../i18n/language'
import { AgentHubPage } from './AgentHubPage'
import { agentHubApi } from './agentHubApi'

vi.mock('./agentHubApi', () => ({
  agentHubApi: {
    detect: vi.fn(),
    pickDir: vi.fn(),
    inbox: vi.fn(),
    list: vi.fn(),
    listArtifacts: vi.fn(),
    start: vi.fn(),
    get: vi.fn(),
    cancel: vi.fn(),
    preview: vi.fn(),
    open: vi.fn(),
    threadList: vi.fn(),
    threadGet: vi.fn(),
    threadCreate: vi.fn(),
    threadUpdate: vi.fn(),
    threadPrompt: vi.fn(),
    workspaceList: vi.fn(),
  },
}))

afterEach(() => {
  cleanup()
  vi.useRealTimers()
  localStorage.removeItem('lunitide:agent-hub-scene')
  localStorage.removeItem('lunitide:agent-hub-workdir')
  localStorage.removeItem('lunitide:agent-hub-workdir:ppt')
  localStorage.removeItem('lunitide:agent-hub-workdir:write')
  localStorage.removeItem('lunitide:agent-hub-workdir:fix')
  localStorage.removeItem('lunitide:agent-hub-workdir:free')
  vi.clearAllMocks()
  vi.restoreAllMocks()
})

function startedFixture(agent: string, prompt: string) {
  return {
    task: {
      taskId: '01ARZ3NDEKTSV4RRFFQ69G5FAE',
      agent,
      prompt,
      workDir: 'C:/tmp',
      sandbox: agent === 'codex' ? 'workspace-write' : '',
      status: 'queued',
      tokensUsed: 0,
      errorMsg: '',
      createdAt: '2026-09-12T00:00:00Z',
    },
    events: [],
    artifacts: [],
  }
}

function stubLists() {
  vi.mocked(agentHubApi.detect).mockResolvedValue({
    agents: [
      { name: 'codex', state: 'available', version: '1.2.3', nonInteractive: true, streamJSON: true, hint: '可用' },
      { name: 'cursor', state: 'not_installed', version: '', nonInteractive: true, streamJSON: true, hint: '未安装 Cursor CLI' },
      { name: 'kimi', state: 'not_installed', version: '', nonInteractive: true, streamJSON: true, hint: '未安装 Kimi Code CLI（kimi）。安装后重新打开调度台。' },
    ],
  })
  vi.mocked(agentHubApi.list).mockResolvedValue({ items: [], counts: { queued: 0, running: 0, success: 0, failed: 0 } })
  vi.mocked(agentHubApi.listArtifacts).mockResolvedValue({ items: [] })
  vi.mocked(agentHubApi.inbox).mockResolvedValue({ canceled: false, workDir: '', files: [] })
  vi.mocked(agentHubApi.threadList).mockResolvedValue({ items: [] })
  vi.mocked(agentHubApi.workspaceList).mockResolvedValue({ items: [] })
}

function openLegacy() {
  fireEvent.click(screen.getByRole('button', { name: '旧版任务' }))
}

it('shows a probe hint before detect returns', async () => {
  let finish: (value: { agents: [] }) => void = () => {}
  vi.mocked(agentHubApi.detect).mockReturnValue(new Promise(resolve => { finish = resolve }))
  vi.mocked(agentHubApi.list).mockResolvedValue({ items: [], counts: { queued: 0, running: 0, success: 0, failed: 0 } })
  vi.mocked(agentHubApi.listArtifacts).mockResolvedValue({ items: [] })
  render(<LanguageProvider value="zh-CN"><AgentHubPage /></LanguageProvider>)
  expect(await screen.findByText('正在探测本机 CLI…')).toBeInTheDocument()
  finish({ agents: [] })
})

it('shortens work dirs in the task list', async () => {
  stubLists()
  vi.mocked(agentHubApi.list).mockResolvedValue({
    items: [{
      taskId: '01ARZ3NDEKTSV4RRFFQ69G5FAE',
      agent: 'codex',
      prompt: '写周报 Markdown',
      workDir: 'E:/Trae-Work-Projects/lunitide',
      sandbox: 'workspace-write',
      status: 'success',
      tokensUsed: 0,
      errorMsg: '',
      createdAt: '2026-09-12T00:00:00Z',
    }],
    counts: { queued: 0, running: 0, success: 1, failed: 0 },
  })
  render(<LanguageProvider value="zh-CN"><AgentHubPage /></LanguageProvider>)
  await openLegacy()
  fireEvent.click(await screen.findByRole('tab', { name: '任务中心' }))
  expect(await screen.findByText('codex · Trae-Work-Projects/lunitide')).toBeInTheDocument()
})

it('renders Home by default and opens 旧版任务 instead of the four tabs', async () => {
  stubLists()
  render(<LanguageProvider value="zh-CN"><AgentHubPage /></LanguageProvider>)
  expect(await screen.findByRole('heading', { name: 'Agent 调度台' })).toBeInTheDocument()
  expect(screen.getByText('消耗的是该 CLI 自己的会员额度')).toBeInTheDocument()
  expect(screen.getByRole('button', { name: '写项目' })).toBeInTheDocument()
  expect(screen.getByRole('button', { name: '改代码' })).toBeInTheDocument()
  expect(screen.getByRole('button', { name: '做 PPT' })).toBeInTheDocument()
  expect(screen.getByRole('button', { name: '自由' })).toBeInTheDocument()
  expect(screen.getByRole('button', { name: '旧版任务' })).toBeInTheDocument()
  expect(screen.queryByText('生成周报')).toBeNull()
  expect(screen.queryByText(/Pro 已登录/)).toBeNull()
  expect(screen.queryByText(/AI 军团/)).toBeNull()
  for (const name of ['工作台', '任务中心', '任务详情', '产物中心']) {
    expect(screen.queryByRole('tab', { name })).toBeNull()
  }
  await openLegacy()
  for (const name of ['工作台', '任务中心', '任务详情', '产物中心']) {
    expect(screen.getByRole('tab', { name })).toBeInTheDocument()
  }
})

it('does not re-detect every 400ms while a task is live', async () => {
  vi.useFakeTimers()
  stubLists()
  const live = {
    taskId: '01ARZ3NDEKTSV4RRFFQ69G5FAE',
    agent: 'codex',
    prompt: '写周报 Markdown',
    workDir: 'C:/tmp',
    sandbox: 'workspace-write',
    status: 'running',
    tokensUsed: 0,
    errorMsg: '',
    createdAt: '2026-09-12T00:00:00Z',
  }
  vi.mocked(agentHubApi.list).mockResolvedValue({ items: [live], counts: { queued: 0, running: 1, success: 0, failed: 0 } })
  render(<LanguageProvider value="zh-CN"><AgentHubPage /></LanguageProvider>)
  await act(async () => { await vi.advanceTimersByTimeAsync(0) })
  const detectAtStart = vi.mocked(agentHubApi.detect).mock.calls.length
  const listAtStart = vi.mocked(agentHubApi.list).mock.calls.length
  await act(async () => { await vi.advanceTimersByTimeAsync(1600) })
  expect(vi.mocked(agentHubApi.detect).mock.calls.length).toBe(detectAtStart)
  expect(vi.mocked(agentHubApi.list).mock.calls.length).toBeGreaterThan(listAtStart)
  vi.useRealTimers()
})

it('shows a finish banner when a live workbench task completes', async () => {
  vi.useFakeTimers()
  stubLists()
  const live = {
    taskId: '01ARZ3NDEKTSV4RRFFQ69G5FAE',
    agent: 'codex',
    prompt: '写周报 Markdown',
    workDir: 'C:/tmp',
    sandbox: 'workspace-write',
    status: 'running',
    tokensUsed: 0,
    errorMsg: '',
    createdAt: '2026-09-12T00:00:00Z',
  }
  vi.mocked(agentHubApi.list)
    .mockResolvedValueOnce({ items: [live], counts: { queued: 0, running: 1, success: 0, failed: 0 } })
    .mockResolvedValue({ items: [{ ...live, status: 'success' }], counts: { queued: 0, running: 0, success: 1, failed: 0 } })
  render(<LanguageProvider value="zh-CN"><AgentHubPage /></LanguageProvider>)
  await act(async () => { await vi.advanceTimersByTimeAsync(0) })
  await openLegacy()
  expect(screen.getByText('进行中')).toBeInTheDocument()
  await act(async () => { await vi.advanceTimersByTimeAsync(400) })
  expect(screen.getByText('任务已结束')).toBeInTheDocument()
  vi.useRealTimers()
})

it('starts a cursor task from an available adapter', async () => {
  vi.mocked(agentHubApi.detect).mockResolvedValue({
    agents: [
      { name: 'codex', state: 'not_installed', version: '', nonInteractive: true, streamJSON: true, hint: '未安装' },
      { name: 'cursor', state: 'available', version: '2026.09.10-fd3934a', nonInteractive: true, streamJSON: true, hint: '可用' },
      { name: 'kimi', state: 'not_installed', version: '', nonInteractive: true, streamJSON: true, hint: '未安装' },
    ],
  })
  vi.mocked(agentHubApi.list).mockResolvedValue({ items: [], counts: { queued: 0, running: 0, success: 0, failed: 0 } })
  vi.mocked(agentHubApi.listArtifacts).mockResolvedValue({ items: [] })
  vi.mocked(agentHubApi.inbox).mockResolvedValue({ canceled: false, workDir: '', files: [] })
  const started = startedFixture('cursor', '写 hello.txt')
  vi.mocked(agentHubApi.start).mockResolvedValue(started)
  vi.mocked(agentHubApi.get).mockResolvedValue(started)
  render(<LanguageProvider value="zh-CN"><AgentHubPage /></LanguageProvider>)
  await openLegacy()
  fireEvent.click(await screen.findByRole('button', { name: /其它任务/ }))
  fireEvent.click(await screen.findByRole('button', { name: /Cursor 可用/ }))
  fireEvent.change(screen.getByLabelText('任务说明'), { target: { value: '写 hello.txt' } })
  fireEvent.click(screen.getByRole('button', { name: '执行' }))
  await waitFor(() => expect(agentHubApi.start).toHaveBeenCalledWith(expect.objectContaining({ agent: 'cursor', prompt: '写 hello.txt' })))
})

it('starts a kimi task from an available adapter', async () => {
  vi.mocked(agentHubApi.detect).mockResolvedValue({
    agents: [
      { name: 'codex', state: 'not_installed', version: '', nonInteractive: true, streamJSON: true, hint: '未安装' },
      { name: 'cursor', state: 'not_installed', version: '', nonInteractive: true, streamJSON: true, hint: '未安装' },
      { name: 'kimi', state: 'available', version: '0.42.0', nonInteractive: true, streamJSON: true, hint: '可用' },
    ],
  })
  vi.mocked(agentHubApi.list).mockResolvedValue({ items: [], counts: { queued: 0, running: 0, success: 0, failed: 0 } })
  vi.mocked(agentHubApi.listArtifacts).mockResolvedValue({ items: [] })
  vi.mocked(agentHubApi.inbox).mockResolvedValue({ canceled: false, workDir: '', files: [] })
  const started = startedFixture('kimi', '写 hello.txt')
  vi.mocked(agentHubApi.start).mockResolvedValue(started)
  vi.mocked(agentHubApi.get).mockResolvedValue(started)
  render(<LanguageProvider value="zh-CN"><AgentHubPage /></LanguageProvider>)
  await openLegacy()
  fireEvent.click(await screen.findByRole('button', { name: /其它任务/ }))
  fireEvent.click(await screen.findByRole('button', { name: /Kimi 可用/ }))
  fireEvent.change(screen.getByLabelText('任务说明'), { target: { value: '写 hello.txt' } })
  fireEvent.click(screen.getByRole('button', { name: '执行' }))
  await waitFor(() => expect(agentHubApi.start).toHaveBeenCalledWith(expect.objectContaining({ agent: 'kimi', prompt: '写 hello.txt' })))
})

it('starts a task from an available adapter', async () => {
  stubLists()
  const started = startedFixture('codex', '写周报 Markdown')
  vi.mocked(agentHubApi.start).mockResolvedValue(started)
  vi.mocked(agentHubApi.get).mockResolvedValue(started)
  render(<LanguageProvider value="zh-CN"><AgentHubPage /></LanguageProvider>)
  await openLegacy()
  fireEvent.click(await screen.findByRole('button', { name: /其它任务/ }))
  fireEvent.change(screen.getByLabelText('任务说明'), { target: { value: '写周报 Markdown' } })
  fireEvent.click(screen.getByRole('button', { name: '执行' }))
  expect(await screen.findByRole('heading', { level: 2, name: '写周报 Markdown' })).toBeInTheDocument()
  expect(agentHubApi.start).toHaveBeenCalledWith(expect.objectContaining({
    agent: 'codex',
    prompt: expect.not.stringContaining('【场景：'),
  }))
})

it('sends a picked work directory with the task', async () => {
  stubLists()
  vi.mocked(agentHubApi.pickDir).mockResolvedValue({ canceled: false, path: 'E:/repo' })
  vi.stubGlobal('confirm', () => true)
  const started = startedFixture('codex', 'hi')
  vi.mocked(agentHubApi.start).mockResolvedValue(started)
  vi.mocked(agentHubApi.get).mockResolvedValue(started)
  render(<LanguageProvider value="zh-CN"><AgentHubPage /></LanguageProvider>)
  await openLegacy()
  fireEvent.click(await screen.findByRole('button', { name: /其它任务/ }))
  fireEvent.click(screen.getByRole('button', { name: '工作目录' }))
  await waitFor(() => expect(screen.getByRole('button', { name: '工作目录' })).toHaveTextContent('repo'))
  fireEvent.change(screen.getByLabelText('任务说明'), { target: { value: 'hi' } })
  fireEvent.click(screen.getByRole('button', { name: '执行' }))
  expect(agentHubApi.start).toHaveBeenCalledWith(expect.objectContaining({ workDir: 'E:/repo' }))
})

it('other-task starts without a directory and without a scene prefix', async () => {
  stubLists()
  vi.mocked(agentHubApi.start).mockResolvedValue(startedFixture('codex', '写说明'))
  render(<LanguageProvider value="zh-CN"><AgentHubPage /></LanguageProvider>)
  await openLegacy()
  fireEvent.click(await screen.findByRole('button', { name: /其它任务/ }))
  fireEvent.change(screen.getByLabelText('任务说明'), { target: { value: '写说明' } })
  fireEvent.click(screen.getByRole('button', { name: '执行' }))
  await waitFor(() => expect(agentHubApi.start).toHaveBeenCalledWith(expect.objectContaining({
    agent: 'codex',
    prompt: expect.not.stringContaining('【场景：'),
  })))
})

it('ppt shortcut does not start without a work directory', async () => {
  stubLists()
  vi.mocked(agentHubApi.detect).mockResolvedValue({
    agents: [
      { name: 'codex', state: 'not_installed', version: '', nonInteractive: true, streamJSON: true, hint: '未安装' },
      { name: 'cursor', state: 'not_installed', version: '', nonInteractive: true, streamJSON: true, hint: '未安装' },
      { name: 'kimi', state: 'available', version: '1', nonInteractive: true, streamJSON: true, hint: '可用' },
    ],
  })
  render(<LanguageProvider value="zh-CN"><AgentHubPage /></LanguageProvider>)
  await openLegacy()
  fireEvent.click(await screen.findByRole('button', { name: /做 PPT/ }))
  fireEvent.change(screen.getByLabelText('任务说明'), { target: { value: '做两页' } })
  fireEvent.click(screen.getByRole('button', { name: '执行' }))
  expect(agentHubApi.start).not.toHaveBeenCalled()
})

it('ppt shortcut locks kimi and prefixes the prompt when a directory is remembered', async () => {
  stubLists()
  localStorage.setItem('lunitide:agent-hub-workdir:ppt', 'E:/slides')
  vi.mocked(agentHubApi.detect).mockResolvedValue({
    agents: [
      { name: 'codex', state: 'not_installed', version: '', nonInteractive: true, streamJSON: true, hint: 'x' },
      { name: 'cursor', state: 'not_installed', version: '', nonInteractive: true, streamJSON: true, hint: 'x' },
      { name: 'kimi', state: 'available', version: '1', nonInteractive: true, streamJSON: true, hint: '可用' },
    ],
  })
  vi.mocked(agentHubApi.inbox).mockResolvedValue({ canceled: false, workDir: 'E:/slides', files: [] })
  vi.mocked(agentHubApi.start).mockResolvedValue(startedFixture('kimi', '做两页'))
  render(<LanguageProvider value="zh-CN"><AgentHubPage /></LanguageProvider>)
  await openLegacy()
  fireEvent.click(await screen.findByRole('button', { name: /做 PPT/ }))
  fireEvent.change(screen.getByLabelText('任务说明'), { target: { value: '做两页' } })
  fireEvent.click(screen.getByRole('button', { name: '执行' }))
  await waitFor(() => expect(agentHubApi.start).toHaveBeenCalledWith(expect.objectContaining({
    agent: 'kimi',
    workDir: 'E:/slides',
    prompt: expect.stringContaining('【场景：做 PPT】'),
  })))
})

it('free ingest lists files in the start prompt and binds the allocated directory', async () => {
  stubLists()
  vi.mocked(agentHubApi.inbox).mockResolvedValue({
    canceled: false,
    workDir: 'E:/hub',
    files: [{ name: '纪要.pdf', path: '纪要.pdf', size: 12 }],
  })
  vi.mocked(agentHubApi.start).mockResolvedValue(startedFixture('codex', '总结'))
  render(<LanguageProvider value="zh-CN"><AgentHubPage /></LanguageProvider>)
  await openLegacy()
  fireEvent.click(await screen.findByRole('button', { name: /其它任务/ }))
  fireEvent.click(screen.getByRole('button', { name: '添加文件' }))
  await waitFor(() => expect(agentHubApi.inbox).toHaveBeenCalledWith(expect.objectContaining({ action: 'files' })))
  fireEvent.change(screen.getByLabelText('任务说明'), { target: { value: '总结这些材料' } })
  fireEvent.click(screen.getByRole('button', { name: '执行' }))
  await waitFor(() => expect(agentHubApi.start).toHaveBeenCalledWith(expect.objectContaining({
    workDir: 'E:/hub',
    prompt: expect.stringMatching(/纪要\.pdf/),
  })))
  const payload = vi.mocked(agentHubApi.start).mock.calls.at(-1)?.[0] as { prompt: string }
  expect(payload.prompt).toContain('.agenthub-inbox')
  expect(payload.prompt).not.toContain('【场景：')
})

it('ppt ingest picks a directory before opening the file dialog', async () => {
  stubLists()
  vi.mocked(agentHubApi.detect).mockResolvedValue({
    agents: [
      { name: 'codex', state: 'not_installed', version: '', nonInteractive: true, streamJSON: true, hint: 'x' },
      { name: 'cursor', state: 'not_installed', version: '', nonInteractive: true, streamJSON: true, hint: 'x' },
      { name: 'kimi', state: 'available', version: '1', nonInteractive: true, streamJSON: true, hint: '可用' },
    ],
  })
  vi.mocked(agentHubApi.pickDir).mockResolvedValue({ canceled: true, path: '' })
  render(<LanguageProvider value="zh-CN"><AgentHubPage /></LanguageProvider>)
  await openLegacy()
  fireEvent.click(await screen.findByRole('button', { name: /做 PPT/ }))
  fireEvent.click(screen.getByRole('button', { name: '添加文件' }))
  await waitFor(() => expect(agentHubApi.pickDir).toHaveBeenCalled())
  expect(agentHubApi.inbox).not.toHaveBeenCalled()
})

it('keeps ppt unselected and shows an in-page hint when kimi is unavailable', async () => {
  stubLists()
  const alert = vi.spyOn(window, 'alert').mockImplementation(() => {})
  render(<LanguageProvider value="zh-CN"><AgentHubPage /></LanguageProvider>)
  await openLegacy()
  fireEvent.click(await screen.findByRole('button', { name: /做 PPT/ }))
  expect(alert).not.toHaveBeenCalled()
  expect(screen.getByRole('button', { name: '执行' })).toBeDisabled()
  expect(screen.getByText('未安装 Kimi Code CLI（kimi）。安装后重新打开调度台。')).toBeInTheDocument()
  expect(screen.queryByText('添加文件后，Agent 会在本目录的 .agenthub-inbox 里读副本。原文件不会被改。')).toBeNull()
})

it('keeps the free agent and shows a hint when a grey capsule is clicked', async () => {
  stubLists()
  const alert = vi.spyOn(window, 'alert').mockImplementation(() => {})
  vi.mocked(agentHubApi.start).mockResolvedValue(startedFixture('codex', '写说明'))
  render(<LanguageProvider value="zh-CN"><AgentHubPage /></LanguageProvider>)
  await openLegacy()
  fireEvent.click(await screen.findByRole('button', { name: /其它任务/ }))
  fireEvent.click(screen.getByRole('button', { name: /Kimi 未安装/ }))
  expect(alert).not.toHaveBeenCalled()
  expect(screen.getByText('未安装 Kimi Code CLI（kimi）。安装后重新打开调度台。')).toBeInTheDocument()
  fireEvent.change(screen.getByLabelText('任务说明'), { target: { value: '写说明' } })
  fireEvent.click(screen.getByRole('button', { name: '执行' }))
  await waitFor(() => expect(agentHubApi.start).toHaveBeenCalledWith(expect.objectContaining({ agent: 'codex' })))
})

it('opens a thread from selectedThreadId and returns to Home when newThreadNonce changes', async () => {
  stubLists()
  const threadId = '01ARZ3NDEKTSV4RRFFQ69G5FAE'
  vi.mocked(agentHubApi.threadGet).mockResolvedValue({
    thread: {
      threadId,
      harnessId: 'cursor',
      nativeSessionId: '',
      title: '写项目会话',
      pinned: false,
      workspaceRoot: 'C:/tmp',
      exportDir: '',
      scene: 'free',
      status: 'idle',
      accessMode: 'approval',
      createdAt: '2026-09-13T00:00:00Z',
      updatedAt: '2026-09-13T00:00:00Z',
    },
    messages: [{ id: threadId, seq: 1, role: 'user', content: '继续', createdAt: '2026-09-13T00:00:00Z' }],
    events: [],
    files: [],
  })
  const view = render(<LanguageProvider value="zh-CN"><AgentHubPage selectedThreadId={threadId} newThreadNonce={0} /></LanguageProvider>)
  expect(await screen.findByLabelText('消息')).toBeInTheDocument()
  expect(screen.getByText('继续')).toBeInTheDocument()
  expect(screen.queryByRole('button', { name: '写项目' })).toBeNull()
  view.rerender(<LanguageProvider value="zh-CN"><AgentHubPage selectedThreadId={undefined} newThreadNonce={1} /></LanguageProvider>)
  expect(await screen.findByRole('button', { name: '写项目' })).toBeInTheDocument()
  expect(screen.queryByLabelText('消息')).toBeNull()
})
