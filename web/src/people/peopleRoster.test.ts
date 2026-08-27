import { describe, expect, test } from 'vitest'
import { displayName, filterContacts, filterMessages, filterThreads, formatBytes, groupContactsByOrg, lastPreview, orgGroupLabel, statusLabel, threadTitle, unreadTotal, type PeopleContact } from './peopleRoster'

const contact = (partial: Partial<PeopleContact>): PeopleContact => ({
  subjectId: '01ARZ3NDEKTSV4RRFFQ69G5FAV',
  nickname: '甲',
  avatar: '',
  status: 'online',
  department: '研发',
  title: '',
  orgName: '月汐',
  bio: '',
  publicKey: '',
  trustState: 'discovered',
  hostAddr: '10.0.0.2',
  lastSeenAt: '',
  self: false,
  ...partial,
})

describe('people roster grouping', () => {
  test('groups by organization then department', () => {
    const groups = groupContactsByOrg([
      contact({ nickname: '乙', department: '设计', subjectId: '01ARZ3NDEKTSV4RRFFQ69G5FAW' }),
      contact({ nickname: '甲' }),
      contact({ nickname: '丙', orgName: '', department: '', subjectId: '01ARZ3NDEKTSV4RRFFQ69G5FAX' }),
    ])
    expect(groups.map(g => g.label)).toEqual(['月汐 · 设计', '月汐 · 研发', '未分组'])
    expect(groups.find(g => g.label === '月汐 · 研发')?.people.map(p => p.nickname)).toEqual(['甲'])
  })

  test('filters loaded history locally', () => {
    expect(filterMessages([{ body: '你好' }, { body: '文件', fileName: 'a.png' }], 'png')).toHaveLength(1)
    expect(statusLabel('busy')).toBe('忙碌')
    expect(orgGroupLabel(contact({ orgName: '', department: '运营' }))).toBe('运营')
  })

  test('previews last message and sums unread', () => {
    expect(lastPreview('image', '', 'a.png')).toBe('[图片]')
    expect(lastPreview('file', '', 'note.txt')).toBe('[文件] note.txt')
    expect(lastPreview('text', '你好')).toBe('你好')
    expect(unreadTotal([{ unreadCount: 2 }, { unreadCount: 0 }, {}])).toBe(2)
    expect(displayName({ nickname: '甲', remark: '阿甲' })).toBe('阿甲')
    expect(threadTitle({ kind: 'direct', members: [{ nickname: '甲', remark: '阿甲', self: false }, { nickname: 'mu', self: true }] })).toBe('阿甲')
  })

  test('filters contacts and threads for WeChat-mode search', () => {
    const people = [
      contact({ nickname: '甲', department: '研发' }),
      contact({ nickname: '乙', department: '设计', remark: '阿乙', subjectId: '01ARZ3NDEKTSV4RRFFQ69G5FAW' }),
    ]
    expect(filterContacts(people, '设计').map(p => p.nickname)).toEqual(['乙'])
    expect(filterContacts(people, '阿乙').map(p => p.nickname)).toEqual(['乙'])
    expect(formatBytes(4)).toBe('4 B')
    expect(formatBytes(2048)).toBe('2.0 KB')
    const threads = [
      { kind: 'direct' as const, title: '', members: [{ nickname: '甲', self: false }], lastMessage: { kind: 'file', fileName: 'secret.txt' } },
      { kind: 'group' as const, title: '研发群', members: [{ nickname: '乙', self: false }], lastMessage: { kind: 'text', body: '你好' } },
    ]
    expect(filterThreads(threads, 'secret')).toHaveLength(1)
    expect(filterThreads(threads, '研发群').map(t => t.title)).toEqual(['研发群'])
  })
})
