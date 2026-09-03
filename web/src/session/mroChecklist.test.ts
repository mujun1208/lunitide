import { describe, expect, it, vi } from 'vitest'
import { ataFromLocator, checklistPayloadFromCites, downloadCitedChecklist } from './mroChecklist'
import type { MroCite } from './mroCite'

describe('ataFromLocator', () => {
  it('reads the ata query from an mro:// locator', () => {
    expect(ataFromLocator('mro://AMM/42?status=controlled&ata=32&tail=B-1234')).toBe('32')
  })
  it('returns empty for a locator without a query or ata', () => {
    expect(ataFromLocator('mro://AMM/42')).toBe('')
    expect(ataFromLocator('{}')).toBe('')
  })
})

describe('checklistPayloadFromCites', () => {
  it('turns each cited quote into an index-aligned step with revision and ata', () => {
    const cites: MroCite[] = [
      { docType: 'AMM', revision: '42', locator: 'mro://AMM/42?ata=32', quote: '隔离液压系统', expertName: '航空机务专家' },
      { docType: 'AMM', revision: '42', locator: 'mro://AMM/42', quote: '断开作动筒', expertName: '航空机务专家' },
    ]
    expect(checklistPayloadFromCites(cites)).toEqual({
      steps: ['隔离液压系统', '断开作动筒'],
      cites: [
        { revision: '42', locator: 'mro://AMM/42?ata=32', quote: '隔离液压系统', expertName: '航空机务专家', ata: '32' },
        { revision: '42', locator: 'mro://AMM/42', quote: '断开作动筒', expertName: '航空机务专家' },
      ],
    })
  })
  it('skips empty quotes so an advisory answer produces no fake steps', () => {
    const cites: MroCite[] = [{ revision: '42', locator: 'mro://AMM/42', quote: '   ', expertName: '航空机务专家' }]
    expect(checklistPayloadFromCites(cites)).toEqual({ steps: [], cites: [] })
  })
})

describe('downloadCitedChecklist', () => {
  it('returns false without building when there is nothing cited', async () => {
    const build = vi.fn()
    const ok = await downloadCitedChecklist([{ revision: '42', locator: 'x', quote: '', expertName: 'e' }], build)
    expect(ok).toBe(false)
    expect(build).not.toHaveBeenCalled()
  })
  it('builds and downloads when cites carry quotes', async () => {
    const build = vi.fn().mockResolvedValue({ banner: '辅助建议，不构成放行', steps: [{ n: 1, text: '隔离液压系统', revision: '42', ata: '32' }] })
    const createObjectURL = vi.fn().mockReturnValue('blob:checklist')
    const revokeObjectURL = vi.fn()
    vi.stubGlobal('URL', { createObjectURL, revokeObjectURL })
    const click = vi.spyOn(HTMLAnchorElement.prototype, 'click').mockImplementation(() => undefined)
    const ok = await downloadCitedChecklist([{ docType: 'AMM', revision: '42', locator: 'mro://AMM/42?ata=32', quote: '隔离液压系统', expertName: '航空机务专家' }], build)
    expect(ok).toBe(true)
    expect(build).toHaveBeenCalledWith({
      steps: ['隔离液压系统'],
      cites: [{ revision: '42', locator: 'mro://AMM/42?ata=32', quote: '隔离液压系统', expertName: '航空机务专家', ata: '32' }],
    })
    expect(createObjectURL).toHaveBeenCalled()
    expect(click).toHaveBeenCalled()
    vi.unstubAllGlobals()
    vi.restoreAllMocks()
  })
})
