import { describe, expect, test } from 'vitest'
import { contactAvatarIsImage, displayName, filterContacts, filterMessages, filterThreads, formatBytes, groupContactsByOrg, isAgentContact, lastPreview, orgGroupCollapsed, orgGroupLabel, peopleShowsOpenThread, resolveColleaguePeerId, shouldPinPeopleLog, shouldReloadOpenThread, statusLabel, threadHeading, threadTitle, trustLabel, unreadTotal, visiblePeopleThreads, type PeopleContact } from './peopleRoster'

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
    expect(threadTitle({ kind: 'direct', members: [{ nickname: 'mu', self: true, subjectId: 'me' }, { nickname: 'PPT专家', self: false, subjectId: 'ppt' }] })).toBe('PPT专家')
    expect(threadTitle({ kind: 'direct', members: [{ nickname: 'mu', self: true, subjectId: 'me' }] })).toBe('同事对话')
    expect(threadHeading({ kind: 'group', title: '三人组', members: [{ nickname: '甲', self: false }, { nickname: '乙', self: false }, { nickname: 'mu', self: true }] })).toBe('三人组 (3)')
  })

  test('hides note-to-self and keeps one direct row per peer', () => {
    const self = { nickname: 'mu', self: true, subjectId: 'me' }
    const ppt = { nickname: 'PPT专家', self: false, subjectId: 'ppt' }
    const peer = { nickname: '甲', self: false, subjectId: 'jia' }
    expect(visiblePeopleThreads([
      { kind: 'direct', members: [self] },
      { kind: 'direct', members: [self, ppt] },
      { kind: 'direct', members: [self, ppt] },
      { kind: 'direct', members: [self, peer] },
      { kind: 'group', title: '项目群', members: [self, ppt, peer] },
    ]).map(item => item.kind === 'group' ? item.title : threadTitle(item))).toEqual(['PPT专家', '甲', '项目群'])
  })

  test('resolves colleague open by subject or agent name', () => {
    const items = [
      contact({ subjectId: 'me', nickname: 'mu', self: true }),
      contact({ subjectId: '01ARZ3NDEKTSV4RRFFQ69G5FAD', nickname: 'PPT专家', orgName: '月汐智能体' }),
    ]
    expect(resolveColleaguePeerId(items, '01ARZ3NDEKTSV4RRFFQ69G5FAD')).toBe('01ARZ3NDEKTSV4RRFFQ69G5FAD')
    expect(resolveColleaguePeerId(items, 'ppt-expert', 'PPT专家')).toBe('01ARZ3NDEKTSV4RRFFQ69G5FAD')
    expect(resolveColleaguePeerId(items, 'me')).toBeUndefined()
  })

  test('pins the people log only when already at the bottom and the last id changed', () => {
    expect(shouldPinPeopleLog({ stickToBottom: true, previousLastId: 'a', nextLastId: 'a' })).toBe(false)
    expect(shouldPinPeopleLog({ stickToBottom: true, previousLastId: 'a', nextLastId: 'b' })).toBe(true)
    expect(shouldPinPeopleLog({ stickToBottom: false, previousLastId: 'a', nextLastId: 'b' })).toBe(false)
    expect(shouldReloadOpenThread({ stickToBottom: true, listedLastId: 'b', localLastId: 'a' })).toBe(true)
    expect(shouldReloadOpenThread({ stickToBottom: false, listedLastId: 'b', localLastId: 'a' })).toBe(false)
    expect(orgGroupCollapsed(undefined, 4, false)).toBe(true)
    expect(orgGroupCollapsed(undefined, 2, false)).toBe(false)
    expect(orgGroupCollapsed(true, 2, true)).toBe(false)
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

  test('hides a leftover thread when the address-book card is self', () => {
    const leftover = { kind: 'direct' as const, title: '', members: [{ nickname: 'Excel表格制作专家', self: false }] }
    expect(peopleShowsOpenThread('contacts', leftover, { self: false })).toBe(true)
    expect(peopleShowsOpenThread('contacts', leftover, { self: true })).toBe(false)
    expect(peopleShowsOpenThread('chats', leftover, { self: true })).toBe(false)
    expect(peopleShowsOpenThread('me', leftover, { self: false })).toBe(false)
    expect(peopleShowsOpenThread('contacts', undefined, { self: false })).toBe(false)
  })

  test('labels local agent contacts without treating emoji as images', () => {
    const agent = contact({ nickname: 'PPT专家', orgName: '月汐智能体', avatar: '📊', trustState: 'trusted' })
    expect(isAgentContact(agent)).toBe(true)
    expect(trustLabel(agent.trustState, agent.orgName)).toBe('同事专家')
    expect(contactAvatarIsImage('📊')).toBe(false)
    expect(contactAvatarIsImage('data:image/png;base64,xx')).toBe(true)
  })
})
