import React, { useEffect, useState } from 'react'
import { useZh } from '../i18n/language'

export type GrowthView = {
  missionSnapshot: string
  ladder: Array<{ name: string; state: 'have' | 'learning' | 'next' }>
  coverage: { docTypes: string[]; gaps: string[] }
  scenarios: Array<{ title: string; phaseKey: string }>
}

export function ExpertGrowthPanel({
  expertId, growthGet,
}: {
  expertId: string
  growthGet?: (payload: { expertId: string }) => Promise<GrowthView>
}): React.JSX.Element {
  const zh = useZh()
  const [path, setPath] = useState<GrowthView | null>(null)
  const [error, setError] = useState('')

  useEffect(() => {
    let alive = true
    if (!growthGet) {
      setPath({ missionSnapshot: '', ladder: [], coverage: { docTypes: [], gaps: [] }, scenarios: [] })
      return
    }
    void growthGet({ expertId }).then(next => { if (alive) setPath(next) }).catch(e => {
      if (alive) setError(e instanceof Error ? e.message : (zh ? '成长路径加载失败' : 'Failed to load path'))
    })
    return () => { alive = false }
  }, [expertId, growthGet, zh])

  const covered = path?.coverage.docTypes ?? []
  const emptyCoverage = covered.length === 0
  const stateLabel = (state: string) => state === 'have' ? (zh ? '会' : 'have') : state === 'learning' ? (zh ? '正在补' : 'learning') : (zh ? '下一步' : 'next')

  return (
    <section className="expert-growth-panel" aria-label={zh ? '路径' : 'Path'}>
      <h3>{zh ? '这位专家的成长' : "This expert's path"}</h3>
      {path?.missionSnapshot ? <p>{zh ? '使命：' : 'Mission: '}{path.missionSnapshot}</p> : null}
      {emptyCoverage ? (
        <p className="expert-growth-empty">{zh ? '把文件交给此专家后，这里会列出已覆盖的类型。' : 'Covered types appear here after you give this expert files.'}</p>
      ) : (
        <>
          <p>{zh ? '已覆盖' : 'Covered'} {covered.map(t => <span key={t}> {t}</span>)}</p>
          {(path?.coverage.gaps.length ?? 0) > 0 ? <p>{zh ? '还缺' : 'Gaps'} {path!.coverage.gaps.map(t => <span key={t}> {t}</span>)}</p> : null}
          {(path?.ladder.length ?? 0) > 0 ? (
            <ul className="expert-growth-ladder">{path!.ladder.map(item => <li key={item.name}>{item.name} · {stateLabel(item.state)}</li>)}</ul>
          ) : null}
        </>
      )}
      {(path?.scenarios.length ?? 0) > 0 ? (
        <div>
          <h4>{zh ? '情景' : 'Scenarios'}</h4>
          <ul>{path!.scenarios.map(card => <li key={card.title}>{card.title}</li>)}</ul>
        </div>
      ) : null}
      {error && <p className="skill-center-error" role="alert">{error}</p>}
    </section>
  )
}
