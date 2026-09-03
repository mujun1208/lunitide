import React, { useEffect, useRef, useState } from 'react'
import { useZh } from '../i18n/language'

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

export type KnowledgeStats = {
  collectionId?: string
  documentCount: number
  readyCount: number
  chunkCount: number
  nodeCount: number
  memoryCount: number
  missing: boolean
}

export function ExpertKnowledgePanel({
  expertId, knowledgeGet, upsertDocument,
}: {
  expertId: string
  knowledgeGet?: (payload: { expertId: string }) => Promise<KnowledgeStats>
  upsertDocument?: (payload: {
    collectionId: string
    documentId: string
    mediaType: string
    contentRef: string
    sha256: string
    sourceLocator: string
    requestId: string
  }) => Promise<{ indexState?: string; preview?: string[]; failReason?: string }>
}): React.JSX.Element {
  const zh = useZh()
  const fileRef = useRef<HTMLInputElement>(null)
  const [stats, setStats] = useState<KnowledgeStats | null>(null)
  const [loaded, setLoaded] = useState(!knowledgeGet)
  const [error, setError] = useState('')
  const [notice, setNotice] = useState('')
  const [preview, setPreview] = useState<string[]>([])

  useEffect(() => {
    let alive = true
    if (!knowledgeGet) {
      setStats({ collectionId: '', documentCount: 0, readyCount: 0, chunkCount: 0, nodeCount: 0, memoryCount: 0, missing: true })
      setLoaded(true)
      return
    }
    setLoaded(false)
    void knowledgeGet({ expertId }).then(next => {
      if (alive) { setStats(next); setLoaded(true) }
    }).catch(e => {
      if (alive) {
        setError(e instanceof Error ? e.message : (zh ? '知识库加载失败' : 'Failed to load knowledge'))
        setLoaded(true)
      }
    })
    return () => { alive = false }
  }, [expertId, knowledgeGet, zh])

  const collectionId = stats?.collectionId?.trim() ?? ''
  const persona = loaded && !collectionId
  const empty = !!collectionId && (stats?.chunkCount ?? 0) === 0

  const onFile = async (file: File | undefined) => {
    if (!file || !collectionId || !upsertDocument) return
    const path = (file as File & { path?: string }).path
    if (!path) {
      setError(zh ? '当前环境拿不到本地路径，请把文件放到工作区后再试。' : 'No local path for this file. Put it in the workspace first.')
      return
    }
    setError('')
    setNotice('')
    setPreview([])
    try {
      const buf = await file.arrayBuffer()
      const digest = await crypto.subtle.digest('SHA-256', buf)
      const sha256 = Array.from(new Uint8Array(digest)).map(b => b.toString(16).padStart(2, '0')).join('')
      const mediaType = knowledgeMediaType(file.name, file.type)
      const result = await upsertDocument({
        collectionId, documentId: newUlid(), mediaType, contentRef: path, sha256,
        sourceLocator: path, requestId: newUlid(),
      })
      if (result.indexState === 'failed') {
        const reason = result.failReason?.trim()
        setError(reason ? `${zh ? '无法抽出正文' : 'Could not extract body'}：${reason}` : (zh ? '无法抽出正文' : 'Could not extract body'))
        return
      }
      setPreview((result.preview ?? []).slice(0, 3))
      setNotice(zh ? `已交给此专家：${file.name}` : `Given to this expert: ${file.name}`)
      const next = await knowledgeGet?.({ expertId })
      if (next) setStats(next)
    } catch (e) {
      const message = e instanceof Error ? e.message : (zh ? '入库失败' : 'Ingest failed')
      setError(message.includes('无法抽出正文') || message.includes('parse function') ? message : (zh ? `无法抽出正文：${message}` : `Could not extract body: ${message}`))
    }
  }

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
          <button type="button" onClick={() => fileRef.current?.click()}>{zh ? '把文件交给此专家' : 'Give a file to this expert'}</button>
        </>
      )}
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

function newUlid(): string {
  const a = '0123456789ABCDEFGHJKMNPQRSTVWXYZ'
  const b = crypto.getRandomValues(new Uint8Array(10))
  let v = (BigInt(Date.now()) << 80n) | b.reduce((n, x) => (n << 8n) | BigInt(x), 0n)
  let r = ''
  for (let i = 0; i < 26; i++) {
    r = a[Number(v & 31n)] + r
    v >>= 5n
  }
  return r
}
