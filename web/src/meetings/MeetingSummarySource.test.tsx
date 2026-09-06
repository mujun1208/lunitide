import { act, cleanup, render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, expect, test, vi } from 'vitest'
import type { MeetingDTO, MeetingsSummarySourceGetResult } from '../generated/bridge'
import { MeetingSummarySource } from './MeetingSummarySource'

afterEach(cleanup)
const sourceDigest = 'a'.repeat(64)
const meeting: MeetingDTO = {
  meetingId: '01ARZ3NDEKTSV4RRFFQ69G5FAV', revision: 7, title: '当前标题', status: 'needs_summary', audioSource: 'microphone',
  startedAt: '', endedAt: '', durationMs: 0, summary: '保留的旧摘要', actions: '', transcript: '当前原稿', createdAt: '', updatedAt: '',
  transcriptRevision: 3, summarySourceRevision: 2, summarySourceDigest: sourceDigest, summaryEdited: true,
}
const page: MeetingsSummarySourceGetResult = { meetingId: meeting.meetingId, sourceRevision: 2, sourceDigest, title: '生成时标题', transcript: '此前的原稿', offset: 0, nextOffset: 16_384, totalRunes: 16_390 }

test('unknown source is explicit and cannot invent a source snapshot', () => {
  const load = vi.fn()
  render(<MeetingSummarySource meeting={{ ...meeting, summarySourceRevision: 0, summarySourceDigest: undefined }} load={load} />)
  expect(screen.getByText(/摘要输入版本未知/)).toBeInTheDocument()
  expect(screen.queryByRole('button', { name: '查看摘要所用原稿' })).not.toBeInTheDocument()
  expect(load).not.toHaveBeenCalled()
})

test('stale and manually edited output exposes only requested source pages', async () => {
  const load = vi.fn().mockResolvedValueOnce(page).mockResolvedValueOnce({ ...page, transcript: '最后六字原文', offset: 16_384, nextOffset: 0 })
  render(<MeetingSummarySource meeting={meeting} load={load} />)
  expect(screen.getByText(/依据版本 2，当前原稿为版本 3/)).toHaveTextContent('摘要或待办已人工编辑')
  expect(load).not.toHaveBeenCalled()
  await userEvent.click(screen.getByRole('button', { name: '查看摘要所用原稿' }))
  expect(await screen.findByLabelText('摘要来源原稿')).toHaveValue(page.transcript)
  await userEvent.click(screen.getByRole('button', { name: '下一页来源' }))
  expect(load).toHaveBeenLastCalledWith({ meetingId: meeting.meetingId, sourceDigest, offset: 16_384 })
  expect(screen.getByLabelText('摘要来源原稿')).toHaveValue('最后六字原文')
  expect(screen.getByRole('button', { name: '下一页来源' })).toBeDisabled()
  expect(screen.getByRole('button', { name: '上一页来源' })).toBeEnabled()
})

test('late source response after changing meetings cannot appear in the next meeting', async () => {
  let resolve!: (value: MeetingsSummarySourceGetResult) => void
  const load = vi.fn(() => new Promise<MeetingsSummarySourceGetResult>(done => { resolve = done }))
  const view = render(<MeetingSummarySource meeting={meeting} load={load} />)
  await userEvent.click(screen.getByRole('button', { name: '查看摘要所用原稿' }))
  view.rerender(<MeetingSummarySource meeting={{ ...meeting, meetingId: '01ARZ3NDEKTSV4RRFFQ69G5FAW' }} load={load} />)
  await act(async () => { resolve(page); await Promise.resolve() })
  expect(screen.queryByLabelText('摘要来源原稿')).not.toBeInTheDocument()
  expect(screen.getByRole('button', { name: '查看摘要所用原稿' })).toBeEnabled()
})
