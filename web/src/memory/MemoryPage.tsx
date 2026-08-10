import React, { useEffect, useState, useCallback } from 'react'
import { memoryBridge, type MemoryBridge } from '../bridge/client'
import type { MemoryDTO, MemoryLayer, MemoryScope } from '../generated/bridge'

const LAYER_LABELS: Record<MemoryLayer, string> = {
  working: '工作记忆', episodic: '情景记忆', semantic: '语义记忆', procedural: '程序记忆'
}
const SCOPE_LABELS: Record<MemoryScope, string> = { workspace: '工作区', project: '项目', session: '会话' }
const LAYER_ORDER: MemoryLayer[] = ['working', 'episodic', 'semantic', 'procedural']

const layerColor = (l: MemoryLayer): string => {
  if (l === 'working') return '#60a5fa'
  if (l === 'episodic') return '#34d399'
  if (l === 'semantic') return '#fbbf24'
  return '#a78bfa'
}

export function MemoryPage({ projectId, bridge = memoryBridge }: { projectId: string; bridge?: MemoryBridge }): React.JSX.Element {
  const [memories, setMemories] = useState<MemoryDTO[]>([])
  const [selected, setSelected] = useState<MemoryDTO | null>(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string>()
  const [busy, setBusy] = useState(false)
  const [searchQuery, setSearchQuery] = useState('')
  const [layerFilter, setLayerFilter] = useState<MemoryLayer | ''>('')
  const [editContent, setEditContent] = useState<string | null>(null)

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
      <div style={{ display: 'flex', gap: '10px', marginBottom: '18px', flexWrap: 'wrap' }}>
        <input value={searchQuery} onChange={e => setSearchQuery(e.target.value)} placeholder="搜索记忆…" style={{ flex: 1, minWidth: '200px' }} onKeyDown={e => { if (e.key === 'Enter') void doSearch() }} />
        <select value={layerFilter} onChange={e => setLayerFilter(e.target.value as MemoryLayer | '')} style={{ minWidth: '140px' }}>
          <option value="">全部层级</option>
          {LAYER_ORDER.map(l => <option key={l} value={l}>{LAYER_LABELS[l]}</option>)}
        </select>
        <button onClick={() => void doSearch()} disabled={loading}>搜索</button>
        <button onClick={() => void load()} disabled={loading} aria-label="刷新">↻</button>
      </div>
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
    </div>
  )
}
