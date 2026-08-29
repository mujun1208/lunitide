import { describe, expect, test } from 'vitest'
import { SETTINGS_NAV_GROUPS, filterSettingsNav } from './settingsNav'

describe('settings nav search and groups', () => {
  test('groups cover every category once', () => {
    const ids = SETTINGS_NAV_GROUPS.flatMap(g => g.ids)
    expect(new Set(ids).size).toBe(ids.length)
    expect(filterSettingsNav('').map(c => c.id)).toEqual(ids)
  })

  test('search matches labels and keywords', () => {
    expect(filterSettingsNav('人设').map(c => c.id)).toEqual(['general'])
    expect(filterSettingsNav('白名单').map(c => c.id)).toEqual(['security'])
    expect(filterSettingsNav('局域网').map(c => c.id)).toEqual(['profile'])
    expect(filterSettingsNav('晓晓').map(c => c.id)).toEqual(['voice'])
    expect(filterSettingsNav('sherpa').map(c => c.id)).toEqual(['voice'])
    expect(filterSettingsNav('生图').map(c => c.id)).toEqual(['providers'])
    expect(filterSettingsNav('飞书').map(c => c.id)).toEqual(['channels'])
    expect(filterSettingsNav('webhook').map(c => c.id)).toEqual(['channels'])
    expect(filterSettingsNav('没有这个设置项xyz').length).toBe(0)
  })
})
