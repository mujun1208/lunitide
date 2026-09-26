import React, { useMemo, useState } from 'react'
import {
  expertSections, pluginRows, settingCoverage, settingRows,
  type ExpertSection, type SettingRow,
} from './hubModel'
import type { HubEdge, HubNode } from './productHubTypes'

type Focus =
  | { kind: 'section'; id: string }
  | { kind: 'plugin'; id: string }
  | { kind: 'setting'; id: string }

function linkedNodes(nodes: HubNode[], edges: HubEdge[], id: string, key: string): HubNode[] {
  const ids = neighbors(edges, id, key)
  return nodes.filter(node => node.id === id || ids.has(node.id) || ids.has(node.stable_key))
}

function neighbors(edges: HubEdge[], id: string, key: string): Set<string> {
  const next = new Set<string>()
  for (const edge of edges) {
    if (edge.from === id || edge.from === key) next.add(edge.to)
    if (edge.to === id || edge.to === key) next.add(edge.from)
  }
  return next
}

function relatedNodes(nodes: HubNode[], edges: HubEdge[], expert: HubNode | undefined, focus: Focus | null): HubNode[] {
  if (focus?.kind === 'plugin' || focus?.kind === 'setting') {
    const target = nodes.find(node => node.id === focus.id)
    if (!target) return []
    return linkedNodes(nodes, edges, target.id, target.stable_key)
  }
  if (!expert) return []
  return linkedNodes(nodes, edges, expert.id, expert.stable_key)
}

function focusLabel(focus: Focus | null, zh: boolean, settings: SettingRow[], sections: ExpertSection[]): string {
  if (!focus || focus.kind === 'section') {
    const hit = sections.find(item => item.key === focus?.id)
    if (hit) return zh ? hit.zh : hit.en
    return zh ? '已连接的节点' : 'Linked nodes'
  }
  if (focus.kind === 'plugin') return zh ? '插件反查功能' : 'Plugin → features'
  const group = settings.find(item => item.id === focus.id)
  return zh ? `设置 · ${group?.name ?? focus.id}` : `Settings · ${group?.name ?? focus.id}`
}

export function AnatomyPane({
  nodes, edges, zh, onOpen,
}: {
  nodes: HubNode[]
  edges: HubEdge[]
  zh: boolean
  onOpen: (key: string) => void
}): React.JSX.Element {
  const experts = nodes.filter(node => node.type === 'Expert')
  const plugins = pluginRows(nodes)
  const settings = settingRows(nodes)
  const [expertId, setExpertId] = useState(experts[0]?.id ?? '')
  const [showAllPlugins, setShowAllPlugins] = useState(false)
  const [focus, setFocus] = useState<Focus | null>(null)
  const shownPlugins = showAllPlugins ? plugins : plugins.slice(0, 5)
  const cover = settingCoverage(nodes)
  const lead = experts.find(node => node.id === expertId) ?? experts[0]
  const sections = lead ? expertSections(lead) : []
  const related = useMemo(() => relatedNodes(nodes, edges, lead, focus), [nodes, edges, lead, focus])

  return (
    <div>
      <div className="ph-section-head">
        <span>{zh ? '专家说明' : 'Expert detail'} · {lead?.name ?? '—'}{lead?.version ? ` ${lead.version}` : ''}</span>
      </div>
      {experts.length > 1 ? (
        <div className="ph-chips">
          {experts.map(node => (
            <button
              key={node.id}
              type="button"
              className={lead?.id === node.id ? 'ph-chip is-on' : 'ph-chip'}
              onClick={() => setExpertId(node.id)}
            >
              <i />{node.name}
            </button>
          ))}
        </div>
      ) : null}
      <div className="ph-asset-grid">
        {sections.length === 0 ? <p className="ph-dim">{zh ? '图谱里还没有 Expert 节点。' : 'No expert node yet.'}</p> : sections.map(section => (
          <button
            key={section.key}
            type="button"
            className={`ph-asset${focus?.kind === 'section' && focus.id === section.key ? ' is-on' : ''}`}
            onClick={() => setFocus({ kind: 'section', id: section.key })}
          >
            <strong>{section.zh} <span className="ph-en">{section.en}</span></strong>
            <p>{section.text}</p>
          </button>
        ))}
      </div>
      <div className="ph-section-head">
        <span>{zh ? '插件 roster' : 'Plugin roster'} · {plugins.length} {zh ? '项' : ''}</span>
        <span>ROSTER</span>
      </div>
      <table className="ph-roster">
        <thead>
          <tr>
            <th>{zh ? '名称' : 'Name'}</th>
            <th>PROVIDES</th>
            <th>{zh ? '版本' : 'Ver'}</th>
            <th>{zh ? '状态' : 'State'}</th>
          </tr>
        </thead>
        <tbody>
          {plugins.length === 0 ? (
            <tr><td colSpan={4} className="ph-dim">{zh ? '暂无插件节点' : 'No plugins'}</td></tr>
          ) : shownPlugins.map(row => (
            <tr
              key={row.id}
              className={focus?.kind === 'plugin' && focus.id === row.id ? 'is-on' : ''}
              onClick={() => setFocus({ kind: 'plugin', id: row.id })}
            >
              <td>{row.name}</td>
              <td>{row.provides}</td>
              <td>{row.version}</td>
              <td><span className={`ph-dot-label${row.state === 'degraded' ? ' is-warn' : ' is-ok'}`}>{row.state}</span></td>
            </tr>
          ))}
        </tbody>
      </table>
      {plugins.length > 5 ? (
        <button type="button" className="ph-more" onClick={() => setShowAllPlugins(value => !value)}>
          {showAllPlugins ? (zh ? '收起' : 'Collapse') : `${zh ? '其余' : 'More'} ${plugins.length - 5} ${zh ? '项折叠' : ''}`}
        </button>
      ) : null}
      <div className="ph-section-head">
        <span>{zh ? '设置' : 'Settings'} · {settings.length} {zh ? '项' : ''}</span>
        <span>SETTINGS</span>
      </div>
      <div className="ph-chips ph-setting-chips">
        {settings.map(group => (
          <button
            key={group.id}
            type="button"
            className={focus?.kind === 'setting' && focus.id === group.id ? 'ph-chip is-on' : 'ph-chip'}
            onClick={() => setFocus({ kind: 'setting', id: group.id })}
          >
            <i />{group.name}
          </button>
        ))}
      </div>
      <p className="ph-dim">{cover.covered === cover.total
        ? (zh ? `本版设置 ${cover.covered} 项` : `Stored settings ${cover.covered}`)
        : (zh ? `本版设置 ${cover.covered}/${cover.total}` : `Stored settings ${cover.covered}/${cover.total}`)}</p>
      <div className="ph-relate">
        <div className="ph-section-head">
          <span>{zh ? '联动' : 'Linked'} · {focusLabel(focus, zh, settings, sections)}</span>
          <span>{related.length}</span>
        </div>
        <p className="ph-dim">
          {zh
            ? '点说明、插件行或设置芯片，下面列出图谱里连上的全部节点。点一行可打开卡。'
            : 'Click a handbook card, plugin row, or setting chip to see live graph neighbors.'}
        </p>
        <div className="ph-list">
          {related.length === 0 ? <p className="ph-dim">{zh ? '这一侧还没有连上的活源节点。' : 'No linked nodes yet.'}</p> : related.map(node => (
            <button key={node.id} type="button" className="ph-feature-row" onClick={() => onOpen(node.stable_key)}>
              <span>{node.name}</span>
              <span>{node.type} · {node.stable_key}</span>
            </button>
          ))}
        </div>
      </div>
    </div>
  )
}
