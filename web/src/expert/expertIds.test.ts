import { expect, it } from 'vitest'
import { findInstalledExpert, isEnabledMroExpert } from './expertIds'

const mro = {
  catalogItemId: 'mro-expert',
  expertId: '01ARZ3NDEKTSV4RRFFQ69G5FAA',
  state: 'enabled',
}

it('resolves an installed MRO catalog id to its ULID', () => {
  expect(findInstalledExpert([mro], 'mro-expert')?.expertId).toBe('01ARZ3NDEKTSV4RRFFQ69G5FAA')
})

it('treats a disabled MRO expert as not enabled', () => {
  expect(isEnabledMroExpert([{...mro, state: 'disabled'}])).toBe(false)
  expect(isEnabledMroExpert([mro])).toBe(true)
})
