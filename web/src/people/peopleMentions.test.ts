import { expect, it } from 'vitest'
import { filterMentionAgents, filterMentionMembers, insertMention, mentionQuery, parseClaimedTasks, pendingClaimTask } from './peopleMentions'

it('completes @ mentions in the people composer', () => {
  expect(mentionQuery('请 @报')).toBe('报')
  expect(mentionQuery('你好 @')).toBe('')
  expect(insertMention('请 @报', '报告编写专家')).toBe('请 @报告编写专家 ')
  expect(filterMentionAgents([{ subjectId: '1', nickname: 'PPT专家' }, { subjectId: '2', nickname: '报告编写专家' }], 'ppt').map(item => item.nickname)).toEqual(['PPT专家'])
  expect(filterMentionMembers([{ subjectId: 'jia', nickname: '同事甲' }, { subjectId: '1', nickname: 'PPT专家' }], '').map(item => item.nickname)).toEqual(['同事甲', 'PPT专家'])
})

it('reads claim chips from colleague messages', () => {
  expect(pendingClaimTask('@PPT专家 认领 周报封面')).toBe('周报封面')
  expect(parseClaimedTasks(['@报告编写专家 认领 周报封面', '这个任务已经由「PPT专家」认领。'])).toEqual([
    { task: '周报封面', owner: 'PPT专家' },
  ])
})
