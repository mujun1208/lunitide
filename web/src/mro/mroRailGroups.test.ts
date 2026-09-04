import {afterEach, expect, it} from 'vitest'
import {
  groupIdForRail,
  MRO_RAIL_GROUPS,
  MRO_RAIL_OPEN_KEY,
  railGroupExpanded,
  readRailOpen,
  writeRailOpen,
} from './mroRailGroups'

afterEach(() => {
  localStorage.removeItem(MRO_RAIL_OPEN_KEY)
})

it('exposes five first-level groups with the locked leaf mapping', () => {
  expect(MRO_RAIL_GROUPS.map(group => group.label)).toEqual(['手册', '机务维修', '航材', '工具化工品', '工具'])
  expect(MRO_RAIL_GROUPS.find(group => group.id === 'mx')?.rails).toEqual(['fault', 'due'])
  expect(MRO_RAIL_GROUPS.find(group => group.id === 'parts')?.rails).toEqual(['parts', 'plan'])
  expect(MRO_RAIL_GROUPS.find(group => group.id === 'utils')?.rails).toEqual(['checklist', 'audit', 'fleet'])
  expect(groupIdForRail('plan')).toBe('parts')
  expect(groupIdForRail('due')).toBe('mx')
})

it('force-opens the group that owns the current rail', () => {
  expect(railGroupExpanded('parts', 'plan', {})).toBe(true)
  expect(railGroupExpanded('mx', 'plan', {mx: true})).toBe(true)
  expect(railGroupExpanded('mx', 'plan', {})).toBe(true)
  expect(railGroupExpanded('mx', 'plan', {mx: false})).toBe(false)
  writeRailOpen({utils: true})
  expect(readRailOpen()).toEqual({utils: true})
})
