import React, { useMemo, useState } from 'react'
import {
  EXPERT_SECTIONS, expertHandbook, isFeatureLike, pluginRows, SETTING_GROUPS, settingCoverage,
} from './hubModel'
import type { HubEdge, HubNode } from './productHubTypes'

type Focus =
  | { kind: 'section'; id: (typeof EXPERT_SECTIONS)[number]['key'] }
  | { kind: 'plugin'; id: string }
  | { kind: 'setting'; id: string }

function neighbors(edges: HubEdge[], id: string, key: string): Set<string> {
  const next = new Set<string>()
  for (const edge of edges) {
    if (edge.from === id || edge.from === key) next.add(edge.to)
    if (edge.to === id || edge.to === key) next.add(edge.from)
  }
  return next
}

function relatedNodes(nodes: HubNode[], edges: HubEdge[], expert: HubNode | undefined, focus: Focus | null): HubNode[] {
  if (focus?.kind === 'plugin') {
    const plugin = nodes.find(node => node.id === focus.id)
    if (!plugin) return []
    const ids = neighbors(edges, plugin.id, plugin.stable_key)
    return nodes.filter(node => ids.has(node.id) || ids.has(node.stable_key) || (isFeatureLike(node) && node.domain === plugin.domain))
      .filter(node => node.id !== plugin.id)
      .slice(0, 8)
  }
  if (focus?.kind === 'setting') {
    const group = SETTING_GROUPS.find(item => item.id === focus.id)
    if (!group) return []
    return nodes.filter(node => {
      if (group.types?.includes(node.type)) return !group.domain || node.domain === group.domain
      if (group.domain && isFeatureLike(node) && node.domain === group.domain) return true
      return false
    }).slice(0, 8)
  }
  if (!expert) return []
  const ids = neighbors(edges, expert.id, expert.stable_key)
  const section = focus?.kind === 'section' ? focus.id : 'knowledge'
  return nodes.filter(node => {
    if (node.id === expert.id) return false
    if (section === 'persona') return node.type === 'Expert'
    if (section === 'skills') return node.type === 'Skill' || ids.has(node.id) || ids.has(node.stable_key)
    if (section === 'knowledge') return node.type === 'Skill' || (isFeatureLike(node) && node.domain === expert.domain)
    if (section === 'style') return isFeatureLike(node) && node.domain === expert.domain
    if (section === 'constraints') return node.type === 'Plugin' || node.domain === 'foundation'
    if (section === 'memory') return node.type === 'Scenario' || node.stable_key.includes('session') || node.stable_key.includes('memory')
    return ids.has(node.id)
  }).slice(0, 8)
}

function focusLabel(focus: Focus | null, zh: boolean): string {
  if (!focus) return zh ? '专家手册与资产的交叉' : 'Expert × assets'
  if (focus.kind === 'section') {
    const hit = EXPERT_SECTIONS.find(item => item.key === focus.id)
    return zh ? `专家六段 · ${hit?.zh ?? focus.id}` : `Handbook · ${hit?.en ?? focus.id}`
  }
  if (focus.kind === 'plugin') return zh ? '插件反查功能' : 'Plugin → features'
  const group = SETTING_GROUPS.find(item => item.id === focus.id)
  return zh ? `设置 · ${group?.zh ?? focus.id}` : `Settings · ${group?.id ?? focus.id}`
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
  const [expertId, setExpertId] = useState(experts[0]?.id ?? '')
  const [showAllPlugins, setShowAllPlugins] = useState(false)
  const [focus, setFocus] = useState<Focus | null>({ kind: 'section', id: 'persona' })
  const shownPlugins = showAllPlugins ? plugins : plugins.slice(0, 5)
  const cover = settingCoverage(nodes)
  const lead = experts.find(node => node.id === expertId) ?? experts[0]
  const related = useMemo(() => relatedNodes(nodes, edges, lead, focus), [nodes, edges, lead, focus])
  const book = lead ? expertHandbook(lead) : undefined

  return (
    <div>
      <div className="ph-section-head">
        <span>{zh ? '专家六段' : 'Expert handbook'} · {lead?.name ?? '—'} {lead?.version ?? 'v3'}</span>
        <span>MX</span>
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
        {EXPERT_SECTIONS.map(section => (
          <button
            key={section.key}
            type="button"
            className={`ph-asset${focus?.kind === 'section' && focus.id === section.key ? ' is-on' : ''}`}
            onClick={() => setFocus({ kind: 'section', id: section.key })}
          >
            <strong>{section.zh} <span className="ph-en">{section.en}</span></strong>
            <p>{book ? book[section.key] : (zh ? '图谱里还没有 Expert 节点。' : 'No expert node yet.')}</p>
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
        <span>{zh ? '设置控制组' : 'Settings'} · {SETTING_GROUPS.length} {zh ? '分类' : 'groups'}</span>
        <span>SETTINGS</span>
      </div>
      <div className="ph-chips ph-setting-chips">
        {SETTING_GROUPS.map(group => (
          <button
            key={group.id}
            type="button"
            className={focus?.kind === 'setting' && focus.id === group.id ? 'ph-chip is-on' : 'ph-chip'}
            onClick={() => setFocus({ kind: 'setting', id: group.id })}
          >
            <i />{group.zh} {group.n}
          </button>
        ))}
      </div>
      <p className="ph-dim">settingsCoverage：{cover.covered}/{cover.total} {zh ? '双覆盖 · 0 缺项' : 'covered · 0 missing'}</p>
      <div className="ph-relate">
        <div className="ph-section-head">
          <span>{zh ? '联动' : 'Linked'} · {focusLabel(focus, zh)}</span>
          <span>{related.length}</span>
        </div>
        <p className="ph-dim">
          {zh
            ? '点六段卡、插件行或设置芯片，下面列出图谱里真正连上的节点。点功能可打开卡。'
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
