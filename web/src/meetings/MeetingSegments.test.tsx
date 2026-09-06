import { act, cleanup, render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, expect, test, vi } from 'vitest'
import type { MeetingDTO, MeetingsSegmentsListResult } from '../generated/bridge'
import { MeetingSegments } from './MeetingSegments'

afterEach(cleanup)
const meeting: MeetingDTO = { meetingId: '01ARZ3NDEKTSV4RRFFQ69G5FAV', revision: 2, title: '原始分段', status: 'transcribed', audioSource: 'microphone', startedAt: '', endedAt: '', durationMs: 0, summary: '', actions: '', transcript: '', createdAt: '', updatedAt: '' }
const page: MeetingsSegmentsListResult = { items: [{ meetingId: meeting.meetingId, segmentId: meeting.meetingId, seq: 10, startedMs: 1000, text: '原始分段正文', createdAt: '' }], nextSeq: 10, hasMore: true, throughSeq: 31, revision: 2 }

test('segment pagination holds the original upper bound while allowing an explicit fresh read', async () => {
  const load = vi.fn().mockResolvedValueOnce(page).mockResolvedValueOnce({ ...page, nextSeq: 20 }).mockResolvedValueOnce({ ...page, throughSeq: 40 })
  render(<MeetingSegments meeting={meeting} load={load} />)
  expect(load).not.toHaveBeenCalled()
  await userEvent.click(screen.getByRole('button', { name: '查看完整分段记录' }))
  await userEvent.click(screen.getByRole('button', { name: '下一页分段' }))
  expect(load).toHaveBeenLastCalledWith({ meetingId: meeting.meetingId, expectedRevision: 2, afterSeq: 10, throughSeq: 31 })
  await userEvent.click(screen.getByRole('button', { name: '查看完整分段记录' }))
  expect(load).toHaveBeenLastCalledWith({ meetingId: meeting.meetingId, expectedRevision: 2, afterSeq: 0 })
})

test('changing the selected meeting rejects a late segment page', async () => {
  let resolve!: (value: MeetingsSegmentsListResult) => void
  const load = vi.fn(() => new Promise<MeetingsSegmentsListResult>(done => { resolve = done }))
  const view = render(<MeetingSegments meeting={meeting} load={load} />)
  await userEvent.click(screen.getByRole('button', { name: '查看完整分段记录' }))
  view.rerender(<MeetingSegments meeting={{ ...meeting, meetingId: '01ARZ3NDEKTSV4RRFFQ69G5FAW' }} load={load} />)
  await act(async () => { resolve(page) })
  expect(screen.queryByText(/原始分段正文/)).not.toBeInTheDocument()
})
