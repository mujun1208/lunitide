import React, { useCallback, useEffect, useState } from 'react'
import { getIdentityBridge, memoryOpsBridge, type IdentityBridge, type MemoryOpsBridge } from '../bridge/client'
import type { MemoryFactsListResult, MemoryGrowthListResult, MemoryStatsResult, MemoryTracesListResult } from '../generated/bridge'

// M10 记忆运营面板：统计条 + 事实库（置顶/隐藏标记）+ 召回记录 + 成长箱
// （转正/放弃）+ 记忆设置 + 导出与一键清除。一键清除是唯一的破坏性
// 操作，保留在显式二次确认对话框之后。

const PAGE_SIZE = 20

type FactItem = MemoryFactsListResult['items'][number]
type TraceItem = MemoryTracesListResult['items'][number]
type GrowthItem = MemoryGrowthListResult['items'][number]
type SettingsState = { memoryEnabled: boolean; autoNominate: boolean; growthDays: number }

const STATE_LABELS: Record<string, string> = { active: '生效中', superseded: '已替代', tombstoned: '已封存' }
const SENSITIVITY_LABELS: Record<string, string> = { public: '公开', private: '私有', sensitive: '敏感' }
const GROWTH_LABELS: Record<string, string> = { observing: '观察中', promoted: '已转正', dropped: '已放弃' }

const panelStyle: React.CSSProperties = { border: '1px solid var(--rule)', borderRadius: '16px', background: 'var(--bg2)', padding: '20px' }
const btnStyle: React.CSSProperties = { padding: '6px 12px', backgroundColor: 'var(--bg3)', color: 'var(--ink)', border: '1px solid var(--line)', borderRadius: '4px', cursor: 'pointer' }
const primaryBtnStyle: React.CSSProperties = { ...btnStyle, backgroundColor: '#2563eb', borderColor: '#3b82f6' }
const dangerBtnStyle: React.CSSProperties = { ...btnStyle, color: '#f87171', borderColor: '#7f1d1d' }
const chipStyle: React.CSSProperties = { padding: '2px 8px', borderRadius: '999px', fontSize: '11px', border: '1px solid var(--line)', color: 'var(--muted)' }

const StatCard = ({ label, value, hint }: { label: string; value: number | string; hint?: string }) => (
  <div style={{ flex: 1, minWidth: '130px', padding: '12px 14px', border: '1px solid var(--rule)', borderRadius: '10px', background: 'var(--bg3)' }}>
    <div style={{ fontSize: '22px', fontWeight: 700, color: 'var(--ink)' }}>{value}</div>
    <div style={{ fontSize: '12px', color: 'var(--muted)', marginTop: '2px' }}>{label}{hint ? ` · ${hint}` : ''}</div>
  </div>
)

function traceHitCount(hits: string): number {
  return traceItemCount(hits)
}

function traceItemCount(raw: string): number {
  try {
    const parsed: unknown = JSON.parse(raw)
    if (Array.isArray(parsed)) return parsed.length
    if (typeof parsed === 'number' && Number.isFinite(parsed)) return parsed
  } catch {
    /* non-JSON audit payloads fall through */
  }
  const n = Number(raw)
  return Number.isFinite(n) ? n : 0
}

export function MemoryOpsPanel({ ops = memoryOpsBridge, identity = getIdentityBridge(), subjectId }: { ops?: MemoryOpsBridge; identity?: IdentityBridge; subjectId?: string }): React.JSX.Element {
  const [resolvedSubject, setResolvedSubject] = useState(subjectId ?? '')
  const [stats, setStats] = useState<MemoryStatsResult>()
  const [facts, setFacts] = useState<FactItem[]>([])
  const [factsTotal, setFactsTotal] = useState(0)
  const [factsPage, setFactsPage] = useState(0)
  const [factsState, setFactsState] = useState<'' | 'active' | 'superseded' | 'tombstoned'>('')
  const [traces, setTraces] = useState<TraceItem[]>([])
  const [tracesTotal, setTracesTotal] = useState(0)
  const [tracesPage, setTracesPage] = useState(0)
  const [growth, setGrowth] = useState<GrowthItem[]>([])
  const [growthTotal, setGrowthTotal] = useState(0)
  const [growthPage, setGrowthPage] = useState(0)
  const [growthStatus, setGrowthStatus] = useState<'' | 'observing' | 'promoted' | 'dropped'>('observing')
  const [settings, setSettings] = useState<SettingsState>({ memoryEnabled: true, autoNominate: false, growthDays: 14 })
  const [error, setError] = useState<string>()
  const [busy, setBusy] = useState('')
  const [notice, setNotice] = useState('')

  const loadStats = useCallback(async () => {
    try { setStats(await ops.stats({})) } catch { /* stats 条在服务不可用时保持空 */ }
  }, [ops])

  const loadFacts = useCallback(async () => {
    setError(undefined)
    try {
      const r = await ops.listFacts({ ...(factsState ? { state: factsState } : {}), limit: PAGE_SIZE, offset: factsPage * PAGE_SIZE })
      setFacts(r.items); setFactsTotal(r.total)
    } catch (e) { setError(e instanceof Error ? e.message : '事实库加载失败') }
  }, [ops, factsState, factsPage])

  const loadTraces = useCallback(async () => {
    setError(undefined)
    try {
      const r = await ops.listTraces({ limit: PAGE_SIZE, offset: tracesPage * PAGE_SIZE })
      setTraces(r.items); setTracesTotal(r.total)
    } catch (e) { setError(e instanceof Error ? e.message : '召回记录加载失败') }
  }, [ops, tracesPage])

  const loadGrowth = useCallback(async () => {
    setError(undefined)
    try {
      const r = await ops.listGrowth({ ...(growthStatus ? { status: growthStatus } : {}), limit: PAGE_SIZE, offset: growthPage * PAGE_SIZE })
      setGrowth(r.items); setGrowthTotal(r.total)
    } catch (e) { setError(e instanceof Error ? e.message : '成长箱加载失败') }
  }, [ops, growthStatus, growthPage])

  useEffect(() => {
    if (subjectId) {
      setResolvedSubject(subjectId)
      return
    }
    let alive = true
    void identity.get().then(value => {
      if (alive && value.subjectId) setResolvedSubject(value.subjectId)
    }).catch(() => {
      if (alive) setResolvedSubject('local-user')
    })
    return () => { alive = false }
  }, [identity, subjectId])

  const loadSettings = useCallback(async () => {
    if (!resolvedSubject) return
    try {
      const r = await ops.getSettings({ subjectId: resolvedSubject })
      setSettings({ memoryEnabled: r.memoryEnabled, autoNominate: r.autoNominate, growthDays: r.growthDays })
    } catch { /* 缺省即默认档 */ }
  }, [ops, resolvedSubject])

  useEffect(() => { void loadStats() }, [loadStats])
  useEffect(() => { void loadFacts() }, [loadFacts])
  useEffect(() => { void loadTraces() }, [loadTraces])
  useEffect(() => { void loadGrowth() }, [loadGrowth])
  useEffect(() => { void loadSettings() }, [loadSettings])

  const flagFact = async (item: FactItem, flag: 'pinned' | 'hidden') => {
    if (busy) return
    setBusy(`${item.factId}:${flag}`); setError(undefined); setNotice('')
    try {
      const on = flag === 'pinned' ? !item.pinned : !item.hidden
      await ops.flagFact({ factId: item.factId, flag, on })
      await loadFacts(); await loadStats()
    } catch (e) { setError(e instanceof Error ? e.message : '标记失败') }
    finally { setBusy('') }
  }

  const decideGrowth = async (item: GrowthItem, decision: 'promoted' | 'dropped') => {
    if (busy) return
    setBusy(`${item.factId}:${decision}`); setError(undefined); setNotice('')
    try {
      await ops.decideGrowth({ factId: item.factId, decision })
      await loadGrowth(); await loadStats()
    } catch (e) { setError(e instanceof Error ? e.message : '成长箱决定失败') }
    finally { setBusy('') }
  }

  const saveSettings = async () => {
    if (busy) return
    setBusy('settings'); setError(undefined); setNotice('')
    try {
      if (!resolvedSubject) return
      await ops.updateSettings({ subjectId: resolvedSubject, ...settings })
      setNotice('记忆设置已保存')
    } catch (e) { setError(e instanceof Error ? e.message : '设置保存失败') }
    finally { setBusy('') }
  }

  const doExport = async () => {
    if (busy) return
    setBusy('export'); setError(undefined); setNotice('')
    try {
      const bundle = await ops.export({})
      const blob = new Blob([JSON.stringify({ exportedAt: new Date().toISOString(), ...bundle }, null, 2)], { type: 'application/json' })
      const url = URL.createObjectURL(blob)
      const a = document.createElement('a')
      a.href = url; a.download = `lunitide-memory-export-${Date.now()}.json`; a.click()
      URL.revokeObjectURL(url)
      setNotice('记忆数据已导出')
    } catch (e) { setError(e instanceof Error ? e.message : '导出失败') }
    finally { setBusy('') }
  }

  const doPurge = async () => {
    if (busy) return
    const first = window.confirm('一键清除将封存全部记忆事实，并删除候选、成长箱、标记、召回记录与四层记忆（设置保留）。此操作不可撤销，确定继续？')
    if (!first) return
    const second = window.confirm('再次确认：真的要清除全部记忆数据吗？')
    if (!second) return
    setBusy('purge'); setError(undefined); setNotice('')
    try {
      const counts = await ops.purge({})
      setNotice(`已清除：封存事实 ${counts.factsTombstoned} 条，候选 ${counts.candidates} 条，成长箱 ${counts.growthRows} 条，标记 ${counts.flags} 条，召回记录 ${counts.traces} 条，四层记忆 ${counts.memories} 条`)
      await Promise.all([loadStats(), loadFacts(), loadTraces(), loadGrowth()])
    } catch (e) { setError(e instanceof Error ? e.message : '清除失败') }
    finally { setBusy('') }
  }

  const factState = (stats?.factsByState ?? {}) as Record<string, number>
  const growthByStatus = (stats?.growthByStatus ?? {}) as Record<string, number>
  const activeFacts = factState.active ?? 0
  const observing = growthByStatus.observing ?? 0

  const Pager = ({ page, total, onPage, label }: { page: number; total: number; onPage: (p: number) => void; label: string }) => (
    <div style={{ display: 'flex', alignItems: 'center', gap: '8px', marginTop: '10px', fontSize: '12px', color: 'var(--muted)' }}>
      <span>{label}：共 {total} 条 · 第 {page + 1} / {Math.max(1, Math.ceil(total / PAGE_SIZE))} 页</span>
      <button style={btnStyle} disabled={page === 0} onClick={() => onPage(page - 1)} aria-label="上一页">上一页</button>
      <button style={btnStyle} disabled={(page + 1) * PAGE_SIZE >= total} onClick={() => onPage(page + 1)} aria-label="下一页">下一页</button>
    </div>
  )

  return (
    <div style={{ display: 'grid', gap: '18px' }}>
      {error && <div className="error" role="alert"><b>{error}</b></div>}
      {notice && <div role="status" style={{ padding: '8px 12px', border: '1px solid #14532d', borderRadius: '8px', background: 'rgba(52,211,153,0.08)', color: '#34d399', fontSize: '13px' }}>{notice}</div>}

      <section aria-label="记忆统计" style={{ ...panelStyle, padding: '16px 20px' }}>
        <h2 style={{ margin: '0 0 12px', fontSize: '16px' }}>记忆统计</h2>
        <div style={{ display: 'flex', gap: '10px', flexWrap: 'wrap' }}>
          <StatCard label="生效事实" value={activeFacts} />
          <StatCard label="待确认候选" value={((stats?.candidatesByState ?? {}) as Record<string, number>).pending ?? 0} />
          <StatCard label="成长箱观察" value={observing} />
          <StatCard label="召回记录" value={stats?.tracesTotal ?? 0} hint={`近7天 ${stats?.tracesLast7Days ?? 0}`} />
          <StatCard label="四层记忆" value={stats?.memoriesTotal ?? 0} />
        </div>
      </section>

      <section aria-label="事实库" style={panelStyle}>
        <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', gap: '10px', marginBottom: '12px', flexWrap: 'wrap' }}>
          <h2 style={{ margin: 0, fontSize: '16px' }}>事实库</h2>
          <select value={factsState} onChange={e => { setFactsState(e.target.value as typeof factsState); setFactsPage(0) }} aria-label="事实状态筛选" style={{ minWidth: '130px' }}>
            <option value="">全部状态</option>
            <option value="active">生效中</option>
            <option value="superseded">已替代</option>
            <option value="tombstoned">已封存</option>
          </select>
        </div>
        {facts.length === 0 ? <div className="empty"><b>暂无事实</b><span>确认沉淀的记忆事实会出现在这里。</span></div> : (
          <div style={{ display: 'grid', gap: '8px' }}>
            {facts.map(f => (
              <div key={f.factId} data-testid="memory-fact-row" style={{ display: 'flex', gap: '10px', alignItems: 'center', justifyContent: 'space-between', padding: '10px 12px', border: '1px solid var(--rule)', borderRadius: '8px', background: 'var(--bg3)', flexWrap: 'wrap' }}>
                <div style={{ display: 'flex', gap: '8px', alignItems: 'center', flexWrap: 'wrap', fontSize: '12px' }}>
                  <span style={{ fontFamily: 'monospace', color: 'var(--muted)' }}>{f.factId.slice(0, 10)}…</span>
                  <span style={chipStyle}>{STATE_LABELS[f.state] ?? f.state}</span>
                  <span style={chipStyle}>{SENSITIVITY_LABELS[f.sensitivity] ?? f.sensitivity}</span>
                  <span style={{ color: 'var(--muted)' }}>v{f.version} · {f.scopeId}</span>
                  {f.pinned && <span style={{ ...chipStyle, color: '#fbbf24', borderColor: '#78350f' }}>置顶</span>}
                  {f.hidden && <span style={{ ...chipStyle, color: '#f87171', borderColor: '#7f1d1d' }}>隐藏</span>}
                  {f.note && <span style={{ color: 'var(--muted)' }}>备注：{f.note}</span>}
                </div>
                <div style={{ display: 'flex', gap: '6px', flexShrink: 0 }}>
                  <button style={f.pinned ? primaryBtnStyle : btnStyle} disabled={busy !== ''} aria-pressed={f.pinned} onClick={() => void flagFact(f, 'pinned')}>{busy === `${f.factId}:pinned` ? '…' : f.pinned ? '取消置顶' : '置顶'}</button>
                  <button style={f.hidden ? dangerBtnStyle : btnStyle} disabled={busy !== ''} aria-pressed={f.hidden} onClick={() => void flagFact(f, 'hidden')}>{busy === `${f.factId}:hidden` ? '…' : f.hidden ? '取消隐藏' : '隐藏'}</button>
                </div>
              </div>
            ))}
          </div>
        )}
        <Pager page={factsPage} total={factsTotal} onPage={setFactsPage} label="事实" />
      </section>

      <section aria-label="成长箱" style={panelStyle}>
        <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', gap: '10px', marginBottom: '12px', flexWrap: 'wrap' }}>
          <h2 style={{ margin: 0, fontSize: '16px' }}>成长箱</h2>
          <select value={growthStatus} onChange={e => { setGrowthStatus(e.target.value as typeof growthStatus); setGrowthPage(0) }} aria-label="成长箱状态筛选" style={{ minWidth: '130px' }}>
            <option value="">全部状态</option>
            <option value="observing">观察中</option>
            <option value="promoted">已转正</option>
            <option value="dropped">已放弃</option>
          </select>
        </div>
        <p style={{ margin: '0 0 10px', color: 'var(--muted)', fontSize: '12px' }}>新沉淀的事实先进入观察期，到期后由你决定转正为长期记忆或放弃。</p>
        {growth.length === 0 ? <div className="empty"><b>暂无成长箱条目</b><span>观察期内的事实会出现在这里等待复盘。</span></div> : (
          <div style={{ display: 'grid', gap: '8px' }}>
            {growth.map(g => (
              <div key={g.factId} data-testid="memory-growth-row" style={{ display: 'flex', gap: '10px', alignItems: 'center', justifyContent: 'space-between', padding: '10px 12px', border: '1px solid var(--rule)', borderRadius: '8px', background: 'var(--bg3)', flexWrap: 'wrap' }}>
                <div style={{ display: 'flex', gap: '8px', alignItems: 'center', flexWrap: 'wrap', fontSize: '12px' }}>
                  <span style={{ fontFamily: 'monospace', color: 'var(--muted)' }}>{g.factId.slice(0, 10)}…</span>
                  <span style={chipStyle}>{GROWTH_LABELS[g.status] ?? g.status}</span>
                  <span style={{ color: 'var(--muted)' }}>引用 {g.referenceCount} 次 · 复查 {new Date(g.reviewAt).toLocaleDateString()} · {g.scopeId}</span>
                </div>
                {g.status === 'observing' && (
                  <div style={{ display: 'flex', gap: '6px', flexShrink: 0 }}>
                    <button style={primaryBtnStyle} disabled={busy !== ''} onClick={() => void decideGrowth(g, 'promoted')}>{busy === `${g.factId}:promoted` ? '…' : '转正'}</button>
                    <button style={dangerBtnStyle} disabled={busy !== ''} onClick={() => void decideGrowth(g, 'dropped')}>{busy === `${g.factId}:dropped` ? '…' : '放弃'}</button>
                  </div>
                )}
              </div>
            ))}
          </div>
        )}
        <Pager page={growthPage} total={growthTotal} onPage={setGrowthPage} label="成长箱" />
      </section>

      <section aria-label="召回记录" style={panelStyle}>
        <h2 style={{ margin: '0 0 6px', fontSize: '16px' }}>召回记录</h2>
        <p style={{ margin: '0 0 10px', color: 'var(--muted)', fontSize: '12px' }}>每次对话开始会按本轮问题自动召回已确认事实；未确认候选不会进入系统指令。命中条数来自审计轨迹。</p>
        {traces.length === 0 ? <div className="empty"><b>暂无召回记录</b><span>开启「启用记忆沉淀」后，对话中的自动召回会出现在这里。</span></div> : (
          <div style={{ display: 'grid', gap: '8px' }}>
            {traces.map(t => (
              <div key={t.id} data-testid="memory-trace-row" style={{ padding: '10px 12px', border: '1px solid var(--rule)', borderRadius: '8px', background: 'var(--bg3)', fontSize: '12px' }}>
                <div style={{ display: 'flex', justifyContent: 'space-between', gap: '8px', flexWrap: 'wrap' }}>
                  <span style={{ fontFamily: 'monospace', color: 'var(--muted)', overflowWrap: 'anywhere' }}>{t.queryDigest}</span>
                  <span style={{ color: 'var(--muted)', flexShrink: 0 }}>{new Date(t.createdAt).toLocaleString()}</span>
                </div>
                <div style={{ marginTop: '6px', color: 'var(--muted)', overflowWrap: 'anywhere' }}>
                  注入命中 {traceHitCount(t.hits)} 条 · 理由 {traceItemCount(t.reasons)} · 脱敏 {traceItemCount(t.redactions)}
                </div>
              </div>
            ))}
          </div>
        )}
        <Pager page={tracesPage} total={tracesTotal} onPage={setTracesPage} label="召回记录" />
      </section>

      <section aria-label="记忆设置与数据" style={panelStyle}>
        <h2 style={{ margin: '0 0 12px', fontSize: '16px' }}>记忆设置与数据</h2>
        <div style={{ display: 'grid', gap: '12px', gridTemplateColumns: 'repeat(auto-fit, minmax(240px, 1fr))' }}>
          <label style={{ display: 'flex', gap: '8px', alignItems: 'center', fontSize: '13px', cursor: 'pointer' }}>
            <input type="checkbox" checked={settings.memoryEnabled} onChange={e => setSettings(s => ({ ...s, memoryEnabled: e.target.checked }))} />
            启用记忆沉淀
          </label>
          <label style={{ display: 'flex', gap: '8px', alignItems: 'center', fontSize: '13px', cursor: 'pointer' }}>
            <input type="checkbox" checked={settings.autoNominate} onChange={e => setSettings(s => ({ ...s, autoNominate: e.target.checked }))} />
            自动提名候选
          </label>
          <label style={{ display: 'flex', gap: '8px', alignItems: 'center', fontSize: '13px' }}>
            成长观察期（天）
            <input type="number" min={1} max={90} value={settings.growthDays} onChange={e => setSettings(s => ({ ...s, growthDays: Math.min(90, Math.max(1, Number(e.target.value) || 1)) }))} style={{ width: '70px', padding: '4px 6px', backgroundColor: 'var(--bg)', color: 'var(--ink)', border: '1px solid var(--line)', borderRadius: '4px' }} />
          </label>
        </div>
        <p style={{ margin: '12px 0 0', color: 'var(--muted)', fontSize: '12px' }}>关闭「启用记忆沉淀」后，对话不再自动召回项目记忆，也不会自动提名；你已确认的偏好仍会写入系统指令。「自动提名候选」只把普通回合要点放进确认台。你说「记住 / 以后 / 默认用」这类声明式偏好时，即使关掉自动提名也会进确认台，不会自动升格为事实。</p>
        <div style={{ marginTop: '16px', display: 'flex', gap: '8px', flexWrap: 'wrap' }}>
          <button style={primaryBtnStyle} disabled={busy !== ''} onClick={() => void saveSettings()}>{busy === 'settings' ? '保存中…' : '保存设置'}</button>
          <button style={btnStyle} disabled={busy !== ''} onClick={() => void doExport()}>{busy === 'export' ? '导出中…' : '导出记忆数据'}</button>
          <button style={dangerBtnStyle} disabled={busy !== ''} onClick={() => void doPurge()}>{busy === 'purge' ? '清除中…' : '一键清除全部记忆'}</button>
        </div>
      </section>
    </div>
  )
}
