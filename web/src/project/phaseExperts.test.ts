import {describe, expect, it, vi} from 'vitest'
import type {ExpertBridge} from '../bridge/client'
import {CONVERSATION_EXPERTS} from '../expert/conversationExperts'
import {
  applySessionPhaseExperts,
  isConversationSpecialistName,
  phaseSeedExpertIds,
  resolvePhaseExpertIds,
  sessionExpertsAfterPhaseSeed,
} from './phaseExperts'

const AI = '01ARZ3NDEKTSV4RRFFQ69G5FAV'
const ARCH = '01ARZ3NDEKTSV4RRFFQ69G5FAW'
const PPT = '01ARZ3NDEKTSV4RRFFQ69G5FAA'
const NOVEL = '01ARZ3NDEKTSV4RRFFQ69G5FAB'
const REPORT = '01ARZ3NDEKTSV4RRFFQ69G5FAC'
const PM = '01ARZ3NDEKTSV4RRFFQ69G5FAD'

describe('PM phase expert seed', () => {
  it('does not treat conversation specialists as the default gate set', () => {
    expect(isConversationSpecialistName('PPT专家')).toBe(true)
    expect(isConversationSpecialistName('小说编写专家')).toBe(true)
    expect(isConversationSpecialistName('报告编写专家')).toBe(true)
    expect(isConversationSpecialistName('产品经理专家')).toBe(true)
    expect(isConversationSpecialistName('AI 工程师')).toBe(false)
    expect(CONVERSATION_EXPERTS).toHaveLength(13)
    const row = {
      phaseKey: 'REQUIREMENT_DEFINITION' as const,
      defaults: [
        {expertId: PPT, division: 'product' as const},
        {expertId: NOVEL, division: 'product' as const},
        {expertId: PM, division: 'product' as const},
        {expertId: REPORT, division: 'product' as const},
      ],
      mountings: [] as Array<{state: string; expertId: string}>,
    }
    expect(phaseSeedExpertIds(row as never)).toEqual([])
  })

  it('never overwrites composer chips with catalog defaults', () => {
    expect(sessionExpertsAfterPhaseSeed([AI, ARCH], [PPT, NOVEL, PM, REPORT])).toEqual([AI, ARCH])
    expect(sessionExpertsAfterPhaseSeed([], [])).toEqual([])
  })

  it('seeds only confirmed phase mounts when the session is empty', () => {
    const mounted = {expertId: AI, versionId: AI, semver: '1.0.0', state: 'mounted' as const, expertState: 'enabled' as const, mountingId: AI}
    expect(phaseSeedExpertIds({
      phaseKey: 'REQUIREMENT_DEFINITION',
      defaults: [{expertId: PPT, division: 'product'}],
      mountings: [mounted],
    })).toEqual([AI])
  })
})

describe('applySessionPhaseExperts', () => {
  it('keeps selected chips and does not attach ppt/novel/report on rethink seed', async () => {
    const sessionMountSet = vi.fn()
    const experts = {
      sessionMountGet: vi.fn().mockResolvedValue({expertIds: [AI, ARCH]}),
      sessionMountSet,
      mountingGet: vi.fn().mockResolvedValue({
        matrix: [{
          phaseKey: 'REQUIREMENT_DEFINITION',
          defaults: [{expertId: PPT, division: 'product'}, {expertId: NOVEL, division: 'product'}],
          mountings: [],
        }],
      }),
    } as unknown as ExpertBridge
    const ids = await applySessionPhaseExperts('01ARZ3NDEKTSV4RRFFQ69G5FAX', '01ARZ3NDEKTSV4RRFFQ69G5FAY', '需求架构规范', experts)
    expect(ids).toEqual([AI, ARCH])
    expect(sessionMountSet).not.toHaveBeenCalled()
    expect(await resolvePhaseExpertIds('01ARZ3NDEKTSV4RRFFQ69G5FAY', '需求架构规范', experts)).toEqual([])
  })
})
