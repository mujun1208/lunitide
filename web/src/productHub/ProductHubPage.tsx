import React, { useEffect, useMemo, useState } from 'react'
import { getProductHubBridge } from '../bridge/client'
import { AnatomyPane } from './AnatomyPane'
import { ChainFlowView } from './ChainFlowView'
import { GraphBoard } from './GraphBoard'
import { ChangelogPanel, FinderBar } from './HubChrome'
import { PAGE_ATLAS, pagesOfCard, settingLabel } from './hubCatalog'
import { PageAtlas } from './PageAtlas'
import { LandscapePane } from './LandscapePane'
import {
  assetStats, DOMAIN_META, domainCards, domainKey, findingTone, formatSnapshot,
  healthTone, isFeatureLike, matchesQuery, moduleRows, parseFixSteps, probeLabel,
  reportId, searchHubNodes, tagKind, versionLabel, type HubModuleRow,
} from './hubModel'
import {
  hubUserError, isHubCard, readHubToken, writeHubToken,
  type HubCard, type HubChange, type HubEdge, type HubFinding, type HubNode, type HubOverview, type HubTab,
} from './productHubTypes'
import './productHub.css'

const TABS: Array<{ id: HubTab; zh: string; en: string }> = [
  { id: 'overview', zh: '功能全景', en: 'Panorama' },
  { id: 'graph', zh: '知识图谱', en: 'Graph' },
  { id: 'anatomy', zh: '解剖视图', en: 'Anatomy' },
  { id: 'diagnostics', zh: '诊断报告', en: 'Diagnostics' },
  { id: 'landscape', zh: '图景', en: 'Landscape' },
]

function UnlockForm({ onUnlocked, zh }: { onUnlocked: (token: string) => void; zh: boolean }): React.JSX.Element {
  const [username, setUsername] = useState('')
  const [password, setPassword] = useState('')
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  return (
    <section className="product-hub-unlock" aria-label={zh ? '管理员解锁' : 'Admin unlock'}>
      <h1>{zh ? '产品总览' : 'Product Hub'}</h1>
      <p className="meta">{zh ? '输入管理员用户名和密码后进入总览。进入后可以改密码。' : 'Enter the admin username and password to open the overview. You can change the password after signing in.'}</p>
      <form onSubmit={event => {
        event.preventDefault()
        setBusy(true)
        setError('')
        void getProductHubBridge().unlock({ username, password }).then(result => {
          writeHubToken(result.sessionToken)
          onUnlocked(result.sessionToken)
        }).catch(err => setError(hubUserError(err, zh ? '用户名或密码不正确' : 'Incorrect username or password'))).finally(() => setBusy(false))
      }}>
        <label>{zh ? '用户名' : 'Username'}<input autoComplete="username" value={username} onChange={e => setUsername(e.target.value)} /></label>
        <label>{zh ? '密码' : 'Password'}<input type="password" autoComplete="current-password" value={password} onChange={e => setPassword(e.target.value)} /></label>
        {error ? <p role="alert">{error}</p> : null}
        <button className="primary" type="submit" disabled={busy}>{zh ? '进入总览' : 'Enter'}</button>
      </form>
    </section>
  )
}

function PasswordForm({ token, zh }: { token: string; zh: boolean }): React.JSX.Element {
  const [open, setOpen] = useState(false)
  const [currentPassword, setCurrent] = useState('')
  const [newPassword, setNext] = useState('')
  const [msg, setMsg] = useState('')
  if (!open) return <button type="button" onClick={() => setOpen(true)}>{zh ? '改密码' : 'Change password'}</button>
  return (
    <form className="ph-actions" onSubmit={event => {
      event.preventDefault()
      void getProductHubBridge().changePassword({ sessionToken: token, currentPassword, newPassword }).then(() => {
        setMsg(zh ? '密码已更新' : 'Password updated')
        setCurrent('')
        setNext('')
        setOpen(false)
      }).catch(err => setMsg(hubUserError(err, zh ? '改密失败' : 'Could not change password')))
    }}>
      <input type="password" placeholder={zh ? '当前密码' : 'Current'} value={currentPassword} onChange={e => setCurrent(e.target.value)} />
      <input type="password" placeholder={zh ? '新密码' : 'New'} value={newPassword} onChange={e => setNext(e.target.value)} />
      <button className="primary" type="submit">{zh ? '保存新密码' : 'Save'}</button>
      <button type="button" onClick={() => setOpen(false)}>{zh ? '取消' : 'Cancel'}</button>
      {msg ? <p role="status">{msg}</p> : null}
    </form>
  )
}

function TagEditor({ token, card, zh, onTagged }: { token: string; card: HubCard; zh: boolean; onTagged: (card: HubCard) => void }): React.JSX.Element {
  const [vocab, setVocab] = useState('status')
  const [value, setValue] = useState('')
  const [msg, setMsg] = useState('')
  return (
    <form className="ph-actions" onSubmit={event => {
      event.preventDefault()
      if (!value.trim()) return
      void getProductHubBridge().tagSet({ sessionToken: token, stableKey: card.stable_key, vocab, value: value.trim() }).then(() => {
        const tag = `${vocab}:${value.trim()}`
        onTagged({ ...card, tags: [...new Set([...(card.tags ?? []), tag])] })
        setMsg(zh ? '标签已保存' : 'Tag saved')
        setValue('')
      }).catch(err => setMsg(hubUserError(err, zh ? '打标签失败' : 'Could not tag')))
    }}>
      <select value={vocab} onChange={e => setVocab(e.target.value)} aria-label={zh ? '标签词表' : 'Tag vocabulary'}>
        <option value="status">status</option>
        <option value="kind">kind</option>
        <option value="scenario">scenario</option>
        <option value="capability">capability</option>
        <option value="alias">alias</option>
      </select>
      <input value={value} onChange={e => setValue(e.target.value)} placeholder={zh ? '标签值，例如 核心' : 'Value'} />
      <button className="primary" type="submit">{zh ? '打标签' : 'Tag'}</button>
      {msg ? <p role="status">{msg}</p> : null}
    </form>
  )
}

function HealthRing({ score, label }: { score: number; label: string }): React.JSX.Element {
  const r = 24
  const c = 2 * Math.PI * r
  const dash = Math.max(0, Math.min(100, score)) / 100 * c
  return (
    <div className="ph-ring" aria-label={label}>
      <svg width="64" height="64" viewBox="0 0 64 64">
        <circle cx="32" cy="32" r={r} fill="none" stroke="rgba(156,156,176,0.18)" strokeWidth="6" />
        <circle cx="32" cy="32" r={r} fill="none" stroke={healthTone(score)} strokeWidth="6" strokeDasharray={`${dash} ${c - dash}`} strokeLinecap="round" />
      </svg>
      <b>{score}%</b>
    </div>
  )
}

function SectionTitle({ letter, zh, en }: { letter: string; zh: string; en: string }): React.JSX.Element {
  return (
    <div className="ph-sec-title">
      <b>{letter}</b>
      <h3>{zh}</h3>
      <span>{en}</span>
    </div>
  )
}

function FeatureCardPage({
  card, token, zh, crumb, onBack, onTagged, onExport, onChip,
}: {
  card: HubCard
  token: string
  zh: boolean
  crumb: string
  onBack: () => void
  onTagged: (card: HubCard) => void
  onExport: () => void
  onChip: (text: string) => void
}): React.JSX.Element {
  const attr = card.attributes ?? {}
  const scaffold = card.scaffold ?? {}
  const pages = pagesOfCard(card)
  const probe = card.probe
  const rows: Array<{ icon: string; label: string; values: string[] }> = [
    { icon: '⚙', label: zh ? '操作' : 'Ops', values: attr.operations ?? [] },
    { icon: '▣', label: zh ? '使用工具' : 'Tools', values: attr.tools ?? [] },
    { icon: '◈', label: '使用 MCP', values: attr.mcps ?? [] },
    { icon: '✦', label: zh ? '使用技能' : 'Skills', values: attr.skills ?? [] },
    { icon: '⚡', label: zh ? '依赖能力' : 'Capabilities', values: attr.capabilities ?? [] },
  ]
  return (
    <article className="ph-body">
      <div className="ph-crumb">
        <span>{crumb}</span>
        <div className="ph-actions">
          <button type="button" onClick={onBack}>{zh ? '返回' : 'Back'}</button>
          <button type="button" onClick={onExport}>{zh ? '导出本卡海报' : 'Export poster'}</button>
        </div>
      </div>
      <header className="ph-card-head">
        <h2>{card.name}<span className="ph-en">{card.name_en}</span></h2>
        <p>{card.stable_key} · {card.provenance} · {probe ? probeLabel(probe.passed, probe.total) : card.version}</p>
        <div className="ph-chips">
          {(card.tags ?? []).map(tag => (
            <button key={tag} type="button" className={`ph-chip ${tagKind(tag)}`} onClick={() => onChip(tag)}><i />{tag}</button>
          ))}
        </div>
      </header>
      <section className="ph-sec">
        <SectionTitle letter="A" zh={zh ? '简介' : 'Summary'} en="SUMMARY" />
        <p className="ph-quote">{card.summary || '—'}</p>
      </section>
      <section className="ph-sec">
        <SectionTitle letter="B" zh={zh ? '功能描述' : 'Description'} en="DESCRIPTION" />
        <p className="ph-desc">{card.description || card.summary || '—'}</p>
      </section>
      <section className="ph-sec">
        <SectionTitle letter="C" zh={zh ? '属性' : 'Attributes'} en="ATTRIBUTES" />
        <div className="ph-attr">
          {rows.map(row => (
            <div key={row.label} className="ph-attr-row">
              <span aria-hidden="true">{row.icon}</span>
              <span>{row.label}</span>
              <div className="ph-chips">
                {row.values.length ? row.values.map(value => (
                  <button key={value} type="button" className="ph-chip" onClick={() => onChip(value)}><i />{value}</button>
                )) : <span>—</span>}
              </div>
            </div>
          ))}
        </div>
      </section>
      <section className="ph-sec">
        <SectionTitle letter="D" zh={zh ? '方法' : 'Methods'} en="METHODS" />
        <div className="ph-methods">
          {(card.methods ?? []).map(method => (
            <article key={`${method.type}-${method.entry}`} className="ph-method">
              <strong>{method.type === 'voice' || method.type === '语音' ? `◎ ${zh ? '语音对话' : 'Voice'}` : method.type === 'type' || method.type === '打字' || method.type === 'ui' ? `▣ ${zh ? '打字对话' : 'Typed'}` : method.type}</strong>
              <p>{zh ? '入口' : 'Entry'}：{method.entry}</p>
              <p className="ph-dim">{zh ? '连续' : 'Follow-up'}：{method.continuous || '—'}</p>
            </article>
          ))}
        </div>
      </section>
      <section className="ph-sec">
        <SectionTitle letter="E" zh={zh ? '调用链路' : 'Call chain'} en="CALL CHAIN" />
        <ChainFlowView steps={card.chain?.steps ?? []} branches={card.chain?.branches ?? []} />
      </section>
      <section className="ph-sec">
        <SectionTitle letter="F" zh={zh ? '前台页面' : 'Pages'} en="PAGES" />
        {pages.length === 0 ? (
          <p className="ph-dim">{zh ? '这张卡还没挂到前台页面。' : 'This card is not bound to a frontend page.'}</p>
        ) : pages.map(id => {
          const spec = PAGE_ATLAS[id]
          return (
            <article key={id} className="ph-page-bind">
              <strong>{spec.name}<span className="ph-en">{spec.nameEn}</span></strong>
              <p className="ph-dim">{zh ? '入口' : 'Nav'}：{spec.nav}</p>
              <p className="ph-quote">{spec.analysis}</p>
            </article>
          )
        })}
      </section>
      <section className="ph-sec">
        <SectionTitle letter="G" zh={zh ? '原理 / 逻辑 / 技术' : 'Principle / logic / tech'} en="FOUNDATION" />
        <p className="ph-desc">{card.principle || '—'}</p>
        <p className="ph-desc">{card.logic || '—'}</p>
        <p className="ph-dim">{card.tech || '—'}</p>
        <p className="ph-dim">Bridge：{(scaffold.bridge ?? []).join(' · ') || '—'}</p>
        <p className="ph-dim">{zh ? '设置' : 'Settings'}：{(scaffold.settings ?? []).map(id => settingLabel(id, zh)).join(' · ') || '—'}</p>
        <TagEditor token={token} card={card} zh={zh} onTagged={onTagged} />
      </section>
      <section className="ph-sec">
        <SectionTitle letter="H" zh={zh ? '总结分析' : 'Analysis'} en="ANALYSIS" />
        <p className="ph-desc">{card.analysis || '—'}</p>
      </section>
    </article>
  )
}

function OverviewPane({
  overview, nodes, edges, findings, changes, zh, query, domainFilter, onOpen, onDomain, onDetail,
}: {
  overview?: HubOverview
  nodes: HubNode[]
  edges: HubEdge[]
  findings: HubFinding[]
  changes: HubChange[]
  zh: boolean
  query: string
  domainFilter?: string
  onOpen: (key: string) => void
  onDomain: (id: string) => void
  onDetail: () => void
}): React.JSX.Element {
  const rows = useMemo(() => moduleRows(nodes, edges), [nodes, edges])
  const domains = useMemo(() => domainCards(overview, nodes, edges, findings), [overview, nodes, edges, findings])
  const stats = useMemo(() => assetStats(nodes, overview), [nodes, overview])
  const [openMods, setOpenMods] = useState<string[] | null>(null)
  const [pageId, setPageId] = useState('home')
  const q = query.trim()
  const visibleRows = rows.filter(row => {
    if (domainFilter && row.domain !== domainFilter) return false
    if (!q) return true
    return row.features.some(node => matchesQuery(node, q)) || row.name.toLocaleLowerCase().includes(q.toLocaleLowerCase())
  })
  const expanded = openMods ?? []
  const score = overview?.healthScore ?? 0
  const openWarn = findings.filter(item => item.status === 'open' && (item.severity === 'error' || item.severity === 'warn')).length
  const timeouts = findings.filter(item => item.status === 'open' && /超时|timeout/i.test(`${item.title} ${item.evidence}`)).length
  const passed = Math.max(0, (overview?.cardCount ?? 0) - openWarn)
  return (
    <div>
      <section className="ph-health">
        <HealthRing score={score} label={zh ? '健康度' : 'Health'} />
        <div>
          <strong>{zh ? '探针' : 'Probes'} {passed}/{overview?.cardCount ?? 0} · {openWarn} {zh ? '警告' : 'warn'} · {timeouts} {zh ? '超时' : 'timeout'} · {zh ? '健康' : 'ok'} {domains.length} {zh ? '域' : 'domains'}</strong>
          <p>{zh ? '健康分忽略已解决项。点击详情看未闭合诊断。' : 'Resolved findings are ignored by the health score.'}</p>
        </div>
        <button type="button" className="ph-detail" onClick={onDetail}>{zh ? '详情' : 'Details'}</button>
      </section>
      <div className="ph-stats">
        {stats.map(stat => (
          <article key={stat.key} className="ph-stat">
            <b>
              {stat.n >= 1000 ? `${(stat.n / 1000).toFixed(1)}k` : stat.n}
              {stat.delta ? <em>+{stat.delta}</em> : null}
            </b>
            <span>{stat.label}</span>
          </article>
        ))}
      </div>
      <div className="ph-domains">
        {domains.map(domain => {
          const pct = Math.round(domain.passed / Math.max(1, domain.total) * 100)
          return (
            <button key={domain.id} type="button" className="ph-domain" onClick={() => onDomain(domain.id)}>
              <div className="ph-domain-top">
                <strong>{domain.name} <span className="ph-en">{domain.en}</span></strong>
                <span className="ph-dim">{domain.modules} {zh ? '模块' : 'mod'} · {domain.cards} {zh ? '功能卡' : 'cards'}</span>
              </div>
              <div className="ph-bar" aria-hidden="true">
                <i style={{ width: `${pct}%` }} className={pct < 80 ? 'warn' : undefined} />
              </div>
              <p className="ph-domain-mods">{domain.moduleNames.slice(0, 3).join(' · ') || '—'}{domain.moduleNames.length > 3 ? ` +${domain.moduleNames.length - 3}` : ''}</p>
            </button>
          )
        })}
      </div>
      <PageAtlas nodes={nodes} changes={changes} findings={findings} query={query} zh={zh} onOpen={onOpen} selectedId={pageId} onSelect={setPageId} />
      <div className="ph-section-head">
        <span>{zh ? '模块明细' : 'Modules'}</span>
        <span>{visibleRows.length} MODULES</span>
      </div>
      <div className="ph-modules">
        {visibleRows.map(row => (
          <ModuleBlock key={row.id} row={row} open={expanded.includes(row.id) || !!q} zh={zh} onToggle={() => setOpenMods(curr => {
            const base = curr ?? rows.map(item => item.id)
            return base.includes(row.id) ? base.filter(id => id !== row.id) : [...base, row.id]
          })} onOpen={onOpen} />
        ))}
      </div>
    </div>
  )
}

function ModuleBlock({
  row, open, zh, onToggle, onOpen,
}: {
  row: HubModuleRow
  open: boolean
  zh: boolean
  onToggle: () => void
  onOpen: (key: string) => void
}): React.JSX.Element {
  return (
    <div>
      <button type="button" className="ph-module" onClick={onToggle}>
        <b>{row.name}</b>
        <span>{row.shortKey}</span>
        <span>{row.summary || (zh ? '该模块功能卡编排中' : 'Cards pending')}</span>
        <span className="ph-count">{row.features.length} {zh ? '卡' : ''}</span>
        <i className={`ph-chevron${open ? ' is-open' : ''}`} aria-hidden="true" />
      </button>
      {open ? (
        <div className="ph-children">
          {row.features.length === 0 ? <p className="ph-dim">{zh ? '该模块功能卡编排中' : 'No cards yet'}</p> : row.features.map(node => (
            <button key={node.id} type="button" className="ph-feature-row" onClick={() => onOpen(node.stable_key)}>
              <span>{node.name}</span>
              <span>{node.stable_key}</span>
            </button>
          ))}
        </div>
      ) : null}
    </div>
  )
}


function DiagnosticsPane({
  findings, overview, zh, busy, onApply, onRefresh, onExport,
}: {
  findings: HubFinding[]
  overview?: HubOverview
  zh: boolean
  busy: boolean
  onApply: (item?: HubFinding) => void
  onRefresh: () => void
  onExport: () => void
}): React.JSX.Element {
  const [open, setOpen] = useState<string[] | null>(null)
  const [showFixed, setShowFixed] = useState(false)
  const active = findings.filter(item => item.status !== 'applied' && item.status !== 'fixed' && item.status !== 'resolved')
  const resolved = findings.filter(item => item.status === 'applied' || item.status === 'fixed' || item.status === 'resolved')
  const errors = active.filter(item => item.severity === 'error').length
  const warns = active.filter(item => item.severity === 'warn' || item.severity === 'warning').length
  const infos = active.filter(item => item.severity === 'info').length
  const [wont, setWont] = useState<string[]>([])
  return (
    <div>
      <section className="ph-health">
        <div>
          <strong>{zh ? '诊断报告' : 'Diagnostics'} {reportId(overview?.generatedAt)}</strong>
          <p>{zh ? '快照' : 'snap'} {formatSnapshot(overview?.generatedAt)} · {versionLabel(overview?.editionId)} · {zh ? '触发 upgrade · 信号源' : 'trigger upgrade · signals'} {active.length}/{findings.length}</p>
        </div>
        <HealthRing score={overview?.healthScore ?? 0} label={zh ? '诊断健康' : 'Diag health'} />
        <div className="ph-actions">
          <button type="button" onClick={onExport}>{zh ? '导出报告' : 'Export report'}</button>
          <button type="button" className="primary" disabled={busy} onClick={onRefresh}>{zh ? '重新检测' : 'Re-scan'}</button>
        </div>
      </section>
      <div className="ph-summary">
        <article className="ph-stat"><b className="is-err">{errors}</b><span>{zh ? '错误 ERROR' : 'Errors'}</span></article>
        <article className="ph-stat"><b className="is-warn">{warns}</b><span>{zh ? '警告 WARN' : 'Warnings'}</span></article>
        <article className="ph-stat"><b>{infos}</b><span>{zh ? '提示 INFO' : 'Info'}</span></article>
        <article className="ph-stat"><b className="is-ok">{resolved.length}</b><span>{zh ? '较上版已解决' : 'Resolved'}</span></article>
      </div>
      <div className="ph-loop">
        <span>v2.4.0 → {versionLabel(overview?.editionId)}</span>
        <span className="ph-dim">{zh ? '新增 2 · 解决 3 · 未解决' : 'added 2 · fixed 3 · open'} {active.length}</span>
        <span className="ph-dim">{zh ? '自检 → 修复 → 复核 → 升级' : 'scan → fix → verify → ship'}</span>
      </div>
      <div className="ph-actions">
        <button className="primary" type="button" disabled={busy} onClick={() => onApply()}>{zh ? '执行全部净化' : 'Apply all'}</button>
      </div>
      {active.map((item, index) => {
        const key = `${item.error_code}-${item.stable_key}-${index}`
        const expanded = open === null ? index === 0 : open.includes(key)
        const steps = parseFixSteps(item.fix)
        return (
          <article key={key} className={`ph-finding ${findingTone(item.severity)}`}>
            <button type="button" onClick={() => setOpen(curr => {
              const base = curr ?? [key]
              return base.includes(key) ? base.filter(id => id !== key) : [...base, key]
            })}>
              <span className="ph-dim">F-{String(index + 1).padStart(2, '0')}</span>
              <span className="ph-sev">{item.severity.toUpperCase()}</span>
              <strong>{item.error_code} {item.title}</strong>
              <span className="ph-dim">{item.stable_key}</span>
              <span className="ph-pill">{wont.includes(key) ? 'wont_fix' : item.status}</span>
            </button>
            {expanded ? (
              <div className="ph-finding-body">
                <SectionTitle letter="" zh={zh ? '证据' : 'Evidence'} en="EVIDENCE" />
                <pre className="ph-pre">{item.evidence || '—'}</pre>
                <SectionTitle letter="" zh={zh ? '根因' : 'Cause'} en="ROOT CAUSE" />
                <p className="ph-quote">{item.root_cause || '—'}</p>
                <SectionTitle letter="" zh={zh ? '改进方案' : 'Fix'} en="FIX" />
                <ol className="ph-fix-steps">
                  {steps.map((step, stepIndex) => (
                    <li key={`${step.action}-${stepIndex}`}>
                      {step.action !== 'note' ? <code>{step.action}</code> : null}
                      {step.target && step.target !== step.detail ? <code>{step.target}</code> : null}
                      {step.detail && step.detail !== step.target ? <span>{step.detail}</span> : null}
                    </li>
                  ))}
                </ol>
                {item.plan ? <p className="ph-dim">{item.plan}</p> : null}
                <SectionTitle letter="" zh={zh ? '验证方式' : 'Verify'} en="VERIFY" />
                <p className="ph-dim">{item.verify || '—'}</p>
                {item.skill_output ? <pre className="ph-pre">{item.skill_output}</pre> : null}
                <div className="ph-actions">
                  <button className="primary" type="button" disabled={busy} onClick={() => onApply(item)}>{zh ? '执行净化' : 'Apply'}</button>
                  <button type="button" onClick={() => {
                    const text = item.apply_prompt || [item.error_code, item.title, item.evidence, item.root_cause, item.fix, item.verify].join('\n')
                    void navigator.clipboard?.writeText(text)
                  }}>{zh ? '复制给内部模型 / 技能' : 'Copy for model / skill'}</button>
                  <button type="button" onClick={() => setWont(curr => curr.includes(key) ? curr.filter(id => id !== key) : [...curr, key])}>{zh ? '标记 wont_fix' : 'Mark wont_fix'}</button>
                </div>
              </div>
            ) : null}
          </article>
        )
      })}
      <p className="ph-dim">{zh ? '共' : ''} {findings.length} · {zh ? '严重待处理' : 'open'} {errors + warns} · {zh ? '其余' : 'rest'} {infos} {zh ? '提示可起票' : 'info'}</p>
      <button type="button" className="ph-module" onClick={() => setShowFixed(value => !value)}>
        <b className="is-ok">✓ {zh ? '已解决' : 'Resolved'} {resolved.length} resolved in {versionLabel(overview?.editionId)}</b>
      </button>
      {showFixed ? resolved.map(item => (
        <p key={`${item.error_code}-${item.stable_key}`} className="ph-dim">{item.error_code} · {item.title} · {item.status}</p>
      )) : null}
      <p className="ph-note">{zh ? '诊断器只读。fix_code 路径不改码。wont_fix 进人工留底。报告导出与页面同源。' : 'Diagnostics are read-only. Apply updates catalog and skill task books only.'}</p>
    </div>
  )
}

function CardPoster({ card, zh, onClose, besideDrawer }: { card: HubCard; zh: boolean; onClose: () => void; besideDrawer?: boolean }): React.JSX.Element {
  const steps = [...(card.chain?.steps ?? [])].toSorted((a, b) => a.index - b.index)
  return (
    <div className={`ph-poster-overlay${besideDrawer ? ' has-drawer' : ''}`} role="dialog" aria-label={zh ? '导出海报' : 'Poster'}>
      <div className="ph-poster">
        <header>
          <span>LUNITIDE · {zh ? '产品知识中枢' : 'Product Hub'}</span>
          <button type="button" onClick={onClose}>✕</button>
        </header>
        <h2>{card.name}<span className="ph-en">{card.name_en}</span></h2>
        <p className="ph-dim">{card.version} · {zh ? '快照' : 'snap'} {card.probe ? probeLabel(card.probe.passed, card.probe.total) : '—'}</p>
        <p className="ph-poster-flow">{steps.map(step => step.name).join(' → ') || '—'}</p>
        <p className="is-ok">✓ {zh ? '成功 · 窗口校验 · TTS 播报 · 入库' : 'Success path'}</p>
        <p className="is-warn">✗ {zh ? '失败 · 诊断 · 重试 · 降级推荐' : 'Fail / retry / fallback'}</p>
        <p><b>A</b> {card.summary}</p>
        <p><b>B</b> {card.description}</p>
        <p><b>C</b> {(card.attributes?.tools ?? []).join(' · ') || '—'}</p>
        <p><b>D</b> {(card.methods ?? []).map(method => method.entry).join(' / ')}</p>
        <p className="ph-dim">{zh ? '要点' : 'Points'} · {card.analysis || card.logic}</p>
        <footer>{new Date().toISOString().slice(0, 16).replace('T', ' ')} · Lunitide</footer>
      </div>
    </div>
  )
}

export function ProductHubPage({ onUnlocked, language = 'zh-CN' }: { onUnlocked?: (unlocked: boolean) => void; language?: 'zh-CN' | 'en' }): React.JSX.Element {
  const zh = language === 'zh-CN'
  const [token, setToken] = useState(readHubToken)
  const [tab, setTab] = useState<HubTab>('overview')
  const [card, setCard] = useState<HubCard>()
  const [overview, setOverview] = useState<HubOverview>()
  const [nodes, setNodes] = useState<HubNode[]>([])
  const [edges, setEdges] = useState<HubEdge[]>([])
  const [findings, setFindings] = useState<HubFinding[]>([])
  const [changes, setChanges] = useState<HubChange[]>([])
  const [reportHtml, setReportHtml] = useState('')
  const [reportMarkdown, setReportMarkdown] = useState('')
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  const [query, setQuery] = useState('')
  const [loading, setLoading] = useState(false)
  const [applyNote, setApplyNote] = useState('')
  const [drawer, setDrawer] = useState(false)
  const [exportOpen, setExportOpen] = useState(false)
  const [manualOpen, setManualOpen] = useState(false)
  const [focusDomain, setFocusDomain] = useState('')
  const [filterDomain, setFilterDomain] = useState('')
  const [filterType, setFilterType] = useState('')
  const [poster, setPoster] = useState<HubCard>()

  const load = (sessionToken: string) => {
    const api = getProductHubBridge()
    setLoading(true)
    return Promise.all([
      api.overview({ sessionToken }),
      api.graph({ sessionToken }),
      api.diagnostics({ sessionToken }),
      api.changelog({ sessionToken }),
    ]).then(([ov, graph, diag, log]) => {
      setOverview({ ...ov, domains: (ov.domains ?? []) as HubOverview['domains'] })
      setNodes((graph.nodes ?? []) as HubNode[])
      setEdges((graph.edges ?? []) as HubEdge[])
      setFindings((diag.findings ?? []) as HubFinding[])
      setReportHtml(diag.reportHtml)
      setReportMarkdown(diag.reportMarkdown)
      setChanges((log.changes ?? []) as HubChange[])
    }).finally(() => setLoading(false))
  }

  useEffect(() => {
    const existing = readHubToken()
    if (!existing) {
      onUnlocked?.(false)
      return
    }
    void getProductHubBridge().authStatus({ sessionToken: existing }).then(status => {
      if (!status.unlocked) {
        writeHubToken('')
        setToken('')
        onUnlocked?.(false)
        return
      }
      setToken(existing)
      onUnlocked?.(true)
      void load(existing).catch(err => setError(hubUserError(err, zh ? '知识中枢加载失败' : 'Failed to load hub')))
    }).catch(() => onUnlocked?.(false))
  }, [])

  useEffect(() => {
    const onKey = (event: KeyboardEvent) => {
      if (event.key !== 'Escape') return
      if (poster) { setPoster(undefined); return }
      if (drawer) { setDrawer(false); return }
      if (manualOpen) { setManualOpen(false); return }
      if (card) { setCard(undefined); return }
    }
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [drawer, manualOpen, card, poster])

  const searchHits = useMemo(() => searchHubNodes(nodes, query), [nodes, query])

  const pickHit = (node: HubNode) => {
    setQuery('')
    if (isFeatureLike(node)) {
      openCard(node.stable_key)
      return
    }
    if (node.type === 'Domain') {
      setFilterDomain(domainKey(node))
      setTab('graph')
      return
    }
    if (node.domain) setFilterDomain(domainKey(node))
    if (node.type === 'Expert' || node.type === 'Plugin') {
      setTab('anatomy')
      return
    }
    if (node.type !== 'Module') setFilterType(node.type)
    setTab(node.type === 'Module' ? 'overview' : 'graph')
  }

  const openCard = (stableKey: string) => {
    if (!token) return
    setDrawer(false)
    void getProductHubBridge().featureCard({ sessionToken: token, stableKey }).then(result => {
      if (isHubCard(result.card)) setCard(result.card)
    }).catch(err => setError(hubUserError(err, zh ? '打不开这张功能卡' : 'Could not open this card')))
  }

  const applyFinding = (item?: HubFinding) => {
    if (!token) return
    setBusy(true)
    setError('')
    setApplyNote('')
    void getProductHubBridge().apply({
      sessionToken: token,
      errorCode: item?.error_code,
      stableKey: item?.stable_key,
    }).then(result => {
      setApplyNote(`${result.status} · ${result.skillName || (zh ? '本地方案' : 'local plan')} · ${result.plan}`)
      return load(token)
    }).catch(err => setError(hubUserError(err, zh ? '净化失败' : 'Apply failed'))).finally(() => setBusy(false))
  }

  const generate = () => {
    if (!token) return
    setBusy(true)
    setError('')
    void getProductHubBridge().refresh({ sessionToken: token }).then(result => {
      setReportHtml(result.reportHtml)
      setReportMarkdown(result.reportMarkdown)
      return load(token)
    }).catch(err => setError(hubUserError(err, zh ? '生成失败，请稍后再试' : 'Generate failed'))).finally(() => setBusy(false))
  }

  const exportDoc = (format: 'html' | 'report') => {
    if (!token) return
    setExportOpen(false)
    void getProductHubBridge().exportDoc({ sessionToken: token, format }).then(result => {
      const blob = new Blob([result.content], { type: result.mime })
      const url = URL.createObjectURL(blob)
      const a = document.createElement('a')
      a.href = url
      a.download = format === 'html' ? 'lunitide-product-manual.html' : 'lunitide-product-manual.md'
      a.click()
      URL.revokeObjectURL(url)
    }).catch(err => setError(hubUserError(err, zh ? '导出失败' : 'Export failed')))
  }

  const openWarn = findings.filter(item => item.status === 'open' && (item.severity === 'error' || item.severity === 'warn')).length
  const crumb = card
    ? `${zh ? '产品总览' : 'Hub'} / ${pagesOfCard(card).map(id => PAGE_ATLAS[id].name).join(' · ') || DOMAIN_META[card.domain]?.zh || card.domain} / ${card.name}`
    : ''

  if (!token) return <div className="product-hub"><UnlockForm zh={zh} onUnlocked={next => { setToken(next); onUnlocked?.(true); void load(next).catch(err => setError(hubUserError(err, zh ? '知识中枢加载失败' : 'Failed to load hub'))) }} /></div>

  return (
    <div className="product-hub">
      <header className="ph-head">
        <div>
          <div className="ph-head-title">
            <span className="ph-mark" aria-hidden="true">◈</span>
            <h1>{zh ? '产品总览' : 'Product Hub'}</h1>
            <span className="ph-en">PRODUCT HUB</span>
          </div>
          <div className="ph-head-meta">
            <span>{versionLabel(overview?.editionId)}</span>
            <span>{zh ? '快照' : 'snap'} {formatSnapshot(overview?.generatedAt)}</span>
            <i className={`ph-dot${busy || loading ? ' is-busy' : ''}`} />
            <span>{busy || loading ? (zh ? '构建中' : 'building') : 'verified'}</span>
          </div>
        </div>
        <div className="ph-actions">
          <button type="button" onClick={() => setDrawer(true)}>
            {zh ? '变更' : 'Changes'}
            {changes.length > 0 ? <span className="ph-badge">{changes.length}</span> : null}
          </button>
          <button className="primary" type="button" disabled={busy} onClick={generate}>{zh ? '刷新' : 'Refresh'}</button>
          <div className="ph-export">
            <button type="button" onClick={() => setExportOpen(value => !value)}>{zh ? '导出' : 'Export'}</button>
            {exportOpen ? (
              <div className="ph-export-menu">
                <button type="button" onClick={() => { setExportOpen(false); setManualOpen(true) }}>{zh ? '查看说明书' : 'View manual'}</button>
                <button type="button" onClick={() => { setExportOpen(false); if (card) setPoster(card) }}>{zh ? '导出海报' : 'Export poster'}</button>
                <button type="button" onClick={() => exportDoc('html')}>{zh ? '导出 HTML' : 'Export HTML'}</button>
                <button type="button" onClick={() => exportDoc('report')}>{zh ? '导出 Markdown' : 'Export Markdown'}</button>
              </div>
            ) : null}
          </div>
          <PasswordForm token={token} zh={zh} />
        </div>
      </header>
      {!card ? (
        <nav className="ph-tabs" aria-label={zh ? '中枢分区' : 'Hub sections'}>
          {TABS.map(item => (
            <button key={item.id} type="button" className={tab === item.id ? 'is-current' : ''} onClick={() => setTab(item.id)}>
              {zh ? item.zh : item.en}
              {item.id === 'diagnostics' ? <span className="ph-badge">{openWarn > 0 ? openWarn : '✓'}</span> : null}
            </button>
          ))}
          <FinderBar
            query={query}
            onQuery={setQuery}
            hits={searchHits}
            onPick={pickHit}
            domain={filterDomain}
            type={filterType}
            onDomain={setFilterDomain}
            onType={setFilterType}
            nodes={nodes}
            zh={zh}
          />
        </nav>
      ) : null}
      {error ? <p className="ph-alert" role="alert">{error}</p> : null}
      {loading ? <p className="ph-dim" style={{ padding: '0 24px' }}>{zh ? '正在构建产品全景…' : 'Building the panorama…'}</p> : null}
      {applyNote ? <p role="status" className="ph-note">{applyNote}</p> : null}

      {card ? (
        <FeatureCardPage
          card={card}
          token={token}
          zh={zh}
          crumb={crumb}
          onBack={() => setCard(undefined)}
          onTagged={setCard}
          onExport={() => setPoster(card)}
          onChip={text => { setCard(undefined); setTab('graph'); setQuery(text) }}
        />
      ) : (
        <div className={`ph-body${tab === 'graph' ? ' is-wide' : ''}`}>
          {tab === 'overview' ? (
            <OverviewPane
              overview={overview}
              nodes={nodes}
              edges={edges}
              findings={findings}
              changes={changes}
              zh={zh}
              query={query}
              domainFilter={filterDomain}
              onOpen={openCard}
              onDomain={id => { setFocusDomain(id); setTab('graph') }}
              onDetail={() => setTab('diagnostics')}
            />
          ) : null}
          {tab === 'graph' ? <GraphBoard nodes={nodes} edges={edges} onOpen={openCard} zh={zh} focusDomain={focusDomain || filterDomain} domainFilter={filterDomain} typeFilter={filterType} /> : null}
          {tab === 'anatomy' ? <AnatomyPane nodes={nodes} edges={edges} zh={zh} onOpen={openCard} /> : null}
          {tab === 'diagnostics' ? (
            <DiagnosticsPane
              findings={findings}
              overview={overview}
              zh={zh}
              busy={busy}
              onApply={applyFinding}
              onRefresh={generate}
              onExport={() => exportDoc('report')}
            />
          ) : null}
          {tab === 'landscape' ? <LandscapePane nodes={nodes} zh={zh} onOpen={openCard} /> : null}
        </div>
      )}

      {drawer ? (
        <ChangelogPanel
          changes={changes}
          nodes={nodes}
          edition={versionLabel(overview?.editionId)}
          zh={zh}
          onClose={() => setDrawer(false)}
          onOpen={openCard}
          onPoster={key => {
            if (!token) return
            void getProductHubBridge().featureCard({ sessionToken: token, stableKey: key }).then(result => {
              if (isHubCard(result.card)) setPoster(result.card)
            }).catch(err => setError(hubUserError(err, zh ? '打不开这张功能卡' : 'Could not open this card')))
          }}
        />
      ) : null}
      {poster ? <CardPoster card={poster} zh={zh} besideDrawer={drawer} onClose={() => setPoster(undefined)} /> : null}
      {manualOpen ? (
        <>
          <div className="ph-drawer-scrim" onClick={() => setManualOpen(false)} />
          <aside className="ph-sheet" role="dialog" aria-label={zh ? '说明书' : 'Manual'}>
            <header className="ph-sheet-head">
              <div>
                <h2>{zh ? '说明书' : 'Manual'}</h2>
                <p className="ph-dim">{zh ? '由当前快照实时生成，不是仓库里的静态 PRD。' : 'Generated from this snapshot, not a static repo PRD.'}</p>
              </div>
              <button type="button" onClick={() => setManualOpen(false)}>{zh ? '关闭' : 'Close'}</button>
            </header>
            <div className="ph-sheet-body">
              {reportHtml ? <div className="product-hub-report" dangerouslySetInnerHTML={{ __html: reportHtml }} /> : <pre className="ph-pre">{reportMarkdown || (zh ? '先点「刷新」生成说明书。' : 'Refresh to generate the manual.')}</pre>}
            </div>
          </aside>
        </>
      ) : null}
    </div>
  )
}
