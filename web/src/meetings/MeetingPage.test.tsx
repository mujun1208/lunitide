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
  prepare: vi.fn().mockResolvedValue({ extraStreams: [], audioSource: 'microphone', notice: '' }),
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
  prepareMeetingCapture: (...args: unknown[]) => speech.prepare(...args),
  releaseMeetingCapture: vi.fn(),
  audioSourceLabel: (source: string, live?: boolean) => source === 'microphone_and_system'
    ? (live ? '正在录制本机麦克风和系统声音' : '本机麦克风 + 本机系统声音（未共享给其他电脑）')
    : (live ? '正在录制本机麦克风' : '仅本机麦克风，未混录系统扬声器'),
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
    speech.prepare.mockReset().mockResolvedValue({ extraStreams: [], audioSource: 'microphone', notice: '' })
    speech.onFinal = undefined
    localStorage.removeItem('lunitide:meeting-include-system-audio')
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
    expect(meetings.start).toHaveBeenCalledWith({ audioSource: 'microphone' })
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

  test('offers this-PC system audio and records the mix when capture succeeds', async () => {
    const started: MeetingDTO = { ...base, status: 'recording', endedAt: '', durationMs: 0, audioSource: 'microphone_and_system' }
    const meetings = bridge({ start: vi.fn().mockResolvedValue(started) })
    speech.prepare.mockResolvedValue({ extraStreams: [{ id: 'loop' }], audioSource: 'microphone_and_system', notice: '' })
    speech.start.mockResolvedValue(speech.handle())
    const user = userEvent.setup()
    render(<MeetingPage meetings={meetings} />)
    await user.click(await screen.findByRole('checkbox', { name: '同时收录本机系统声音' }))
    await user.click(screen.getByRole('button', { name: '开始' }))
    expect(await screen.findByRole('button', { name: '停止' })).toBeInTheDocument()
    expect(speech.prepare).toHaveBeenCalledWith(true)
    expect(meetings.start).toHaveBeenCalledWith({ audioSource: 'microphone_and_system' })
    expect(await screen.findByText('正在录制本机麦克风和系统声音')).toBeInTheDocument()
  })

  test('canceled system-audio picker does not create a meeting', async () => {
    speech.prepare.mockRejectedValue(new DOMException('Permission denied', 'NotAllowedError'))
    const meetings = bridge({ start: vi.fn() })
    const user = userEvent.setup()
    render(<MeetingPage meetings={meetings} />)
    await user.click(await screen.findByRole('checkbox', { name: '同时收录本机系统声音' }))
    await user.click(screen.getByRole('button', { name: '开始' }))
    await vi.waitFor(() => expect(speech.prepare).toHaveBeenCalledWith(true))
    expect(meetings.start).not.toHaveBeenCalled()
    expect(screen.getByRole('button', { name: '开始' })).toBeInTheDocument()
  })

  test('finalizes a leftover recording instead of pretending capture is still live', async () => {
    const leftover: MeetingDTO = { ...base, status: 'recording', endedAt: '', durationMs: 0, transcript: '已听到的一句' }
    const stopped: MeetingDTO = { ...leftover, status: 'transcribed', transcript: '已听到的一句' }
    const meetings = bridge({
      list: vi.fn().mockResolvedValue({ items: [leftover] }),
      stop: vi.fn().mockResolvedValue(stopped),
    })
    render(<MeetingPage meetings={meetings} />)
    expect(await screen.findByRole('status')).toHaveTextContent(/离开页面后中断/)
    expect(meetings.stop).toHaveBeenCalledWith({ meetingId })
    expect(screen.getByRole('button', { name: '开始' })).toBeInTheDocument()
  })
})
