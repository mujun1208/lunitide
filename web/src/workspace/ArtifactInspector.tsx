import React, { useEffect, useMemo, useRef, useState } from 'react'
import { artifactReviewBridge, sessionFolderBridge } from '../bridge/client'
import type { WorkspaceArtifactPreviewResult } from '../generated/bridge'
import { MarkdownMessage } from '../session/MarkdownMessage'
import { isolatedHTML } from './isolatedHTML'
import { artifactLooksLikePdfBytes, artifactPreviewIsReady, artifactViewMode } from './artifactPreviewMode'
import { languageFromPath } from './codePanelUtils'
import './artifactInspector.css'

function inspectorUserError(err: unknown, fallback: string): string {
  const detail = err instanceof Error ? err.message.trim() : ''
  return /[\u4e00-\u9fff]/.test(detail) ? detail : fallback
}

function fileName(path: string): string {
  return path.split(/[/\\]/).pop() || path
}

export function ArtifactInspector({ sessionId, path, onClose, expanded = false, onToggleExpand }: {
  sessionId: string; path: string; onClose: () => void
  expanded?: boolean
  onToggleExpand?: () => void
}): React.JSX.Element {
  const [preview, setPreview] = useState<WorkspaceArtifactPreviewResult>()
  const [error, setError] = useState('')
  const [opening, setOpening] = useState(false)
  const [loading, setLoading] = useState(true)
  const [revision, setRevision] = useState(0)
  const actionInFlight = useRef(false)
  useEffect(() => {
    let active = true
    setPreview(undefined)
    setError('')
    setLoading(true)
    artifactReviewBridge.preview({ sessionId, path }).then(result => {
      if (active) setPreview(result)
    }).catch(cause => {
      if (active) setError(inspectorUserError(cause, '文件预览失败'))
    }).finally(() => { if (active) setLoading(false) })
    return () => { active = false }
  }, [sessionId, path, revision])

  const open = async (reveal: boolean) => {
    if (actionInFlight.current) return
    actionInFlight.current = true
    setOpening(true)
    setError('')
    try { await sessionFolderBridge.open({ sessionId, relativePath: path, reveal }) }
    catch (cause) { setError(inspectorUserError(cause, '无法打开文件')) }
    finally { actionInFlight.current = false; setOpening(false) }
  }
  const location = preview?.absolutePath ?? path
  const previewReady = !!preview && artifactPreviewIsReady(preview.kind, preview.path, preview.content)
  return <section className={`artifact-inspector${expanded ? ' is-expanded' : ''}`} aria-label="产物详情">
    <header className="artifact-inspector-chrome">
      <div className="artifact-inspector-title">
        <b title={location}>{fileName(path)}</b>
      </div>
      <div className="artifact-inspector-tools">
        <button type="button" className="artifact-icon-btn" aria-label="刷新预览" title="刷新" onClick={() => setRevision(value => value + 1)}>↻</button>
        <button type="button" className="artifact-icon-btn" aria-label="本机打开" title="本机打开" disabled={opening} onClick={() => void open(false)}>↗</button>
        <button type="button" className="artifact-icon-btn" aria-label="所在文件夹" title="所在文件夹" disabled={opening} onClick={() => void open(true)}>▢</button>
        {onToggleExpand && <button type="button" className="artifact-icon-btn" aria-label={expanded ? '恢复对话' : '放大预览'} title={expanded ? '恢复对话' : '放大'} onClick={onToggleExpand}>{expanded ? '❐' : '⛶'}</button>}
        <button type="button" className="artifact-icon-btn" aria-label="关闭产物详情" title="关闭" onClick={onClose}>×</button>
      </div>
    </header>
    {error && <p role="alert">{error}</p>}
    {loading && <p role="status">正在读取文件…</p>}
    {preview?.notice && !previewReady && <p className="artifact-inspector-notice" role="status">{preview.notice}</p>}
    {preview && <ArtifactPreviewContent preview={preview} />}
  </section>
}

export function ArtifactPreviewContent({ preview }: { preview: WorkspaceArtifactPreviewResult }): React.JSX.Element | null {
  const mode = artifactViewMode(preview.kind, preview.path)
  if (mode === 'image') {
    if (/^data:image\/(png|jpeg|gif);base64,[A-Za-z0-9+/=]+$/.test(preview.content)) {
      return <img className="artifact-inspector-image" src={preview.content} alt={fileName(preview.path)} />
    }
    if (!preview.content || /^(?:https?:|data:)/i.test(preview.content.trim())) {
      return <p role="alert">图片预览格式无效，请用本机软件打开</p>
    }
    return <pre className="artifact-inspector-text">{preview.content}</pre>
  }
  if (mode === 'html') {
    if (!preview.content) return null
    return <iframe className="artifact-inspector-frame" title={`产物预览 ${preview.path}`} sandbox="" referrerPolicy="no-referrer" srcDoc={isolatedHTML(preview.content)} />
  }
  if (mode === 'sheet') {
    if (!preview.content) return null
    try {
      const parsed: unknown = JSON.parse(preview.content)
      if (!parsed || typeof parsed !== 'object' || !('sheets' in parsed) || !Array.isArray(parsed.sheets)) throw new Error('invalid sheets')
      return <div className="artifact-inspector-sheets">{parsed.sheets.slice(0, 32).map((sheet: unknown, index) => {
        if (!sheet || typeof sheet !== 'object' || !('preview' in sheet) || !Array.isArray(sheet.preview)) return null
        const name = 'name' in sheet && typeof sheet.name === 'string' ? sheet.name : `工作表 ${index + 1}`
        return <section key={index}><h4>{name}</h4><div className="artifact-inspector-grid"><table><tbody>{sheet.preview.slice(0, 40).map((row: unknown, ri) => Array.isArray(row) ? <tr key={ri}>{row.slice(0, 50).map((cell: unknown, ci) => <td key={ci}>{typeof cell === 'string' || typeof cell === 'number' ? cell : ''}</td>)}</tr> : null)}</tbody></table></div></section>
      })}</div>
    } catch {
      return preview.content
        ? <pre className="artifact-inspector-text">{preview.content}</pre>
        : <p role="alert">表格预览格式无效，请用本机软件打开</p>
    }
  }
  if (mode === 'pdf') {
    if (preview.content && artifactLooksLikePdfBytes(preview.content)) {
      return <ArtifactPdfFrame path={preview.path} content={preview.content} />
    }
    return preview.content ? <pre className="artifact-inspector-text">{preview.content}</pre> : null
  }
  if (!preview.content) return null
  if (mode === 'markdown') {
    return <article className="artifact-inspector-md message-body" aria-label="文档预览">
      <MarkdownMessage text={preview.content} />
    </article>
  }
  if (mode === 'code') return <ArtifactCodeFrame path={preview.path} content={preview.content} />
  if (mode === 'paper') {
    return <article className="artifact-inspector-paper" aria-label="文档预览"><pre>{preview.content}</pre></article>
  }
  return <pre className="artifact-inspector-text">{preview.content}</pre>
}

function ArtifactCodeFrame({ path, content }: { path: string; content: string }): React.JSX.Element {
  const lines = content.split('\n')
  return <div className="artifact-inspector-code" aria-label="源代码预览">
    <pre className="artifact-inspector-gutter" aria-hidden="true">{lines.map((_, index) => `${index + 1}\n`).join('')}</pre>
    <pre className="artifact-inspector-source"><code data-lang={languageFromPath(path)}>{content || ' '}</code></pre>
  </div>
}

function ArtifactPdfFrame({ path, content }: { path: string; content: string }): React.JSX.Element {
  const url = useMemo(() => {
    try {
      const binary = atob(content.trim())
      const bytes = new Uint8Array(binary.length)
      for (let i = 0; i < binary.length; i++) bytes[i] = binary.charCodeAt(i)
      return URL.createObjectURL(new Blob([bytes], { type: 'application/pdf' }))
    } catch {
      return ''
    }
  }, [content])
  useEffect(() => () => { if (url) URL.revokeObjectURL(url) }, [url])
  if (!url) return <pre className="artifact-inspector-text">无法解析 PDF 预览，请用本机软件打开</pre>
  return <iframe className="artifact-inspector-frame" title={`产物预览 ${path}`} src={url} />
}
