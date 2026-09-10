import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterEach, expect, it, vi } from 'vitest'
import type { PeopleBridge } from '../bridge/client'
import type { PeopleMessageDTO } from '../generated/bridge'
import { PeopleImage } from './PeopleImage'

afterEach(cleanup)
const now = '2026-08-27T00:00:00.000Z'
const message: PeopleMessageDTO = {
  messageId: '01ARZ3NDEKTSV4RRFFQ69G5FAY', threadId: '01ARZ3NDEKTSV4RRFFQ69G5FAX', senderSubjectId: '01ARZ3NDEKTSV4RRFFQ69G5FAW',
  kind: 'image', body: '', fileName: 'shot.png', offerId: '01ARZ3NDEKTSV4RRFFQ69G5FAZ',
  offerStatus: 'accepted', destPath: 'C:/inbox/shot.png', fileSize: 4, createdAt: now,
}

it('does not show raw English file open failures, and ignores OS cancel', async () => {
  const onError = vi.fn()
  const fileOpen = vi.fn().mockRejectedValue(new Error('Failed to fetch'))
  const people = { filePreview: vi.fn().mockResolvedValue({ dataUrl: 'data:image/png;base64,AQID' }), fileOpen } as unknown as PeopleBridge
  render(<PeopleImage message={message} people={people} onError={onError} />)
  fireEvent.click(await screen.findByRole('button', { name: '查看图片 shot.png' }))
  fireEvent.click(screen.getByRole('button', { name: '打开原文件' }))
  await waitFor(() => expect(onError).toHaveBeenCalledWith('无法打开文件'))
  expect(onError).not.toHaveBeenCalledWith('Failed to fetch')
  cleanup()
  onError.mockClear()
  fileOpen.mockRejectedValue(new Error('用户取消了选择'))
  render(<PeopleImage message={message} people={people} onError={onError} />)
  fireEvent.click(await screen.findByRole('button', { name: '查看图片 shot.png' }))
  fireEvent.click(screen.getByRole('button', { name: '打开原文件' }))
  await waitFor(() => expect(fileOpen).toHaveBeenCalledTimes(2))
  expect(onError).not.toHaveBeenCalled()
})
