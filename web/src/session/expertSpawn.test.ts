import {describe, expect, it} from 'vitest'
import {CONVERSATION_EXPERTS} from '../expert/conversationExperts'
import {expertRefPrefix} from './composerParser'
import {
  extractExpertRefIDs,
  pmRethinkSpawnsConversationSpecialists,
  selectedTurnExpertIDs,
  spawnedExpertsMatchSelection,
} from './expertSpawn'

const AI = '01ARZ3NDEKTSV4RRFFQ69G5FAV'
const ARCH = '01ARZ3NDEKTSV4RRFFQ69G5FAW'
const SEC = '01ARZ3NDEKTSV4RRFFQ69G5FAX'
const OPT = '01ARZ3NDEKTSV4RRFFQ69G5FAY'
const PPT = '01ARZ3NDEKTSV4RRFFQ69G5FAA'
const NOVEL = '01ARZ3NDEKTSV4RRFFQ69G5FAB'
const PM = '01ARZ3NDEKTSV4RRFFQ69G5FAC'
const REPORT = '01ARZ3NDEKTSV4RRFFQ69G5FAD'

describe('expert spawn filter', () => {
  it('keeps only the selected subset from session mounts', () => {
    expect(selectedTurnExpertIDs([AI, ARCH, SEC, OPT], '重新思考，给出一个新的方案。')).toEqual([AI, ARCH, SEC, OPT])
    expect(selectedTurnExpertIDs([SEC], '请评审')).toEqual([SEC])
    expect(spawnedExpertsMatchSelection([SEC], selectedTurnExpertIDs([SEC], '请评审'))).toBe(true)
  })

  it('does not spawn the rest of the catalog when one 对话 expert is selected', () => {
    const ppt = CONVERSATION_EXPERTS.find(item => item.id === 'ppt-expert')!.name
    expect(pmRethinkSpawnsConversationSpecialists([ppt], [ppt])).toBe(false)
    expect(pmRethinkSpawnsConversationSpecialists([ppt], CONVERSATION_EXPERTS.map(item => item.name))).toBe(true)
    expect(selectedTurnExpertIDs([PPT], '写一页封面')).toEqual([PPT])
  })

  it('PM rethink uses composer chips, not PPT/小说/报告 unless selected', () => {
    const turn = expertRefPrefix([
      {name: 'AI 工程师', expertId: AI},
      {name: 'solution-architect', expertId: ARCH},
      {name: '安全工程师', expertId: SEC},
      {name: '自主优化架构师', expertId: OPT},
    ]) + '重新思考，给出一个新的方案。'
    expect(extractExpertRefIDs(turn)).toEqual([AI, ARCH, SEC, OPT])
    const spawned = selectedTurnExpertIDs([PPT, NOVEL, PM, REPORT], turn)
    expect(spawned).toEqual([AI, ARCH, SEC, OPT])
    const prev = expertRefPrefix([{name: 'PPT专家', expertId: PPT}, {name: '小说编写专家', expertId: NOVEL}]) + '旧方案'
    expect(selectedTurnExpertIDs([PPT, NOVEL, PM, REPORT], turn, prev)).toEqual([AI, ARCH, SEC, OPT])
    expect(selectedTurnExpertIDs([PPT], '继续', turn)).toEqual([AI, ARCH, SEC, OPT])
    expect(spawnedExpertsMatchSelection([AI, ARCH, SEC, OPT], spawned)).toBe(true)
    expect(selectedTurnExpertIDs([PPT], '继续', turn)).toEqual([AI, ARCH, SEC, OPT])
    expect(pmRethinkSpawnsConversationSpecialists(
      ['AI 工程师', 'solution-architect', '安全工程师', '自主优化架构师'],
      ['PPT专家', '小说编写专家', '产品经理专家', '报告编写专家'],
    )).toBe(true)
    expect(pmRethinkSpawnsConversationSpecialists(
      ['AI 工程师', 'solution-architect', '安全工程师', '自主优化架构师'],
      ['AI 工程师', 'solution-architect', '安全工程师', '自主优化架构师'],
    )).toBe(false)
  })

  it('empty PM rethink does not attach the 13-specialist catalog', () => {
    expect(selectedTurnExpertIDs([], '重新思考，给出一个新的方案。')).toEqual([])
  })
})
