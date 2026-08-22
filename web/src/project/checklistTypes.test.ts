import { describe, expect, it } from 'vitest'
import { buildTestItemsFromDev } from './ChecklistPanel'
import { emptyChecklist, parseChecklist, devPhaseForType } from './checklistTypes'

describe('checklistTypes', () => {
  it('parses checklist json', () => {
    const doc = parseChecklist(JSON.stringify({
      version: 1,
      items: [{ id: 'F001', title: '登录', status: 'pending' }],
    }))
    expect(doc.items).toHaveLength(1)
    expect(doc.items[0]?.title).toBe('登录')
  })

  it('returns empty doc for invalid payload', () => {
    expect(parseChecklist('not-json')).toEqual(emptyChecklist())
  })

  it('maps dev phase by project type', () => {
    expect(devPhaseForType('implementation')).toBe(5)
    expect(devPhaseForType('operations')).toBe(4)
  })
})

describe('buildTestItemsFromDev', () => {
  it('copies dev_done rows into test checklist', () => {
    const dev = {
      version: 1 as const,
      items: [
        { id: 'D001', title: '模块A', status: 'dev_done' as const },
        { id: 'D002', title: '模块B', status: 'pending' as const },
      ],
    }
    const imported = buildTestItemsFromDev(dev, emptyChecklist())
    expect(imported).toHaveLength(1)
    expect(imported[0]?.sourceId).toBe('D001')
    expect(imported[0]?.status).toBe('pending')
  })
})
