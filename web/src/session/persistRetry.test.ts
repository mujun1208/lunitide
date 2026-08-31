import { expect, it } from 'vitest'
import { clearPersistFailed, persistFailedKey, readPersistFailed, writePersistFailed } from './persistRetry'

it('keeps persist-failed draft across remount', () => {
  const id = '01ARZ3NDEKTSV4RRFFQ69G5FAV'
  localStorage.removeItem(persistFailedKey(id))
  expect(readPersistFailed(id)).toBeUndefined()
  writePersistFailed(id, '已经生成但没落库')
  expect(readPersistFailed(id)?.draft).toBe('已经生成但没落库')
  localStorage.setItem(persistFailedKey(id), '1')
  expect(readPersistFailed(id)).toEqual({})
  clearPersistFailed(id)
  expect(readPersistFailed(id)).toBeUndefined()
})
