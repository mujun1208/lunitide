import { BridgeClientError } from '../bridge/client'
import { cleanup, render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, describe, expect, test, vi } from 'vitest'
import type { MeetingsBridge } from '../bridge/client'
import type { MeetingDTO, MeetingSegmentDTO } from '../generated/bridge'
import type { CompanionSpeechHandle } from '../session/companion/speech'
import { MeetingPage, MEETING_CAPTION_STALL_MS, MEETING_CAPTION_STALL_POLL_MS } from './MeetingPage'

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
  onInterim: undefined as ((text: string) => void) | undefined,
  onError: undefined as ((error: Error) => void) | undefined,
  handle: (): CompanionSpeechHandle => ({
    stop: vi.fn(),
    setAssistantPlayback: vi.fn(),
    setCommitPaused: vi.fn(),
    pulseRecognition: vi.fn(),
    forceCommit: vi.fn(),
    resumeCapture: vi.fn(),
  }),
}))

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
          models: [{ modelId: 'qwen-plus', displayName: 'Qwen', isDefault: true, kind: 'llm', kindDefault: true }],
        }, {
          id: '01ARZ3NDEKTSV4RRFFQ69G5FAX',
          name: 'Volc Speech',
          protocol: 'volc_speech',
          baseUrl: 'https://openspeech.bytedance.com',
          status: 'enabled',
          credentialState: 'configured',
          createdAt: now,
          updatedAt: now,
          version: 1,
          models: [{ modelId: 'volc.seedasr.sauc.duration', displayName: 'seed-asr', isDefault: true, kind: 'asr', kindDefault: true }],
        }],
      }),
    }),
  }
})

vi.mock('./meetingAsr', async importOriginal => {
  const actual = await importOriginal<typeof import('./meetingAsr')>()
  return {
    ...actual,
    startMeetingSpeech: (options: { onFinal: (text: string) => void; onInterim?: (text: string) => void; onError?: (error: Error) => void }) => {
      speech.onFinal = options.onFinal
      speech.onInterim = options.onInterim
      speech.onError = options.onError
      return speech.start(options)
    },
    prepareMeetingCapture: (...args: unknown[]) => speech.prepare(...args),
    recoverMeetingSystemAudio: (plan: { extraStreams: unknown[]; audioSource: string; notice: string }) => Promise.resolve(plan),
  }
})

vi.mock('./meetingAudio', async importOriginal => {
  const actual = await importOriginal<typeof import('./meetingAudio')>()
  return {
    ...actual,
    startMeetingAudioRecorder: vi.fn(async () => ({
      stop: vi.fn().mockResolvedValue(undefined),
      flush: vi.fn().mockResolvedValue(undefined),
      attachExtraStream: vi.fn(),
    })),
  }
})

function bridge(overrides: Partial<MeetingsBridge> = {}): MeetingsBridge {
  return {
    list: vi.fn().mockResolvedValue({ items: [] }),
    start: vi.fn(),
    append: vi.fn(),
    audioAppend: vi.fn().mockResolvedValue({ meetingId, audioMs: 1000 }),
    loopbackPoll: vi.fn().mockResolvedValue({ meetingId, active: false, pcm: '' }),
    stop: vi.fn(),
    get: vi.fn(),
    heartbeat: vi.fn().mockResolvedValue({ ...base, status: 'recording' as const }),
    catchup: vi.fn().mockImplementation(async ({ meetingId: id }: { meetingId: string }) => ({
      ...base, meetingId: id, status: 'transcribed' as const, transcript: '逐字稿',
    })),
    summarize: vi.fn(),
    exportMeeting: vi.fn(),
    update: vi.fn(),
    delete: vi.fn(),
    ...overrides,
  }
}

describe('MeetingPage', () => {
  afterEach(() => {
    cleanup()
    localStorage.clear()
    vi.useRealTimers()
    speech.start.mockReset()
    speech.prepare.mockReset().mockResolvedValue({ extraStreams: [], audioSource: 'microphone', notice: '' })
    speech.onFinal = undefined
    speech.onInterim = undefined
    speech.onError = undefined
  })

  test('lists past meetings and keeps the workspace independent of 对话', async () => {
    const past = { ...base, title: '评审会', status: 'ready' as const, summary: '对齐范围', actions: '- 导出安装包', transcript: '大家好' }
    render(<MeetingPage meetings={bridge({ list: vi.fn().mockResolvedValue({ items: [past] }) })} />)
    expect(await screen.findByRole('button', { name: '＋ 新纪要' })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: '历史纪要' })).toHaveAttribute('aria-expanded', 'true')
    expect(screen.getByText('评审会')).toBeInTheDocument()
    expect(screen.getByText(/已完成/)).toBeInTheDocument()
    expect(screen.getByRole('heading', { name: '新的会议' })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: '开始录制' })).toBeInTheDocument()
    expect(screen.queryByLabelText('纪要模型')).not.toBeInTheDocument()
    expect(screen.queryByRole('heading', { name: '今天想聊什么？' })).not.toBeInTheDocument()
    expect(screen.queryByRole('checkbox', { name: '同时收录本机系统声音' })).not.toBeInTheDocument()
  })

  test('听写与纪要设置 opens an in-page overlay and does not leave the meeting page', async () => {
    const openSettings = vi.fn()
    render(<MeetingPage meetings={bridge()} onOpenSettings={openSettings} />)
    await userEvent.setup().click(await screen.findByRole('button', { name: '听写与纪要设置' }))
    expect(openSettings).not.toHaveBeenCalled()
    expect(await screen.findByRole('dialog', { name: '听写与纪要设置' })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: '← 返回会议' })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: '开始录制' })).toBeInTheDocument()
  })

  test('opening settings overlay while recording does not stop capture', async () => {
    const started: MeetingDTO = { ...base, status: 'recording', endedAt: '', durationMs: 0 }
    const segment = (seq: number, text: string): MeetingSegmentDTO => ({
      segmentId: `01ARZ3NDEKTSV4RRFFQ69G5FA${seq}`, meetingId, seq, startedMs: seq * 800, text, createdAt: now,
    })
    const meetings = bridge({
      start: vi.fn().mockResolvedValue(started),
      stop: vi.fn(),
      append: vi.fn()
        .mockResolvedValueOnce(segment(1, '先对齐范围'))
        .mockResolvedValueOnce(segment(2, '确认纪要模型'))
        .mockResolvedValueOnce(segment(3, '继续逐字记录')),
    })
    const handle = speech.handle()
    speech.start.mockResolvedValue(handle)
    const user = userEvent.setup()
    render(<MeetingPage meetings={meetings} />)
    await user.click(await screen.findByRole('button', { name: '开始录制' }))
    expect(await screen.findByRole('button', { name: '停止' })).toBeInTheDocument()
    speech.onFinal?.('先对齐范围')
    speech.onFinal?.('确认纪要模型')
    speech.onFinal?.('继续逐字记录')
    expect(await screen.findByText('先对齐范围')).toBeInTheDocument()
    expect(await screen.findByText('确认纪要模型')).toBeInTheDocument()
    expect(await screen.findByText('继续逐字记录')).toBeInTheDocument()
    await user.click(screen.getByRole('button', { name: '听写与纪要设置' }))
    expect(await screen.findByRole('dialog', { name: '听写与纪要设置' })).toBeInTheDocument()
    expect(screen.getByText('先对齐范围')).toBeInTheDocument()
    expect(screen.getByText('确认纪要模型')).toBeInTheDocument()
    expect(screen.getByText('继续逐字记录')).toBeInTheDocument()
    expect(meetings.stop).not.toHaveBeenCalled()
    expect(meetings.heartbeat).toHaveBeenCalled()
    expect(handle.stop).not.toHaveBeenCalled()
    expect(screen.getByRole('radio', { name: /火山/ })).toBeDisabled()
  })

  test('opening history can return to a blank 新纪要 and history can collapse', async () => {
    const past = { ...base, title: '评审会', status: 'ready' as const, summary: '对齐范围', actions: '- 导出安装包', transcript: '大家好' }
    const user = userEvent.setup()
    render(<MeetingPage meetings={bridge({ list: vi.fn().mockResolvedValue({ items: [past] }), get: vi.fn().mockResolvedValue(past) })} />)
    await user.click(await screen.findByText('评审会'))
    expect(await screen.findByRole('heading', { name: '评审会' })).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: '开始录制' })).not.toBeInTheDocument()
    await user.click(screen.getByRole('button', { name: '＋ 新纪要' }))
    expect(screen.getByRole('heading', { name: '新的会议' })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: '开始录制' })).toBeInTheDocument()
    await user.click(screen.getByRole('button', { name: '历史纪要' }))
    expect(screen.getByRole('button', { name: '历史纪要' })).toHaveAttribute('aria-expanded', 'false')
    expect(screen.queryByText('评审会')).not.toBeInTheDocument()
  })

  test('ready empty actions stay honest and the four boxes scroll inside the page', async () => {
    const past = { ...base, title: '空待办会', status: 'ready' as const, summary: '只对齐了方向', actions: '', transcript: '大家好\n'.repeat(40) }
    const meetings = bridge({
      list: vi.fn().mockResolvedValue({ items: [past] }),
      get: vi.fn().mockResolvedValue(past),
    })
    const user = userEvent.setup()
    const { container } = render(<MeetingPage meetings={meetings} />)
    await user.click(await screen.findByText('空待办会'))
    expect(await screen.findByRole('heading', { name: '决议/待办' })).toBeInTheDocument()
    expect(screen.getByRole('textbox', { name: '决议/待办' })).toHaveAttribute('placeholder', '这场没有抽出可执行待办。')
    expect(container.querySelector('.meeting-main')).toBeTruthy()
    expect(container.querySelector('.meeting-transcript')).toBeTruthy()
    expect(container.querySelector('.meeting-doc')).toBeTruthy()
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
      catchup: vi.fn().mockResolvedValue(stopped),
      summarize: vi.fn().mockResolvedValue(ready),
      exportMeeting: vi.fn().mockResolvedValue({ path: 'C:/notes.md', format: 'markdown' }),
    })
    speech.start.mockResolvedValue(speech.handle())
    const user = userEvent.setup()
    render(<MeetingPage meetings={meetings} />)
    await screen.findByRole('button', { name: '开始录制' })
    await user.click(screen.getByRole('button', { name: '开始录制' }))
    expect(await screen.findByRole('button', { name: '停止' })).toBeInTheDocument()
    expect(meetings.start).toHaveBeenCalledWith({ audioSource: 'microphone_and_system' })
    expect(speech.start).toHaveBeenCalledWith(expect.objectContaining({ listen: 'cloud' }))
    expect(speech.prepare).toHaveBeenCalledWith({ interactive: false })
    speech.onFinal?.('先对齐范围')
    expect(await screen.findByText('先对齐范围')).toBeInTheDocument()
    expect(meetings.append).toHaveBeenCalledWith(expect.objectContaining({ meetingId, text: '先对齐范围' }))
    await user.click(screen.getByRole('button', { name: '停止' }))
    expect(meetings.catchup).toHaveBeenCalledWith({ meetingId })
    expect(meetings.summarize).toHaveBeenCalledWith(expect.objectContaining({ meetingId }))
    expect(await screen.findByRole('heading', { name: '会议摘要' })).toBeInTheDocument()
    expect(screen.getByDisplayValue('已对齐范围。')).toBeInTheDocument()
    expect(screen.getByDisplayValue('- 写纪要')).toBeInTheDocument()
    await user.click(screen.getByRole('button', { name: '导出 Markdown' }))
    expect(meetings.exportMeeting).toHaveBeenCalledWith({ meetingId, format: 'markdown' })
    expect(await screen.findByText(/C:\/notes.md/)).toBeInTheDocument()
  })

  test('volc listen drops 正在连接 after the websocket opens', async () => {
    const started: MeetingDTO = { ...base, status: 'recording', endedAt: '', durationMs: 0 }
    const meetings = bridge({ start: vi.fn().mockResolvedValue(started) })
    let releaseStart: ((handle: CompanionSpeechHandle) => void) | undefined
    speech.start.mockImplementation(() => new Promise(resolve => {
      releaseStart = resolve
    }))
    localStorage.setItem('lunitide:meeting', JSON.stringify({ listen: 'volc', modelId: '' }))
    const user = userEvent.setup()
    render(<MeetingPage meetings={meetings} />)
    await user.click(await screen.findByRole('button', { name: '开始录制' }))
    expect(await screen.findByRole('status')).toHaveTextContent('正在连接火山听写')
    releaseStart?.(speech.handle())
    await waitFor(() => {
      expect(screen.queryByText(/正在连接火山听写/)).not.toBeInTheDocument()
    })
    expect(screen.getByRole('status')).toHaveTextContent('正在听写')
  })

  test('shows the live ASR diagnostics bar while recording', async () => {
    const started: MeetingDTO = { ...base, status: 'recording', endedAt: '', durationMs: 0 }
    const meetings = bridge({ start: vi.fn().mockResolvedValue(started) })
    speech.start.mockResolvedValue(speech.handle())
    const user = userEvent.setup()
    render(<MeetingPage meetings={meetings} />)
    await user.click(await screen.findByRole('button', { name: '开始录制' }))
    const diag = await screen.findByLabelText('听写诊断')
    expect(diag).toHaveTextContent('引擎：系统听写')
    expect(diag).toHaveTextContent('字幕：直采')
  })

  test('local listen failure stays on sherpa and shows the error', async () => {
    const started: MeetingDTO = { ...base, status: 'recording', endedAt: '', durationMs: 0 }
    const meetings = bridge({ start: vi.fn().mockResolvedValue(started) })
    speech.start.mockRejectedValue(new Error('会议听写选了本机，但 sherpa 未就绪。请改选系统或火山，或先装本机识别。'))
    const user = userEvent.setup()
    localStorage.setItem('lunitide:meeting', JSON.stringify({ listen: 'local', modelId: '' }))
    render(<MeetingPage meetings={meetings} />)
    await user.click(screen.getByRole('button', { name: '开始录制' }))
    expect(await screen.findByRole('status')).toHaveTextContent(/sherpa 未就绪/)
    expect(speech.start).toHaveBeenCalledWith(expect.objectContaining({ listen: 'local' }))
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
    await user.click(await screen.findByRole('button', { name: '开始录制' }))
    await user.click(await screen.findByRole('button', { name: '停止' }))
    expect(await screen.findByRole('status')).toHaveTextContent(/尚未生成摘要/)
    expect(screen.getByRole('button', { name: '重试生成摘要' })).toBeInTheDocument()
    expect(screen.queryByText('编造的决议')).not.toBeInTheDocument()
  })

  test('engine mix skips browser capture and polls native loopback', async () => {
    const started: MeetingDTO = { ...base, status: 'recording', endedAt: '', durationMs: 0, audioSource: 'microphone_and_system' }
    const meetings = bridge({
      start: vi.fn().mockResolvedValue(started),
      loopbackPoll: vi.fn().mockResolvedValue({ meetingId, active: true, pcm: '' }),
    })
    speech.start.mockResolvedValue(speech.handle())
    const user = userEvent.setup()
    render(<MeetingPage meetings={meetings} />)
    await user.click(screen.getByRole('button', { name: '开始录制' }))
    expect(await screen.findByRole('button', { name: '停止' })).toBeInTheDocument()
    expect(meetings.start).toHaveBeenCalledWith({ audioSource: 'microphone_and_system' })
    expect(speech.prepare).not.toHaveBeenCalled()
    await vi.waitFor(() => expect(meetings.loopbackPoll).toHaveBeenCalledWith({ meetingId }))
    expect(await screen.findByText('正在录制麦克风与系统声音')).toBeInTheDocument()
  })

  test('starts mic-only recording when engine loopback is unavailable without opening the share picker', async () => {
    speech.prepare.mockResolvedValue({ extraStreams: [], audioSource: 'microphone', notice: '未能收录系统声音，已继续录制麦克风。' })
    const started: MeetingDTO = { ...base, status: 'recording', endedAt: '', durationMs: 0 }
    const meetings = bridge({ start: vi.fn().mockResolvedValue(started) })
    speech.start.mockResolvedValue(speech.handle())
    const user = userEvent.setup()
    render(<MeetingPage meetings={meetings} />)
    await user.click(screen.getByRole('button', { name: '开始录制' }))
    await vi.waitFor(() => expect(speech.prepare).toHaveBeenCalledWith({ interactive: false }))
    expect(meetings.start).toHaveBeenCalledWith({ audioSource: 'microphone_and_system' })
    expect(await screen.findByRole('button', { name: '停止' })).toBeInTheDocument()
  })

  test('resumes a leftover recording instead of stopping capture', async () => {
    const leftover: MeetingDTO = { ...base, status: 'recording', endedAt: '', durationMs: 0, transcript: '已听到的一句' }
    const meetings = bridge({
      list: vi.fn().mockResolvedValue({ items: [leftover] }),
      get: vi.fn().mockResolvedValue(leftover),
      stop: vi.fn(),
      heartbeat: vi.fn().mockResolvedValue(leftover),
    })
    speech.start.mockResolvedValue(speech.handle())
    render(<MeetingPage meetings={meetings} />)
    expect(await screen.findByRole('button', { name: '停止' })).toBeInTheDocument()
    expect(meetings.stop).not.toHaveBeenCalled()
    expect(meetings.get).toHaveBeenCalledWith({ meetingId })
    expect(speech.start).toHaveBeenCalled()
    expect(screen.getByText(/录制中/)).toBeInTheDocument()
  })

  test('resumes leftover engine mix without browser capture', async () => {
    const leftover: MeetingDTO = { ...base, status: 'recording', endedAt: '', durationMs: 0, audioSource: 'microphone_and_system', transcript: '已听到的一句' }
    const meetings = bridge({
      list: vi.fn().mockResolvedValue({ items: [leftover] }),
      get: vi.fn().mockResolvedValue(leftover),
      stop: vi.fn(),
      heartbeat: vi.fn().mockResolvedValue(leftover),
      loopbackPoll: vi.fn().mockResolvedValue({ meetingId, active: true, pcm: '' }),
    })
    speech.start.mockResolvedValue(speech.handle())
    render(<MeetingPage meetings={meetings} />)
    expect(await screen.findByRole('button', { name: '停止' })).toBeInTheDocument()
    expect(speech.prepare).not.toHaveBeenCalled()
    await vi.waitFor(() => expect(meetings.loopbackPoll).toHaveBeenCalledWith({ meetingId }))
  })

  test('keeps recording after a retryable append timeout and still writes the next line', async () => {
    const started: MeetingDTO = { ...base, status: 'recording', endedAt: '', durationMs: 0 }
    const segment: MeetingSegmentDTO = {
      segmentId: '01ARZ3NDEKTSV4RRFFQ69G5FAW', meetingId, seq: 1, startedMs: 800, text: '先对齐范围', createdAt: now,
    }
    const later: MeetingSegmentDTO = { ...segment, seq: 2, text: '下一句' }
    const meetings = bridge({
      start: vi.fn().mockResolvedValue(started),
      append: vi.fn()
        .mockRejectedValueOnce(new BridgeClientError('Bridge 请求超时', 'REQUEST_DEADLINE_EXCEEDED', true, 'trace'))
        .mockResolvedValueOnce(segment)
        .mockResolvedValue(later),
    })
    speech.start.mockResolvedValue(speech.handle())
    const user = userEvent.setup()
    render(<MeetingPage meetings={meetings} />)
    await user.click(await screen.findByRole('button', { name: '开始录制' }))
    expect(await screen.findByRole('button', { name: '停止' })).toBeInTheDocument()
    speech.onFinal?.('先对齐范围')
    expect(await screen.findByText('先对齐范围')).toBeInTheDocument()
    expect(vi.mocked(meetings.append).mock.calls.length).toBeGreaterThanOrEqual(2)
    speech.onFinal?.('下一句')
    expect(await screen.findByText('下一句')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: '停止' })).toBeInTheDocument()
  })

  test('restarts speech after a Bridge timeout and keeps recording until stop', async () => {
    const started: MeetingDTO = { ...base, status: 'recording', endedAt: '', durationMs: 0 }
    const later: MeetingSegmentDTO = {
      segmentId: '01ARZ3NDEKTSV4RRFFQ69G5FAX', meetingId, seq: 2, startedMs: 1600, text: '继续对齐', createdAt: now,
    }
    const meetings = bridge({
      start: vi.fn().mockResolvedValue(started),
      append: vi.fn().mockResolvedValue(later),
    })
    speech.start.mockResolvedValue(speech.handle())
    const user = userEvent.setup()
    render(<MeetingPage meetings={meetings} />)
    await user.click(await screen.findByRole('button', { name: '开始录制' }))
    expect(await screen.findByRole('button', { name: '停止' })).toBeInTheDocument()
    expect(meetings.heartbeat).toHaveBeenCalledWith({ meetingId })
    speech.onError?.(new BridgeClientError('Bridge 请求超时', 'REQUEST_DEADLINE_EXCEEDED', true, 'trace'))
    expect(await screen.findByRole('status')).toHaveTextContent(/实时转写中断/)
    await vi.waitFor(() => expect(speech.start).toHaveBeenCalledTimes(2), { timeout: 3000 })
    expect(screen.getByRole('button', { name: '停止' })).toBeInTheDocument()
    speech.onFinal?.('继续对齐')
    expect(await screen.findByText('继续对齐')).toBeInTheDocument()
  })

  test('list duration follows the live clock while recording', async () => {
    const startedAt = new Date(Date.now() - 59_000).toISOString()
    const started: MeetingDTO = { ...base, status: 'recording', endedAt: '', durationMs: 0, startedAt, title: '会议 2026-08-29 09:11' }
    const meetings = bridge({
      start: vi.fn().mockResolvedValue(started),
      heartbeat: vi.fn().mockResolvedValue({ ...started, durationMs: 59_000 }),
    })
    speech.start.mockResolvedValue(speech.handle())
    const user = userEvent.setup()
    render(<MeetingPage meetings={meetings} />)
    await user.click(await screen.findByRole('button', { name: '开始录制' }))
    expect(await screen.findByRole('button', { name: '停止' })).toBeInTheDocument()
    await vi.waitFor(() => expect(meetings.heartbeat).toHaveBeenCalledWith({ meetingId }))
    await vi.waitFor(() => {
      expect(screen.getAllByText(/0:59|1:00/).length).toBeGreaterThanOrEqual(2)
      expect(screen.queryByText(/· 0:00 ·/)).not.toBeInTheDocument()
    })
  })

  test('restarts speech after the system-audio track ends without dropping the meeting', async () => {
    const listeners = new Map<string, () => void>()
    const track = {
      kind: 'audio',
      readyState: 'live',
      addEventListener: (name: string, fn: () => void) => { listeners.set(name, fn) },
      removeEventListener: (name: string) => { listeners.delete(name) },
      stop: vi.fn(),
    }
    const extra = { getAudioTracks: () => [track], getVideoTracks: () => [], getTracks: () => [track] }
    const started: MeetingDTO = { ...base, status: 'recording', endedAt: '', durationMs: 0 }
    const meetings = bridge({ start: vi.fn().mockResolvedValue(started) })
    speech.prepare.mockResolvedValue({ extraStreams: [extra], audioSource: 'microphone_and_system', notice: '' })
    speech.start.mockResolvedValue(speech.handle())
    const user = userEvent.setup()
    render(<MeetingPage meetings={meetings} />)
    await user.click(screen.getByRole('button', { name: '开始录制' }))
    expect(await screen.findByRole('button', { name: '停止' })).toBeInTheDocument()
    expect(speech.start).toHaveBeenCalledOnce()
    listeners.get('ended')?.()
    await vi.waitFor(() => expect(speech.start).toHaveBeenCalledTimes(2))
    expect(screen.getByRole('button', { name: '停止' })).toBeInTheDocument()
  })

  test('summarize timeout leaves retry instead of spinning 生成纪要中', async () => {
    const started: MeetingDTO = { ...base, status: 'recording', endedAt: '', durationMs: 0 }
    const stopped: MeetingDTO = { ...started, status: 'transcribed', transcript: '只有逐字稿' }
    const pending: MeetingDTO = { ...stopped, status: 'needs_summary', summaryError: '尚未生成摘要：context deadline exceeded' }
    const meetings = bridge({
      start: vi.fn().mockResolvedValue(started),
      stop: vi.fn().mockResolvedValue(stopped),
      summarize: vi.fn().mockRejectedValue(new BridgeClientError('Bridge 请求超时', 'REQUEST_DEADLINE_EXCEEDED', true, 'trace')),
      get: vi.fn().mockResolvedValue(pending),
    })
    speech.start.mockResolvedValue(speech.handle())
    const user = userEvent.setup()
    render(<MeetingPage meetings={meetings} />)
    await user.click(await screen.findByRole('button', { name: '开始录制' }))
    await user.click(await screen.findByRole('button', { name: '停止' }))
    expect(await screen.findByRole('button', { name: '重试生成摘要' })).toBeInTheDocument()
    expect(screen.getByRole('status')).toHaveTextContent(/尚未生成摘要|超时/)
    expect(screen.queryByText('生成纪要中')).not.toBeInTheDocument()
  })

  test('abandoned summarizing on open offers retry after get flips', async () => {
    const hung: MeetingDTO = { ...base, status: 'summarizing', transcript: '稿' }
    const pending: MeetingDTO = { ...hung, status: 'needs_summary', summaryError: '摘要生成中断，逐字稿已保存。可重试生成摘要。' }
    const meetings = bridge({
      list: vi.fn().mockResolvedValue({ items: [hung] }),
      get: vi.fn().mockResolvedValue(pending),
    })
    const user = userEvent.setup()
    render(<MeetingPage meetings={meetings} />)
    await user.click(await screen.findByText('周会'))
    expect(await screen.findByRole('button', { name: '重试生成摘要' })).toBeInTheDocument()
    expect(screen.getByText(/尚未生成摘要/)).toBeInTheDocument()
  })

  test('vertical splitter, delete confirm, and in-place edit persist before export', async () => {
    const past = { ...base, title: '评审会', status: 'ready' as const, summary: '对齐范围', actions: '- 导出安装包', transcript: '大家好' }
    const updated = { ...past, summary: '改过的摘要', actions: '- 新待办', transcript: '改过的稿' }
    const meetings = bridge({
      list: vi.fn().mockResolvedValue({ items: [past] }),
      get: vi.fn().mockResolvedValue(past),
      update: vi.fn().mockResolvedValue(updated),
      delete: vi.fn().mockResolvedValue({ meetingId: past.meetingId }),
      exportMeeting: vi.fn().mockResolvedValue({ path: 'C:/edited.md', format: 'markdown' }),
    })
    const user = userEvent.setup()
    render(<MeetingPage meetings={meetings} />)
    expect(await screen.findByRole('separator', { name: '调整会议列表宽度' })).toBeInTheDocument()
    await user.click(screen.getByText('评审会'))
    expect(await screen.findByDisplayValue('对齐范围')).toBeInTheDocument()
    await user.clear(screen.getByRole('textbox', { name: '会议摘要' }))
    await user.type(screen.getByRole('textbox', { name: '会议摘要' }), '改过的摘要')
    await user.clear(screen.getByRole('textbox', { name: '决议/待办' }))
    await user.type(screen.getByRole('textbox', { name: '决议/待办' }), '- 新待办')
    await user.click(screen.getByRole('button', { name: '导出 Markdown' }))
    await vi.waitFor(() => expect(meetings.update).toHaveBeenCalledWith(expect.objectContaining({
      meetingId: past.meetingId, summary: '改过的摘要',
    })))
    expect(meetings.exportMeeting).toHaveBeenCalled()
    await user.click(screen.getByRole('button', { name: '删除 评审会' }))
    expect(await screen.findByRole('dialog')).toBeInTheDocument()
    await user.click(screen.getByRole('button', { name: '确认删除' }))
    await vi.waitFor(() => expect(meetings.delete).toHaveBeenCalledWith({ meetingId: past.meetingId }))
  })

  test('keeps recording when live ASR cannot start', async () => {
    const started: MeetingDTO = { ...base, status: 'recording', endedAt: '', durationMs: 0 }
    const meetings = bridge({ start: vi.fn().mockResolvedValue(started), stop: vi.fn() })
    speech.start.mockRejectedValue(new Error('本地语音识别中断'))
    const user = userEvent.setup()
    render(<MeetingPage meetings={meetings} />)
    await user.click(await screen.findByRole('button', { name: '开始录制' }))
    expect(await screen.findByRole('button', { name: '停止' })).toBeInTheDocument()
    expect(meetings.stop).not.toHaveBeenCalled()
    expect(screen.getByRole('status')).toHaveTextContent(/本地语音识别中断/)
  })

  test('ASR end does not call meetings.stop; only 停止 does', async () => {
    const started: MeetingDTO = { ...base, status: 'recording', endedAt: '', durationMs: 0 }
    const meetings = bridge({ start: vi.fn().mockResolvedValue(started), stop: vi.fn() })
    speech.start.mockResolvedValue(speech.handle())
    const user = userEvent.setup()
    render(<MeetingPage meetings={meetings} />)
    await user.click(await screen.findByRole('button', { name: '开始录制' }))
    expect(await screen.findByRole('button', { name: '停止' })).toBeInTheDocument()
    speech.onError?.(new Error('recognition ended'))
    expect(await screen.findByRole('status')).toHaveTextContent(/录制中/)
    expect(meetings.stop).not.toHaveBeenCalled()
    expect(screen.getByRole('button', { name: '停止' })).toBeInTheDocument()
  })

  test('restarts captions after a silent stall without stopping the WAV', async () => {
    const stallFns: Array<() => void> = []
    const nativeSetInterval = window.setInterval.bind(window)
    const intervalSpy = vi.spyOn(window, 'setInterval').mockImplementation(((handler: TimerHandler, ms?: number, ...args: unknown[]) => {
      if (ms === MEETING_CAPTION_STALL_POLL_MS && typeof handler === 'function') {
        stallFns.push(handler as () => void)
      }
      return nativeSetInterval(handler, ms, ...args)
    }) as typeof setInterval)
    try {
      const started: MeetingDTO = { ...base, status: 'recording', endedAt: '', durationMs: 0 }
      const meetings = bridge({ start: vi.fn().mockResolvedValue(started), stop: vi.fn() })
      speech.start.mockResolvedValue(speech.handle())
      const user = userEvent.setup()
      render(<MeetingPage meetings={meetings} />)
      await user.click(await screen.findByRole('button', { name: '开始录制' }))
      expect(await screen.findByRole('button', { name: '停止' })).toBeInTheDocument()
      expect(speech.start).toHaveBeenCalledTimes(1)
      expect(stallFns.length).toBeGreaterThan(0)
      const origin = Date.now()
      const nowSpy = vi.spyOn(Date, 'now').mockReturnValue(origin + MEETING_CAPTION_STALL_MS + 1)
      try {
        stallFns[stallFns.length - 1]!()
        await vi.waitFor(() => expect(speech.start).toHaveBeenCalledTimes(2))
        expect(meetings.stop).not.toHaveBeenCalled()
        expect(screen.getByRole('button', { name: '停止' })).toBeInTheDocument()
      } finally {
        nowSpy.mockRestore()
      }
    } finally {
      intervalSpy.mockRestore()
    }
  })

  test('stop freezes the clock and hides 录制中 while catchup hangs', async () => {
    let resolveCatchup: (value: MeetingDTO) => void = () => undefined
    const startedAt = new Date(Date.now() - 10 * 60 * 1000).toISOString()
    const started: MeetingDTO = { ...base, status: 'recording', endedAt: '', durationMs: 0, startedAt, title: '会议 2026-08-30 00:43' }
    const stopped: MeetingDTO = { ...started, status: 'transcribed', endedAt: now, durationMs: 600_000, transcript: '歌词' }
    const ready: MeetingDTO = { ...stopped, status: 'ready', summary: '摘要', actions: '- 待办' }
    const meetings = bridge({
      start: vi.fn().mockResolvedValue(started),
      stop: vi.fn().mockResolvedValue(stopped),
      catchup: vi.fn().mockImplementation(() => new Promise<MeetingDTO>(resolve => { resolveCatchup = resolve })),
      summarize: vi.fn().mockResolvedValue(ready),
    })
    speech.start.mockResolvedValue(speech.handle())
    const user = userEvent.setup()
    render(<MeetingPage meetings={meetings} />)
    await user.click(await screen.findByRole('button', { name: '开始录制' }))
    expect(await screen.findByRole('button', { name: '停止' })).toBeInTheDocument()
    await user.click(screen.getByRole('button', { name: '停止' }))
    expect(await screen.findByRole('button', { name: '处理中…' })).toBeInTheDocument()
    expect(screen.getAllByText(/正在结束录制|正在转写补全|录音已停止/).length).toBeGreaterThan(0)
    expect(screen.queryByText('正在录制麦克风与系统声音')).not.toBeInTheDocument()
    expect(screen.queryByText('录制中')).not.toBeInTheDocument()
    expect(screen.getByText('整理中', { exact: false })).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: '停止' })).not.toBeInTheDocument()
    resolveCatchup(stopped)
    expect(await screen.findByRole('heading', { name: '会议摘要' })).toBeInTheDocument()
  })
})
