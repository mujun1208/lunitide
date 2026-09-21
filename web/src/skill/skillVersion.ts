export function skillNameKey(name: string): string {
  return name.trim().toLowerCase().replace(/^tpl-/, '')
}

export function compareSemver(a: string, b: string): number {
  const parse = (value: string) => value.split('.').map(part => {
    const n = parseInt(part, 10)
    return Number.isFinite(n) ? n : 0
  })
  const pa = parse(a)
  const pb = parse(b)
  const len = Math.max(pa.length, pb.length)
  for (let i = 0; i < len; i++) {
    const va = pa[i] ?? 0
    const vb = pb[i] ?? 0
    if (va !== vb) return va > vb ? 1 : -1
  }
  return 0
}

export function rankSkills<T extends {version: string; status?: string}>(items: T[]): T[] {
  return items.toSorted((a, b) => {
    const byVersion = compareSemver(b.version, a.version)
    if (byVersion) return byVersion
    if (a.status === 'published' && b.status !== 'published') return -1
    if (b.status === 'published' && a.status !== 'published') return 1
    return 0
  })
}

export function deduplicateByName<T extends {name: string; version: string}>(entries: T[]): T[] {
  const map = new Map<string, T>()
  for (const entry of entries) {
    const key = skillNameKey(entry.name)
    const prev = map.get(key)
    if (!prev || compareSemver(entry.version, prev.version) > 0) map.set(key, entry)
  }
  return [...map.values()]
}

export function olderDuplicates<T extends {id: string; name: string; version: string; status?: string}>(items: T[]): T[] {
  const groups = new Map<string, T[]>()
  for (const item of items) {
    const key = skillNameKey(item.name)
    groups.set(key, [...(groups.get(key) ?? []), item])
  }
  const extras: T[] = []
  for (const group of groups.values()) {
    if (group.length < 2) continue
    extras.push(...rankSkills(group).slice(1))
  }
  return extras
}

export function skillCreateExistingHint(existing: ReadonlyArray<{name: string; displayName?: string; version: string}>): string {
  const others = existing.filter(item => skillNameKey(item.name) !== 'skill-creator')
  if (!others.length) return ''
  const lines = others.slice(0, 30).map(item => `- ${item.displayName || item.name}（${item.name} v${item.version}）`)
  return `\n\n系统已有技能，同名或同类请升级原技能，不要新建重复：\n${lines.join('\n')}\n新技能请自动起英文短横线名；若能力已在上表，先提示用户在原技能上升级。`
}
