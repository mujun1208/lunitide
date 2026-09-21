import React, { useMemo, useState } from 'react'
import { splitLines, wrapLabel } from './hubModel'
import type { HubBranch, HubStep } from './productHubTypes'
import { useBoxSize } from './useBoxSize'

type Kind = 'main' | 'ok' | 'fail' | 'retry' | 'down'

type LayoutNode = {
  id: string
  label: string
  detail: string
  kind: Kind
  x: number
  y: number
}

type LayoutEdge = {
  id: string
  from: string
  to: string
  kind: 'm' | 'ok' | 'f' | 'rt' | 'dg'
  label?: string
}

type ChainLayout = {
  nodes: LayoutNode[]
  edges: LayoutEdge[]
  width: number
  height: number
  nodeW: number
  nodeH: number
}

const COLORS = {
  m: '#8B7CF6',
  ok: '#34D399',
  f: '#FBBF24',
  rt: '#34D399',
  dg: '#FBBF24',
  card: '#171728',
  text: '#E8E8F2',
  dim: '#9C9CB0',
  cyan: '#22D3EE',
  line: 'rgba(139,124,246,0.22)',
}

function splitBranch(branch: HubBranch | undefined, fallback: string): string[] {
  if (!branch) return []
  const parts = splitLines(branch.description)
  if (parts.length > 1) return parts
  return [branch.name || fallback]
}

function buildLayout(steps: HubStep[], branches: HubBranch[], boxW: number): ChainLayout {
  const ordered = [...steps].toSorted((a, b) => a.index - b.index)
  const nodes: LayoutNode[] = []
  const edges: LayoutEdge[] = []
  const n = Math.max(1, ordered.length)
  const pad = 24
  const nodeH = 48
  const width = boxW >= 80 ? boxW : Math.max(720, pad * 2 + n * 148)
  const nodeW = Math.min(148, Math.max(96, Math.floor((width - pad * 2) / (n + 0.7))))
  const rightCol = nodeW + 12
  const trunkWidth = Math.max(nodeW, width - pad * 2 - rightCol)
  const gap = n > 1 ? (trunkWidth - nodeW) / (n - 1) : 0
  const startX = pad
  const trunkY = 16

  ordered.forEach((step, index) => {
    nodes.push({
      id: `main-${step.index}`,
      label: step.name,
      detail: step.detail,
      kind: 'main',
      x: startX + index * gap,
      y: trunkY,
    })
    if (index > 0) {
      edges.push({
        id: `m-${step.index}`,
        from: `main-${ordered[index - 1].index}`,
        to: `main-${step.index}`,
        kind: 'm',
      })
    }
  })

  const last = ordered[ordered.length - 1]
  const lastId = last ? `main-${last.index}` : ''
  const lastX = last ? startX + (ordered.length - 1) * gap : startX
  const okX = width - pad - nodeW
  const failX = Math.max(pad, Math.min(lastX - nodeW * 0.15, okX - nodeW - 20))

  const success = branches.find(item => item.type === 'success' || item.type === 'ok')
  const failure = branches.find(item => item.type === 'failure' || item.type === 'fail')
  const retryBranch = branches.find(item => item.type === 'retry')
  const retry = retryBranch ?? failure?.retry
  const fallback = branches.find(item => item.type === 'fallback' || item.type === 'degrade') ?? failure?.fallback ?? retryBranch?.fallback

  const okParts = splitBranch(success, '成功校验')
  const failParts = splitBranch(failure, '错误诊断·失败')
  const retryParts = splitBranch(retry as HubBranch | undefined, '重试·备用启动')
  const downParts = splitBranch(fallback as HubBranch | undefined, '降级·播报+推荐')

  const placeColumn = (parts: string[], kind: Kind, x: number, startY: number, prefix: string) => {
    parts.forEach((label, index) => {
      const [title, ...rest] = label.split(/[·:：]/)
      nodes.push({
        id: `${prefix}-${index}`,
        label: title || label,
        detail: rest.join('·') || (kind === 'ok' ? 'success' : kind === 'fail' ? 'failure' : kind === 'retry' ? 'retry' : 'fallback'),
        kind,
        x,
        y: startY + index * 56,
      })
      if (index > 0) {
        edges.push({
          id: `${prefix}-e-${index}`,
          from: `${prefix}-${index - 1}`,
          to: `${prefix}-${index}`,
          kind: kind === 'ok' ? 'ok' : kind === 'retry' ? 'rt' : kind === 'down' ? 'dg' : 'f',
        })
      }
    })
  }

  const branchY = 88
  placeColumn(okParts, 'ok', okX, branchY, 'ok')
  placeColumn(failParts, 'fail', failX, branchY, 'fail')
  const retryY = branchY + Math.max(failParts.length, 1) * 56
  placeColumn(retryParts, 'retry', failX, retryY, 'rt')
  const downY = retryY + Math.max(retryParts.length, 1) * 56
  placeColumn(downParts, 'down', failX, downY, 'dn')

  if (lastId && okParts.length) edges.push({ id: 'to-ok', from: lastId, to: 'ok-0', kind: 'ok', label: '成功' })
  if (lastId && failParts.length) edges.push({ id: 'to-fail', from: lastId, to: 'fail-0', kind: 'f', label: '失败' })
  if (failParts.length && retryParts.length) edges.push({ id: 'fail-rt', from: `fail-${failParts.length - 1}`, to: 'rt-0', kind: 'rt' })
  if (retryParts.length && okParts.length) edges.push({ id: 'rt-ok', from: `rt-${retryParts.length - 1}`, to: 'ok-0', kind: 'rt', label: '重试成功' })
  if (retryParts.length && downParts.length) edges.push({ id: 'rt-dn', from: `rt-${retryParts.length - 1}`, to: 'dn-0', kind: 'dg' })
  if (!retryParts.length && failParts.length && downParts.length) edges.push({ id: 'fail-dn', from: `fail-${failParts.length - 1}`, to: 'dn-0', kind: 'dg' })

  const maxY = nodes.reduce((n, node) => Math.max(n, node.y), 18)
  return { nodes, edges, width, height: Math.max(240, maxY + nodeH + 20), nodeW, nodeH }
}

function edgePath(from: LayoutNode, to: LayoutNode, nodeW: number, nodeH: number): string {
  const x1 = from.x + nodeW
  const y1 = from.y + nodeH / 2
  const x2 = to.x
  const y2 = to.y + nodeH / 2
  if (Math.abs(from.x - to.x) < 8) {
    return `M${from.x + nodeW / 2} ${from.y + nodeH} V${to.y}`
  }
  if (Math.abs(y1 - y2) < 10) return `M${x1} ${y1} H${x2}`
  const mid = (x1 + x2) / 2
  return `M${x1} ${y1} C${mid} ${y1}, ${mid} ${y2}, ${x2} ${y2}`
}

function prefix(kind: Kind): string {
  if (kind === 'ok') return '✓ '
  if (kind === 'fail') return '✗ '
  if (kind === 'down') return '▼ '
  if (kind === 'retry') return '↻ '
  return ''
}

function mark(kind: Kind, index: number): string {
  if (kind === 'main') return String.fromCharCode(0x2460 + Math.min(19, Math.max(0, index)))
  return prefix(kind).trim()
}

function ChainChart({
  layout,
  sel,
  pick,
  fill,
  boxRef,
  markerKey,
}: {
  layout: ChainLayout
  sel: string
  pick: (id: string) => void
  fill?: boolean
  boxRef: (node: HTMLElement | null) => void
  markerKey: string
}): React.JSX.Element {
  const { nodeW, nodeH } = layout
  return (
    <div ref={boxRef} className={`product-hub-chain${fill ? ' is-fill' : ''}`}>
      <svg
        viewBox={`0 0 ${layout.width} ${layout.height}`}
        width="100%"
        height={fill ? '100%' : undefined}
        preserveAspectRatio="xMidYMid meet"
        style={fill ? { width: '100%', height: '100%' } : { width: '100%', height: 'auto', aspectRatio: `${layout.width} / ${layout.height}` }}
        role="img"
        aria-label="功能调用链路"
      >
        <defs>
          <marker id={`${markerKey}-m`} viewBox="0 0 8 8" refX="7" refY="4" markerWidth="7" markerHeight="7" markerUnits="userSpaceOnUse" orient="auto">
            <path d="M1 1 L7 4 L1 7 Z" fill={COLORS.m} />
          </marker>
          <marker id={`${markerKey}-ok`} viewBox="0 0 8 8" refX="7" refY="4" markerWidth="7" markerHeight="7" markerUnits="userSpaceOnUse" orient="auto">
            <path d="M1 1 L7 4 L1 7 Z" fill={COLORS.ok} />
          </marker>
          <marker id={`${markerKey}-f`} viewBox="0 0 8 8" refX="7" refY="4" markerWidth="7" markerHeight="7" markerUnits="userSpaceOnUse" orient="auto">
            <path d="M1 1 L7 4 L1 7 Z" fill={COLORS.f} />
          </marker>
        </defs>
        {layout.edges.map(edge => {
          const from = layout.nodes.find(node => node.id === edge.from)
          const to = layout.nodes.find(node => node.id === edge.to)
          if (!from || !to) return null
          const color = edge.kind === 'ok' || edge.kind === 'rt' ? COLORS.ok : edge.kind === 'f' || edge.kind === 'dg' ? COLORS.f : COLORS.m
          const dash = edge.kind === 'rt' ? '5 4' : edge.kind === 'dg' ? '2 3' : undefined
          const marker = edge.kind === 'ok' || edge.kind === 'rt' ? `url(#${markerKey}-ok)` : edge.kind === 'f' || edge.kind === 'dg' ? `url(#${markerKey}-f)` : `url(#${markerKey}-m)`
          const on = sel === edge.from || sel === edge.to
          return (
            <g key={edge.id}>
              <path d={edgePath(from, to, nodeW, nodeH)} fill="none" stroke={color} strokeWidth={on ? 2 : 1.5} strokeDasharray={dash} strokeLinecap="round" markerEnd={marker} />
              {edge.label ? (
                <text x={(from.x + to.x + nodeW) / 2} y={(from.y + to.y) / 2 + 12} textAnchor="middle" fill={COLORS.dim} fontSize="10">{edge.label}</text>
              ) : null}
            </g>
          )
        })}
        {layout.nodes.map(node => {
          const on = sel === node.id
          const stroke = on ? COLORS.cyan : node.kind === 'ok' ? COLORS.ok : node.kind === 'fail' || node.kind === 'down' ? COLORS.f : COLORS.line
          const titleMax = Math.max(6, Math.floor((nodeW - 20) / 8))
          const titles = wrapLabel(node.label, titleMax, 2)
          const showDetail = titles.length === 1
          return (
            <g key={node.id} role="button" tabIndex={0} aria-label={node.label} aria-current={on ? 'true' : undefined} style={{ cursor: 'pointer' }} onClick={() => pick(node.id)} onKeyDown={e => { if (e.key === 'Enter' || e.key === ' ') pick(node.id) }}>
              <title>{`${node.label}${node.detail ? ` · ${node.detail}` : ''}`}</title>
              <rect x={node.x} y={node.y} width={nodeW} height={nodeH} rx={10} fill={on ? 'rgba(34,211,238,0.08)' : COLORS.card} stroke={stroke} strokeWidth={on ? 2 : 1.25} />
              <text x={node.x + 10} y={node.y + 16} fill={COLORS.cyan} fontSize="11">{mark(node.kind, Number(node.id.split('-')[1]) - 1)}</text>
              {titles.map((line, index) => (
                <text key={`${node.id}-t-${index}`} x={node.x + nodeW / 2 + 6} y={node.y + 16 + index * 13} textAnchor="middle" fill={COLORS.text} fontSize="12" fontWeight="600">{line}</text>
              ))}
              {showDetail ? <text x={node.x + nodeW / 2} y={node.y + 33} textAnchor="middle" fill={COLORS.dim} fontSize="10">{wrapLabel(node.detail, Math.max(8, titleMax + 2), 1)[0]}</text> : null}
            </g>
          )
        })}
      </svg>
      <p className="ph-legend" aria-hidden="true">
        <span><i />主干</span>
        <span><i className="ok" />成功</span>
        <span><i className="fail" />失败</span>
        <span><i className="retry" />重试</span>
        <span><i className="down" />降级</span>
        <span className="ph-dim">点击节点 / 讲解行联动</span>
      </p>
    </div>
  )
}

export function ChainFlowView({
  steps,
  branches,
  allowFullscreen = true,
}: {
  steps: HubStep[]
  branches: HubBranch[]
  allowFullscreen?: boolean
}): React.JSX.Element {
  const ordered = useMemo(() => [...steps].toSorted((a, b) => a.index - b.index), [steps])
  const [sel, setSel] = useState('main-2')
  const [full, setFull] = useState(false)
  const [setInlineBox, inlineBox] = useBoxSize()
  const [setFullBox, fullBox] = useBoxSize()
  const inlineLayout = useMemo(() => buildLayout(ordered, branches, inlineBox.w), [ordered, branches, inlineBox.w])
  const fullLayout = useMemo(() => buildLayout(ordered, branches, fullBox.w), [ordered, branches, fullBox.w])

  if (ordered.length === 0) return <p className="product-hub-muted">该版本未提供链路声明</p>

  const pick = (id: string) => setSel(id)
  const success = branches.filter(item => item.type === 'success' || item.type === 'ok')
  const recover = branches.filter(item => item.type !== 'success' && item.type !== 'ok')

  const row = (id: string, markText: string, name: string, desc: string) => (
    <li key={id} role="button" tabIndex={0} data-step={id} className={sel === id ? 'is-on' : ''} aria-current={sel === id ? 'true' : undefined} onClick={() => pick(id)} onKeyDown={e => { if (e.key === 'Enter' || e.key === ' ') pick(id) }}>
      <span>{markText}</span>
      <b>{name}</b>
      <span>{desc}</span>
    </li>
  )

  const explain = (
    <div className="ph-step-groups">
      <p className="ph-step-kicker">主干 MAIN</p>
      <ol className="ph-steps">
        {ordered.map(step => row(`main-${step.index}`, String.fromCharCode(0x2460 + Math.min(19, step.index - 1)), step.name, step.description || step.detail))}
      </ol>
      {success.length ? <p className="ph-step-kicker">成功路径 SUCCESS</p> : null}
      <ol className="ph-steps">
        {success.flatMap(branch => {
          const parts = splitLines(branch.description)
          const items = parts.length > 1 ? parts : [branch.name]
          return items.map((label, i) => row(`ok-${i}`, '✓', label.split(/[·:：]/)[0] || branch.name, label || branch.description))
        })}
      </ol>
      {recover.length ? <p className="ph-step-kicker">失败与恢复 FAILURE · RECOVERY</p> : null}
      <ol className="ph-steps">
        {recover.flatMap(branch => {
          const kind = branch.type === 'retry' ? 'rt' : branch.type === 'fallback' || branch.type === 'degrade' ? 'dn' : 'fail'
          const markText = kind === 'rt' ? '↻' : kind === 'dn' ? '▼' : '✗'
          const parts = splitLines(branch.description)
          const items = parts.length > 1 ? parts : [branch.name]
          return items.map((label, i) => row(`${kind === 'rt' ? 'rt' : kind === 'dn' ? 'dn' : 'fail'}-${i}`, markText, label.split(/[·:：]/)[0] || branch.name, label || branch.description))
        })}
      </ol>
    </div>
  )

  return (
    <div>
      {allowFullscreen ? (
        <div className="ph-sec-title ph-chain-tools">
          <span />
          <button type="button" onClick={() => setFull(true)}>全屏</button>
        </div>
      ) : null}
      <ChainChart layout={inlineLayout} sel={sel} pick={pick} boxRef={setInlineBox} markerKey="ph-inline" />
      <div className="ph-sec-title">
        <b>F</b>
        <h3>逐步讲解</h3>
        <span>STEP-BY-STEP</span>
      </div>
      {full ? null : explain}
      {full ? (
        <div className="ph-chain-overlay" role="dialog" aria-label="调用链路全屏">
          <div className="ph-chain-overlay-card">
            <header>
              <strong>调用链路</strong>
              <button type="button" onClick={() => setFull(false)}>关闭</button>
            </header>
            <div className="ph-chain-overlay-body">
              <div className="ph-chain-scroll">
                <ChainChart layout={fullLayout} sel={sel} pick={pick} fill boxRef={setFullBox} markerKey="ph-full" />
              </div>
              <aside>{explain}</aside>
            </div>
          </div>
        </div>
      ) : null}
    </div>
  )
}
