import React, { useEffect, useState, useCallback } from 'react'
import { feedbackBridge, memoryBridge, nominationBridge, type FeedbackBridge, type MemoryBridge, type NominationBridge } from '../bridge/client'
import type { MemoryDTO, MemoryLayer, MemoryScope, MemoryNominationListResult } from '../generated/bridge'
import { MemoryOpsPanel } from './MemoryOpsPanel'

type PendingCandidate = { candidateId: string; content: string; scopeId: string; confirmationToken: string; createdAt: string; expiresAt: string }
type NominationItem = MemoryNominationListResult['items'][number]

type MemoryTab = 'overview' | 'inbox' | 'history' | 'ops'

const TAB_LABELS: Record<MemoryTab, string> = { overview: '记忆中心', inbox: '偏好与提名', history: '处理历史', ops: '记忆运营' }
const NOM_STATE_LABELS: Record<string, string> = { nominated: '待处理', decided: '已处理', withdrawn: '已撤回' }

const SCOPE_LABELS: Record<MemoryScope, string> = { workspace: '工作区', project: '项目', session: '会话' }
const SCOPE_OPTIONS: MemoryScope[] = ['workspace', 'project', 'session']

// 四层记忆（对齐设计原型 05 · 记忆中心）：图标、中文名与后端 layer 的映射
const LAYERS: Array<{ key: MemoryLayer; icon: string; name: string; tag: string; grad: string }> = [
  { key: 'working', icon: 'W', name: '当前任务', tag: 'run 临时状态', grad: 'linear-gradient(135deg,var(--tide1),var(--glow))' },
  { key: 'episodic', icon: 'S', name: '会话记忆', tag: '会话摘要', grad: 'linear-gradient(135deg,var(--glow),var(--tide2))' },
  { key: 'procedural', icon: 'L', name: '长期记忆', tag: '已确认事实', grad: 'linear-gradient(135deg,var(--tide3),var(--tide1))' },
  { key: 'semantic', icon: 'Σ', name: '项目知识', tag: '结构化知识', grad: 'linear-gradient(135deg,var(--tide2),var(--glow2))' },
]
const layerMeta = (key: MemoryLayer) => LAYERS.find(l => l.key === key)!
const LAYER_ORDER: MemoryLayer[] = LAYERS.map(l => l.key)

const inputStyle: React.CSSProperties = { width: '100%', padding: '6px 8px', backgroundColor: '#0a0e1a', color: '#e5e7eb', border: '1px solid #334155', borderRadius: '4px', boxSizing: 'border-box' }
const btnStyle: React.CSSProperties = { padding: '6px 12px', backgroundColor: '#1e293b', color: '#e5e7eb', border: '1px solid #334155', borderRadius: '4px', cursor: 'pointer' }
const primaryBtnStyle: React.CSSProperties = { ...btnStyle, backgroundColor: '#2563eb', borderColor: '#3b82f6' }
const ttlText = (m: MemoryDTO) => m.expiresAt ? `TTL: ${new Date(m.expiresAt).toLocaleDateString()}` : 'TTL: 长期'

export function MemoryPage({ projectId, bridge = memoryBridge, feedback = feedbackBridge, nominations = nominationBridge }: { projectId: string; bridge?: MemoryBridge; feedback?: FeedbackBridge; nominations?: NominationBridge }): React.JSX.Element {
  const [tab, setTab] = useState<MemoryTab>('overview')
  const [memories, setMemories] = useState<MemoryDTO[]>([])
  const [selected, setSelected] = useState<MemoryDTO | null>(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string>()
  const [busy, setBusy] = useState(false)
  const [searchQuery, setSearchQuery] = useState('')
  const [layerTab, setLayerTab] = useState<MemoryLayer | ''>('')
  const [editContent, setEditContent] = useState<string | null>(null)
  const [pending, setPending] = useState<PendingCandidate[]>([])
  const [pendingBusy, setPendingBusy] = useState('')
  const [inbox, setInbox] = useState<NominationItem[]>([])
  const [history, setHistory] = useState<NominationItem[]>([])
  const [nomBusy, setNomBusy] = useState('')

  const [showCreate, setShowCreate] = useState(false)
  const [newLayer, setNewLayer] = useState<MemoryLayer>('working')
  const [newScope, setNewScope] = useState<MemoryScope>('project')
  const [newKey, setNewKey] = useState('')
  const [newContent, setNewContent] = useState('')

  const load = useCallback(async () => {
    if (!projectId) { setLoading(false); return }
    setLoading(true); setError(undefined)
    try {
      const r = await bridge.list({ projectId })
      setMemories(r.items)
    } catch (e) { setError(e instanceof Error ? e.message : '加载失败') }
    finally { setLoading(false) }
  }, [projectId, bridge])

  useEffect(() => { load() }, [load])

  const doSearch = async () => {
    if (!projectId || !searchQuery.trim()) return
    setLoading(true); setError(undefined)
    try { const r = await bridge.search({ projectId, query: searchQuery.trim() }); setMemories(r.items) }
    catch (e) { setError(e instanceof Error ? e.message : '搜索失败') }
    finally { setLoading(false) }
  }

  const doCreate = async (e: React.FormEvent) => {
    e.preventDefault()
    if (!projectId || !newKey.trim() || !newContent.trim()) return
    setBusy(true); setError(undefined)
    try {
      await bridge.create({ projectId, layer: newLayer, scope: newScope, key: newKey.trim(), content: newContent })
      setNewKey(''); setNewContent(''); setShowCreate(false)
      await load()
    } catch (e) { setError(e instanceof Error ? e.message : '创建失败') }
    finally { setBusy(false) }
  }

  const doUpdate = async (id: string) => {
    if (editContent === null) return
    setBusy(true); setError(undefined)
    try { await bridge.update({ id, content: editContent }); setEditContent(null); await load() }
    catch (e) { setError(e instanceof Error ? e.message : '更新失败') }
    finally { setBusy(false) }
  }

  const doDelete = async (id: string) => {
    setBusy(true); setError(undefined)
    try { await bridge.delete({ id }); if (selected?.id === id) setSelected(null); await load() }
    catch (e) { setError(e instanceof Error ? e.message : '删除失败') }
    finally { setBusy(false) }
  }

  const loadPending = useCallback(async () => {
    try {
      const r = await feedback.candidates({ limit: 50 })
      setPending(r.items)
    } catch { setPending([]) }
  }, [feedback])

  useEffect(() => { void loadPending() }, [loadPending])

  const decideCandidate = async (item: PendingCandidate, action: 'confirm' | 'reject') => {
    if (!bridge.confirmCandidate || pendingBusy) return
    setPendingBusy(item.candidateId); setError(undefined)
    try {
      await bridge.confirmCandidate({ candidateId: item.candidateId, confirmationToken: item.confirmationToken, action, requestId: `ui-${Date.now()}` })
      setPending(values => values.filter(v => v.candidateId !== item.candidateId))
    } catch (e) { setError(e instanceof Error ? e.message : '偏好确认失败') }
    finally { setPendingBusy('') }
  }

  const loadNominations = useCallback(async () => {
    try {
      const [nominated, decided, withdrawn] = await Promise.all([
        nominations.list({ state: 'nominated', limit: 50 }),
        nominations.list({ state: 'decided', limit: 50 }),
        nominations.list({ state: 'withdrawn', limit: 50 }),
      ])
      setInbox(nominated.items)
      setHistory([...decided.items, ...withdrawn.items].sort((a, b) => b.createdAt.localeCompare(a.createdAt)))
    } catch { setInbox([]); setHistory([]) }
  }, [nominations])

  useEffect(() => { void loadNominations() }, [loadNominations])

  const decideNomination = async (item: NominationItem, action: 'confirm' | 'reject') => {
    if (!bridge.confirmCandidate || nomBusy) return
    setNomBusy(item.nominationId); setError(undefined)
    try {
      await bridge.confirmCandidate({ candidateId: item.candidateId, confirmationToken: item.confirmationToken, action, requestId: `ui-${Date.now()}` })
      await loadNominations()
    } catch (e) { setError(e instanceof Error ? e.message : '提名处理失败') }
    finally { setNomBusy('') }
  }

  const withdrawNomination = async (item: NominationItem) => {
    if (nomBusy) return
    setNomBusy(item.nominationId); setError(undefined)
    try {
      await nominations.withdraw({ nominationId: item.nominationId })
      await loadNominations()
    } catch (e) { setError(e instanceof Error ? e.message : '撤回提名失败') }
    finally { setNomBusy('') }
  }

  if (!projectId) {
    return <div className="shell"><div className="empty"><b>请先选择项目</b><span>在项目总览中选择一个项目后即可管理记忆。</span></div></div>
  }

  const panelStyle: React.CSSProperties = { border: '1px solid #1f2937', borderRadius: '16px', background: '#0e1c30', padding: '20px' }

  const grouped = memories.reduce<Record<string, MemoryDTO[]>>((acc, m) => {
    (acc[m.layer] ??= []).push(m); return acc
  }, {})
  const visibleLayers = layerTab ? LAYERS.filter(l => l.key === layerTab) : LAYERS
  const layerCount = (key: MemoryLayer) => grouped[key]?.length ?? 0

  return (
    <div className="memory-center">
      <header className="expert-view-head">
        <div><div className="view-title">记忆中心</div><div className="view-meta">🔒 仅当前项目 · 跨项目泄漏率为 0 · 模型推断不会自动升级为已确认事实</div></div>
        <div className="view-actions">
          <button type="button" className="ui-btn" onClick={() => void load()} disabled={loading}>↻ 刷新</button>
          <button type="button" className="ui-btn primary" onClick={() => setShowCreate(v => !v)}>＋ 添加记忆</button>
        </div>
      </header>
      <div className="memory-tabs" role="tablist" aria-label="记忆面板分区">
        {(Object.keys(TAB_LABELS) as MemoryTab[]).map(key => (
          <button key={key} type="button" role="tab" className={`memory-tab ${tab === key ? 'on' : ''}`} aria-selected={tab === key} onClick={() => setTab(key)}>
            {TAB_LABELS[key]}{key === 'inbox' && (inbox.length + pending.length) > 0 ? ` · ${inbox.length + pending.length}` : ''}
          </button>
        ))}
      </div>
      {error && <div className="error" role="alert"><b>{error}</b></div>}
      {tab === 'overview' && (<>
      <div className="memory-toolbar">
        <input type="search" aria-label="搜索记忆" placeholder="搜索记忆内容、键名…" value={searchQuery} onChange={e => setSearchQuery(e.target.value)} onKeyDown={e => { if (e.key === 'Enter') void doSearch() }}/>
        <div className="memory-tabs">
          <button type="button" className={`memory-tab ${layerTab === '' ? 'on' : ''}`} onClick={() => { setLayerTab(''); void load() }}>全部</button>
          {LAYERS.map(l => <button key={l.key} type="button" className={`memory-tab ${layerTab === l.key ? 'on' : ''}`} onClick={() => setLayerTab(l.key)}>{l.name}</button>)}
        </div>
        <button className="ui-btn" onClick={() => void doSearch()} disabled={loading}>搜索</button>
      </div>
      {showCreate && (
        <form onSubmit={e => void doCreate(e)} className="mem-create">
          <label>层级
            <select style={inputStyle} value={newLayer} onChange={e => setNewLayer(e.target.value as MemoryLayer)} aria-label="层级">
              {LAYER_ORDER.map(l => <option key={l} value={l}>{layerMeta(l).name}</option>)}
            </select>
          </label>
          <label>作用域
            <select style={inputStyle} value={newScope} onChange={e => setNewScope(e.target.value as MemoryScope)} aria-label="作用域">
              {SCOPE_OPTIONS.map(s => <option key={s} value={s}>{SCOPE_LABELS[s]}</option>)}
            </select>
          </label>
          <label>键名
            <input style={inputStyle} value={newKey} onChange={e => setNewKey(e.target.value)} aria-label="键名" placeholder="输入记忆键名" />
          </label>
          <label className="wide">内容
            <textarea style={{ ...inputStyle, resize: 'vertical', minHeight: '64px' }} value={newContent} onChange={e => setNewContent(e.target.value)} aria-label="内容" placeholder="输入记忆内容" />
          </label>
          <div className="wide" style={{ display: 'flex', gap: '8px' }}>
            <button type="submit" style={primaryBtnStyle} disabled={busy || !newKey.trim() || !newContent.trim()}>{busy ? '创建中…' : '创建记忆'}</button>
            <button type="button" style={btnStyle} onClick={() => setShowCreate(false)}>取消</button>
          </div>
        </form>
      )}
      {selected && (
        <section className="mem-detail" aria-label="记忆详情">
          <div className="mem-detail-head">
            <div>
              <b>{selected.key}</b>
              <span className="me-meta" style={{ marginTop: 4 }}>
                <span>{layerMeta(selected.layer).name}</span><span>{SCOPE_LABELS[selected.scope]}</span>
                <span className="conf">conf {selected.confidence.toFixed(2)}</span><span>访问 {selected.accessCount} 次</span>
              </span>
            </div>
            <button type="button" className="ui-btn" onClick={() => { setSelected(null); setEditContent(null) }} aria-label="关闭详情">×</button>
          </div>
          {editContent !== null ? (
            <div style={{ display: 'grid', gap: 10 }}>
              <textarea value={editContent} onChange={e => setEditContent(e.target.value)} rows={4} style={{ width: '100%', resize: 'vertical' }} aria-label="编辑记忆内容"/>
              <div style={{ display: 'flex', gap: 8 }}>
                <button className="ui-btn primary" disabled={busy} onClick={() => void doUpdate(selected.id)}>保存修改</button>
                <button className="ui-btn" disabled={busy} onClick={() => setEditContent(null)}>取消</button>
              </div>
            </div>
          ) : (
            <>
              <div className="me-content" style={{ whiteSpace: 'pre-wrap', overflowWrap: 'anywhere' }}>{selected.content}</div>
              <div className="me-meta" style={{ margin: '8px 0' }}>
                <span>创建: {new Date(selected.createdAt).toLocaleDateString()}</span>
                <span>更新: {new Date(selected.updatedAt).toLocaleDateString()}</span>
                <span>{ttlText(selected)}</span>
                {selected.lastAccessed && <span>最后访问: {new Date(selected.lastAccessed).toLocaleDateString()}</span>}
              </div>
              <div className="appr-actions">
                <button className="ui-btn" disabled={busy} onClick={() => setEditContent(selected.content)}>编辑</button>
                <button className="ui-btn danger" disabled={busy} onClick={() => void doDelete(selected.id)}>删除</button>
              </div>
            </>
          )}
        </section>
      )}
      {loading ? <p role="status">正在载入记忆…</p> : (
      <div className={`mem-grid ${layerTab ? 'single' : ''}`}>
        {visibleLayers.map(l => (
          <div className="mem-layer" key={l.key}>
            <div className="ml-head">
              <div className="ml-ic" style={{ background: l.grad }}>{l.icon}</div>
              <div className="ml-name">{l.name}</div>
              <span className="ml-tag">{l.tag} · {layerCount(l.key)} 条</span>
            </div>
            {layerCount(l.key) === 0 ? <p className="mem-empty">暂无{ l.name }条目</p> : grouped[l.key]!.map(m => (
              <button type="button" className={`mem-entry ${selected?.id === m.id ? 'on' : ''}`} key={m.id} onClick={() => { setSelected(m); setEditContent(null) }}>
                <div className="me-content">{m.content}</div>
                <div className="me-meta"><span>{m.key}</span><span>{SCOPE_LABELS[m.scope]}</span><span className="conf">conf {m.confidence.toFixed(2)}</span><span>{ttlText(m)}</span></div>
              </button>
            ))}
          </div>
        ))}
      </div>
      )}
      <div className="callout"><b>管理方式</b>：选择记忆后可查看来源、修订、编辑或删除；偏好候选与助手提名需人工确认后才会沉淀为长期记忆。召回策略可在「记忆运营」中调整。</div>
      </>)}
      {tab === 'inbox' && (
        <section aria-label="偏好与提名" style={panelStyle}>
          {pending.length > 0 && (<>
            <h2 style={{ margin: '0 0 6px', fontSize: '15px' }}>偏好确认（{pending.length}）</h2>
            <p style={{ margin: '0 0 10px', color: '#8fa3bf', fontSize: '12px' }}>来自会话反馈的偏好候选。仅在你显式确认后才会沉淀为长期偏好并注入后续对话。</p>
            <div style={{ display: 'grid', gap: '8px', marginBottom: '18px' }}>
              {pending.map(item => (
                <div key={item.candidateId} style={{ display: 'flex', gap: '10px', alignItems: 'center', justifyContent: 'space-between', padding: '10px', border: '1px solid #1f2937', borderRadius: '8px', background: '#111827' }}>
                  <span style={{ flex: 1, fontSize: '13px', overflowWrap: 'anywhere' }}>{item.content}</span>
                  <span style={{ display: 'flex', gap: '6px', flexShrink: 0 }}>
                    <button style={primaryBtnStyle} disabled={pendingBusy === item.candidateId} onClick={() => void decideCandidate(item, 'confirm')}>{pendingBusy === item.candidateId ? '处理中…' : '确认沉淀'}</button>
                    <button style={btnStyle} disabled={pendingBusy === item.candidateId} onClick={() => void decideCandidate(item, 'reject')}>拒绝</button>
                  </span>
                </div>
              ))}
            </div>
          </>)}
          <h2 style={{ margin: '0 0 6px', fontSize: '15px' }}>提名收件箱</h2>
          <p style={{ margin: '0 0 14px', color: '#8fa3bf', fontSize: '12px' }}>助手从会话中提名的记忆候选。确认后沉淀为长期记忆，处理与撤回都会记入历史。</p>
          {inbox.length === 0 ? <div className="empty"><b>暂无待处理提名</b><span>新的提名会出现在这里等待你的确认。</span></div> : (
            <div style={{ display: 'grid', gap: '10px' }}>
              {inbox.map(item => (
                <div key={item.nominationId} style={{ padding: '14px', border: '1px solid #1f2937', borderRadius: '10px', background: '#111827' }}>
                  <div style={{ fontSize: '14px', overflowWrap: 'anywhere', marginBottom: '8px' }}>{item.content}</div>
                  <div style={{ display: 'flex', gap: '8px', flexWrap: 'wrap', alignItems: 'center', fontSize: '12px', color: '#8fa3bf', marginBottom: '10px' }}>
                    <span>提名理由：{item.reason}</span>
                    <span>· {item.nominator}</span>
                    <span>· {new Date(item.createdAt).toLocaleString()}</span>
                  </div>
                  <div style={{ display: 'flex', gap: '8px' }}>
                    <button style={primaryBtnStyle} disabled={nomBusy === item.nominationId} onClick={() => void decideNomination(item, 'confirm')}>{nomBusy === item.nominationId ? '处理中…' : '确认沉淀'}</button>
                    <button style={btnStyle} disabled={nomBusy === item.nominationId} onClick={() => void decideNomination(item, 'reject')}>拒绝</button>
                    <button style={btnStyle} disabled={nomBusy === item.nominationId} onClick={() => void withdrawNomination(item)}>撤回提名</button>
                  </div>
                </div>
              ))}
            </div>
          )}
        </section>
      )}
      {tab === 'history' && (
        <section aria-label="处理历史" style={panelStyle}>
          <h2 style={{ margin: '0 0 6px', fontSize: '15px' }}>处理历史</h2>
          <p style={{ margin: '0 0 14px', color: '#8fa3bf', fontSize: '12px' }}>已处理与已撤回的提名记录（最近 50 条）。</p>
          {history.length === 0 ? <div className="empty"><b>暂无历史记录</b><span>处理过的提名会归档在这里。</span></div> : (
            <div style={{ display: 'grid', gap: '8px' }}>
              {history.map(item => (
                <div key={item.nominationId} style={{ display: 'flex', gap: '10px', alignItems: 'center', justifyContent: 'space-between', padding: '10px 12px', border: '1px solid #1f2937', borderRadius: '8px', background: '#111827' }}>
                  <span style={{ flex: 1, fontSize: '13px', overflowWrap: 'anywhere' }}>{item.content}</span>
                  <span style={{ fontSize: '12px', color: item.state === 'decided' ? '#34d399' : '#8fa3bf', flexShrink: 0 }}>
                    {NOM_STATE_LABELS[item.state] ?? item.state} · {new Date(item.decidedAt || item.createdAt).toLocaleString()}
                  </span>
                </div>
              ))}
            </div>
          )}
        </section>
      )}
      {tab === 'ops' && <MemoryOpsPanel />}
    </div>
  )
}
