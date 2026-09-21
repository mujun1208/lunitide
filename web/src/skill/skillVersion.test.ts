import {expect, it} from 'vitest'
import {
  compareSemver,
  deduplicateByName,
  olderDuplicates,
  skillCreateExistingHint,
  skillNameKey,
} from './skillVersion'

it('normalizes catalog names and ranks semver', () => {
  expect(skillNameKey('tpl-Weekly-Report')).toBe('weekly-report')
  expect(compareSemver('1.2.0', '1.1.9')).toBe(1)
  expect(compareSemver('1.0.0', '1.0.0')).toBe(0)
  expect(compareSemver('0.9.0', '1.0.0')).toBe(-1)
})

it('keeps the newest skill per name and lists older duplicates', () => {
  const items = [
    {id: 'old', name: 'tpl-weekly-report', version: '1.0.0', status: 'published'},
    {id: 'new', name: 'weekly-report', version: '1.1.0', status: 'published'},
    {id: 'other', name: 'docx-writer', version: '1.0.0', status: 'published'},
  ]
  expect(deduplicateByName(items).map(item => item.id)).toEqual(['new', 'other'])
  expect(olderDuplicates(items).map(item => item.id)).toEqual(['old'])
})

it('hints create-in-chat to upgrade existing skills instead of duplicating', () => {
  const hint = skillCreateExistingHint([
    {name: 'skill-creator', displayName: 'skill-creator', version: '1.0.0'},
    {name: 'tpl-weekly-report', displayName: '周报', version: '1.2.0'},
  ])
  expect(hint).toContain('周报')
  expect(hint).toContain('升级原技能')
  expect(hint).not.toContain('skill-creator')
})
