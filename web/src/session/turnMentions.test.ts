import { expect, it } from 'vitest'
import { extractExpertRefNames, parseTurnMentions } from './turnMentions'
import vectors from './turnMentions.vectors.json'

const ID = '01ARZ3NDEKTSV4RRFFQ69G5FAV'

it('parses expert tokens and @ names as the same actor', () => {
  const token = parseTurnMentions(`[引用专家 安全工程师|${ID}] 请审一下`)
  const at = parseTurnMentions('@安全工程师 请审一下')
  expect(token).toEqual([{ kind: 'expert', id: ID, name: '安全工程师' }])
  expect(at).toEqual([{ kind: 'member', id: '', name: '安全工程师' }])
  expect(extractExpertRefNames(`[引用专家 安全工程师|${ID}] 请审一下`)).toEqual(['安全工程师'])
})

it('matches the shared C4 mention vectors', () => {
  for (const vec of vectors) {
    expect(parseTurnMentions(vec.text), vec.name).toEqual(vec.want)
  }
})
