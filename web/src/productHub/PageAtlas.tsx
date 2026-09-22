import React, { useMemo, useState } from 'react'
import { PAGE_GROUPS, pageDossiers, settingLabel, type PageDossier } from './hubCatalog'
import { changeKindLabel, findingTone } from './hubModel'
import type { HubChange, HubFinding, HubNode } from './productHubTypes'

export function PageAtlas({
  nodes, changes, findings, query, zh, onOpen, selectedId, onSelect,
}: {
  nodes: HubNode[]
  changes: HubChange[]
  findings: HubFinding[]
  query: string
  zh: boolean
  onOpen: (key: string) => void
  selectedId?: string
  onSelect: (id: string) => void
}): React.JSX.Element {
  const dossiers = useMemo(() => pageDossiers(nodes, changes, findings), [nodes, changes, findings])
  const q = query.trim().toLocaleLowerCase()
  const visible = dossiers.filter(item => {
    if (!q) return true
    return item.spec.name.toLocaleLowerCase().includes(q)
      || item.spec.nameEn.toLocaleLowerCase().includes(q)
      || item.spec.id.toLocaleLowerCase().includes(q)
      || item.features.some(node => node.name.toLocaleLowerCase().includes(q) || node.stable_key.toLocaleLowerCase().includes(q))
  })
  const selected = visible.find(item => item.spec.id === selectedId) ?? visible[0]
  return (
    <div className="ph-atlas">
      <div className="ph-section-head">
        <span>{zh ? '按前台页面' : 'By page'}</span>
        <span>{visible.length} / {dossiers.length} PAGES</span>
      </div>
      {PAGE_GROUPS.map(group => {
        const rows = visible.filter(item => item.spec.group === group.id)
        if (rows.length === 0) return null
        return (
          <section key={group.id} className="ph-atlas-group" aria-label={zh ? group.zh : group.en}>
            <div className="ph-section-head">
              <span>{zh ? group.zh : group.en}</span>
              <span>{rows.length}</span>
            </div>
            <div className="ph-pages">
              {rows.map(item => (
                <PageCard key={item.spec.id} item={item} zh={zh} current={selected?.spec.id === item.spec.id} onSelect={() => onSelect(item.spec.id)} />
              ))}
            </div>
          </section>
        )
      })}
      {selected ? <PageDossierPane item={selected} zh={zh} onOpen={onOpen} /> : null}
    </div>
  )
}

function PageCard({ item, zh, current, onSelect }: { item: PageDossier; zh: boolean; current: boolean; onSelect: () => void }): React.JSX.Element {
  const added = item.changes.filter(change => change.kind === 'added').length
  const updated = item.changes.filter(change => change.kind === 'updated').length
  const removed = item.changes.filter(change => change.kind === 'removed').length
  const open = item.findings.filter(finding => finding.status === 'open').length
  return (
    <button type="button" className={`ph-page${current ? ' is-current' : ''}`} onClick={onSelect}>
      <div className="ph-domain-top">
        <strong>{item.spec.name}<span className="ph-en">{item.spec.nameEn}</span></strong>
        <span className="ph-dim">{item.features.length} {zh ? '功能' : 'verbs'}</span>
      </div>
      <p className="ph-domain-mods">{item.spec.summary}</p>
      <p className="ph-page-nav">{item.spec.nav}</p>
      <div className="ph-page-meta">
        {added ? <span className="kind-added">+{added}</span> : null}
        {updated ? <span className="kind-updated">~{updated}</span> : null}
        {removed ? <span className="kind-removed">-{removed}</span> : null}
        {open ? <span className="is-warn">{open} {zh ? '诊断' : 'diag'}</span> : <span className="is-ok">{zh ? '诊断清' : 'clear'}</span>}
      </div>
    </button>
  )
}

function PageDossierPane({ item, zh, onOpen }: { item: PageDossier; zh: boolean; onOpen: (key: string) => void }): React.JSX.Element {
  const [openMods, setOpen] = useState({ features: true, changes: true, findings: true })
  return (
    <article className="ph-dossier" aria-label={zh ? `${item.spec.name} 页面卷宗` : `${item.spec.nameEn} dossier`}>
      <header>
        <p className="ph-dim">{zh ? item.spec.groupZh : item.spec.groupEn} · page.{item.spec.id}</p>
        <h3>{item.spec.name}<span className="ph-en">{item.spec.nameEn}</span></h3>
        <p className="ph-quote">{item.spec.analysis}</p>
        <p className="ph-dim">{zh ? '入口' : 'Nav'}：{item.spec.nav}</p>
        {item.settings.length > 0 ? <p className="ph-dim">{zh ? '相关设置' : 'Settings'}：{item.settings.map(id => settingLabel(id, zh)).join(' · ')}</p> : null}
      </header>
      <section>
        <button type="button" className="ph-module" onClick={() => setOpen(curr => ({ ...curr, features: !curr.features }))}>
          <b>{zh ? '本页功能' : 'Verbs on this page'}</b>
          <span className="ph-count">{item.features.length}</span>
        </button>
        {openMods.features ? (
          <div className="ph-children">
            {item.features.length === 0 ? <p className="ph-dim">{zh ? '本页功能卡尚未挂上。' : 'No cards on this page yet.'}</p> : item.features.map(node => (
              <button key={node.id} type="button" className="ph-feature-row" onClick={() => onOpen(node.stable_key)}>
                <span className="ph-feature-name">{node.name}</span>
                <span className="ph-feature-sum">{node.summary || (zh ? '打开卡片看这一步做什么' : 'Open the card')}</span>
                <span>{node.stable_key}</span>
              </button>
            ))}
          </div>
        ) : null}
      </section>
      <section>
        <button type="button" className="ph-module" onClick={() => setOpen(curr => ({ ...curr, changes: !curr.changes }))}>
          <b>{zh ? '本页动态' : 'Dynamics'}</b>
          <span className="ph-count">{item.changes.length}</span>
        </button>
        {openMods.changes ? (
          <div className="ph-children">
            {item.changes.length === 0 ? <p className="ph-dim">{zh ? '这一轮该页没有变更。' : 'No changes on this page.'}</p> : item.changes.map(change => (
              <button key={`${change.kind}-${change.stable_key}`} type="button" className="ph-feature-row" onClick={() => change.kind !== 'removed' && onOpen(change.stable_key)}>
                <span className={`ph-tl-kind kind-${change.kind}`}>{changeKindLabel(change.kind, zh)}</span>
                <span>{change.title || change.stable_key}</span>
                <span>{change.summary}</span>
                {(change.impacts ?? []).filter(key => key === item.spec.id || key === `page.${item.spec.id}`).length > 0 ? (
                  <span className="ph-dim">{zh ? '本页受影响' : 'Impacts this page'}</span>
                ) : null}
              </button>
            ))}
          </div>
        ) : null}
      </section>
      <section>
        <button type="button" className="ph-module" onClick={() => setOpen(curr => ({ ...curr, findings: !curr.findings }))}>
          <b>{zh ? '本页诊断' : 'Findings'}</b>
          <span className="ph-count">{item.findings.length}</span>
        </button>
        {openMods.findings ? (
          <div className="ph-children">
            {item.findings.length === 0 ? <p className="ph-dim">{zh ? '本页没有未闭合诊断。' : 'No findings on this page.'}</p> : item.findings.map(finding => (
              <p key={`${finding.error_code}-${finding.stable_key}`} className={`ph-dim ${findingTone(finding.severity)}`}>
                {finding.error_code} · {finding.title}
              </p>
            ))}
          </div>
        ) : null}
      </section>
    </article>
  )
}
