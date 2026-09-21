import React, { useMemo, useState } from 'react'
import { isLandscape } from './hubModel'
import {
  compareLandscape, LANDSCAPE_AXES, LANDSCAPE_SUGGESTIONS, landscapeScoreLabel,
  loadLandscapeNames, parseCompetitorInput, saveLandscapeNames,
  type LandscapeAxisId, type LandscapeRow,
} from './landscapeModel'
import type { HubNode } from './productHubTypes'

export function LandscapePane({
  nodes, zh, onOpen,
}: {
  nodes: HubNode[]
  zh: boolean
  onOpen: (key: string) => void
}): React.JSX.Element {
  const slots = nodes.filter(isLandscape)
  const [draft, setDraft] = useState('')
  const [names, setNames] = useState<string[]>(() => loadLandscapeNames())
  const [rows, setRows] = useState<LandscapeRow[] | null>(null)
  const [axis, setAxis] = useState<LandscapeAxisId>('local')
  const [picked, setPicked] = useState('lunitide')

  const axisMeta = LANDSCAPE_AXES.find(item => item.id === axis) ?? LANDSCAPE_AXES[0]
  const pickedRow = rows?.find(row => row.id === picked) ?? rows?.[0]
  const pickedCell = pickedRow?.cells[axis]
  const related = useMemo(() => {
    const keys = new Set<string>(axisMeta.related)
    return nodes.filter(node => keys.has(node.id) || keys.has(node.stable_key))
  }, [axisMeta.related, nodes])

  const addNames = (next: string[]) => {
    setNames(curr => {
      const merged = [...curr]
      for (const name of next) {
        if (!merged.some(item => item.toLocaleLowerCase() === name.toLocaleLowerCase())) merged.push(name)
      }
      saveLandscapeNames(merged)
      return merged
    })
  }

  const runCompare = (list = names) => {
    const extra = parseCompetitorInput(draft)
    const all = extra.length ? [...list, ...extra] : list
    if (extra.length) {
      addNames(extra)
      setDraft('')
    }
    const compared = compareLandscape(all)
    setRows(compared)
    setPicked(compared[1]?.id ?? compared[0].id)
  }

  return (
    <section className="ph-landscape">
      <p className="ph-dim">
        {zh
          ? '图景不计入健康分。对照只比四维可核验行为：本机优先、媒体核验、技能/MCP、知识自描述。结论必须带来源与日期；名单外竞品标成待核验，不编造排名。'
          : 'Landscape is not part of the health score. Compare only four verifiable axes. Unknown names stay unverified.'}
      </p>
      <form
        className="ph-land-form"
        onSubmit={event => {
          event.preventDefault()
          const extra = parseCompetitorInput(draft)
          if (extra.length) addNames(extra)
          setDraft('')
        }}
      >
        <input
          value={draft}
          onChange={e => setDraft(e.target.value)}
          placeholder={zh ? '输入竞品名称，逗号分隔，例如 Cursor, Copilot' : 'Competitor names, comma separated'}
          aria-label={zh ? '竞品名称' : 'Competitor names'}
        />
        <button type="submit">{zh ? '加入名单' : 'Add'}</button>
        <button type="button" className="primary" onClick={() => runCompare()}>{zh ? '执行对照' : 'Compare'}</button>
      </form>
      <div className="ph-chips ph-land-suggest">
        {LANDSCAPE_SUGGESTIONS.map(name => (
          <button
            key={name}
            type="button"
            className={names.some(item => item.toLocaleLowerCase() === name.toLocaleLowerCase()) ? 'ph-chip is-on' : 'ph-chip'}
            onClick={() => addNames([name])}
          >
            <i />{name}
          </button>
        ))}
      </div>
      {names.length ? (
        <div className="ph-chips">
          {names.map(name => (
            <button
              key={name}
              type="button"
              className="ph-chip"
              onClick={() => {
                setNames(curr => {
                  const next = curr.filter(item => item !== name)
                  saveLandscapeNames(next)
                  return next
                })
              }}
            >
              <i />{name} ×
            </button>
          ))}
        </div>
      ) : <p className="ph-dim">{zh ? '先点建议芯片或自己输入竞品，再点「执行对照」。' : 'Add names, then compare.'}</p>}

      {rows ? (
        <div className="ph-land-grid">
          <table className="ph-roster ph-compare">
            <thead>
              <tr>
                <th>{zh ? '对照维' : 'Axis'}</th>
                {rows.map(row => (
                  <th key={row.id}>
                    <button type="button" className={picked === row.id ? 'is-current' : ''} onClick={() => setPicked(row.id)}>{row.name}</button>
                  </th>
                ))}
              </tr>
            </thead>
            <tbody>
              {LANDSCAPE_AXES.map(item => (
                <tr key={item.id} className={axis === item.id ? 'is-on' : ''}>
                  <th>
                    <button type="button" onClick={() => setAxis(item.id)}>{zh ? item.zh : item.en}</button>
                  </th>
                  {rows.map(row => {
                    const cell = row.cells[item.id]
                    return (
                      <td key={`${row.id}-${item.id}`}>
                        <button type="button" className={`ph-score is-${cell.score}`} onClick={() => { setPicked(row.id); setAxis(item.id) }}>
                          {landscapeScoreLabel(cell.score, zh)}
                        </button>
                      </td>
                    )
                  })}
                </tr>
              ))}
            </tbody>
          </table>
          <aside className="ph-land-detail">
            <p className="ph-step-kicker">{zh ? '当前交叉' : 'Selected cell'}</p>
            <strong>{pickedRow?.name} · {zh ? axisMeta.zh : axisMeta.en}</strong>
            <p>{axisMeta.meaning}</p>
            <p>{pickedCell?.note || '—'}</p>
            <p className="ph-dim">{pickedCell?.source ? `${zh ? '来源' : 'Source'} ${pickedCell.date} · ${pickedCell.source}` : (zh ? '待核验：补公开出处后再写入图景卡。' : 'Unverified until a dated source exists.')}</p>
            {related.length ? (
              <div className="ph-list">
                {related.map(node => (
                  <button key={node.id} type="button" className="ph-feature-row" onClick={() => onOpen(node.stable_key)}>
                    <span>{node.name}</span>
                    <span>{node.type} · {node.stable_key}</span>
                  </button>
                ))}
              </div>
            ) : null}
          </aside>
        </div>
      ) : null}

      <div className="ph-section-head">
        <span>{zh ? '已入库图景槽位' : 'Stored landscape slots'}</span>
        <span>{slots.length}</span>
      </div>
      <div className="ph-list">
        {slots.length === 0 ? <p className="ph-dim">{zh ? '引擎种子里的竞品/前沿槽位会显示在这里。' : 'Seeded landscape slots appear here.'}</p> : slots.map(node => (
          <button
            key={node.id}
            type="button"
            className="ph-feature-row"
            onClick={() => {
              const hint = node.name.replace(/^竞品[·:：\s]*/, '').trim()
              if (hint && !/对照|前沿/.test(hint)) addNames([hint])
              onOpen(node.stable_key)
            }}
          >
            <span>{node.name}</span>
            <span>{node.stable_key}</span>
          </button>
        ))}
      </div>
    </section>
  )
}
