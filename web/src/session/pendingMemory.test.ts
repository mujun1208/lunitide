import { expect, it } from 'vitest'
import { locatorIsUncontrolled, pendingFromUncontrolledCites, pendingMroFromMessages } from './pendingMemory'

it('detects uncontrolled locators in JSON and mro URLs', () => {
  expect(locatorIsUncontrolled('{"status":"uncontrolled"}')).toBe(true)
  expect(locatorIsUncontrolled('mro://AMM/42?ata=32&status=uncontrolled')).toBe(true)
  expect(locatorIsUncontrolled('{"status":"controlled"}')).toBe(false)
})

it('builds the H12 uncontrolled pending item', () => {
  const item = pendingFromUncontrolledCites([{ locator: '{"status":"uncontrolled"}' }])
  expect(item?.kind).toBe('mro-uncontrolled')
  expect(item?.content).toBe('待确认：将使用未受控手册回答')
  expect(item?.content).not.toMatch(/放行/)
})

it('reads uncontrolled cites from an assistant mro-cite marker', () => {
  const item = pendingMroFromMessages([{
    role: 'assistant',
    id: '01ARZ3NDEKTSV4RRFFQ69G5FAD',
    text: '辅助建议，不构成放行。<!--mro-cite:{"cites":[{"revision":"42","locator":"{\\"status\\":\\"uncontrolled\\"}","quote":"x","expertName":"航空机务专家"}]}-->',
  }])
  expect(item?.kind).toBe('mro-uncontrolled')
  expect(item?.candidateId).toBe('01ARZ3NDEKTSV4RRFFQ69G5FAD')
})
