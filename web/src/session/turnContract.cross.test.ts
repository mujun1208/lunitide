import { expect, it } from 'vitest'
import { shouldOpenExpertAsColleague } from '../expert/conversationExperts'
import { insertMention } from '../people/peopleMentions'
import { insertComposerAtPick } from './composerAtMenu'
import { parseTurnMentions } from './turnMentions'
import vectors from './turnMentions.vectors.json'
import { PERSIST_RETRY_SENTINEL } from './persistRetry'
import { bannersFromTurnState } from './sessionResume'

it('C4 script: composer tokens plus shared mention vectors (engine C4 is the handler script)', () => {
  const textPref = '记住：晚上用深色主题'
  expect(textPref).toContain('晚上用深色主题')
  expect(vectors.some(vec => vec.text.includes('晚上用深色') || vec.text.includes('继续刚才的'))).toBe(true)

  const sessionContinue = insertComposerAtPick('继续刚才的 @', { kind: 'expert', id: '01ARZ3NDEKTSV4RRFFQ69G5FAC', label: 'PPT专家' })
  expect(sessionContinue).toContain('[引用专家 PPT专家|01ARZ3NDEKTSV4RRFFQ69G5FAC]')
  expect(sessionContinue).toContain('继续刚才的')

  const companionResume = bannersFromTurnState({
    live: false,
    server: { status: 'interrupted', persistFailed: false, persistDraft: '' },
  })
  expect(companionResume).toEqual({ persistFailed: false, persistDraft: undefined, resume: true })
  const companionRunningDraft = bannersFromTurnState({
    live: false,
    server: { status: 'running', persistFailed: false, persistDraft: '流到一半还没写完' },
  })
  expect(companionRunningDraft).toEqual({ persistFailed: false, persistDraft: '流到一半还没写完', resume: true })
  const companionPersist = bannersFromTurnState({
    live: false,
    server: { status: 'completed', persistFailed: true, persistDraft: '已经生成但没落库' },
  })
  expect(companionPersist).toMatchObject({ persistFailed: true, resume: false })
  expect(PERSIST_RETRY_SENTINEL.startsWith('\u2063')).toBe(true)

  const colleague = insertMention('继续刚才的 @', 'PPT专家')
  expect(colleague).toBe('继续刚才的 @PPT专家 ')
  expect(colleague).not.toContain('[引用专家')
  const mentions = parseTurnMentions(`${sessionContinue}\n${colleague}`)
  expect(mentions.some(item => item.kind === 'expert' && item.name === 'PPT专家')).toBe(true)
  expect(mentions.some(item => item.kind === 'member' && item.name === 'PPT专家')).toBe(true)
  expect(shouldOpenExpertAsColleague('PPT专家')).toBe(true)
  expect(shouldOpenExpertAsColleague('安全工程师')).toBe(false)
  for (const vec of vectors) {
    expect(parseTurnMentions(vec.text), vec.name).toEqual(vec.want)
  }
})
