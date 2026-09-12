import { act, cleanup, render, screen } from '@testing-library/react'
import { afterEach, expect, it, vi } from 'vitest'
import { LanguageProvider } from '../i18n/language'
import { AgentHubDetail } from './AgentHubDetail'
import { agentHubApi } from './agentHubApi'

vi.mock('./agentHubApi', () => ({
  agentHubApi: { get: vi.fn(), cancel: vi.fn(), preview: vi.fn(), open: vi.fn() },
}))

afterEach(() => {
  cleanup()
  vi.useRealTimers()
  vi.restoreAllMocks()
})

it('polls task.get every 400ms and keeps events unique by seq', async () => {
  vi.useFakeTimers()
  const first = {
    task: {
      taskId: '01ARZ3NDEKTSV4RRFFQ69G5FAE',
      agent: 'codex',
      prompt: 'hello',
      workDir: 'C:/tmp',
      sandbox: '',
      status: 'running',
      tokensUsed: 0,
      errorMsg: '',
      createdAt: '2026-09-12T00:00:00Z',
    },
    events: [{ seq: 1, type: 'started', title: '开始', detail: '', ts: '2026-09-12T00:00:01Z' }],
    artifacts: [],
  }
  const second = {
    ...first,
    events: [
      { seq: 1, type: 'started', title: '开始', detail: '', ts: '2026-09-12T00:00:01Z' },
      { seq: 2, type: 'message', title: '输出', detail: 'ok', ts: '2026-09-12T00:00:02Z' },
    ],
  }
  vi.mocked(agentHubApi.get)
    .mockResolvedValueOnce(first)
    .mockResolvedValue(second)
  render(<LanguageProvider value="zh-CN"><AgentHubDetail taskId="01ARZ3NDEKTSV4RRFFQ69G5FAE" /></LanguageProvider>)
  await act(async () => { await vi.advanceTimersByTimeAsync(0) })
  expect(screen.getByText('开始')).toBeInTheDocument()
  await act(async () => { await vi.advanceTimersByTimeAsync(400) })
  expect(screen.getByText('ok')).toBeInTheDocument()
  expect(screen.getAllByText('开始')).toHaveLength(1)
})

it('stops polling after the task finishes', async () => {
  vi.useFakeTimers()
  vi.mocked(agentHubApi.get).mockResolvedValue({
    task: {
      taskId: '01ARZ3NDEKTSV4RRFFQ69G5FAE',
      agent: 'codex',
      prompt: 'hello',
      workDir: 'C:/tmp',
      sandbox: '',
      status: 'success',
      tokensUsed: 0,
      errorMsg: '',
      createdAt: '2026-09-12T00:00:00Z',
    },
    events: [],
    artifacts: [],
  })
  render(<LanguageProvider value="zh-CN"><AgentHubDetail taskId="01ARZ3NDEKTSV4RRFFQ69G5FAE" /></LanguageProvider>)
  await act(async () => { await vi.advanceTimersByTimeAsync(0) })
  const n = vi.mocked(agentHubApi.get).mock.calls.length
  await act(async () => { await vi.advanceTimersByTimeAsync(2000) })
  expect(vi.mocked(agentHubApi.get).mock.calls.length).toBe(n)
})

it('shows inbox and changed artifacts by default and hides scan', async () => {
  vi.mocked(agentHubApi.get).mockResolvedValue({
    task: {
      taskId: '01ARZ3NDEKTSV4RRFFQ69G5FAE',
      agent: 'kimi',
      prompt: '总结',
      workDir: 'C:/tmp',
      sandbox: '',
      status: 'success',
      tokensUsed: 0,
      errorMsg: '',
      createdAt: '2026-09-12T00:00:00Z',
    },
    events: [],
    artifacts: [
      { name: '纪要.pdf', path: '.agenthub-inbox/纪要.pdf', size: 12, mime: 'application/pdf', source: 'inbox' },
      { name: 'hello.txt', path: 'hello.txt', size: 2, mime: 'text/plain', source: 'changed' },
      { name: 'lib.js', path: 'lib.js', size: 3, mime: 'text/plain', source: 'scan' },
      { name: 'away.txt', path: 'C:/away.txt', size: 1, mime: 'text/plain', source: 'outside' },
    ],
  })
  render(<LanguageProvider value="zh-CN"><AgentHubDetail taskId="01ARZ3NDEKTSV4RRFFQ69G5FAE" /></LanguageProvider>)
  expect(await screen.findByText('纪要.pdf')).toBeInTheDocument()
  expect(screen.getByText('hello.txt')).toBeInTheDocument()
  expect(screen.queryByText('lib.js')).toBeNull()
  expect(screen.queryByText('away.txt')).toBeNull()
})

it('says honestly when a finished PPT task has no deck', async () => {
  vi.mocked(agentHubApi.get).mockResolvedValue({
    task: {
      taskId: '01ARZ3NDEKTSV4RRFFQ69G5FAE',
      agent: 'kimi',
      prompt: '【场景：做 PPT】\n工作目录：C:/tmp\n只在本目录写文件。优先使用 kimi-slides。产出 pptx。\n\n用户任务：\n做两页',
      workDir: 'C:/tmp',
      sandbox: '',
      status: 'success',
      tokensUsed: 0,
      errorMsg: '',
      createdAt: '2026-09-12T00:00:00Z',
    },
    events: [],
    artifacts: [
      { name: 'notes.md', path: 'notes.md', size: 4, mime: 'text/markdown', source: 'changed' },
    ],
  })
  render(<LanguageProvider value="zh-CN"><AgentHubDetail taskId="01ARZ3NDEKTSV4RRFFQ69G5FAE" /></LanguageProvider>)
  expect(await screen.findByText('没有文稿。打开目录查看本轮文件，或看时间线说明。')).toBeInTheDocument()
})
