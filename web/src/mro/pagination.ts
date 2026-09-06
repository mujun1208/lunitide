import { useEffect, useRef, useState } from 'react'

export type MroPageRequest = { cursor?: string }
export type MroPageMeta = { nextCursor?: string; continuedFields?: string[] }
export type MroPage = MroPageMeta & { items?: unknown[]; alternates?: unknown[]; violations?: unknown[] }
const collections = ['items', 'alternates', 'violations'] as const
const children = ['events', 'tails', 'missing', 'sources'] as const

function sameValue(a: unknown, b: unknown): boolean {
  if (a === b) return true
  if (!a || !b || typeof a !== 'object' || typeof b !== 'object') return false
  const left = Object.entries(a),
    right = Object.entries(b)
  return (
    left.length === right.length &&
    left.every(
      ([key, value]) =>
        Object.prototype.hasOwnProperty.call(b, key) && sameValue(value, (b as Record<string, unknown>)[key]),
    )
  )
}

/** Only an explicit continuation may join the previous last parent. Never dedupe
 * ordinary rows: that could hide a broken cursor or lose legitimate entries. */
export function mergeMroPage<T extends MroPage>(previous: T | undefined, page: T): T {
  const continued = page.continuedFields ?? []
  if (
    new Set(continued).size !== continued.length ||
    continued.some((key) => !collections.includes(key as (typeof collections)[number]))
  ) {
    throw new Error('Invalid MRO continuation fields')
  }
  const result: MroPage = { ...page }
  for (const key of collections) {
    const incoming = page[key]
    if (!incoming) {
      if (continued.includes(key)) throw new Error('Missing MRO continuation collection')
      if (previous?.[key]) result[key] = previous[key]
      continue
    }
    const existing = previous?.[key] ?? []
    if (!continued.includes(key)) {
      result[key] = [...existing, ...incoming]
      continue
    }
    const last = existing.at(-1) as Record<string, unknown> | undefined
    const first = incoming[0] as Record<string, unknown> | undefined
    if (!last || !first || typeof last.id !== 'string' || last.id !== first.id) {
      throw new Error('MRO continuation parent changed; refresh the list')
    }
    const childKeys = children.filter((child) => Array.isArray(first[child]) || Array.isArray(last[child]))
    if (childKeys.length !== 1) throw new Error('Invalid MRO continuation child')
    const child = childKeys[0]
    const { [child]: oldChildren, ...oldFields } = last
    const { [child]: newChildren, ...newFields } = first
    if (!Array.isArray(oldChildren) || !Array.isArray(newChildren) || !sameValue(oldFields, newFields)) {
      throw new Error('MRO continuation content changed; refresh the list')
    }
    result[key] = [
      ...existing.slice(0, -1),
      { ...first, [child]: [...oldChildren, ...newChildren] },
      ...incoming.slice(1),
    ]
  }
  return result as T
}

export type MroPageBinding = {
  fetch?: (input?: MroPageRequest) => Promise<MroPage>
  accept: (page: MroPage) => void
  invalidate?: () => void
}
export function bindMroPage<T extends MroPage>(
  fetch: ((input?: MroPageRequest) => Promise<T>) | undefined,
  accept: (page: T) => void,
  invalidate?: () => void,
): MroPageBinding {
  return { fetch, accept: (page) => accept(page as T), invalidate }
}
type PageState = { data?: MroPage; busy: boolean; error?: string; generation: number; seen: Set<string> }

/** Each refresh invalidates older replies, including A→B→A scope changes. */
export function useMroPagination(bindings: Record<string, MroPageBinding>, scopeKey: string) {
  const bindingsRef = useRef(bindings)
  bindingsRef.current = bindings
  const scope = useRef(scopeKey)
  const epoch = useRef(0)
  const mounted = useRef(false)
  const states = useRef<Record<string, PageState>>({})
  const [, redraw] = useState(0)
  if (scope.current !== scopeKey) {
    scope.current = scopeKey
    epoch.current++
    states.current = {}
  }
  useEffect(() => {
    mounted.current = true
    return () => {
      mounted.current = false
      epoch.current++
      states.current = {}
    }
  }, [])
  const load = async (key: string, more = false): Promise<void> => {
    const binding = bindingsRef.current[key]
    if (!mounted.current || !binding?.fetch) return
    const old = states.current[key]
    if (more && (old?.busy || old?.error || !old?.data?.nextCursor)) return
    const cursor = more ? old?.data?.nextCursor : undefined
    const state: PageState = {
      data: old?.data,
      busy: true,
      generation: (old?.generation ?? 0) + 1,
      seen: more ? new Set(old?.seen) : new Set(),
    }
    const startEpoch = epoch.current
    states.current[key] = state
    binding.invalidate?.()
    redraw((n) => n + 1)
    const current = () => mounted.current && epoch.current === startEpoch && states.current[key] === state
    try {
      const page = await binding.fetch(cursor ? { cursor } : undefined)
      if (!current()) return
      if (page.nextCursor && (page.nextCursor === cursor || state.seen.has(page.nextCursor)))
        throw new Error('MRO cursor did not advance; refresh the list')
      const merged = mergeMroPage(more ? old?.data : undefined, page)
      if (cursor) state.seen.add(cursor)
      state.data = merged
      binding.accept(merged)
    } catch (error) {
      if (!current()) return
      state.error = error instanceof Error ? error.message : String(error)
      throw error
    } finally {
      if (current()) {
        state.busy = false
        redraw((n) => n + 1)
      }
    }
  }
  return { states: states.current, refresh: (key: string) => load(key), next: (key: string) => load(key, true) }
}

/** Quality bulletins need the complete selected lot before composing a prompt.
 * This only repeats the read, with its original lot filter and bound cursor. */
export async function readAllMroPages<T extends MroPage>(
  fetch: (input?: MroPageRequest) => Promise<T>,
  active: () => boolean = () => true,
): Promise<T> {
  let result: T | undefined
  let cursor: string | undefined
  const seen = new Set<string>()
  do {
    if (!active()) throw new Error('MRO view changed')
    const page = await fetch(cursor ? { cursor } : undefined)
    if (!active()) throw new Error('MRO view changed')
    result = mergeMroPage(result, page)
    cursor = page.nextCursor
    if (cursor && seen.has(cursor)) throw new Error('MRO cursor did not advance; refresh the list')
    if (cursor) seen.add(cursor)
  } while (cursor)
  return result
}
