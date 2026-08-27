import { cleanup, render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, describe, expect, test, vi } from 'vitest'
import type { MeetingsBridge } from '../bridge/client'
import type { MeetingDTO, MeetingSegmentDTO } from '../generated/bridge'
import type { CompanionSpeechHandle } from '../session/companion/speech'
import { MeetingPage } from './MeetingPage'

const now = '2026-08-27T03:00:00.000Z'
const meetingId = '01ARZ3NDEKTSV4RRFFQ69G5FAV'

const base: MeetingDTO = {
  meetingId, title: '周会', status: 'transcribed', audioSource: 'microphone',
  startedAt: now, endedAt: now, durationMs: 90000, summary: '', actions: '', transcript: '',
  createdAt: now, updatedAt: now, segments: [], docs: [],
}

const speech = vi.hoisted(() => ({
  start: vi.fn(),
  onFinal: undefined as ((text: string) => void) | undefined,
  handle: (): CompanionSpeechHandle => ({
    stop: vi.fn(),
    setAssistantPlayback: vi.fn(),
    setCommitPaused: vi.fn(),
    pulseRecognition: vi.fn(),
    forceCommit: vi.fn(),
    resumeCapture: vi.fn(),
  }),
}))

vi.mock('./meetingAsr', () => ({
  startMeetingSpeech: (options: { onFinal: (text: string) => void }) => {
    speech.onFinal = options.onFinal
    return speech.start(options)
  },
}))

function bridge(overrides: Partial<MeetingsBridge> = {}): MeetingsBridge {
  return {
    list: vi.fn().mockResolvedValue({ items: [] }),
    start: vi.fn(),
    append: vi.fn(),
    stop: vi.fn(),
    get: vi.fn(),
    summarize: vi.fn(),
    exportMeeting: vi.fn(),
    ...overrides,
  }
}

describe('MeetingPage', () => {
  afterEach(() => {
    cleanup()
    speech.start.mockReset()
    speech.onFinal = undefined
  })

  test('lists past meetings and keeps the workspace independent of 对话', async () => {
    const past = { ...base, title: '评审会', status: 'ready' as const, summary: '对齐范围', actions: '- 导出安装包', transcript: '大家好' }
    render(<MeetingPage meetings={bridge({ list: vi.fn().mockResolvedValue({ items: [past] }) })} />)
    expect(await screen.findByRole('heading', { name: '会议记录' })).toBeInTheDocument()
    expect(screen.getByText('评审会')).toBeInTheDocument()
    expect(screen.getByText(/已完成/)).toBeInTheDocument()
    expect(screen.queryByRole('heading', { name: '今天想聊什么？' })).not.toBeInTheDocument()
  })

  test('start records live captions then stop generates notes and export', async () => {
    const started: MeetingDTO = { ...base, status: 'recording', endedAt: '', durationMs: 0, title: '会议 2026-08-27 11:00' }
    const segment: MeetingSegmentDTO = {
      segmentId: '01ARZ3NDEKTSV4RRFFQ69G5FAW', meetingId, seq: 1, startedMs: 800, text: '先对齐范围', createdAt: now,
    }
    const stopped: MeetingDTO = { ...started, status: 'transcribed', endedAt: now, durationMs: 1200, transcript: '先对齐范围', segments: [segment] }
    const ready: MeetingDTO = { ...stopped, status: 'ready', title: '范围评审', summary: '已对齐范围。', actions: '- 写纪要' }
    const meetings = bridge({
      start: vi.fn().mockResolvedValue(started),
      append: vi.fn().mockResolvedValue(segment),
      stop: vi.fn().mockResolvedValue(stopped),
      summarize: vi.fn().mockResolvedValue(ready),
      exportMeeting: vi.fn().mockResolvedValue({ path: 'C:/notes.md', format: 'markdown' }),
    })
    speech.start.mockResolvedValue(speech.handle())
    const user = userEvent.setup()
    render(<MeetingPage meetings={meetings} />)
    await screen.findByRole('button', { name: '开始' })
    await user.click(screen.getByRole('button', { name: '开始' }))
    expect(await screen.findByRole('button', { name: '停止' })).toBeInTheDocument()
    expect(meetings.start).toHaveBeenCalledWith({})
    speech.onFinal?.('先对齐范围')
    expect(await screen.findByText('先对齐范围')).toBeInTheDocument()
    expect(meetings.append).toHaveBeenCalledWith(expect.objectContaining({ meetingId, text: '先对齐范围' }))
    await user.click(screen.getByRole('button', { name: '停止' }))
    expect(await screen.findByRole('heading', { name: '会议摘要' })).toBeInTheDocument()
    expect(screen.getByText('已对齐范围。')).toBeInTheDocument()
    expect(screen.getByText('- 写纪要')).toBeInTheDocument()
    await user.click(screen.getByRole('button', { name: '导出 Markdown' }))
    expect(meetings.exportMeeting).toHaveBeenCalledWith({ meetingId, format: 'markdown' })
    expect(await screen.findByText(/C:\/notes.md/)).toBeInTheDocument()
  })

  test('honest needs_summary state offers retry without inventing notes', async () => {
    const started: MeetingDTO = { ...base, status: 'recording', endedAt: '', durationMs: 0 }
    const stopped: MeetingDTO = { ...started, status: 'transcribed', transcript: '只有逐字稿' }
    const pending: MeetingDTO = { ...stopped, status: 'needs_summary', summaryError: '尚未生成摘要：没有已启用的模型' }
    const meetings = bridge({
      start: vi.fn().mockResolvedValue(started),
      stop: vi.fn().mockResolvedValue(stopped),
      summarize: vi.fn().mockResolvedValue(pending),
    })
    speech.start.mockResolvedValue(speech.handle())
    const user = userEvent.setup()
    render(<MeetingPage meetings={meetings} />)
    await user.click(await screen.findByRole('button', { name: '开始' }))
    await user.click(await screen.findByRole('button', { name: '停止' }))
    expect(await screen.findByRole('status')).toHaveTextContent(/尚未生成摘要/)
    expect(screen.getByRole('button', { name: '重试生成摘要' })).toBeInTheDocument()
    expect(screen.queryByText('编造的决议')).not.toBeInTheDocument()
  })
})
