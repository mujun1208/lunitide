import React, { useEffect, useRef, useState } from 'react'
import { groupChangesByPage } from './hubCatalog'
import {
  changeImpactLabel, changeKindLabel, changeTitle, DOMAIN_META, DOMAIN_ORDER,
  domainKey, NODE_TYPES, typeLabel,
} from './hubModel'
import type { HubChange, HubNode } from './productHubTypes'

type MenuOption = { id: string; label: string; hint?: string }

export function MenuSelect({
  label, ariaLabel, value, options, onChange, onOpenChange,
}: {
  label: string
  ariaLabel?: string
  value: string
  options: MenuOption[]
  onChange: (id: string) => void
  onOpenChange?: (open: boolean) => void
}): React.JSX.Element {
  const [open, setOpen] = useState(false)
  const rootRef = useRef<HTMLDivElement>(null)
  const current = options.find(item => item.id === value) ?? options[0]
  const setMenu = (next: boolean | ((curr: boolean) => boolean)) => {
    setOpen(curr => {
      const value = typeof next === 'function' ? next(curr) : next
      if (value !== curr) queueMicrotask(() => onOpenChange?.(value))
      return value
    })
  }
  useEffect(() => {
    if (!open) return
    const onDoc = (event: MouseEvent) => {
      const target = event.target as HTMLElement | null
      if (target && rootRef.current?.contains(target)) return
      setMenu(false)
    }
    document.addEventListener('mousedown', onDoc)
    return () => document.removeEventListener('mousedown', onDoc)
  }, [open])
  return (
    <div ref={rootRef} className={`ph-menu${open ? ' is-open' : ''}`}>
      <button
        type="button"
        aria-label={ariaLabel ?? label}
        aria-expanded={open}
        aria-haspopup="listbox"
        onClick={() => setMenu(curr => !curr)}
      >
        <span className="ph-menu-kicker">{label}</span>
        <strong>{current?.label ?? label}</strong>
      </button>
      {open ? (
        <ul role="listbox" aria-label={label}>
          {options.map(item => (
            <li key={item.id || 'all'}>
              <button
                type="button"
                role="option"
                aria-selected={item.id === value}
                className={item.id === value ? 'is-on' : ''}
                onMouseDown={event => {
                  event.preventDefault()
                  onChange(item.id)
                  setMenu(false)
                }}
              >
                <b>{item.label}</b>
                {item.hint ? <span>{item.hint}</span> : null}
              </button>
            </li>
          ))}
        </ul>
      ) : null}
    </div>
  )
}

export function FinderBar({
  query, onQuery, hits, onPick, domain, type, onDomain, onType, nodes, zh,
}: {
  query: string
  onQuery: (value: string) => void
  hits: HubNode[]
  onPick: (node: HubNode) => void
  domain: string
  type: string
  onDomain: (value: string) => void
  onType: (value: string) => void
  nodes: HubNode[]
  zh: boolean
}): React.JSX.Element {
  const [focus, setFocus] = useState(false)
  const [filterOpen, setFilterOpen] = useState(0)
  const domainOptions: MenuOption[] = [
    { id: '', label: zh ? '全部域' : 'All domains', hint: zh ? `${nodes.filter(node => node.type === 'Domain').length} 个域` : 'All' },
    ...DOMAIN_ORDER.map(id => ({
      id,
      label: DOMAIN_META[id].zh,
      hint: zh
        ? `${nodes.filter(node => domainKey(node) === id && (node.type === 'Feature' || node.type === 'Scenario')).length} 张卡`
        : DOMAIN_META[id].en,
    })),
  ]
  const typeOptions: MenuOption[] = [
    { id: '', label: zh ? '全部类型' : 'All types', hint: zh ? '图谱与检索共用' : 'Graph + search' },
    { id: '体系', label: typeLabel('体系', zh), hint: zh ? '产品 / 域 / 模块骨架' : 'Product / domain / module' },
    ...NODE_TYPES.map(item => ({
      id: item,
      label: typeLabel(item, zh),
      hint: item,
    })),
  ]
  const typed = query.trim().length > 0
  const showDrop = (typed || focus) && filterOpen === 0
  return (
    <div className="ph-finder">
      <div className="ph-search-wrap">
        <input
          className="ph-search"
          value={query}
          onChange={e => onQuery(e.target.value)}
          onFocus={() => setFocus(true)}
          onBlur={() => window.setTimeout(() => setFocus(false), 160)}
          onKeyDown={e => {
            if (e.key !== 'Enter' || !hits[0]) return
            e.preventDefault()
            onPick(hits[0])
          }}
          placeholder={zh ? '找功能、模块或专家' : 'Find a card, module, or expert'}
          aria-label={zh ? '搜索产品知识' : 'Search product knowledge'}
        />
        {typed ? (
          <button type="button" className="ph-search-clear" aria-label={zh ? '清空搜索' : 'Clear search'} onMouseDown={event => { event.preventDefault(); onQuery('') }}>
            ×
          </button>
        ) : null}
        {showDrop ? (
          <div className="ph-search-drop" role="listbox" aria-label={zh ? '搜索结果' : 'Search results'}>
            {query.trim().length === 0 ? (
              <p className="ph-search-hint">{zh ? '输入名称即可。例如「打开音乐」「电脑控制」「办公助手」。' : 'Type a name, for example music player or computer control.'}</p>
            ) : hits.length === 0 ? (
              <p className="ph-search-hint">{zh ? '没有匹配。换功能名、模块名，或完整 stable key。' : 'No match. Try a card name, module, or stable key.'}</p>
            ) : hits.map(node => (
              <button
                key={node.id}
                type="button"
                role="option"
                onMouseDown={e => {
                  e.preventDefault()
                  onPick(node)
                }}
              >
                <span className="ph-search-type">{typeLabel(node.type, zh)}</span>
                <span>
                  <b>{node.name}</b>
                  <span className="ph-dim">{DOMAIN_META[domainKey(node)]?.zh ?? node.domain ?? node.stable_key}</span>
                </span>
              </button>
            ))}
          </div>
        ) : null}
      </div>
      <MenuSelect label={zh ? '能力域' : 'Domain'} ariaLabel={zh ? '全部域' : 'All domains'} value={domain} options={domainOptions} onChange={onDomain} onOpenChange={open => setFilterOpen(curr => open ? curr + 1 : Math.max(0, curr - 1))} />
      <MenuSelect label={zh ? '节点类型' : 'Type'} ariaLabel={zh ? '全部类型' : 'All types'} value={type} options={typeOptions} onChange={onType} onOpenChange={open => setFilterOpen(curr => open ? curr + 1 : Math.max(0, curr - 1))} />
    </div>
  )
}

function uniqueImpactChips(impacts: string[], nodes: HubNode[]): Array<{ key: string; label: string }> {
  const seen = new Set<string>()
  return impacts.flatMap(key => {
    const label = changeImpactLabel(key, nodes)
    if (seen.has(label)) return []
    seen.add(label)
    return [{ key, label }]
  })
}

export function ChangelogPanel({
  changes, nodes, edition, zh, onClose, onOpen, onPoster,
}: {
  changes: HubChange[]
  nodes: HubNode[]
  edition?: string
  zh: boolean
  onClose: () => void
  onOpen: (key: string) => void
  onPoster?: (key: string) => void
}): React.JSX.Element {
  const [filter, setFilter] = useState<'all' | 'added' | 'updated' | 'removed'>('all')
  const shown = changes.filter(item => filter === 'all' || item.kind === filter)
  const groups = groupChangesByPage(shown)
  const counts = {
    all: changes.length,
    added: changes.filter(item => item.kind === 'added').length,
    updated: changes.filter(item => item.kind === 'updated').length,
    removed: changes.filter(item => item.kind === 'removed').length,
  }
  return (
    <>
      <div className="ph-drawer-scrim" onClick={onClose} />
      <aside className="ph-sheet" role="dialog" aria-label={zh ? '变更时间线' : 'Changelog'}>
        <header className="ph-sheet-head">
          <div>
            <h2>{zh ? '本轮变更' : 'This snapshot'}</h2>
            <p className="ph-dim">{edition || 'v2.4.1'} · {zh ? `${changes.length} 条，对照上一份已验证快照` : `${changes.length} vs last verified snapshot`}</p>
          </div>
          <button type="button" onClick={onClose}>{zh ? '关闭' : 'Close'}</button>
        </header>
        <div className="ph-sheet-filters">
          {(['all', 'added', 'updated', 'removed'] as const).map(kind => (
            <button key={kind} type="button" className={filter === kind ? 'is-current' : ''} onClick={() => setFilter(kind)}>
              {kind === 'all' ? (zh ? '全部' : 'All') : changeKindLabel(kind, zh)}
              <span className="ph-badge">{counts[kind]}</span>
            </button>
          ))}
        </div>
        <div className="ph-sheet-body">
          {shown.length === 0 ? (
            <p className="ph-empty">{zh ? '这一轮没有该类变更。刷新快照后会出现新增、更新或弃用。' : 'No changes in this filter.'}</p>
          ) : (
            <>
              {groups.map(group => (
                <section key={group.page?.id ?? 'other'} className="ph-tl-group">
                  <h3>{group.page ? `${group.page.name} · ${group.page.nameEn}` : (zh ? '未挂页面' : 'Unmapped')}</h3>
                  <ol className="ph-timeline">
                    {group.items.map(item => {
                      const impacts = uniqueImpactChips(item.impacts ?? [], nodes)
                      return (
                        <li key={`${item.kind}-${item.stable_key}`} className={`ph-tl-item is-${item.kind}`}>
                          <span className={`ph-tl-kind kind-${item.kind}`}>{changeKindLabel(item.kind, zh)}</span>
                          <strong>{changeTitle(item, nodes)}</strong>
                          <p>{item.summary || (zh ? '功能卡或链路声明有更新。' : 'Card or chain declaration updated.')}</p>
                          <code>{item.stable_key}</code>
                          {impacts.length > 0 ? (
                            <div className="ph-tl-impacts">
                              <span className="ph-dim">{zh ? '影响到' : 'Impacts'}</span>
                              {impacts.map(chip => (
                                <span key={chip.key} className="ph-chip">{chip.label}</span>
                              ))}
                            </div>
                          ) : null}
                          {item.kind !== 'removed' ? (
                            <div className="ph-actions">
                              <button type="button" className="primary" onClick={() => onOpen(item.stable_key)}>{zh ? '查看功能卡' : 'Open card'}</button>
                              {onPoster ? <button type="button" onClick={() => onPoster(item.stable_key)}>{zh ? '导出海报' : 'Poster'}</button> : null}
                            </div>
                          ) : null}
                        </li>
                      )
                    })}
                  </ol>
                </section>
              ))}
            </>
          )}
          <p className="ph-dim ph-sheet-foot">{zh ? '升级或注册表轮询后自动重建。变更不计入健康分。' : 'Rebuilt after upgrade or registry poll. Changes are not part of the health score.'}</p>
        </div>
      </aside>
    </>
  )
}
