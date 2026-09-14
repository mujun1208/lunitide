import { describe, expect, it } from 'vitest'
import { pickPhaseAssetBindings } from './phaseAssets'

describe('pickPhaseAssetBindings', () => {
  it('binds the newest enabled template to cards that still lack a templateId', () => {
    const docs = [
      { key: 'biz_req_analysis', title: '业务需求分析报告' },
      { key: 'dev_standard', title: '系统开发规范' },
    ]
    const items = [{ documentType: 'dev_standard', templateId: '01ARZ3NDEKTSV4RRFFQ69G5FAT' }]
    const templates = new Map([
      ['biz_req_analysis', [
        { id: '01ARZ3NDEKTSV4RRFFQ69G5FA1', name: '旧需求分析', updatedAt: '2026-01-01T00:00:00Z' },
        { id: '01ARZ3NDEKTSV4RRFFQ69G5FA2', name: '新需求分析', updatedAt: '2026-09-01T00:00:00Z' },
      ]],
      ['dev_standard', [
        { id: '01ARZ3NDEKTSV4RRFFQ69G5FA3', name: '开发规范模版', updatedAt: '2026-09-02T00:00:00Z' },
      ]],
    ])
    expect(pickPhaseAssetBindings(docs, items, templates)).toEqual([
      { key: 'biz_req_analysis', title: '业务需求分析报告', templateId: '01ARZ3NDEKTSV4RRFFQ69G5FA2', name: '新需求分析' },
    ])
  })
})
