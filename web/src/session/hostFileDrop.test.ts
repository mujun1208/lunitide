import { expect, it } from 'vitest'
import { hostDropCovers, rememberHostDrop } from './hostFileDrop'

it('remembers only the files the host just registered', () => {
  const now = 1_000
  rememberHostDrop([{ fileName: '需求.docx' }], now)
  expect(hostDropCovers(['需求.docx'], now + 100)).toBe(true)
  expect(hostDropCovers(['其他.txt'], now + 100)).toBe(false)
  expect(hostDropCovers(['需求.docx'], now + 10_000)).toBe(false)
})
