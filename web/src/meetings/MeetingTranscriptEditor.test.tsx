import { act, cleanup, fireEvent, render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, expect, test, vi } from 'vitest'
import type { MeetingsBridge } from '../bridge/client'
import type { MeetingDTO, MeetingsTranscriptGetResult } from '../generated/bridge'
import { MeetingTranscriptEditor } from './MeetingTranscriptEditor'

afterEach(cleanup)
const meeting: MeetingDTO = { meetingId: '01ARZ3NDEKTSV4RRFFQ69G5FAV', revision: 5, transcriptRevision: 2, title: '长会', status: 'transcribed', audioSource: 'microphone', startedAt: '', endedAt: '', durationMs: 0, summary: '', actions: '', transcript: '首屏预览', transcriptComplete: false, transcriptTotalRunes: 40_000, createdAt: '', updatedAt: '' }
const first: MeetingsTranscriptGetResult = { meetingId: meeting.meetingId, transcriptRevision: 2, text: `😀${'文'.repeat(16_383)}`, offset: 0, nextOffset: 16_384, totalRunes: 40_000 }
const second = { ...first, text: '第二页内容', offset: 16_384, nextOffset: 32_768 }
const saved = { ...meeting, revision: 6, transcriptRevision: 3, transcriptTotalRunes: 23_618 }

function api(overrides: Partial<MeetingsBridge> = {}) {
  return { transcriptGet: vi.fn().mockResolvedValue(first), update: vi.fn(), get: vi.fn().mockResolvedValue(meeting), ...overrides } as unknown as MeetingsBridge
}

test('page edits use Unicode character offsets, preserve the rest, and wait for commit before navigation', async () => {
  let acknowledge!: (value: MeetingDTO) => void
  const update = vi.fn(() => new Promise<MeetingDTO>(resolve => { acknowledge = resolve }))
  const bridge = api({ update, transcriptGet: vi.fn().mockResolvedValueOnce(first).mockResolvedValueOnce({ ...first, transcriptRevision: 3, text: '修改后的完整一页' }) })
  const onSaved = vi.fn()
  render(<MeetingTranscriptEditor meeting={meeting} meetings={bridge} onSaved={onSaved} />)
  const editor = await screen.findByLabelText('本页逐字稿')
  fireEvent.change(editor, { target: { value: '修订' } })
  expect(screen.getByRole('button', { name: '下一页原稿' })).toBeDisabled()
  await userEvent.click(screen.getByRole('button', { name: '保存本页原稿' }))
  expect(update).toHaveBeenCalledWith({ meetingId: meeting.meetingId, expectedRevision: 5, transcriptEdit: { transcriptRevision: 2, offset: 0, deleteRunes: 16_384, text: '修订' } })
  expect(editor).toBeDisabled()
  expect(onSaved).not.toHaveBeenCalled()
  await act(async () => { acknowledge(saved) })
  expect(onSaved).toHaveBeenCalledWith(saved)
  expect(bridge.transcriptGet).toHaveBeenLastCalledWith({ meetingId: meeting.meetingId, transcriptRevision: 3, offset: 0 })
  expect(screen.getByLabelText('本页逐字稿')).toHaveValue('修改后的完整一页')
})

test('source pages navigate with the pinned transcript revision', async () => {
  const bridge = api({ transcriptGet: vi.fn().mockResolvedValueOnce(first).mockResolvedValueOnce(second).mockResolvedValueOnce(first) })
  render(<MeetingTranscriptEditor meeting={meeting} meetings={bridge} onSaved={vi.fn()} />)
  await screen.findByLabelText('本页逐字稿')
  await userEvent.click(screen.getByRole('button', { name: '下一页原稿' }))
  expect(screen.getByLabelText('本页逐字稿')).toHaveValue('第二页内容')
  expect(bridge.transcriptGet).toHaveBeenLastCalledWith({ meetingId: meeting.meetingId, transcriptRevision: 2, offset: 16_384 })
  await userEvent.click(screen.getByRole('button', { name: '上一页原稿' }))
  expect(bridge.transcriptGet).toHaveBeenLastCalledWith({ meetingId: meeting.meetingId, transcriptRevision: 2, offset: 0 })
})

test('conflict retains the draft and requires reviewing the latest page before replacing its baseline', async () => {
  const update = vi.fn().mockRejectedValue(new Error('会议已变化'))
  const bridge = api({ update, get: vi.fn().mockResolvedValue(saved), transcriptGet: vi.fn().mockResolvedValueOnce(first).mockResolvedValueOnce({ ...first, text: '其他人的新页', transcriptRevision: 3 }) })
  const onSaved = vi.fn()
  render(<MeetingTranscriptEditor meeting={meeting} meetings={bridge} onSaved={onSaved} />)
  fireEvent.change(await screen.findByLabelText('本页逐字稿'), { target: { value: '我的未提交修订' } })
  await userEvent.click(screen.getByRole('button', { name: '保存本页原稿' }))
  expect(screen.getByLabelText('本页逐字稿')).toHaveValue('我的未提交修订')
  await userEvent.click(screen.getByRole('button', { name: '核对最新原稿' }))
  expect(screen.getByLabelText('最新页逐字稿')).toHaveValue('其他人的新页')
  expect(screen.getByLabelText('本页逐字稿')).toHaveValue('我的未提交修订')
  expect(update).toHaveBeenCalledTimes(1)
  expect(onSaved).not.toHaveBeenCalled()
  await userEvent.click(screen.getByRole('button', { name: '采用最新页并放弃本页草稿' }))
  expect(onSaved).toHaveBeenCalledWith(saved)
  expect(screen.getByLabelText('本页逐字稿')).toHaveValue('其他人的新页')
})

test('late page save acknowledgement after leaving cannot update another meeting', async () => {
  let acknowledge!: (value: MeetingDTO) => void
  const bridge = api({ update: vi.fn(() => new Promise<MeetingDTO>(resolve => { acknowledge = resolve })) })
  const onSaved = vi.fn()
  const view = render(<MeetingTranscriptEditor meeting={meeting} meetings={bridge} onSaved={onSaved} />)
  fireEvent.change(await screen.findByLabelText('本页逐字稿'), { target: { value: '待保存内容' } })
  await userEvent.click(screen.getByRole('button', { name: '保存本页原稿' }))
  view.unmount()
  await act(async () => { acknowledge(saved) })
  expect(onSaved).not.toHaveBeenCalled()
  expect(bridge.transcriptGet).toHaveBeenCalledTimes(1)
})

test('oversized page edit stays visible and is never silently truncated or sent', async () => {
  const bridge = api()
  render(<MeetingTranscriptEditor meeting={meeting} meetings={bridge} onSaved={vi.fn()} />)
  const text = '字'.repeat(16_385)
  fireEvent.change(await screen.findByLabelText('本页逐字稿'), { target: { value: text } })
  expect(screen.getByLabelText('本页逐字稿')).toHaveValue(text)
  expect(screen.getByRole('alert')).toHaveTextContent('尚未保存')
  expect(screen.getByRole('button', { name: '保存本页原稿' })).toBeDisabled()
  expect(bridge.update).not.toHaveBeenCalled()
})
