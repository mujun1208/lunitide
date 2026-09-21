import { expect, it } from 'vitest'
import { CATALOG_MEDIA_ACTIONS, CATALOG_OFFICE_MENU, CATALOG_PAGES } from '../generated/productCatalog'
import {
  HUB_FEATURES, PAGE_ATLAS, buildCatalogGraph, catalogChanges, groupChangesByPage,
  pageDossiers, pagesOfCard, pagesOfFeature, settingLabel,
} from './hubCatalog'

it('covers every frontend Page with analysis and a page-enter card', () => {
  expect(Object.keys(PAGE_ATLAS).sort()).toEqual([...CATALOG_PAGES].sort())
  for (const id of CATALOG_PAGES) {
    const page = PAGE_ATLAS[id]
    expect(page.analysis.length).toBeGreaterThan(20)
    expect(HUB_FEATURES.some(item => item.key === `feature.${page.domain}.page.${id}` && item.pages.includes(id))).toBe(true)
  }
})

// The office menu is what makes an office page reachable at all. A page filed
// under the office group with no toggle can never be opened, and a toggle for a
// page outside the group shows a menu entry that leads nowhere.
it('keeps the office menu and the office page group in step', () => {
  const grouped = CATALOG_PAGES.filter(id => PAGE_ATLAS[id].group === 'office')
  expect([...CATALOG_OFFICE_MENU].sort()).toEqual([...grouped].sort())
})

// One media action, one card. The action list comes from the Bridge contract, so
// a new transport verb shows up here before anyone writes a card by hand.
it('gives every media action its own card on the media page', () => {
  const keys = new Set(HUB_FEATURES.map(item => item.key))
  for (const action of CATALOG_MEDIA_ACTIONS) {
    expect(keys.has(`feature.office.media.${action}`)).toBe(true)
    expect(pagesOfFeature(`feature.office.media.${action}`)).toEqual(['media'])
  }
})

it('groups features and changelog by the real frontend page', () => {
  const { nodes } = buildCatalogGraph()
  const changes = catalogChanges()
  const dossiers = pageDossiers(nodes, changes, [])
  expect(dossiers).toHaveLength(17)
  expect(dossiers.find(item => item.spec.id === 'home')?.features.map(node => node.name)).toEqual(expect.arrayContaining(['打字聊天', '进入主页', '放歌']))
  expect(dossiers.find(item => item.spec.id === 'media')?.features.length).toBeGreaterThan(10)
  expect(dossiers.find(item => item.spec.id === 'settings')?.features.length).toBeGreaterThan(17)
  expect(groupChangesByPage(changes).some(group => group.page?.id === 'media' && group.items.length > 0)).toBe(true)
  expect(pagesOfFeature('feature.office.media.play')).toEqual(['media'])
  expect(pagesOfFeature('feature.dialog.music.play')).toEqual(['home', 'media'])
  expect(pagesOfCard({ stable_key: 'feature.dialog.music.play', scaffold: { pages: ['media'] } })).toEqual(['media', 'home'])
  expect(settingLabel('office-menu')).toBe('办公菜单')
  expect(dossiers.every(item => item.spec.analysis.length > 20 && item.features.length > 0)).toBe(true)
  expect(dossiers.find(item => item.spec.id === 'settings')?.settings).toHaveLength(18)
  const withFindings = pageDossiers(nodes, changes, [
    { severity: 'warn', error_code: 'PH-003', stable_key: 'capability.tts.voice', title: 'TTS', evidence: '', root_cause: '', fix: '', verify: '', status: 'open' },
    { severity: 'error', error_code: 'PH-004', stable_key: 'feature.dialog.music.play', title: '放歌', evidence: '', root_cause: '', fix: '', verify: '', status: 'open' },
    { severity: 'warn', error_code: 'PH-003', stable_key: 'feature.foundation.settings.channels', title: '通道', evidence: '', root_cause: '', fix: '', verify: '', status: 'open' },
  ])
  expect(withFindings.find(item => item.spec.id === 'home')?.findings.map(item => item.stable_key)).toEqual(['feature.dialog.music.play'])
  expect(withFindings.find(item => item.spec.id === 'media')?.findings.map(item => item.stable_key)).toEqual(['feature.dialog.music.play'])
  expect(withFindings.find(item => item.spec.id === 'settings')?.findings.map(item => item.stable_key)).toEqual(['feature.foundation.settings.channels'])
})
