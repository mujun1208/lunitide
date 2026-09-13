import { describe, expect, it } from 'vitest'
import { buildTestItemsFromDev } from './ChecklistPanel'
import { checklistFromBase64, checklistToBase64, emptyChecklist, isChecklistDocument, parseChecklist, devPhaseForType } from './checklistTypes'

describe('checklistTypes', () => {
  it('round-trips Chinese checklist text through base64', () => {
    const raw = JSON.stringify({ version: 1, items: [{ id: 'F001', title: '登录', status: 'pending' }] })
    expect(checklistFromBase64(checklistToBase64(raw))).toBe(raw)
  })

  it('parses checklist json', () => {
    const doc = parseChecklist(JSON.stringify({
      version: 1,
      items: [{ id: 'F001', title: '登录', status: 'pending' }],
    }))
    expect(doc.items).toHaveLength(1)
    expect(doc.items[0]?.title).toBe('登录')
  })

  it('keeps task extras so a UI save cannot wipe them', () => {
    const doc = parseChecklist(JSON.stringify({
      version: 1,
      items: [{
        id: 'F001',
        title: '登录',
        status: 'in_progress',
        acceptance: '能登录',
        targetRelPath: 'src/auth',
        executor: 'cursor',
        testReturn: { id: 'T-F001', reason: '登录失败', at: '2026-09-13T00:00:00Z' },
      }],
    }))
    expect(doc.items[0]?.acceptance).toBe('能登录')
    expect(doc.items[0]?.targetRelPath).toBe('src/auth')
    expect(doc.items[0]?.executor).toBe('cursor')
    expect(doc.items[0]?.testReturn?.reason).toBe('登录失败')
  })

  it('returns empty doc for invalid payload', () => {
    expect(parseChecklist('not-json')).toEqual(emptyChecklist())
  })

  it('maps dev phase by project type', () => {
    expect(devPhaseForType('implementation')).toBe(5)
    expect(devPhaseForType('operations')).toBe(4)
  })

  it('treats phase-2 api_list and phase-4 interface_list as checklists', () => {
    expect(isChecklistDocument('api_list')).toBe(true)
    expect(isChecklistDocument('interface_list')).toBe(true)
    expect(isChecklistDocument('req_task_list')).toBe(true)
    expect(isChecklistDocument('biz_flow_list')).toBe(true)
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
