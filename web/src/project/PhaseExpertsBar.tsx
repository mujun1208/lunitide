import React, { useCallback, useEffect, useState } from 'react'
import { expertBridge, type ExpertBridge } from '../bridge/client'
import { CONVERSATION_EXPERTS, conversationExpertByNameOrID } from '../expert/conversationExperts'
import { resolvePhaseExpertIds } from './phaseExperts'

const MAX_MOUNTS = 4

const expertName = (id: string): string => conversationExpertByNameOrID(id)?.name ?? id

/**
 * First-class phase-expert control for the workbench (Issue 4f). The set of
 * experts mounted on a phase's session used to be reachable only through the
 * composer @ menu; here it is surfaced as an explicit add/remove picker so the
 * user can see and shape "谁在陪这一阶段" without hunting inside the composer.
 * Persisted via session.experts.get/set — the same store the phase seed uses.
 */
export function PhaseExpertsBar({
  sessionId,
  projectId,
  phaseLabel,
  experts = expertBridge,
}: {
  sessionId?: string
  projectId: string
  phaseLabel?: string
  experts?: ExpertBridge
}): React.JSX.Element | null {
  const [ids, setIds] = useState<string[]>([])
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')

  const load = useCallback(async () => {
    if (!sessionId || !experts.sessionMountGet) { setIds([]); return }
    try {
      const got = await experts.sessionMountGet({ sessionId })
      setIds(got?.expertIds ?? [])
    } catch {
      setIds([])
    }
  }, [sessionId, experts])

  useEffect(() => { void load() }, [load])

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

  const remove = (id: string) => { if (!busy) void persist(ids.filter(x => x !== id)) }
  const add = (id: string) => { if (!busy && id && !ids.includes(id) && ids.length < MAX_MOUNTS) void persist([...ids, id]) }
  const reseed = async () => {
    if (busy || !sessionId) return
    const seed = await resolvePhaseExpertIds(projectId, phaseLabel, experts).catch(() => [] as string[])
    if (seed.length) void persist(seed.slice(0, MAX_MOUNTS))
  }

  if (!sessionId) return null
  const available = CONVERSATION_EXPERTS.filter(e => !ids.includes(e.id))
  return (
    <section className="pm-phase-experts" aria-label="本阶段专家">
      <div className="pm-phase-experts-head"><b>本阶段专家</b><small>{ids.length}/{MAX_MOUNTS}</small></div>
      <div className="pm-phase-experts-chips">
        {ids.length === 0 && <span className="pm-phase-experts-empty">未挂载专家</span>}
        {ids.map(id => (
          <span key={id} className="pm-phase-expert-chip">{expertName(id)}<button type="button" aria-label={`移除 ${expertName(id)}`} disabled={busy} onClick={() => remove(id)}>×</button></span>
        ))}
      </div>
      <div className="pm-phase-experts-actions">
        <select aria-label="添加本阶段专家" value="" disabled={busy || ids.length >= MAX_MOUNTS || available.length === 0} onChange={e => { add(e.target.value); e.target.value = '' }}>
          <option value="">{ids.length >= MAX_MOUNTS ? '已达上限' : '＋ 添加专家'}</option>
          {available.map(e => <option key={e.id} value={e.id}>{e.name}</option>)}
        </select>
        <button type="button" disabled={busy} onClick={() => void reseed()}>推荐</button>
      </div>
      {error && <p className="pm-phase-experts-error" role="alert">{error}</p>}
    </section>
  )
}
