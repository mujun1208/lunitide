import { expect, it } from 'vitest'
import { pickLatestPending, previewPendingMemory } from './pendingMemory'

it('picks the newest pending candidate and skips a dismissed id', () => {
  const older = { candidateId: '01ARZ3NDEKTSV4RRFFQ69G5FAA', confirmationToken: 'a', content: '旧', createdAt: '2026-08-31T00:00:00Z' }
  const newer = { candidateId: '01ARZ3NDEKTSV4RRFFQ69G5FAB', confirmationToken: 'b', content: '新偏好用中文', createdAt: '2026-08-31T01:00:00Z' }
  expect(pickLatestPending([older, newer])?.candidateId).toBe(newer.candidateId)
  expect(pickLatestPending([older, newer], newer.candidateId)?.candidateId).toBe(older.candidateId)
  expect(pickLatestPending([], '')).toBeUndefined()
})

it('clips the working gist for the session banner', () => {
  expect(previewPendingMemory('用户：以后用中文\n要点：好')).toBe('用户：以后用中文 要点：好')
  expect(previewPendingMemory('x'.repeat(80)).endsWith('…')).toBe(true)
})
