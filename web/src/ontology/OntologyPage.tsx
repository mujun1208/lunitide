import React, { useEffect, useState, useCallback } from 'react'
import { ontologyBridge, type OntologyBridge } from '../bridge/client'
import type { OntologyNodeDTO, OntologyEdgeDTO, OntologyNodeType, OntologyEdgeType } from '../generated/bridge'

const NODE_TYPE_LABELS: Record<OntologyNodeType, string> = {
  class: '类', interface: '接口', function: '函数', module: '模块', table: '表', file: '文件',
  requirement: '需求', artifact: '制品', component: '组件', endpoint: '端点', test: '测试'
}
const EDGE_TYPE_LABELS: Record<OntologyEdgeType, string> = {
  implements: '实现', extends: '继承', depends_on: '依赖', references: '引用', contains: '包含',
  tests: '测试', imports: '导入', satisfies: '满足', traces: '追溯', generates: '生成',
  configures: '配置', authenticates: '认证', authorizes: '授权'
}
const NODE_TYPE_ORDER: OntologyNodeType[] = ['class', 'interface', 'function', 'module', 'table', 'file', 'requirement', 'artifact', 'component', 'endpoint', 'test']
const typeColor = (t: OntologyNodeType): string => {
  const colors: Record<OntologyNodeType, string> = {
    class: '#60a5fa', interface: '#a78bfa', function: '#34d399', module: '#fbbf24', table: '#f87171',
    file: '#8fa3bf', requirement: '#2fd6b5', artifact: '#7fb4ff', component: '#cfe0ff', endpoint: '#3bd6ff', test: '#ffc46b'
  }
  return colors[t]
}

export function OntologyPage({ projectId, bridge = ontologyBridge }: { projectId: string; bridge?: OntologyBridge }): React.JSX.Element {
  const [nodes, setNodes] = useState<OntologyNodeDTO[]>([])
  const [selected, setSelected] = useState<OntologyNodeDTO | null>(null)
  const [edges, setEdges] = useState<OntologyEdgeDTO[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string>()
  const [searchQuery, setSearchQuery] = useState('')
  const [typeFilter, setTypeFilter] = useState<OntologyNodeType | ''>('')
  const [edgeDirection, setEdgeDirection] = useState<'outgoing' | 'incoming'>('outgoing')

  const load = useCallback(async () => {
    if (!projectId) { setLoading(false); return }
    setLoading(true); setError(undefined)
    try {
      const r = await bridge.listNodes({ ...(typeFilter ? { type: typeFilter } : {}), projectId })
      setNodes(r.items)
    } catch (e) { setError(e instanceof Error ? e.message : '加载失败') }
    finally { setLoading(false) }
  }, [projectId, bridge, typeFilter])

  useEffect(() => { load() }, [load])

  const loadEdges = useCallback(async (nodeId: string, direction: 'outgoing' | 'incoming') => {
    try { const r = await bridge.listEdges({ nodeId, direction }); setEdges(r.items) }
    catch (e) { setError(e instanceof Error ? e.message : '加载边失败') }
  }, [bridge])

  useEffect(() => { if (selected) loadEdges(selected.id, edgeDirection); else setEdges([]) }, [selected, edgeDirection, loadEdges])

  const doSearch = async () => {
    if (!projectId || !searchQuery.trim()) return
    setLoading(true); setError(undefined)
    try { const r = await bridge.searchNodes({ projectId, query: searchQuery.trim() }); setNodes(r.items) }
    catch (e) { setError(e instanceof Error ? e.message : '搜索失败') }
    finally { setLoading(false) }
  }

  if (!projectId) {
    return <div className="shell"><div className="empty"><b>请先选择项目</b><span>在项目总览中选择一个项目后即可浏览本体。</span></div></div>
  }

  const panelStyle: React.CSSProperties = { border: '1px solid #1f2937', borderRadius: '16px', background: '#0e1c30', padding: '20px' }
  const cardStyle: React.CSSProperties = { padding: '12px', border: '1px solid #1f2937', borderRadius: '10px', background: '#111827', cursor: 'pointer', transition: '0.15s' }

  const grouped = nodes.reduce<Record<string, OntologyNodeDTO[]>>((acc, n) => {
    (acc[n.type] ??= []).push(n); return acc
  }, {})

  return (
    <div className="shell">
      <header className="brand"><div><p className="eyebrow">ONTOLOGY EXPLORER</p><h1>本体浏览</h1><p>浏览代码与架构本体图谱。</p></div></header>
      {error && <div className="error" role="alert"><b>{error}</b></div>}
      <div style={{ display: 'flex', gap: '10px', marginBottom: '18px', flexWrap: 'wrap' }}>
        <input value={searchQuery} onChange={e => setSearchQuery(e.target.value)} placeholder="搜索节点…" style={{ flex: 1, minWidth: '200px' }} onKeyDown={e => { if (e.key === 'Enter') void doSearch() }} />
        <select value={typeFilter} onChange={e => setTypeFilter(e.target.value as OntologyNodeType | '')} style={{ minWidth: '140px' }}>
          <option value="">全部类型</option>
          {NODE_TYPE_ORDER.map(t => <option key={t} value={t}>{NODE_TYPE_LABELS[t]}</option>)}
        </select>
        <button onClick={() => void doSearch()} disabled={loading}>搜索</button>
        <button onClick={() => void load()} disabled={loading} aria-label="刷新">↻</button>
      </div>
      <div style={{ display: 'grid', gridTemplateColumns: 'minmax(280px,380px) 1fr', gap: '20px' }}>
        <section style={panelStyle}>
          <h2 style={{ margin: '0 0 14px', fontSize: '18px' }}>节点列表</h2>
          {loading ? <p style={{ color: '#8fa3bf' }}>正在载入…</p> :
           nodes.length === 0 ? <div className="empty"><b>暂无节点</b></div> :
           NODE_TYPE_ORDER.filter(t => grouped[t]).map(type => (
             <div key={type} style={{ marginBottom: '14px' }}>
               <div style={{ fontSize: '12px', color: typeColor(type), fontWeight: 600, marginBottom: '6px', textTransform: 'uppercase', letterSpacing: '0.05em' }}>{NODE_TYPE_LABELS[type]} ({grouped[type].length})</div>
               {grouped[type].map(n => (
                 <div key={n.id} onClick={() => setSelected(n)} style={{ ...cardStyle, marginBottom: '6px', ...(selected?.id === n.id ? { borderColor: '#60a5fa', background: 'rgba(96,165,250,0.08)' } : {}) }}>
                   <strong style={{ fontSize: '13px' }}>{n.name}</strong>
                   <p style={{ margin: '3px 0 0', color: '#8fa3bf', fontSize: '11px', fontFamily: 'monospace', overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>{n.fullPath}</p>
                 </div>
               ))}
             </div>
           ))}
        </section>
        <section style={panelStyle}>
          <h2 style={{ margin: '0 0 14px', fontSize: '18px' }}>节点详情</h2>
          {!selected ? <div className="empty"><b>请选择节点</b><span>从左侧选择一个节点查看详情。</span></div> : (
            <div>
              <div style={{ marginBottom: '12px' }}>
                <div style={{ display: 'flex', alignItems: 'center', gap: '8px' }}>
                  <strong style={{ fontSize: '16px' }}>{selected.name}</strong>
                  <span style={{ color: typeColor(selected.type), fontSize: '12px', padding: '2px 8px', border: `1px solid ${typeColor(selected.type)}`, borderRadius: '99px' }}>{NODE_TYPE_LABELS[selected.type]}</span>
                </div>
                <p style={{ margin: '6px 0 0', color: '#8fa3bf', fontSize: '12px', fontFamily: 'monospace' }}>{selected.fullPath}</p>
                {selected.description && <p style={{ margin: '6px 0 0', fontSize: '13px', lineHeight: '1.5' }}>{selected.description}</p>}
                <div style={{ marginTop: '6px', fontSize: '11px', color: '#8fa3bf' }}>版本 {selected.version} · 创建 {new Date(selected.createdAt).toLocaleString()} · 更新 {new Date(selected.updatedAt).toLocaleString()}</div>
              </div>
              <div>
                <div style={{ display: 'flex', alignItems: 'center', gap: '10px', marginBottom: '10px' }}>
                  <h3 style={{ margin: 0, fontSize: '14px' }}>关联边</h3>
                  <div style={{ display: 'flex', gap: '4px' }}>
                    <button onClick={() => setEdgeDirection('outgoing')} style={{ padding: '4px 10px', fontSize: '11px', ...(edgeDirection === 'outgoing' ? { borderColor: '#60a5fa', background: 'rgba(96,165,250,0.12)' } : {}) }}>出边</button>
                    <button onClick={() => setEdgeDirection('incoming')} style={{ padding: '4px 10px', fontSize: '11px', ...(edgeDirection === 'incoming' ? { borderColor: '#60a5fa', background: 'rgba(96,165,250,0.12)' } : {}) }}>入边</button>
                  </div>
                </div>
                {edges.length === 0 ? <p style={{ color: '#8fa3bf', fontSize: '12px' }}>暂无{edgeDirection === 'outgoing' ? '出' : '入'}边</p> : (
                  <div style={{ display: 'grid', gap: '6px' }}>
                    {edges.map(edge => (
                      <div key={edge.id} style={{ padding: '10px', border: '1px solid #1f2937', borderRadius: '10px', background: '#111827', fontSize: '12px' }}>
                        <div style={{ display: 'flex', gap: '6px', alignItems: 'center' }}>
                          <span style={{ color: '#60a5fa' }}>{EDGE_TYPE_LABELS[edge.type]}</span>
                          {edge.label && <span style={{ color: '#e5e7eb' }}>{edge.label}</span>}
                        </div>
                        <div style={{ marginTop: '3px', color: '#8fa3bf', fontSize: '11px', fontFamily: 'monospace' }}>
                          {edgeDirection === 'outgoing' ? '→ ' : '← '}{edge.targetNodeId === selected.id ? edge.sourceNodeId : edge.targetNodeId}
                        </div>
                      </div>
                    ))}
                  </div>
                )}
              </div>
            </div>
          )}
        </section>
      </div>
    </div>
  )
}
