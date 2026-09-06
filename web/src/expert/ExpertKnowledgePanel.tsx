import React, { useEffect, useRef, useState } from 'react'
import { useZh } from '../i18n/language'
import type {KnowledgeSource,KnowledgeIngest,KnowledgeStats,KnowledgeGetPayload} from './knowledgeTypes'
export type {KnowledgeStats} from './knowledgeTypes'

const KNOWLEDGE_MEDIA: Record<string, string> = {
  md: 'text/markdown',
  markdown: 'text/markdown',
  txt: 'text/plain',
  pdf: 'application/pdf',
  docx: 'application/vnd.openxmlformats-officedocument.wordprocessingml.document',
  pptx: 'application/vnd.openxmlformats-officedocument.presentationml.presentation',
  xlsx: 'application/vnd.openxmlformats-officedocument.spreadsheetml.sheet',
}

// knowledgeMediaType resolves a coherent media hint from the filename so an
// office/pdf file is routed to the binary text extractor even when the browser
// leaves File.type empty (common for .md and sometimes .xlsx). The backend
// re-detects by extension and magic bytes, so this is a hint, not the source of
// truth. Unknown extensions fall back to the browser type, then plain text.
export function knowledgeMediaType(name: string, fileType: string): string {
  const ext = name.split('.').pop()?.toLowerCase() ?? ''
  return KNOWLEDGE_MEDIA[ext] ?? (fileType || 'text/plain')
}

export function ExpertKnowledgePanel({
  expertId, knowledgeGet, knowledgeIngest,
}: {
  expertId: string
  knowledgeGet?: (payload: KnowledgeGetPayload) => Promise<KnowledgeStats>
  knowledgeIngest?: KnowledgeIngest
}): React.JSX.Element {
  const zh = useZh()
  const fileRef = useRef<HTMLInputElement>(null)
  const [stats, setStats] = useState<KnowledgeStats | null>(null)
  const [loaded, setLoaded] = useState(!knowledgeGet)
  const [error, setError] = useState('')
  const [notice, setNotice] = useState('')
  const [preview, setPreview] = useState<string[]>([])
  const [busy, setBusy] = useState(false)
  const [localPath, setLocalPath] = useState('')
  const epoch = useRef(0)
  const active = useRef(false)
  const [sourceCursor,setSourceCursor]=useState('')
  const [previousCursors,setPreviousCursors]=useState<string[]>([])
  const [historyPages,setHistoryPages]=useState<Record<string,boolean>>({})
  useEffect(()=>{setSourceCursor('');setPreviousCursors([]);setHistoryPages({})},[expertId])

  useEffect(() => {
    let alive = true
    const generation = ++epoch.current
    active.current = false
    setBusy(false); setError(''); setNotice(''); setPreview([]); setLocalPath(''); setStats(null)
    if (!knowledgeGet) {
      setStats({ collectionId: '', documentCount: 0, readyCount: 0, chunkCount: 0, nodeCount: 0, memoryCount: 0, missing: true })
      setLoaded(true)
      return
    }
    setLoaded(false)
    void knowledgeGet({ expertId,...(sourceCursor?{sourcesAfter:sourceCursor}:{}) }).then(next => {
      if (alive) { setStats(next); setLoaded(true) }
    }).catch(e => {
      if (alive) {
        setError(e instanceof Error ? e.message : (zh ? '知识库加载失败' : 'Failed to load knowledge'))
        setLoaded(true)
      }
    })
    return () => { alive = false; if (epoch.current === generation) epoch.current++ }
  }, [expertId, knowledgeGet, zh, sourceCursor])

  const collectionId = stats?.collectionId?.trim() ?? ''
  const persona = loaded && !collectionId
  const empty = !!collectionId && (stats?.chunkCount ?? 0) === 0

  const ingestPath = async (path: string, mediaType?:string, source?:KnowledgeSource) => {
    if (!path.trim() || !knowledgeIngest || active.current) return
    const generation = epoch.current
    active.current = true; setBusy(true); setError(''); setNotice(''); setPreview([])
    try {
      const result = await knowledgeIngest({expertId,path:path.trim(),mediaType, ...(source ? {sourceLocator:source.sourceLocator,expectedRevision:source.revision} : {})})
      if (epoch.current !== generation) return
      const failed = result.documents.find(doc=>doc.indexState==='failed')
      if (failed) throw new Error(failed.failReason || (zh?'无法抽出正文':'Could not extract body'))
      setPreview(result.documents.flatMap(doc=>doc.preview??[]).slice(0,3))
      setNotice(zh ? '来源已校验，索引已更新。' : 'Source verified and index updated.')
    } catch (e) {
      if (epoch.current === generation) setError(zh ? `无法抽出正文或刷新来源：${e instanceof Error?e.message:String(e)}` : `Source update failed: ${e instanceof Error?e.message:String(e)}`)
    } finally {
      // A rejected parse still has a durable failed-source receipt. Reload it
      // so the UI does not leave an old "ready" label after refresh failure.
      if (epoch.current === generation) {
        try { const next = await knowledgeGet?.({expertId,...(sourceCursor?{sourcesAfter:sourceCursor}:{})}); if (next && epoch.current===generation) setStats(next) }
        catch(e) { if(epoch.current===generation) setError(e instanceof Error?e.message:String(e)) }
        if(epoch.current===generation) {active.current=false;setBusy(false)}
      }
    }
  }
  const onFile = async (file: File | undefined) => {
    if (!file) return
    const path = (file as File & {path?:string}).path
    if (!path) {setError(zh?'请在下方填写文件的完整本地路径。':'Enter the full local file path below.');return}
    if (file.size>32*1024*1024) {setError(zh?'单个来源不能超过 32 MiB。':'A source must be at most 32 MiB.');return}
    await ingestPath(path,knowledgeMediaType(file.name,file.type))
  }
  const loadHistory = async (source:KnowledgeSource,before?:number) => {
    if(!knowledgeGet||active.current)return
    const generation=epoch.current;active.current=true;setBusy(true);setError('')
    try {
      const next=await knowledgeGet({expertId,historySourceId:source.sourceId,...(before?{historyBeforeVersion:before}:{})})
      if(epoch.current!==generation)return
      const updated=next.sources?.find(row=>row.sourceId===source.sourceId)
      if(updated){setStats(current=>current?{...current,sources:current.sources?.map(row=>row.sourceId===source.sourceId?updated:row)}:current);setHistoryPages(current=>({...current,[source.sourceId]:!!before}))}
    }catch(e){if(epoch.current===generation)setError(e instanceof Error?e.message:String(e))}
    finally{if(epoch.current===generation){active.current=false;setBusy(false)}}
  }
  const sourceLabel = (state:KnowledgeSource['state']) => ({fresh:zh?'已索引，使用时校验':'Indexed; verified when used',refreshing:zh?'刷新未完成，可重试':'Refresh incomplete; retry available',stale:zh?'原文件已变更':'Source changed',missing:zh?'原文件不存在':'Source missing',failed:zh?'来源或解析失败':'Source or parsing failed'})[state]

  return (
    <section className="expert-knowledge-panel" aria-label={zh ? '知识' : 'Knowledge'}>
      <h3>{zh ? '知识' : 'Knowledge'}</h3>
      {persona ? <p className="expert-growth-empty">{zh ? '人设卡不建知识库。升级为同事专家后再说。' : 'Persona cards do not get a knowledge base. Upgrade to a colleague expert first.'}</p> : empty ? (
        <div className="empty">
          <b>{zh ? '还没有交给这位专家的文件' : 'No files given to this expert yet'}</b>
          <span>{zh ? '把 PDF / Word / Excel / PPT / Markdown 交给他之后，对话会只在这个库里检索。' : 'After you give PDF / Word / Excel / PowerPoint / Markdown, chat searches only this library.'}</span>
        </div>
      ) : stats ? (
        <p className="expert-knowledge-stats">{stats.documentCount} {zh ? '份文档' : 'docs'} · {stats.readyCount} {zh ? '已就绪' : 'ready'} · {stats.chunkCount} {zh ? '块' : 'chunks'} · {zh ? '图谱' : 'graph'} {stats.nodeCount} · {zh ? '记忆' : 'memory'} {stats.memoryCount}</p>
      ) : <p role="status">{zh ? '正在载入知识…' : 'Loading knowledge…'}</p>}
      {loaded && !persona && (
        <>
          <input ref={fileRef} hidden type="file" accept=".pdf,.docx,.pptx,.xlsx,.md,.markdown,.txt,application/pdf" onChange={e => { void onFile(e.target.files?.[0]); e.target.value = '' }} />
          <button type="button" disabled={busy || !knowledgeIngest} onClick={() => fileRef.current?.click()}>{zh ? '把文件交给此专家' : 'Give a file to this expert'}</button>
          <label>{zh?'文件完整路径':'Full local file path'}<input value={localPath} onChange={e=>setLocalPath(e.target.value)} disabled={busy} /></label>
          <button type="button" disabled={busy || !knowledgeIngest || !localPath.trim()} onClick={()=>void ingestPath(localPath)}>{zh?'导入此路径':'Import this path'}</button>
        </>
      )}
      {(stats?.sources?.length ?? 0)>0 && <ul aria-label={zh?'知识来源':'Knowledge sources'}>{stats?.sources?.map(source=><li key={source.sourceId}>
        <p>{source.path} · v{source.version} · {sourceLabel(source.state)}</p>
        {source.error && <p>{source.error}</p>}
        <small>{zh?'上次校验':'Last checked'}：{source.checkedAt || '—'}</small>
        <button type="button" disabled={busy || !knowledgeIngest} onClick={()=>void ingestPath(source.path,source.mediaType,source)}>{zh?'刷新来源':'Refresh source'}</button>
        <details><summary>{zh?'来源版本记录（每页最多 50 版）':'Source version history (up to 50 per page)'}</summary><ol>{source.versions.map(version=><li key={version.version}>v{version.version} · {version.state==='fresh'?(zh?'已发布':'Published'):(zh?'失败':'Failed')} · {version.createdAt} · {version.sha256 || '—'} {version.error}</li>)}</ol>
          {!!source.nextBeforeVersion && <button type="button" disabled={busy} onClick={()=>void loadHistory(source,source.nextBeforeVersion)}>{zh?'更早版本':'Earlier versions'}</button>}
          {historyPages[source.sourceId] && <button type="button" disabled={busy} onClick={()=>void loadHistory(source)}>{zh?'返回最新版本':'Back to latest versions'}</button>}
        </details>
      </li>)}</ul>}
      {(previousCursors.length>0 || stats?.nextSourceCursor) && <nav aria-label={zh?'来源分页':'Source pages'}>
        <button type="button" disabled={busy || previousCursors.length===0} onClick={()=>{setSourceCursor(previousCursors[previousCursors.length-1]??'');setPreviousCursors(values=>values.slice(0,-1));setHistoryPages({})}}>{zh?'上一页来源':'Previous sources'}</button>
        <button type="button" disabled={busy || !stats?.nextSourceCursor} onClick={()=>{setPreviousCursors(values=>[...values,sourceCursor]);setSourceCursor(stats?.nextSourceCursor??'');setHistoryPages({})}}>{zh?'下一页来源':'Next sources'}</button>
      </nav>}
      {busy && <p role="status">{zh?'正在验证与更新来源…':'Verifying and updating source…'}</p>}
      {preview.length > 0 && (
        <ol className="expert-knowledge-preview" aria-label={zh ? '解析预览' : 'Parse preview'}>
          {preview.map((block, index) => <li key={index}>{block}</li>)}
        </ol>
      )}
      {notice && <p className="skill-center-notice" role="status">{notice}</p>}
      {error && <p className="skill-center-error is-failed" role="alert">{error}</p>}
    </section>
  )
}
