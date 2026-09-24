import React, { useEffect, useMemo, useRef, useState } from 'react'
import {
  browserBridge,
  createLocalWorkspaceBridge,
  type AttachmentBridge,
  type BrowserBridge,
  type LocalWorkspaceBridge,
  type StreamArtifact,
  type SkillBridge,
} from '../bridge/client'
import type { AttachmentGetResult, AttachmentListResult } from '../generated/bridge'
import { TerminalPanel } from '../terminal/TerminalPanel'
import { ChangesPanel } from './ChangesPanel'
import { CodePanel } from './CodePanel'
import { CoordinationPlanPanel } from './CoordinationPlanPanel'
import { PlanDagPanel } from './PlanDagPanel'
import { FilesPanel, type FilesFocus, type SkillFilePreview } from './FilesPanel'
import { SkillPackagePanel } from '../skill/SkillPackagePanel'
import { LocalExplorer } from './LocalExplorer'
import { SessionFolderPanel } from './SessionFolderPanel'
import { ArtifactPanel, type ArtifactCard } from './ArtifactPanel'
import { ArtifactPreviewContent } from './ArtifactInspector'
import { previewKindFromPath } from './artifactPreviewMode'
import { SafeLinkedText } from './safeLinks'
import { isBrowserAddress, latestBrowserAddress, memberCatalogPage, parseSearchCards } from './browserAddress'
import { extractTaskFiles, isChangeTool } from './codePanelUtils'
import { workspaceDownloadEnabled } from './workspaceDownload'

function workspaceUserError(err: unknown, fallback: string): string {
  const detail = err instanceof Error ? err.message.trim() : ''
  return /[\u4e00-\u9fff]/.test(detail) ? detail : fallback
}

function panelPage(url: string): boolean {
  return isBrowserAddress(url) && (url.startsWith('https://') || url.startsWith('http://'))
}

function rememberBrowserURL(current: { urls: string[]; index: number }, url: string) {
  if (current.urls[current.index] === url) return current
  const urls = current.urls.slice(0, current.index + 1)
  urls.push(url)
  return { urls, index: urls.length - 1 }
}

export type WorkspaceTab = 'files' | 'code' | 'browser' | 'terminal' | 'plan' | 'changes'
/** @deprecated legacy tab ids mapped in normalizeWorkspaceTab */
export type LegacyWorkspaceTab = WorkspaceTab | 'preview' | 'develop'

export type WorkspaceToolActivity = {
  callId: string
  name: string
  status: string
  summary?: string
  artifact?: StreamArtifact
}

export function normalizeWorkspaceTab(tab?: LegacyWorkspaceTab): WorkspaceTab | undefined {
  if (!tab) return undefined
  if (tab === 'preview') return 'files'
  if (tab === 'develop') return 'code'
  return tab
}

export const workspaceTabForTool = (name: string): WorkspaceTab | undefined => {
  if (name === 'command.run' || name.startsWith('terminal.') || name.startsWith('cmd.') || name.startsWith('cli.')) {
    return 'terminal'
  }
  if (name === 'subagent.spawn' || name === 'subagent.join') return undefined
  if (
    name === 'web.search' || name === 'web.fetch' || name === 'browser.open' || name === 'html.gen'
    || name.startsWith('browser.') || name.startsWith('website.')
  ) {
    return 'browser'
  }
  if (name === 'pptx.gen' || name === 'docx.gen' || name === 'excel.gen' || name === 'pdf.gen') return 'files'
  if (/^workspace\.(write|edit|read|search)/.test(name) || name.startsWith('fs.')) return 'code'
  return undefined
}

export const autoRevealWorkspaceTab = (name: string, userWantsBrowser = false): WorkspaceTab | undefined => {
  const tab = workspaceTabForTool(name)
  if (!tab || tab === 'terminal' || tab === 'files' || tab === 'code') return undefined
  if (tab === 'browser') {
    if (name === 'browser.open') return 'browser'
    return userWantsBrowser ? 'browser' : undefined
  }
  return tab
}

export const autoRevealWorkspaceForHtmlTool = (name: string, userWantsBrowser = false): WorkspaceTab | undefined =>
  name === 'browser.open' || userWantsBrowser ? 'browser' : undefined

const TABS: Array<{ id: WorkspaceTab; label: string }> = [
  { id: 'files', label: '文件' },
  { id: 'code', label: '代码' },
  { id: 'terminal', label: '终端' },
  { id: 'browser', label: '浏览器' },
  { id: 'changes', label: '变更' },
  { id: 'plan', label: '计划' },
]

const MIN_ZOOM = 50
const MAX_ZOOM = 200
const ZOOM_STEP = 25

const fmtSize = (n: number) =>
  n < 1024 ? `${n} B` : n < 1048576 ? `${(n / 1024).toFixed(1)} KB` : `${(n / 1048576).toFixed(1)} MB`

export { isolatedHTML } from './isolatedHTML'
import { isolatedHTML } from './isolatedHTML'

export function Workspace({
  attachments,
  projectId,
  sessionId,
  onClose,
  expanded = false,
  onToggleExpand,
  browser = browserBridge,
  localWorkspace,
  targetTab,
  targetPath,
  toolActivities = [],
  refreshRevision = 0,
  onRevise,
  executionMode = 'auto-edit',
  filesFocus = 'session',
  isolateRoot = false,
  projectRoot,
  showPlanDag = false,
  onOpenApproval,
  skillId,
  skillRevision,
  skills,
  onCloseSkill,
}: {
  attachments: AttachmentBridge
  projectId: string
  sessionId: string
  onClose: () => void
  expanded?: boolean
  onToggleExpand?: () => void
  browser?: BrowserBridge
  localWorkspace?: LocalWorkspaceBridge
  targetTab?: LegacyWorkspaceTab
  targetPath?: string
  toolActivities?: WorkspaceToolActivity[]
  refreshRevision?: number
  onRevise?: (path: string, note: string) => void
  providerId?: string
  modelId?: string
  executionMode?: 'approval' | 'auto-edit' | 'full-access'
  filesFocus?: FilesFocus
  isolateRoot?: boolean
  projectRoot?: string
  showPlanDag?: boolean
  onOpenApproval?: () => void
  skillRevision?: number
  skillId?: string
  skills?: SkillBridge
  onCloseSkill?: () => void
}): React.JSX.Element {
  const initialTab = normalizeWorkspaceTab(targetTab) ?? 'files'
  const [tab, setTab] = useState<WorkspaceTab>(initialTab)
  const [items, setItems] = useState<AttachmentListResult['items']>([])
  const [selectedId, setSelectedId] = useState('')
  const [detail, setDetail] = useState<AttachmentGetResult>()
  const [localDetail, setLocalDetail] = useState<{ path: string; content: string; size: number }>()
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')
  const [zoom, setZoom] = useState(100)
  const [browserURL, setBrowserURL] = useState('https://')
  const [browserStatus, setBrowserStatus] = useState('未打开')
  const [browserBusy, setBrowserBusy] = useState(false)
  const [frameKey, setFrameKey] = useState(0)
  const [trail, setTrail] = useState<{ urls: string[]; index: number }>({ urls: [], index: -1 })
  const request = useRef(0)
  const local = useRef<LocalWorkspaceBridge | undefined>(localWorkspace)
  const filesStageRef = useRef<HTMLDivElement>(null)
  const filesDragCleanup = useRef<(() => void) | undefined>(undefined)
  const [treeOpen, setTreeOpen] = useState(true)
  const [treeWidth, setTreeWidth] = useState(260)
  const [skillDetail, setSkillDetail] = useState<SkillFilePreview>()

  const localBridge = () => {
    if (local.current) return local.current
    try {
      return (local.current = createLocalWorkspaceBridge())
    } catch {
      return undefined
    }
  }

  const changeCount = useMemo(() => extractTaskFiles(toolActivities).length, [toolActivities])
  const terminalCount = toolActivities.filter(a => workspaceTabForTool(a.name) === 'terminal' && a.status !== 'tool_completed').length

  useEffect(() => {
    const next = normalizeWorkspaceTab(targetTab)
    if (next) setTab(next)
  }, [targetTab])

  const openedMemberSong = useRef('')
  useEffect(() => {
    const next = latestBrowserAddress(toolActivities)
    if (next) {
      setBrowserURL(next)
      setBrowserStatus('已在此页打开')
      setTrail((current) => rememberBrowserURL(current, next))
      if (memberCatalogPage(next) && openedMemberSong.current !== next) {
        openedMemberSong.current = next
        void browser.open({ url: next }).catch(() => {})
      }
    }
  }, [browser, toolActivities])

  useEffect(() => {
    let active = true
    setLoading(true)
    setError('')
    void attachments.list({ projectId }).then(r => {
      if (!active) return
      const current = r.items.filter(item => item.sessionId === sessionId)
      setItems(current)
      setSelectedId(id => (current.some(x => x.attachmentId === id) ? id : (current[0]?.attachmentId ?? '')))
    }).catch(e => {
      if (active) setError(workspaceUserError(e, '附件载入失败'))
    }).finally(() => {
      if (active) setLoading(false)
    })
    return () => { active = false }
  }, [attachments, projectId, sessionId, refreshRevision])

  useEffect(() => {
    setDetail(undefined)
    setError('')
    if (!selectedId) return
    const current = ++request.current
    void attachments.get({ attachmentId: selectedId }).then(r => {
      if (current === request.current && r.sessionId === sessionId && r.projectId === projectId) setDetail(r)
      else if (current === request.current) setError('附件不属于当前会话')
    }).catch(e => {
      if (current === request.current) setError(workspaceUserError(e, '预览载入失败'))
    })
    return () => { request.current++ }
  }, [attachments, selectedId, projectId, sessionId])
  useEffect(() => () => filesDragCleanup.current?.(), [])

  const openBrowser = async () => {
    if (!isBrowserAddress(browserURL) || browserURL.startsWith('file:')) {
      setBrowserStatus('请输入可打开的 https 地址')
      return
    }
    setBrowserBusy(true)
    try {
      const result = await browser.open({ url: browserURL })
      setBrowserURL(result.url)
      setBrowserStatus('已接受，正在打开独立浏览器窗口')
    } catch (e) {
      setBrowserStatus(workspaceUserError(e, '打开失败'))
    } finally {
      setBrowserBusy(false)
    }
  }

  const closeBrowser = async () => {
    setBrowserBusy(true)
    try {
      await browser.close()
      setBrowserStatus('已关闭')
    } catch (e) {
      setBrowserStatus(workspaceUserError(e, '关闭失败'))
    } finally {
      setBrowserBusy(false)
    }
  }

  const artifact = [...toolActivities].reverse().find(a => a.status === 'tool_completed' && a.artifact?.kind === 'html')?.artifact
  const searchBusy = toolActivities.some(a =>
    (a.name === 'web.search' || a.name === 'web.fetch') && (a.status === 'tool_started' || a.status === 'tool_output'),
  )
  const searchCards = artifact ? parseSearchCards(artifact.content ?? '') : { query: '', hits: [] as Array<{ title: string; url: string; snippet?: string }> }
  const browserHost = (() => { try { return new URL(browserURL).hostname } catch { return '' } })()
  const browserTabLabel = searchCards.query ? `搜索 · ${searchCards.query}` : browserHost || '安全浏览器'

  const showInPanel = (url: string) => {
    if (!isBrowserAddress(url) || url.startsWith('file:')) {
      setBrowserStatus('请输入可打开的 https 地址')
      return
    }
    setBrowserURL(url)
    setBrowserStatus('已在此页打开')
    setTrail((current) => rememberBrowserURL(current, url))
    setFrameKey((key) => key + 1)
  }

  const openSearchHit = (url: string) => {
    showInPanel(url)
  }

  const goBrowser = (step: -1 | 1) => {
    const next = trail.index + step
    const url = trail.urls[next]
    if (!url) return
    setTrail({ ...trail, index: next })
    setBrowserURL(url)
    setBrowserStatus('已在此页打开')
    setFrameKey((key) => key + 1)
  }

  const artifactCards: ArtifactCard[] = toolActivities
    .filter(a => a.status === 'tool_completed' && a.artifact && a.artifact.kind !== 'image')
    .map(a => ({
      callId: a.callId,
      toolName: a.name,
      kind: a.artifact!.kind,
      path: a.artifact!.path,
      content: a.artifact!.content ?? '',
    }))

  const catalogFocus = filesFocus === 'skills' || filesFocus === 'experts' || filesFocus === 'plugins' || filesFocus === 'assets'
  const skillsCatalog = filesFocus === 'skills'
  const catalogFiles = catalogFocus && !skillsCatalog ? <FilesPanel projectId={projectId} focus={filesFocus} /> : null
  const sessionFiles = filesFocus === 'session'
    ? (
      <SessionFolderPanel
        sessionId={sessionId}
        refreshKey={refreshRevision + artifactCards.length}
        onPreview={file => {
          setLocalDetail(file)
          setDetail(undefined)
        }}
      />
    )
    : null
  const localFiles = localBridge()
  const localTree = !catalogFocus && localFiles
    ? (
      <LocalExplorer
        bridge={localFiles}
        sessionId={sessionId}
        isolateRoot={isolateRoot && !projectRoot}
        projectRoot={projectRoot}
        targetPath={targetPath}
        refreshKey={refreshRevision}
        onPreview={file => {
          setLocalDetail(file)
          setDetail(undefined)
        }}
      />
    )
    : null

  const dragFilesTree = (event: React.PointerEvent<HTMLButtonElement>) => {
    event.preventDefault()
    const box = filesStageRef.current?.getBoundingClientRect()
    if (!box) return
    filesDragCleanup.current?.()
    const onMove = (move: PointerEvent) => {
      setTreeWidth(Math.min(480, Math.max(180, box.right - move.clientX)))
    }
    const onUp = () => {
      window.removeEventListener('pointermove', onMove)
      window.removeEventListener('pointerup', onUp)
      filesDragCleanup.current = undefined
    }
    window.addEventListener('pointermove', onMove)
    window.addEventListener('pointerup', onUp)
    filesDragCleanup.current = onUp
  }

  const tabLabel = (item: typeof TABS[number]) => {
    if (item.id === 'changes' && changeCount > 0) return `${item.label} · ${changeCount}`
    if (item.id === 'terminal' && terminalCount > 0) return `${item.label} · ${terminalCount}`
    return item.label
  }

  const previewName = localDetail?.path.split(/[/\\]/).pop() || detail?.originalName || ''
  const canDownload = Boolean(previewName) && workspaceDownloadEnabled({
    name: previewName,
    mime: detail?.mime,
    contentBase64: detail?.contentBase64,
    parsedText: detail?.parsedText,
    localText: localDetail?.content,
  })
  const downloadCurrent = () => {
    if (!canDownload) return
    const name = previewName || 'file.txt'
    const mime = detail?.mime || 'application/octet-stream'
    let href = ''
    if (detail?.contentBase64) href = `data:${mime};base64,${detail.contentBase64}`
    else if (workspaceDownloadEnabled({ name, mime: detail?.mime || 'text/plain', parsedText: detail?.parsedText, localText: localDetail?.content })) {
      href = URL.createObjectURL(new Blob([detail?.parsedText ?? localDetail?.content ?? ''], { type: 'text/plain;charset=utf-8' }))
    }
    if (!href) return
    const link = document.createElement('a')
    link.href = href
    link.download = name
    link.click()
    if (href.startsWith('blob:')) window.setTimeout(() => URL.revokeObjectURL(href), 1000)
  }

  return (
    <aside className="workspace" aria-label="统一工作区">
      <header className="workspace-chrome">
        <strong>{TABS.find(item => item.id === tab)?.label ?? '工作区'}</strong>
        {skillId&&onCloseSkill&&<button type="button" onClick={onCloseSkill}>返回会话文件</button>}
        <div className="workspace-chrome-tools">
          {onToggleExpand && (
            <button type="button" className="artifact-icon-btn" aria-label={expanded ? '恢复对话' : '放大工作区'} title={expanded ? '恢复对话' : '放大'} onClick={onToggleExpand}>{expanded ? '❐' : '⛶'}</button>
          )}
          <button type="button" className="artifact-icon-btn" aria-label="关闭工作区" title="收起" onClick={onClose}>×</button>
        </div>
      </header>
      <nav aria-label="工作区标签">
        {TABS.map(x => (
          <button type="button" role="tab" aria-selected={tab === x.id} key={x.id} onClick={() => setTab(x.id)}>
            {tabLabel(x)}
          </button>
        ))}
      </nav>

      {tab === 'files' && skillId && <SkillPackagePanel skillId={skillId} bridge={skills} refreshKey={skillRevision??refreshRevision} layout="split"/>}
      {tab === 'files' && !skillId && skillsCatalog && (
        <div
          className={`workspace-files-stage${treeOpen ? '' : ' is-tree-hidden'}`}
          ref={filesStageRef}
          style={{'--tree-width': `${treeWidth}px`} as React.CSSProperties}
        >
          <div className="workspace-preview workspace-files-preview">
            <div className="workspace-preview-toolbar">
              <span className="workspace-file-name">{skillDetail?.path || '文件'}</span>
              <div className="workspace-chrome-tools">
                <button
                  type="button"
                  className="artifact-icon-btn"
                  aria-label={treeOpen ? '折叠文件树' : '显示文件树'}
                  title={treeOpen ? '折叠文件树' : '显示文件树'}
                  onClick={() => setTreeOpen(open => !open)}
                >
                  {treeOpen ? '◧' : '◨'}
                </button>
              </div>
            </div>
            {skillDetail ? (
              skillDetail.encoding === 'binary' ? (
                <p className="workspace-files-empty">这是二进制文件（{skillDetail.size} 字节），无法作为文本展示。</p>
              ) : (
                <div className="workspace-inline-preview" style={{ fontSize: `${zoom}%` }}>
                  <ArtifactPreviewContent sessionId={sessionId} preview={{ kind: previewKindFromPath(skillDetail.path), path: skillDetail.path, content: skillDetail.content, size: skillDetail.size }} />
                </div>
              )
            ) : (
              <div className="workspace-files-empty">
                <b>技能文件</b>
                <p>从右侧技能目录展开技能并选择文件即可预览</p>
              </div>
            )}
          </div>
          {treeOpen && (
            <>
              <button type="button" className="workspace-tree-resizer" role="separator" aria-orientation="vertical" aria-label="调整文件树宽度" onPointerDown={dragFilesTree} />
              <aside className="workspace-file-tree" aria-label="文件树">
                <FilesPanel
                  projectId={projectId}
                  focus="skills"
                  skills={skills}
                  refreshKey={skillRevision ?? refreshRevision}
                  onPreview={setSkillDetail}
                  selectedPath={skillDetail ? `skills/${skillDetail.skillId}/${skillDetail.path}` : undefined}
                />
              </aside>
            </>
          )}
        </div>
      )}
      {tab === 'files' && !skillId && catalogFocus && catalogFiles}
      {tab === 'files' && !skillId && !catalogFocus && (
        <div
          className={`workspace-files-stage${treeOpen ? '' : ' is-tree-hidden'}`}
          ref={filesStageRef}
          style={{'--tree-width': `${treeWidth}px`} as React.CSSProperties}
        >
          <div className="workspace-preview workspace-files-preview">
            <div className="workspace-preview-toolbar">
              <span className="workspace-file-name">{previewName || '文件'}</span>
              <div className="workspace-chrome-tools">
                <button type="button" className="artifact-icon-btn" aria-label="下载文件" title="下载" disabled={!canDownload} onClick={downloadCurrent}>↓</button>
                <button
                  type="button"
                  className="artifact-icon-btn"
                  aria-label={treeOpen ? '折叠文件树' : '显示文件树'}
                  title={treeOpen ? '折叠文件树' : '显示文件树'}
                  onClick={() => setTreeOpen(open => !open)}
                >
                  {treeOpen ? '◧' : '◨'}
                </button>
                <div className="workspace-zoom">
                  <button type="button" aria-label="缩小预览" disabled={zoom === MIN_ZOOM} onClick={() => setZoom(v => Math.max(MIN_ZOOM, v - ZOOM_STEP))}>−</button>
                  <output aria-label="预览缩放">{zoom}%</output>
                  <button type="button" aria-label="放大预览" disabled={zoom === MAX_ZOOM} onClick={() => setZoom(v => Math.min(MAX_ZOOM, v + ZOOM_STEP))}>＋</button>
                </div>
              </div>
            </div>
            {error && <p role="alert">{error}</p>}
            {localDetail ? (
              <div className="workspace-inline-preview" style={{ fontSize: `${zoom}%` }}>
                <ArtifactPreviewContent sessionId={sessionId} preview={{ kind: previewKindFromPath(localDetail.path), path: localDetail.path, content: localDetail.content, size: localDetail.size }} />
              </div>
            ) : detail ? (
              <article className="workspace-document" style={{ fontSize: `${zoom}%` }}>
                <h3>{detail.originalName}</h3>
                <dl>
                  <dt>类型</dt><dd>{detail.mime}</dd>
                  <dt>大小</dt><dd>{fmtSize(detail.size)}</dd>
                  <dt>解析状态</dt><dd>{detail.mime.startsWith('image/') ? '图片 · 可供视觉分析' : detail.parseStatus}</dd>
                </dl>
                {detail.mime.startsWith('image/') ? (
                  detail.contentBase64 ? (
                    <>
                      <p>图片 · 可供视觉分析</p>
                      <img alt={detail.originalName} src={`data:${detail.mime};base64,${detail.contentBase64}`} style={{maxWidth:'100%',borderRadius:8}} />
                    </>
                  ) : (
                    <p>图片已记录，但预览字节不可用（ATTACHMENT_IMAGE_BYTES_MISSING）。</p>
                  )
                ) : detail.parsedText !== undefined ? (
                  <pre><SafeLinkedText text={detail.parsedText} /></pre>
                ) : (
                  <p>
                    {detail.parseStatus === 'failed'
                      ? `此格式暂不支持内容预览${detail.parseErrorCode ? `（${detail.parseErrorCode}）` : ''}`
                      : '没有可预览的解析文本。'}
                  </p>
                )}
              </article>
            ) : (
              <div className="workspace-files-empty">
                <b>文件</b>
                <p>从文件树选择文件即可在此处预览</p>
                {loading && <p role="status">正在载入附件…</p>}
              </div>
            )}
            {artifactCards.length > 0 && <ArtifactPanel sessionId={sessionId} artifacts={artifactCards} onRevise={onRevise} />}
          </div>
          {treeOpen && (
            <>
              <button type="button" className="workspace-tree-resizer" role="separator" aria-orientation="vertical" aria-label="调整文件树宽度" onPointerDown={dragFilesTree} />
              <aside className="workspace-file-tree" aria-label="文件树">
                {items.length > 0 && (
                  <div className="workspace-attachments" role="listbox" aria-label="当前会话附件">
                    {items.map(item => (
                      <button
                        type="button"
                        role="option"
                        aria-selected={selectedId === item.attachmentId}
                        key={item.attachmentId}
                        onClick={() => {
                          setLocalDetail(undefined)
                          setSelectedId(item.attachmentId)
                        }}
                      >
                        <b>{item.originalName}</b>
                      </button>
                    ))}
                  </div>
                )}
                {sessionFiles}
                {localTree}
              </aside>
            </>
          )}
        </div>
      )}

      {tab === 'code' && (
        <CodePanel
          bridge={localBridge()}
          sessionId={sessionId}
          isolateRoot={isolateRoot && !projectRoot}
          projectRoot={projectRoot}
          targetPath={targetPath}
          toolActivities={toolActivities}
          refreshKey={refreshRevision}
        />
      )}

      {tab === 'changes' && <ChangesPanel toolActivities={toolActivities.filter(a => isChangeTool(a.name))} />}

      {tab === 'browser' && (
        <div className="workspace-browser">
          <div className="workspace-browser-chrome">
            <div className="workspace-browser-toolbar">
              <div className="workspace-browser-nav">
                <button type="button" aria-label="后退" disabled={trail.index <= 0} onClick={() => goBrowser(-1)}>←</button>
                <button type="button" aria-label="前进" disabled={trail.index < 0 || trail.index >= trail.urls.length - 1} onClick={() => goBrowser(1)}>→</button>
                <button type="button" className="artifact-icon-btn" aria-label="刷新地址" disabled={!browserURL.startsWith('https://')} onClick={() => setFrameKey((key) => key + 1)}>↻</button>
              </div>
              <label className="workspace-browser-address">
                <input aria-label="浏览器地址" type="text" inputMode="url" value={browserURL} onChange={e => setBrowserURL(e.target.value)} onKeyDown={e => { if (e.key === 'Enter') showInPanel(browserURL) }} />
              </label>
              <div className="workspace-browser-actions">
                <button className="artifact-icon-btn" type="button" aria-label="打开独立浏览器" title={browserTabLabel} disabled={browserBusy || !browserURL.startsWith('https://')} onClick={() => void openBrowser()}>↗</button>
                <button className="artifact-icon-btn" type="button" aria-label="关闭浏览器" title="关闭独立浏览器" disabled={browserBusy} onClick={() => void closeBrowser()}>×</button>
              </div>
            </div>
          </div>
          <output className="workspace-browser-status" aria-label="浏览器状态" role="status">{browserStatus}</output>
          <div className="workspace-browser-viewport">
            {searchCards.hits.length ? (
              <section className="workspace-search-results" aria-label="搜索结果">
                <header>
                  <b>{searchCards.query ? `搜索结果 · ${searchCards.query}` : artifact?.path ?? '搜索结果'}</b>
                  <small>点击结果在此页打开</small>
                </header>
                <ol>
                  {searchCards.hits.map(hit => (
                    <li key={hit.url}>
                      <button type="button" onClick={() => openSearchHit(hit.url)}>
                        <b>{hit.title}</b>
                        <small>{hit.url}</small>
                        {hit.snippet && <p>{hit.snippet}</p>}
                      </button>
                    </li>
                  ))}
                </ol>
              </section>
            ) : null}
            {panelPage(browserURL) ? (
              <iframe
                key={frameKey}
                className="workspace-browser-frame"
                title={`应用内页面 ${browserURL}`}
                src={browserURL}
                sandbox="allow-scripts allow-same-origin allow-forms allow-modals allow-popups allow-downloads"
                referrerPolicy="no-referrer"
              />
            ) : artifact ? (
              <section className="workspace-html-preview">
                <header>
                  <b>{artifact.path}</b>
                  <small>页面摘录</small>
                </header>
                <iframe title={`HTML 预览 ${artifact.path}`} sandbox="" referrerPolicy="no-referrer" srcDoc={isolatedHTML(artifact.content)} />
              </section>
            ) : searchBusy ? (
              <div className="workspace-browser-empty">
                <span aria-hidden="true">⌕</span>
                <b>正在检索网页…</b>
                <p>搜索结果会显示在这个预览里。</p>
              </div>
            ) : (
              <div className="workspace-browser-empty">
                <span aria-hidden="true">◫</span>
                <b>尚无页面</b>
                <p>输入 HTTPS 地址后回车，页面就在这里打开。↗ 仍可放到独立窗口。</p>
              </div>
            )}
          </div>
        </div>
      )}

      {tab === 'terminal' && (
        <TerminalPanel
          projectId={projectId}
          sessionId={sessionId}
          executionMode={executionMode}
          toolActivities={toolActivities.filter(a => workspaceTabForTool(a.name) === 'terminal')}
        />
      )}

      {tab === 'plan' && (
        showPlanDag
          ? <PlanDagPanel projectId={projectId} onOpenApproval={onOpenApproval} />
          : <CoordinationPlanPanel projectId={projectId} />
      )}
    </aside>
  )
}
