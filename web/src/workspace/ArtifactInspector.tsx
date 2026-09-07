import React, { useEffect, useRef, useState } from 'react'
import { artifactReviewBridge, sessionFolderBridge } from '../bridge/client'
import type { WorkspaceArtifactPreviewResult } from '../generated/bridge'
import { isolatedHTML } from './isolatedHTML'
import './artifactInspector.css'

export function ArtifactInspector({ sessionId, path, onClose }: {
  sessionId: string; path: string; onClose: () => void
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
      if (active) setError(cause instanceof Error ? cause.message : '文件预览失败')
    }).finally(() => { if (active) setLoading(false) })
    return () => { active = false }
  }, [sessionId, path, revision])

  const open = async (reveal: boolean) => {
    if (actionInFlight.current) return
    actionInFlight.current = true
    setOpening(true)
    setError('')
    try { await sessionFolderBridge.open({ sessionId, relativePath: path, reveal }) }
    catch (cause) { setError(cause instanceof Error ? cause.message : '无法打开文件') }
    finally { actionInFlight.current = false; setOpening(false) }
  }
  return <section className="artifact-inspector" aria-label="产物详情">
    <header><div><small>对话产物</small><h3>{path.split(/[/\\]/).pop()}</h3></div>
      <button type="button" aria-label="关闭产物详情" onClick={onClose}>×</button></header>
    <div className="artifact-inspector-actions">
      <button type="button" disabled={opening} onClick={() => void open(false)}>本机打开</button>
      <button type="button" disabled={opening} onClick={() => void open(true)}>所在文件夹</button>
      <button type="button" onClick={() => setRevision(value => value + 1)}>刷新预览</button>
    </div>
    <dl><dt>文件位置</dt><dd>{preview?.absolutePath ?? path}</dd>
      {preview?.size !== undefined && <><dt>大小</dt><dd>{preview.size < 1024 ? `${preview.size} B` : `${(preview.size / 1024).toFixed(1)} KB`}</dd></>}
    </dl>
    {error && <p role="alert">{error}</p>}
    {loading && <p role="status">正在读取文件…</p>}
    {preview?.notice && <p className="artifact-inspector-notice" role="status">{preview.notice}</p>}
    {preview && <ArtifactPreviewContent preview={preview} />}
  </section>
}

function ArtifactPreviewContent({ preview }: { preview: WorkspaceArtifactPreviewResult }): React.JSX.Element | null {
  if (!preview.content) return null
  if (preview.kind === 'image') {
    // Only the host's bounded raster data is displayed. Paths/URLs never become image requests.
    return /^data:image\/(png|jpeg|gif);base64,[A-Za-z0-9+/=]+$/.test(preview.content)
      ? <img className="artifact-inspector-image" src={preview.content} alt={preview.path.split(/[/\\]/).pop() ?? '图片产物'} />
      : <p role="alert">图片预览格式无效，请用本机软件打开</p>
  }
  if (preview.kind === 'html') return <iframe title={`产物预览 ${preview.path}`} sandbox="" referrerPolicy="no-referrer" srcDoc={isolatedHTML(preview.content)} />
  if (preview.kind === 'xlsx') {
    try {
      const parsed: unknown = JSON.parse(preview.content)
      if (!parsed || typeof parsed !== 'object' || !('sheets' in parsed) || !Array.isArray(parsed.sheets)) throw new Error('invalid sheets')
      return <div className="artifact-inspector-sheets">{parsed.sheets.slice(0, 32).map((sheet: unknown, index) => {
        if (!sheet || typeof sheet !== 'object' || !('preview' in sheet) || !Array.isArray(sheet.preview)) return null
        const name = 'name' in sheet && typeof sheet.name === 'string' ? sheet.name : `工作表 ${index + 1}`
        return <section key={index}><h4>{name}</h4><div className="artifact-inspector-grid"><table><tbody>{sheet.preview.slice(0, 40).map((row: unknown, ri) => Array.isArray(row) ? <tr key={ri}>{row.slice(0, 50).map((cell: unknown, ci) => <td key={ci}>{typeof cell === 'string' || typeof cell === 'number' ? cell : ''}</td>)}</tr> : null)}</tbody></table></div></section>
      })}</div>
    } catch { return <p role="alert">表格预览格式无效，请用本机软件打开</p> }
  }
  return <pre className="artifact-inspector-text">{preview.content}</pre>
}
