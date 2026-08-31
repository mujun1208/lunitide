export type PeopleTrust = 'self' | 'trusted' | 'discovered'

export type PeopleContact = {
  subjectId: string
  nickname: string
  avatar: string
  status: string
  department: string
  title: string
  orgName: string
  bio: string
  publicKey: string
  trustState: PeopleTrust
  hostAddr: string
  lastSeenAt: string
  createdAt?: string
  updatedAt?: string
  remark?: string
  blocked?: boolean
  lastReadAt?: string
  self?: boolean
}

export type OrgGroup<T extends PeopleContact = PeopleContact> = {
  key: string
  orgName: string
  department: string
  label: string
  people: T[]
}

export function orgGroupKey(c: PeopleContact): string {
  const org = c.orgName.trim() || '未填写组织'
  const dept = c.department.trim() || '未分组'
  return `${org}\u0000${dept}`
}

export function orgGroupLabel(c: PeopleContact): string {
  const org = c.orgName.trim()
  const dept = c.department.trim()
  if (org && dept) return `${org} · ${dept}`
  if (org) return org
  if (dept) return dept
  return '未分组'
}

export function groupContactsByOrg<T extends PeopleContact>(items: T[]): OrgGroup<T>[] {
  const map = new Map<string, OrgGroup<T>>()
  for (const person of items) {
    const key = orgGroupKey(person)
    const existing = map.get(key)
    if (existing) {
      existing.people.push(person)
      continue
    }
    map.set(key, {
      key,
      orgName: person.orgName.trim(),
      department: person.department.trim(),
      label: orgGroupLabel(person),
      people: [person],
    })
  }
  return [...map.values()].sort((a, b) => {
    const aEmpty = a.label === '未分组' ? 1 : 0
    const bEmpty = b.label === '未分组' ? 1 : 0
    return aEmpty - bEmpty || a.label.localeCompare(b.label, 'zh-CN') || a.key.localeCompare(b.key)
  })
}

export function statusLabel(status: string): string {
  switch (status) {
    case 'online': return '在线'
    case 'away': return '离开'
    case 'busy': return '忙碌'
    case 'invisible': return '隐身'
    case 'offline': return '离线'
    default: return status || '未知'
  }
}

export const AGENT_ORG_NAME = '月汐智能体'

export function isAgentContact(person: Pick<PeopleContact, 'orgName'>): boolean {
  return person.orgName.trim() === AGENT_ORG_NAME
}

export function trustLabel(state: PeopleTrust, orgName?: string): string {
  if ((orgName ?? '').trim() === AGENT_ORG_NAME) return '智能体'
  switch (state) {
    case 'self': return '我'
    case 'trusted': return '已配对'
    case 'discovered': return '局域网'
    default: return state
  }
}

export function contactAvatarIsImage(avatar: string): boolean {
  const value = avatar.trim()
  return value.startsWith('data:') || value.startsWith('http://') || value.startsWith('https://') || value.startsWith('/')
}

export function initials(name: string): string {
  const text = name.trim()
  return text ? [...text].slice(0, 1).join('') : '月'
}

export function filterMessages<T extends { body?: string; fileName?: string }>(items: T[], query: string): T[] {
  const q = query.trim().toLocaleLowerCase()
  if (!q) return items
  return items.filter(item => `${item.body ?? ''} ${item.fileName ?? ''}`.toLocaleLowerCase().includes(q))
}

export function contactSearchText(person: PeopleContact): string {
  return [person.nickname, person.remark, person.orgName, person.department, person.title, person.hostAddr, person.bio].filter(Boolean).join(' ')
}

export function filterContacts<T extends PeopleContact>(items: T[], query: string): T[] {
  const q = query.trim().toLocaleLowerCase()
  if (!q) return items
  return items.filter(item => contactSearchText(item).toLocaleLowerCase().includes(q))
}

export function filterThreads<T extends {
  kind?: string
  title?: string
  members?: Array<{ nickname: string; remark?: string; self?: boolean }>
  lastMessage?: { kind?: string; body?: string; fileName?: string }
}>(items: T[], query: string): T[] {
  const q = query.trim().toLocaleLowerCase()
  if (!q) return items
  return items.filter(item => {
    const preview = lastPreview(item.lastMessage?.kind, item.lastMessage?.body, item.lastMessage?.fileName)
    return `${threadTitle(item)} ${preview}`.toLocaleLowerCase().includes(q)
  })
}

export function formatBytes(n?: number): string {
  if (n == null || n < 0 || !Number.isFinite(n)) return ''
  if (n < 1024) return `${n} B`
  if (n < 1024 * 1024) return `${(n / 1024).toFixed(n < 10 * 1024 ? 1 : 0)} KB`
  return `${(n / (1024 * 1024)).toFixed(1)} MB`
}

export function lastPreview(kind?: string, body?: string, fileName?: string): string {
  switch (kind) {
    case 'image': return '[图片]'
    case 'file': return fileName ? `[文件] ${fileName}` : '[文件]'
    case 'emoji': return body?.trim() || '[表情]'
    case 'system': return body?.trim() || '[系统]'
    default: return body?.trim() || ''
  }
}

export function relativeTime(iso?: string): string {
  if (!iso) return ''
  const date = new Date(iso)
  if (Number.isNaN(date.getTime())) return ''
  const pad = (n: number) => n.toString().padStart(2, '0')
  const now = new Date()
  if (date.toDateString() === now.toDateString()) return `${pad(date.getHours())}:${pad(date.getMinutes())}`
  return `${pad(date.getMonth() + 1)}-${pad(date.getDate())}`
}

export function unreadTotal(items: Array<{ unreadCount?: number }>): number {
  return items.reduce((sum, item) => sum + Math.max(0, item.unreadCount ?? 0), 0)
}

export function displayName(person: { nickname: string; remark?: string; self?: boolean }, withSelf = false): string {
  const name = person.remark?.trim() || person.nickname
  return withSelf && person.self ? `${name}（我）` : name
}

export function threadTitle(thread: { kind?: string; title?: string; members?: Array<{ nickname: string; remark?: string; self?: boolean }> }, fallback = '同事对话'): string {
  if (thread.kind === 'group' && thread.title?.trim()) return thread.title.trim()
  const peer = thread.members?.find(m => !m.self) ?? thread.members?.[0]
  return (peer ? displayName(peer) : '') || fallback
}

export const PEOPLE_EMOJI = ['😀', '😃', '😄', '😊', '😍', '🤔', '👍', '👎', '🎉', '❤️', '🌙', '✨', '✅', '🙏', '📎']
