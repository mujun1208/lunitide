import { describe, expect, it } from 'vitest'
import { fileFromTemplate, goalWithAssetDraft, officeTemplates } from './officeAssetTemplates'
import type { OfficeTemplate } from './officeAssetTemplates'

const item = (patch: Partial<OfficeTemplate>): OfficeTemplate => ({
  id: '01ARZ3NDEKTSV4RRFFQ69G5FAT',
  templateCode: 'TPL00001',
  name: '季度汇报',
  templateType: 'ppt',
  fileName: '季度汇报.pptx',
  status: 'enabled',
  createdAt: '2026-09-24T00:00:00Z',
  updatedAt: '2026-09-24T00:00:00Z',
  version: 1,
  ...patch,
})

describe('office asset templates', () => {
  it('keeps only enabled ppt word and excel templates', () => {
    const got = officeTemplates([
      item({}),
      item({ id: '01ARZ3NDEKTSV4RRFFQ69G5FAU', templateType: 'word', fileName: '纪要.docx', name: '纪要' }),
      item({ id: '01ARZ3NDEKTSV4RRFFQ69G5FAV', templateType: 'excel', fileName: 'ledger.xlsx', status: 'draft' }),
      item({ id: '01ARZ3NDEKTSV4RRFFQ69G5FAY', templateType: 'ppt', fileName: 'old.ppt' }),
      item({ id: '01ARZ3NDEKTSV4RRFFQ69G5FAW', templateType: 'document' }),
      item({ id: '01ARZ3NDEKTSV4RRFFQ69G5FAX', templateType: 'scaffold', status: 'enabled' }),
    ])
    expect(got.map(row => row.name)).toEqual(['季度汇报', '纪要'])
  })

  it('rebuilds the uploaded file and names it in the office goal', () => {
    const file = fileFromTemplate('deck.pptx', btoa('pptx-bytes'))
    expect(file.name).toBe('deck.pptx')
    expect(file.size).toBe('pptx-bytes'.length)
    expect(goalWithAssetDraft('做一季度汇报', '季度汇报')).toContain('底稿使用资产模版「季度汇报」')
    expect(goalWithAssetDraft('做一季度汇报', '')).toBe('做一季度汇报')
  })
})
