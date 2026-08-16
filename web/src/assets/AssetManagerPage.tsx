import React, { useEffect, useState } from 'react'
import { attachmentBridge, projectBridge, type AttachmentBridge, type ProjectBridge } from '../bridge/client'
import type { AttachmentGetResult, AttachmentListResult, ProjectDTO } from '../generated/bridge'

// AssetManagerPage 是左侧导航「项目管理」下的独立资产管理页面（从设置页迁出，界面保持不变）。
export function AssetManagerPage({ attachments = attachmentBridge, projects = projectBridge }: { attachments?: AttachmentBridge; projects?: ProjectBridge }): React.JSX.Element {
  const [projectItems, setProjectItems] = useState<ProjectDTO[]>([])
  const [projectId, setProjectId] = useState('')
  const [items, setItems] = useState<AttachmentListResult['items']>([])
  const [detail, setDetail] = useState<AttachmentGetResult>()
  const [query, setQuery] = useState('')
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')
  useEffect(() => {
    let alive = true
    try {
      projects.list().then(result => {
        if (!alive) return
        setProjectItems(result.items)
        setProjectId(current => (result.items.some(item => item.id === current) ? current : (result.items[0]?.id ?? '')))
      }).catch(e => { if (alive) setError(e instanceof Error ? e.message : '项目列表载入失败') })
    } catch (e) { if (alive) setError(e instanceof Error ? e.message : '项目列表载入失败') }
    return () => { alive = false }
  }, [projects])
  useEffect(() => {
    if (!projectId) return
    let alive = true
    setLoading(true)
    try {
      attachments.list({ projectId }).then(result => { if (alive) setItems(result.items) }).catch(e => { if (alive) setError(e instanceof Error ? e.message : '资产载入失败') }).finally(() => { if (alive) setLoading(false) })
    } catch (e) { if (alive) { setError(e instanceof Error ? e.message : '资产载入失败'); setLoading(false) } }
    return () => { alive = false }
  }, [attachments, projectId])
  useEffect(() => {
    if (!items.length) { setDetail(undefined); return }
    const id = items[0].attachmentId
    let alive = true
    attachments.get({ attachmentId: id }).then(result => { if (alive) setDetail(result) }).catch(() => { if (alive) setDetail(undefined) })
    return () => { alive = false }
  }, [attachments, items])
  const visible = items.filter(item => !query.trim() || item.originalName.toLowerCase().includes(query.trim().toLowerCase()))
  const fmtSize = (n: number) => n < 1024 ? `${n} B` : n < 1048576 ? `${(n / 1024).toFixed(1)} KB` : `${(n / 1048576).toFixed(1)} MB`
  const parseLabel: Record<string, string> = { pending: '待解析', parsing: '解析中', succeeded: '已解析', failed: '解析失败' }
  return (
    <main className="skill-center">
      <header className="skill-center-header"><div><h1>资产管理</h1><p>{items.length} 项资产 · 按项目归集</p><small>会话中上传的附件会归集到这里，可检索并查看解析内容。</small></div></header>
      <div className="asset-manager-panel">
      <div className="setting-group-title">资产管理 · 分类、检索与生命周期</div>
      <div className="asset-manager-toolbar">
        <label>所属项目
          <select aria-label="筛选资产所属项目" value={projectId} onChange={e => setProjectId(e.target.value)}>
            {projectItems.map(item => <option key={item.id} value={item.id}>{item.name}</option>)}
          </select>
        </label>
        <label className="asset-manager-search">搜索资产
          <input aria-label="搜索资产" placeholder="文件名关键词…" value={query} onChange={e => setQuery(e.target.value)} />
        </label>
        <span className="asset-manager-count" role="status">{loading ? '正在载入…' : `${items.length} 项资产`}</span>
      </div>
      {error ? <p className="notice" role="alert">{error}</p> : !projectId ? <p className="notice">还没有可用项目；请先在“项目管理”中创建。</p> : (
        <div className="asset-manager-layout">
          <div className="asset-manager-list" role="listbox" aria-label="资产列表">
            {loading ? <p role="status">正在载入资产…</p> : visible.length ? visible.map(item => (
              <button type="button" role="option" key={item.attachmentId} aria-selected={detail?.attachmentId === item.attachmentId} onClick={() => {
                const id = item.attachmentId
                attachments.get({ attachmentId: id }).then(setDetail).catch(() => setDetail(undefined))
              }}>
                <b>{item.originalName}</b>
                <small>{item.mime} · {fmtSize(item.size)} · {parseLabel[item.parseStatus]}</small>
              </button>
            )) : <p>该项目还没有资产；会话中上传的附件会归集到这里。</p>}
          </div>
          <div className="asset-manager-detail">
            {detail ? (
              <>
                <h3>{detail.originalName}</h3>
                <dl>
                  <dt>类型</dt><dd>{detail.mime}</dd>
                  <dt>大小</dt><dd>{fmtSize(detail.size)}</dd>
                  <dt>解析状态</dt><dd>{parseLabel[detail.parseStatus]}{detail.parseErrorCode ? `（${detail.parseErrorCode}）` : ''}</dd>
                  <dt>校验（SHA-256）</dt><dd className="asset-sha">{detail.sha256}</dd>
                  <dt>来源</dt><dd>会话上传 · {new Date(detail.createdAt).toLocaleString()}</dd>
                </dl>
                {detail.parsedText !== undefined
                  ? <pre className="asset-parsed">{detail.parsedText.slice(0, 4000)}{detail.parsedText.length > 4000 ? '\n…' : ''}</pre>
                  : <p>{detail.mime.startsWith('image/') ? '图片资产：可在对话中交给视觉模型分析。' : '此格式暂不支持文本预览。'}</p>}
              </>
            ) : <p>选择左侧资产查看详情、校验值与解析内容。</p>}
          </div>
        </div>
      )}
    </div>
    </main>
  )
}
