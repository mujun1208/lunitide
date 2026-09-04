import React, { useCallback, useEffect, useState } from 'react'
import { expertBridge, type ExpertBridge } from '../bridge/client'
import { conversationExpertByNameOrID } from '../expert/conversationExperts'
import { resolveInstalledExpertIds } from '../expert/expertIds'
import { resolvePhaseExpertIds } from './phaseExperts'

const MAX_MOUNTS = 4
const SESSION_EXPERTS_EVENT = 'lunitide:session-experts'

const expertName = (id: string): string => conversationExpertByNameOrID(id)?.name ?? id

/**
 * Read-only mirror of experts already mounted on this phase session.
 * The composer @ menu is the only write path; Recommend writes the same
 * session.experts.set store using installed ULIDs.
 */
export function PhaseExpertsBar({
  sessionId,
  projectId,
  phaseLabel,
  experts = expertBridge,
  revision,
}: {
  sessionId?: string
  projectId: string
  phaseLabel?: string
  experts?: ExpertBridge
  revision?: number | string
}): React.JSX.Element | null {
  const [ids, setIds] = useState<string[]>([])
  const [names, setNames] = useState<Record<string, string>>({})
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')

  const load = useCallback(async () => {
    if (!sessionId || !experts.sessionMountGet) { setIds([]); return }
    try {
      const got = await experts.sessionMountGet({ sessionId })
      const next = got?.expertIds ?? []
      setIds(next)
      const listed = await experts.list?.().catch(() => undefined)
      const map: Record<string, string> = {}
      for (const id of next) {
        const hit = listed?.experts.find(item => item.expertId === id)
        map[id] = hit?.name ?? expertName(id)
      }
      setNames(map)
    } catch {
      setIds([])
    }
  }, [sessionId, experts])

  useEffect(() => { void load() }, [load, revision])

  useEffect(() => {
    const onChange = (event: Event) => {
      const detail = (event as CustomEvent<{ sessionId?: string }>).detail
      if (!sessionId || !detail?.sessionId || detail.sessionId === sessionId) void load()
    }
    window.addEventListener(SESSION_EXPERTS_EVENT, onChange)
    return () => window.removeEventListener(SESSION_EXPERTS_EVENT, onChange)
  }, [load, sessionId])

  const persist = async (next: string[]) => {
    if (!sessionId || !experts.sessionMountSet) return
    setBusy(true)
    setError('')
    const prev = ids
    setIds(next)
    try {
      const saved = await experts.sessionMountSet({ sessionId, expertIds: next })
      setIds(saved?.expertIds ?? next)
    } catch (e) {
      setIds(prev)
      setError(e instanceof Error ? e.message : '保存失败')
    } finally {
      setBusy(false)
    }
  }

  const reseed = async () => {
    if (busy || !sessionId) return
    const seed = await resolvePhaseExpertIds(projectId, phaseLabel, experts).catch(() => [] as string[])
    const listed = await experts.list?.().catch(() => undefined)
    const resolved = listed?.experts
      ? resolveInstalledExpertIds(seed, listed.experts)
      : { ids: seed, missing: [] as string[] }
    const next = (resolved.ids.length ? resolved.ids : seed.filter(id => !resolved.missing.includes(id))).slice(0, MAX_MOUNTS)
    if (next.length) void persist(next)
  }

  if (!sessionId) return null
  return (
    <section className="pm-phase-experts" aria-label="本阶段将使用">
      <div className="pm-phase-experts-head"><b>本阶段将使用</b><small>{ids.length}/{MAX_MOUNTS}</small></div>
      <div className="pm-phase-experts-chips">
        {ids.length === 0 && <span className="pm-phase-experts-empty">未挂载 · 在下方输入框添加</span>}
        {ids.map(id => (
          <span key={id} className="pm-phase-expert-chip">{names[id] ?? expertName(id)}</span>
        ))}
      </div>
      <div className="pm-phase-experts-actions">
        <button type="button" disabled={busy} onClick={() => void reseed()}>推荐</button>
      </div>
      {error && <p className="pm-phase-experts-error" role="alert">{error}</p>}
    </section>
  )
}

export { SESSION_EXPERTS_EVENT }
