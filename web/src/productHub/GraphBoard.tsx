import React, { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import {
  DOMAIN_META, DOMAIN_ORDER, GRAPH_NODE_H, GRAPH_NODE_W, graphFitScale, graphLayout, isFeatureLike,
  neighborIds, NODE_TYPES, wrapLabel,
} from './hubModel'
import type { HubEdge, HubNode } from './productHubTypes'
import { useBoxSize } from './useBoxSize'

const REL_COLOR: Record<string, string> = {
  contains: 'rgba(156,156,176,0.55)',
  uses: '#8B7CF6',
  calls: '#22D3EE',
}

const ZOOM_MIN = 0.25
const ZOOM_MAX = 4

function treePath(from: { x: number; y: number }, to: { x: number; y: number }, nodeH: number): string {
  const y1 = from.y + nodeH / 2
  const y2 = to.y - nodeH / 2
  const mid = (y1 + y2) / 2
  return `M${from.x} ${y1} C${from.x} ${mid}, ${to.x} ${mid}, ${to.x} ${y2}`
}

function clampZoom(value: number): number {
  return Math.min(ZOOM_MAX, Math.max(ZOOM_MIN, value))
}

function ZoomBar({
  scale, zh, onFit, onZoom,
}: {
  scale: number
  zh: boolean
  onFit: () => void
  onZoom: (factor: number) => void
}): React.JSX.Element {
  return (
    <div className="ph-graph-tools" role="toolbar" aria-label={zh ? '图谱缩放' : 'Graph zoom'}>
      <button type="button" aria-label={zh ? '缩小' : 'Zoom out'} onClick={() => onZoom(1 / 1.2)}>−</button>
      <button type="button" className="ph-graph-fit" onClick={onFit}>{zh ? '适合窗口' : 'Fit'}</button>
      <button type="button" aria-label={zh ? '放大' : 'Zoom in'} onClick={() => onZoom(1.2)}>+</button>
      <span className="ph-dim">{Math.round(scale * 100)}%</span>
    </div>
  )
}

export function GraphBoard({
  nodes, edges, onOpen, zh, focusDomain, domainFilter, typeFilter,
}: {
  nodes: HubNode[]
  edges: HubEdge[]
  onOpen: (key: string) => void
  zh: boolean
  focusDomain?: string
  domainFilter?: string
  typeFilter?: string
}): React.JSX.Element {
  const firstFeature = nodes.find(isFeatureLike)
  const [pickedId, setPickedId] = useState(firstFeature?.id ?? '')
  const [hop, setHop] = useState(false)
  const [setBox, box] = useBoxSize()
  const canvasRef = useRef<HTMLDivElement | null>(null)
  const dragRef = useRef<{ x: number; y: number } | null>(null)
  const [dragging, setDragging] = useState(false)
  const [camera, setCamera] = useState({ k: 1, x: 0, y: 0, mode: 'fit' as 'fit' | 'free' })

  useEffect(() => {
    if (!focusDomain) return
    const hit = nodes.find(node => isFeatureLike(node) && node.domain === focusDomain)
    if (hit) setPickedId(hit.id)
  }, [focusDomain, nodes])

  const picked = nodes.find(node => node.id === pickedId || node.stable_key === pickedId)
  const layout = useMemo(
    () => graphLayout(nodes, edges, {
      domain: domainFilter || focusDomain,
      type: typeFilter,
      focusId: picked?.id,
    }),
    [nodes, edges, domainFilter, focusDomain, typeFilter, picked?.id],
  )
  const nodeW = layout.nodeW || GRAPH_NODE_W
  const nodeH = layout.nodeH || GRAPH_NODE_H
  const ready = box.w > 40 && box.h > 40
  const fit = ready ? graphFitScale(layout, box) : 1

  useEffect(() => {
    if (!ready || camera.mode !== 'fit') return
    setCamera(curr => {
      const x = (box.w - layout.width * fit) / 2
      const y = (box.h - layout.height * fit) / 2
      if (curr.mode === 'fit' && curr.k === fit && curr.x === x && curr.y === y) return curr
      return { k: fit, x, y, mode: 'fit' }
    })
  }, [ready, fit, box.w, box.h, layout.width, layout.height, camera.mode])

  const hops = useMemo(() => {
    if (!picked || !hop) return new Set<string>()
    return neighborIds(edges.flatMap(edge => {
      const from = nodes.find(node => node.id === edge.from || node.stable_key === edge.from)?.id ?? edge.from
      const to = nodes.find(node => node.id === edge.to || node.stable_key === edge.to)?.id ?? edge.to
      return [{ ...edge, from, to }]
    }), picked.id)
  }, [edges, hop, nodes, picked])

  const applyZoom = (factor: number, origin?: { x: number; y: number }) => {
    setCamera(curr => {
      const nextK = clampZoom(curr.k * factor)
      const ratio = nextK / curr.k
      const mx = origin?.x ?? box.w / 2
      const my = origin?.y ?? box.h / 2
      return {
        k: nextK,
        x: mx - (mx - curr.x) * ratio,
        y: my - (my - curr.y) * ratio,
        mode: 'free',
      }
    })
  }

  const fitView = () => {
    if (!ready) return
    setCamera({
      k: fit,
      x: (box.w - layout.width * fit) / 2,
      y: (box.h - layout.height * fit) / 2,
      mode: 'fit',
    })
  }

  const setCanvas = useCallback((node: HTMLDivElement | null) => {
    canvasRef.current = node
    setBox(node)
  }, [setBox])

  useEffect(() => {
    const el = canvasRef.current
    if (!el) return
    const onWheel = (event: WheelEvent) => {
      event.preventDefault()
      const rect = el.getBoundingClientRect()
      applyZoom(event.deltaY < 0 ? 1.12 : 1 / 1.12, { x: event.clientX - rect.left, y: event.clientY - rect.top })
    }
    el.addEventListener('wheel', onWheel, { passive: false })
    return () => el.removeEventListener('wheel', onWheel)
  }, [box.w, box.h])

  const viewW = ready ? box.w : layout.width
  const viewH = ready ? box.h : layout.height
  const k = ready ? camera.k : 1
  const tx = ready ? camera.x : 0
  const ty = ready ? camera.y : 0

  return (
    <div className="ph-graph-page">
      <div
        ref={setCanvas}
        className={`product-hub-graph${dragging ? ' is-drag' : ''}`}
        role="region"
        aria-label={zh ? '知识图谱画布' : 'Graph canvas'}
        onPointerDown={event => {
          if ((event.target as HTMLElement | null)?.closest('[data-ph-node]')) return
          dragRef.current = { x: event.clientX, y: event.clientY }
          setDragging(true)
          event.currentTarget.setPointerCapture(event.pointerId)
        }}
        onPointerMove={event => {
          const start = dragRef.current
          if (!start) return
          const dx = event.clientX - start.x
          const dy = event.clientY - start.y
          dragRef.current = { x: event.clientX, y: event.clientY }
          setCamera(curr => ({ ...curr, x: curr.x + dx, y: curr.y + dy, mode: 'free' }))
        }}
        onPointerUp={event => {
          dragRef.current = null
          setDragging(false)
          if (event.currentTarget.hasPointerCapture(event.pointerId)) event.currentTarget.releasePointerCapture(event.pointerId)
        }}
        onDoubleClick={event => {
          if ((event.target as HTMLElement | null)?.closest('[data-ph-node]')) return
          fitView()
        }}
      >
        <ZoomBar scale={k} zh={zh} onFit={fitView} onZoom={factor => applyZoom(factor)} />
        <svg
          viewBox={`0 0 ${Math.max(1, viewW)} ${Math.max(1, viewH)}`}
          width="100%"
          height="100%"
          preserveAspectRatio="xMidYMid meet"
          role="img"
          aria-label={zh ? '知识图谱' : 'Knowledge graph'}
        >
          <g transform={`translate(${tx} ${ty}) scale(${k})`}>
            {layout.edges.map((edge, i) => {
              const from = layout.nodes.find(node => node.id === edge.from)
              const to = layout.nodes.find(node => node.id === edge.to)
              if (!from || !to) return null
              const on = !hop || hops.has(from.id) || hops.has(to.id)
              return (
                <path
                  key={`${edge.from}-${edge.to}-${i}`}
                  d={treePath(from, to, nodeH)}
                  fill="none"
                  stroke={REL_COLOR[edge.rel] ?? REL_COLOR.contains}
                  strokeWidth={edge.rel === 'contains' ? 1.15 : 1.5}
                  opacity={on ? 1 : 0.18}
                />
              )
            })}
            {layout.nodes.map(node => {
              const source = nodes.find(item => item.id === node.id)
              const selected = picked?.id === node.id
              const neighbor = hop && hops.has(node.id)
              const stroke = selected ? '#22D3EE' : neighbor ? '#8B7CF6' : 'rgba(139,124,246,0.28)'
              const lines = wrapLabel(node.label, Math.max(5, Math.floor((nodeW - 16) / 8)), 2)
              const lineStep = 13
              const textY = node.y - ((lines.length - 1) * lineStep) / 2 + 4
              return (
                <g
                  key={node.id}
                  data-ph-node={node.id}
                  role="button"
                  tabIndex={0}
                  aria-label={node.label}
                  aria-current={selected ? 'true' : undefined}
                  style={{ cursor: 'pointer' }}
                  onClick={() => setPickedId(node.id)}
                  onDoubleClick={() => { if (source && isFeatureLike(source)) onOpen(source.stable_key) }}
                  onKeyDown={e => { if (e.key === 'Enter' || e.key === ' ') setPickedId(node.id) }}
                >
                  <rect
                    x={node.x - nodeW / 2}
                    y={node.y - nodeH / 2}
                    width={nodeW}
                    height={nodeH}
                    rx={18}
                    fill="#12121f"
                    stroke={stroke}
                    strokeWidth={selected ? 2 : 1.25}
                  />
                  {lines.map((line, index) => (
                    <text
                      key={`${node.id}-${index}`}
                      x={node.x}
                      y={textY + index * lineStep}
                      textAnchor="middle"
                      fill="#E8E8F2"
                      fontSize={12}
                      fontFamily="Segoe UI, Microsoft YaHei UI, sans-serif"
                    >
                      {line}
                    </text>
                  ))}
                </g>
              )
            })}
          </g>
        </svg>
      </div>
      <p className="ph-legend" aria-hidden="true">
        <span><i />contains</span>
        <span><i className="uses" />uses</span>
        <span><i className="calls" />calls</span>
        <span className="ph-dim">{zh ? `${layout.nodes.length} 个节点 · 滚轮缩放 · 拖动平移 · 双击空白适合窗口` : `${layout.nodes.length} nodes · scroll to zoom · drag to pan · double-click empty to fit`}</span>
      </p>
      <div className="ph-graph-detail">
        {picked ? (
          <>
            <div>
              <strong>{picked.name}</strong>
              <p className="ph-dim">{picked.stable_key}</p>
              <p className="ph-dim">{picked.type} · {DOMAIN_META[picked.domain ?? '']?.zh ?? picked.domain ?? '—'}{picked.version ? ` · ${picked.version}` : ''}</p>
            </div>
            <span className="ph-chip">{picked.type}</span>
            {isFeatureLike(picked) ? (
              <button type="button" className="primary" onClick={() => onOpen(picked.stable_key)}>{zh ? '打开功能卡' : 'Open card'}</button>
            ) : null}
            <button type="button" className={hop ? 'is-current' : ''} onClick={() => setHop(value => !value)}>
              {zh ? '高亮一跳邻居' : 'Highlight neighbors'}
            </button>
          </>
        ) : <span className="ph-dim">{zh ? '点击节点查看详情。默认整图适合窗口，放大后可看清标签。' : 'Click a node. Fit the window first, then zoom to read labels.'}</span>}
      </div>
    </div>
  )
}

export function GraphFilters({
  domain, type, onDomain, onType, zh,
}: {
  domain: string
  type: string
  onDomain: (value: string) => void
  onType: (value: string) => void
  zh: boolean
}): React.JSX.Element {
  return (
    <div className="ph-tabs-extra">
      <label>
        {zh ? '筛选' : 'Filter'}
        <select value={domain} onChange={e => onDomain(e.target.value)} aria-label={zh ? '全部域' : 'All domains'}>
          <option value="">{zh ? '全部域' : 'All domains'}</option>
          {DOMAIN_ORDER.map(id => <option key={id} value={id}>{DOMAIN_META[id].zh}</option>)}
        </select>
      </label>
      <label>
        <select value={type} onChange={e => onType(e.target.value)} aria-label={zh ? '全部类型' : 'All types'}>
          <option value="">{zh ? '全部类型' : 'All types'}</option>
          <option value="体系">{zh ? '体系' : 'System'}</option>
          {NODE_TYPES.map(item => <option key={item} value={item}>{item}</option>)}
        </select>
      </label>
    </div>
  )
}
