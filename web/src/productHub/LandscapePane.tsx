import React, { useEffect, useMemo, useState } from 'react'
import { getProductHubBridge } from '../bridge/client'
import { isLandscape } from './hubModel'
import {
  compareLandscape, LANDSCAPE_AXES, LANDSCAPE_SUGGESTIONS, landscapeAxisLabel, landscapeScoreLabel,
  loadLandscapeNames, parseCompetitorInput, saveLandscapeNames, type LandscapeAxisId, type LandscapeDraft, type LandscapeRow,
} from './landscapeModel'
import type { HubNode } from './productHubTypes'

export function LandscapePane({
  nodes, zh, onOpen, token,
}: {
  nodes: HubNode[]
  zh: boolean
  onOpen: (key: string) => void
  token: string
}): React.JSX.Element {
  const slots = nodes.filter(isLandscape)
  const [draft, setDraft] = useState('')
  const [names, setNames] = useState<string[]>(() => loadLandscapeNames())
  const [rows, setRows] = useState<LandscapeRow[] | null>(null)
  const [axis, setAxis] = useState<LandscapeAxisId>('local')
  const [picked, setPicked] = useState('lunitide')
  const [collecting, setCollecting] = useState(false)
  const [collectNote, setCollectNote] = useState('')
  const [collected, setCollected] = useState<LandscapeDraft[]>([])

  const axisMeta = LANDSCAPE_AXES.find(item => item.id === axis) ?? LANDSCAPE_AXES[0]
  const pickedRow = rows?.find(row => row.id === picked) ?? rows?.[0]
  const pickedCell = pickedRow?.cells[axis]
  const related = useMemo(() => {
    const keys = new Set<string>(axisMeta.related)
    return nodes.filter(node => keys.has(node.id) || keys.has(node.stable_key))
  }, [axisMeta.related, nodes])

  const loadDrafts = () => {
    if (!token) return
    getProductHubBridge().landscapeDrafts({ sessionToken: token }).then(result => {
      setCollected(((result.drafts ?? []) as unknown[]).filter((item): item is LandscapeDraft => {
        const d = item as Partial<LandscapeDraft>
        return !!d && typeof d.id === 'string' && typeof d.quote === 'string' && (d.status === 'draft' || d.status === 'confirmed')
      }))
    }).catch(() => { /* locked or offline: the list simply stays empty */ })
  }

  useEffect(loadDrafts, [token])

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

  const runCollect = () => {
    if (!token || collecting) return
    setCollecting(true)
    setCollectNote(zh ? '采集中：只读抓取登记过的公开页，摘录必须与原文一致。' : 'Collecting: read-only fetch of registered public pages; quotes must match the source.')
    getProductHubBridge().landscapeCollect({ sessionToken: token, names }).then(result => {
      const skipped = (result.skipped ?? []) as string[]
      const parts = [
        `${zh ? '这一轮通过校验入库草稿' : 'drafts stored this round'}: ${result.collected}`,
        ...skipped,
      ]
      setCollectNote(parts.join('；'))
      loadDrafts()
    }).catch(error => {
      setCollectNote(`${zh ? '采集失败' : 'collection failed'}: ${(error as { message?: string })?.message ?? ''}`)
    }).finally(() => setCollecting(false))
  }

  const confirmDraft = (id: string) => {
    getProductHubBridge().landscapeConfirm({ sessionToken: token, id }).then(loadDrafts).catch(() => { /* surfaced by the next list refresh */ })
  }

  const discardDraft = (id: string) => {
    getProductHubBridge().landscapeDiscard({ sessionToken: token, id }).then(loadDrafts).catch(() => { /* surfaced by the next list refresh */ })
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
        <button type="button" disabled={collecting || !names.length} onClick={runCollect}>
          {collecting ? (zh ? '采集中…' : 'Collecting…') : (zh ? '采集' : 'Collect')}
        </button>
      </form>
      {collectNote ? <p className="ph-dim">{collectNote}</p> : null}
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
        <span>{zh ? '采集草稿与已确认摘录' : 'Collected drafts and confirmed quotes'}</span>
        <span>{collected.length}</span>
      </div>
      <p className="ph-dim">
        {zh
          ? '「采集」只读抓取登记过的公开页；摘录必须与原文逐字一致才入库。草稿经人工确认后才会写进诊断报告的竞品对照，全程不改健康分。'
          : 'Collect fetches registered public pages read-only; only verbatim quotes are stored. A draft enters the report after human confirmation; the score never changes.'}
      </p>
      <div className="ph-list">
        {collected.length === 0 ? <p className="ph-dim">{zh ? '还没有采集条目。点「采集」按已保存名单抓一轮。' : 'Nothing collected yet. Press Collect for the saved list.'}</p> : collected.map(item => (
          <div key={item.id} className="ph-feature-row">
            <span>
              {item.status === 'confirmed' ? (zh ? '已确认' : 'confirmed') : (zh ? '待确认草稿' : 'draft')}
              {' · '}{item.name} · {landscapeAxisLabel(item.axis, zh)} · {item.date}
            </span>
            <span>{item.quote}</span>
            <span className="ph-dim">{item.url}</span>
            <span>
              {item.status === 'confirmed'
                ? null
                : <button type="button" className="primary" onClick={() => confirmDraft(item.id)}>{zh ? '确认' : 'Confirm'}</button>}
              {' '}
              <button type="button" onClick={() => discardDraft(item.id)}>{zh ? '丢弃' : 'Discard'}</button>
            </span>
          </div>
        ))}
      </div>

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
