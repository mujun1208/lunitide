import { expect, it } from 'vitest'
import {
  askCatalogForRail,
  bulletinExpertIds,
  findInstalledExpert,
  installedOpsExpertIds,
  isEnabledMroExpert,
  isEnabledOpsWorkbench,
  isUASModel,
  resolveAskExpertId,
  resolveInstalledExpertIds,
  workbenchRailForCatalog,
} from './expertIds'

const mro = {
  catalogItemId: 'mro-expert',
  expertId: '01ARZ3NDEKTSV4RRFFQ69G5FAA',
  state: 'enabled',
}

const uas = {
  catalogItemId: 'uas-airworthiness-expert',
  expertId: '01ARZ3NDEKTSV4RRFFQ69G5FAB',
  state: 'enabled',
}

it('resolves an installed MRO catalog id to its ULID', () => {
  expect(findInstalledExpert([mro], 'mro-expert')?.expertId).toBe('01ARZ3NDEKTSV4RRFFQ69G5FAA')
})

it('translates catalog slugs to installed ULIDs and reports missing', () => {
  expect(resolveInstalledExpertIds(['mro-expert', 'ppt-expert', mro.expertId], [mro])).toEqual({
    ids: [mro.expertId],
    missing: ['ppt-expert'],
  })
})

it('treats a disabled MRO expert as not enabled', () => {
  expect(isEnabledMroExpert([{...mro, state: 'disabled'}])).toBe(false)
  expect(isEnabledMroExpert([mro])).toBe(true)
})

it('shows the workbench when any ops colleague is enabled', () => {
  expect(isEnabledOpsWorkbench([{...mro, state: 'disabled'}])).toBe(false)
  expect(isEnabledOpsWorkbench([uas])).toBe(true)
  expect(installedOpsExpertIds([mro, uas])).toEqual({
    'mro-expert': mro.expertId,
    'uas-airworthiness-expert': uas.expertId,
  })
})

it('maps catalog cards to workbench rails and Ask targets', () => {
  expect(workbenchRailForCatalog('低空适航专家')).toBe('due')
  expect(workbenchRailForCatalog('航空工具化工品专家')).toBe('tools')
  expect(workbenchRailForCatalog('工具化工品专家')).toBe('tools')
  expect(workbenchRailForCatalog('航空航材专家')).toBe('parts')
  expect(workbenchRailForCatalog('航空维修计划专家')).toBe('plan')
  expect(workbenchRailForCatalog('航空机务维修专家')).toBe('manuals')
  expect(isUASModel('eVTOL-X')).toBe(true)
  expect(askCatalogForRail('due', 'B737')).toBe('mx-planning-expert')
  expect(askCatalogForRail('due', 'UAS-M300')).toBe('uas-airworthiness-expert')
  expect(askCatalogForRail('tools')).toBe('tooling-chemical-expert')
  expect(resolveAskExpertId('parts', {'parts-expert': uas.expertId}, mro.expertId)).toBe(uas.expertId)
  expect(resolveAskExpertId('manuals', {}, mro.expertId)).toBe(mro.expertId)
  expect(bulletinExpertIds({
    'tooling-chemical-expert': '01ARZ3NDEKTSV4RRFFQ69G5FAC',
    'uas-airworthiness-expert': uas.expertId,
    'mro-expert': mro.expertId,
  }, mro.expertId)).toEqual(['01ARZ3NDEKTSV4RRFFQ69G5FAC', uas.expertId])
})
