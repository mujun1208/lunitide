import React, { useEffect, useState, useCallback } from 'react'
import { feedbackBridge, memoryBridge, nominationBridge, type FeedbackBridge, type MemoryBridge, type NominationBridge } from '../bridge/client'
import type { MemoryDTO, MemoryLayer, MemoryScope, MemoryNominationListResult } from '../generated/bridge'
import { MemoryOpsPanel } from './MemoryOpsPanel'

type PendingCandidate = { candidateId: string; content: string; scopeId: string; confirmationToken: string; createdAt: string; expiresAt: string }
type NominationItem = MemoryNominationListResult['items'][number]

type MemoryTab = 'overview' | 'inbox' | 'history' | 'ops'

const TAB_LABELS: Record<MemoryTab, string> = { overview: '记忆总览', inbox: '提名收件箱', history: '处理历史', ops: '记忆运营' }
const NOM_STATE_LABELS: Record<string, string> = { nominated: '待处理', decided: '已处理', withdrawn: '已撤回' }

const LAYER_LABELS: Record<MemoryLayer, string> = {
  working: '工作记忆', episodic: '情景记忆', semantic: '语义记忆', procedural: '程序记忆'
}
const SCOPE_LABELS: Record<MemoryScope, string> = { workspace: '工作区', project: '项目', session: '会话' }
const LAYER_ORDER: MemoryLayer[] = ['working', 'episodic', 'semantic', 'procedural']
const SCOPE_OPTIONS: MemoryScope[] = ['workspace', 'project', 'session']

const layerColor = (l: MemoryLayer): string => {
  if (l === 'working') return '#60a5fa'
  if (l === 'episodic') return '#34d399'
  if (l === 'semantic') return '#fbbf24'
  return '#a78bfa'
}

const inputStyle: React.CSSProperties = { width: '100%', padding: '6px 8px', backgroundColor: '#0a0e1a', color: '#e5e7eb', border: '1px solid #334155', borderRadius: '4px', boxSizing: 'border-box' }
const btnStyle: React.CSSProperties = { padding: '6px 12px', backgroundColor: '#1e293b', color: '#e5e7eb', border: '1px solid #334155', borderRadius: '4px', cursor: 'pointer' }
const primaryBtnStyle: React.CSSProperties = { ...btnStyle, backgroundColor: '#2563eb', borderColor: '#3b82f6' }

export function MemoryPage({ projectId, bridge = memoryBridge, feedback = feedbackBridge, nominations = nominationBridge }: { projectId: string; bridge?: MemoryBridge; feedback?: FeedbackBridge; nominations?: NominationBridge }): React.JSX.Element {
  const [tab, setTab] = useState<MemoryTab>('overview')
  const [memories, setMemories] = useState<MemoryDTO[]>([])
  const [selected, setSelected] = useState<MemoryDTO | null>(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string>()
  const [busy, setBusy] = useState(false)
  const [searchQuery, setSearchQuery] = useState('')
  const [layerFilter, setLayerFilter] = useState<MemoryLayer | ''>('')
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
      const r = await bridge.list({ ...(layerFilter ? { layer: layerFilter } : {}), projectId })
      setMemories(r.items)
    } catch (e) { setError(e instanceof Error ? e.message : '加载失败') }
    finally { setLoading(false) }
  }, [projectId, bridge, layerFilter])

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
  const cardStyle: React.CSSProperties = { padding: '12px', border: '1px solid #1f2937', borderRadius: '10px', background: '#111827', cursor: 'pointer', transition: '0.15s' }

  const grouped = memories.reduce<Record<string, MemoryDTO[]>>((acc, m) => {
    (acc[m.layer] ??= []).push(m); return acc
  }, {})

  return (
    <div className="shell">
      <header className="brand"><div><p className="eyebrow">MEMORY PALACE</p><h1>记忆面板</h1><p>浏览、搜索与管理项目记忆。</p></div></header>
      {error && <div className="error" role="alert"><b>{error}</b></div>}
      <div role="tablist" aria-label="记忆面板分区" style={{ display: 'flex', gap: '6px', marginBottom: '18px', borderBottom: '1px solid #1f2937' }}>
        {(Object.keys(TAB_LABELS) as MemoryTab[]).map(key => (
          <button key={key} role="tab" aria-selected={tab === key} onClick={() => setTab(key)}
            style={{ padding: '8px 14px', border: 'none', borderBottom: tab === key ? '2px solid #60a5fa' : '2px solid transparent', background: 'transparent', color: tab === key ? '#e5e7eb' : '#8fa3bf', cursor: 'pointer', fontWeight: tab === key ? 600 : 400 }}>
            {TAB_LABELS[key]}{key === 'inbox' && inbox.length > 0 ? `（${inbox.length}）` : ''}
          </button>
        ))}
      </div>
      {tab === 'overview' && (<>
      <div style={{ display: 'flex', gap: '10px', marginBottom: '18px', flexWrap: 'wrap' }}>
        <input value={searchQuery} onChange={e => setSearchQuery(e.target.value)} placeholder="搜索记忆…" style={{ flex: 1, minWidth: '200px' }} onKeyDown={e => { if (e.key === 'Enter') void doSearch() }} />
        <select value={layerFilter} onChange={e => setLayerFilter(e.target.value as MemoryLayer | '')} style={{ minWidth: '140px' }}>
          <option value="">全部层级</option>
          {LAYER_ORDER.map(l => <option key={l} value={l}>{LAYER_LABELS[l]}</option>)}
        </select>
        <button onClick={() => void doSearch()} disabled={loading}>搜索</button>
        <button onClick={() => void load()} disabled={loading} aria-label="刷新">↻</button>
        <button style={btnStyle} onClick={() => setShowCreate(v => !v)}>新建记忆</button>
      </div>
      {showCreate && (
        <form onSubmit={e => void doCreate(e)} style={{ marginBottom: '18px', padding: '14px', border: '1px solid #334155', borderRadius: '8px', background: '#0a0e1a', display: 'grid', gap: '8px', gridTemplateColumns: '1fr 1fr' }}>
          <label style={{ display: 'grid', gap: '4px', fontSize: '13px' }}>层级
            <select style={inputStyle} value={newLayer} onChange={e => setNewLayer(e.target.value as MemoryLayer)} aria-label="层级">
              {LAYER_ORDER.map(l => <option key={l} value={l}>{LAYER_LABELS[l]}</option>)}
            </select>
          </label>
          <label style={{ display: 'grid', gap: '4px', fontSize: '13px' }}>作用域
            <select style={inputStyle} value={newScope} onChange={e => setNewScope(e.target.value as MemoryScope)} aria-label="作用域">
              {SCOPE_OPTIONS.map(s => <option key={s} value={s}>{SCOPE_LABELS[s]}</option>)}
            </select>
          </label>
          <label style={{ display: 'grid', gap: '4px', fontSize: '13px', gridColumn: '1 / -1' }}>键名
            <input style={inputStyle} value={newKey} onChange={e => setNewKey(e.target.value)} aria-label="键名" placeholder="输入记忆键名" />
          </label>
          <label style={{ display: 'grid', gap: '4px', fontSize: '13px', gridColumn: '1 / -1' }}>内容
            <textarea style={{ ...inputStyle, resize: 'vertical', minHeight: '80px' }} value={newContent} onChange={e => setNewContent(e.target.value)} aria-label="内容" placeholder="输入记忆内容" />
          </label>
          <div style={{ gridColumn: '1 / -1', display: 'flex', gap: '8px' }}>
            <button type="submit" style={primaryBtnStyle} disabled={busy || !newKey.trim() || !newContent.trim()}>{busy ? '创建中…' : '创建记忆'}</button>
            <button type="button" style={btnStyle} onClick={() => setShowCreate(false)}>取消</button>
          </div>
        </form>
      )}
      {pending.length > 0 && (
        <section aria-label="偏好确认" style={{ marginBottom: '18px', padding: '14px', border: '1px solid #334155', borderRadius: '8px', background: '#0a0e1a' }}>
          <h2 style={{ margin: '0 0 6px', fontSize: '15px' }}>偏好确认（{pending.length}）</h2>
          <p style={{ margin: '0 0 10px', color: '#8fa3bf', fontSize: '12px' }}>来自会话反馈的偏好候选。仅在你显式确认后才会沉淀为长期偏好并注入后续对话。</p>
          <div style={{ display: 'grid', gap: '8px' }}>
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
        </section>
      )}
      <div style={{ display: 'grid', gridTemplateColumns: 'minmax(280px,380px) 1fr', gap: '20px' }}>
        <section style={panelStyle}>
          <h2 style={{ margin: '0 0 14px', fontSize: '18px' }}>记忆列表</h2>
          {loading ? <p style={{ color: '#8fa3bf' }}>正在载入…</p> :
           memories.length === 0 ? <div className="empty"><b>暂无记忆</b></div> :
           LAYER_ORDER.filter(l => grouped[l]).map(layer => (
             <div key={layer} style={{ marginBottom: '14px' }}>
               <div style={{ fontSize: '12px', color: layerColor(layer), fontWeight: 600, marginBottom: '6px', textTransform: 'uppercase', letterSpacing: '0.05em' }}>{LAYER_LABELS[layer]} ({grouped[layer].length})</div>
               {grouped[layer].map(m => (
                 <div key={m.id} onClick={() => { setSelected(m); setEditContent(null) }} style={{ ...cardStyle, marginBottom: '6px', ...(selected?.id === m.id ? { borderColor: '#60a5fa', background: 'rgba(96,165,250,0.08)' } : {}) }}>
                   <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'flex-start', gap: '6px' }}>
                     <strong style={{ fontSize: '13px' }}>{m.key}</strong>
                     <span style={{ fontSize: '10px', color: '#8fa3bf' }}>{SCOPE_LABELS[m.scope]}</span>
                   </div>
                   <p style={{ margin: '4px 0 0', color: '#8fa3bf', fontSize: '11px', overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>{m.content}</p>
                 </div>
               ))}
             </div>
           ))}
        </section>
        <section style={panelStyle}>
          <h2 style={{ margin: '0 0 14px', fontSize: '18px' }}>记忆详情</h2>
          {!selected ? <div className="empty"><b>请选择记忆</b><span>从左侧选择一条记忆查看详情。</span></div> : (
            <div>
              <div style={{ marginBottom: '12px' }}>
                <strong style={{ fontSize: '15px' }}>{selected.key}</strong>
                <div style={{ marginTop: '4px', display: 'flex', gap: '8px', flexWrap: 'wrap' }}>
                  <span style={{ color: layerColor(selected.layer), fontSize: '12px' }}>{LAYER_LABELS[selected.layer]}</span>
                  <span style={{ color: '#8fa3bf', fontSize: '12px' }}>· {SCOPE_LABELS[selected.scope]}</span>
                  <span style={{ color: '#8fa3bf', fontSize: '12px' }}>· 置信度 {(selected.confidence * 100).toFixed(0)}%</span>
                  <span style={{ color: '#8fa3bf', fontSize: '12px' }}>· 访问 {selected.accessCount} 次</span>
                </div>
              </div>
              {editContent !== null ? (
                <div style={{ display: 'grid', gap: '10px' }}>
                  <textarea value={editContent} onChange={e => setEditContent(e.target.value)} rows={6} style={{ width: '100%', resize: 'vertical' }} />
                  <div style={{ display: 'flex', gap: '8px' }}>
                    <button className="primary" disabled={busy} onClick={() => void doUpdate(selected.id)}>保存</button>
                    <button disabled={busy} onClick={() => setEditContent(null)}>取消</button>
                  </div>
                </div>
              ) : (
                <>
                  <div style={{ padding: '14px', border: '1px solid #1f2937', borderRadius: '10px', background: '#111827', whiteSpace: 'pre-wrap', overflowWrap: 'anywhere', fontSize: '13px', lineHeight: '1.6' }}>{selected.content}</div>
                  <div style={{ marginTop: '10px', fontSize: '11px', color: '#8fa3bf' }}>
                    创建: {new Date(selected.createdAt).toLocaleString()} · 更新: {new Date(selected.updatedAt).toLocaleString()}{selected.expiresAt ? ` · 过期: ${new Date(selected.expiresAt).toLocaleString()}` : ''}{selected.lastAccessed ? ` · 最后访问: ${new Date(selected.lastAccessed).toLocaleString()}` : ''}
                  </div>
                  <div style={{ marginTop: '14px', display: 'flex', gap: '8px' }}>
                    <button disabled={busy} onClick={() => setEditContent(selected.content)}>编辑</button>
                    <button disabled={busy} onClick={() => void doDelete(selected.id)} style={{ color: '#f87171' }}>删除</button>
                  </div>
                </>
              )}
            </div>
          )}
        </section>
      </div>
      </>)}
      {tab === 'inbox' && (
        <section aria-label="提名收件箱" style={panelStyle}>
          <h2 style={{ margin: '0 0 6px', fontSize: '18px' }}>提名收件箱</h2>
          <p style={{ margin: '0 0 14px', color: '#8fa3bf', fontSize: '12px' }}>助手从会话中提名的记忆候选。确认后沉淀为长期记忆，处理与撤回都会记入历史。</p>
          {inbox.length === 0 ? <div className="empty"><b>暂无待处理提名</b><span>新的提名会出现在这里等待你的确认。</span></div> : (
            <div style={{ display: 'grid', gap: '10px' }}>
              {inbox.map(item => (
                <div key={item.nominationId} style={{ padding: '14px', border: '1px solid #1f2937', borderRadius: '10px', background: '#111827' }}>
                  <div style={{ fontSize: '14px', overflowWrap: 'anywhere', marginBottom: '8px' }}>{item.content}</div>
                  <div style={{ display: 'flex', gap: '8px', flexWrap: 'wrap', alignItems: 'center', fontSize: '12px', color: '#8fa3bf', marginBottom: '10px' }}>
                    <span>提名理由：{item.reason}</span>
                    <span>· {item.nominator}</span>
                    {item.sourceSessionId && <span>· 来源会话</span>}
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
          <h2 style={{ margin: '0 0 6px', fontSize: '18px' }}>处理历史</h2>
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
