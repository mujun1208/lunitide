import React, { useEffect, useMemo, useRef, useState } from 'react'
import {
  browserBridge,
  createLocalWorkspaceBridge,
  type AttachmentBridge,
  type BrowserBridge,
  type LocalWorkspaceBridge,
  type StreamArtifact,
} from '../bridge/client'
import type { AttachmentGetResult, AttachmentListResult } from '../generated/bridge'
import { TerminalPanel } from '../terminal/TerminalPanel'
import { ChangesPanel } from './ChangesPanel'
import { CodePanel } from './CodePanel'
import { CoordinationPlanPanel } from './CoordinationPlanPanel'
import { PlanDagPanel } from './PlanDagPanel'
import { FilesPanel, type FilesFocus } from './FilesPanel'
import { LocalExplorer } from './LocalExplorer'
import { SessionFolderPanel } from './SessionFolderPanel'
import { ArtifactPanel, type ArtifactCard } from './ArtifactPanel'
import { SafeLinkedText } from './safeLinks'
import { isBrowserAddress, latestBrowserAddress, parseSearchCards } from './browserAddress'
import { extractTaskFiles, isChangeTool } from './codePanelUtils'

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
  if (!tab || tab === 'terminal' || tab === 'files') return undefined
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

export const isolatedHTML = (content: string) =>
  `<meta http-equiv="Content-Security-Policy" content="default-src 'none'; img-src data: blob:; style-src 'unsafe-inline'; font-src data:; media-src data: blob:; connect-src 'none'; frame-src 'none'; object-src 'none'; base-uri 'none'; form-action 'none'">${content}`

export function Workspace({
  attachments,
  projectId,
  sessionId,
  onClose,
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
  showPlanDag = false,
  onOpenApproval,
}: {
  attachments: AttachmentBridge
  projectId: string
  sessionId: string
  onClose: () => void
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
  showPlanDag?: boolean
  onOpenApproval?: () => void
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
  const request = useRef(0)
  const local = useRef<LocalWorkspaceBridge | undefined>(localWorkspace)

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

  useEffect(() => {
    const next = latestBrowserAddress(toolActivities)
    if (next) {
      setBrowserURL(next)
      setBrowserStatus('已打开')
    }
  }, [toolActivities])

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
      if (active) setError(e instanceof Error ? e.message : '附件载入失败')
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
      if (current === request.current) setError(e instanceof Error ? e.message : '预览载入失败')
    })
    return () => { request.current++ }
  }, [attachments, selectedId, projectId, sessionId])

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
      setBrowserStatus(e instanceof Error ? e.message : '打开失败')
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
      setBrowserStatus(e instanceof Error ? e.message : '关闭失败')
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

  const openSearchHit = async (url: string) => {
    setBrowserURL(url)
    setBrowserStatus('已打开')
    if (url.startsWith('https://')) {
      setBrowserBusy(true)
      try {
        const result = await browser.open({ url })
        setBrowserURL(result.url || url)
        setBrowserStatus('已在独立窗口打开')
      } catch (e) {
        setBrowserStatus(e instanceof Error ? e.message : '已更新地址栏')
      } finally {
        setBrowserBusy(false)
      }
    }
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
  const catalogFiles = <FilesPanel projectId={projectId} focus={filesFocus === 'session' ? undefined : filesFocus} />
  const sessionFiles = filesFocus === 'session'
    ? (
      <SessionFolderPanel
        sessionId={sessionId}
        onPreview={file => {
          setLocalDetail(file)
          setDetail(undefined)
          setTab('code')
        }}
      />
    )
    : null
  const localFiles = localBridge()
  const localTree = !catalogFocus && filesFocus !== 'session' && localFiles
    ? (
      <LocalExplorer
        bridge={localFiles}
        sessionId={sessionId}
        isolateRoot={isolateRoot}
        targetPath={targetPath}
        onPreview={file => {
          setLocalDetail(file)
          setDetail(undefined)
          setTab('code')
        }}
      />
    )
    : null

  const tabLabel = (item: typeof TABS[number]) => {
    if (item.id === 'changes' && changeCount > 0) return `${item.label} · ${changeCount}`
    if (item.id === 'terminal' && terminalCount > 0) return `${item.label} · ${terminalCount}`
    return item.label
  }

  return (
    <aside className="workspace" aria-label="统一工作区">
      <header>
        <strong>工作区</strong>
        <button type="button" aria-label="关闭工作区" onClick={onClose}>收起</button>
      </header>
      <nav aria-label="工作区标签">
        {TABS.map(x => (
          <button type="button" role="tab" aria-selected={tab === x.id} key={x.id} onClick={() => setTab(x.id)}>
            {tabLabel(x)}
          </button>
        ))}
      </nav>

      {tab === 'files' && (
        <>
          {sessionFiles}
          {catalogFocus ? catalogFiles : null}
          {localTree ?? (catalogFocus || filesFocus === 'session' ? null : (
            <p className="workspace-unavailable" role="status">本地工作区暂不可用。</p>
          ))}
          <div className="workspace-preview workspace-files-preview">
            <div className="workspace-preview-toolbar">
              <span>{items.length ? `${items.length} 个附件` : '附件预览'}</span>
              <div className="workspace-zoom">
                <button type="button" aria-label="缩小预览" disabled={zoom === MIN_ZOOM} onClick={() => setZoom(v => Math.max(MIN_ZOOM, v - ZOOM_STEP))}>−</button>
                <output aria-label="预览缩放">{zoom}%</output>
                <button type="button" aria-label="放大预览" disabled={zoom === MAX_ZOOM} onClick={() => setZoom(v => Math.min(MAX_ZOOM, v + ZOOM_STEP))}>＋</button>
              </div>
            </div>
            {loading ? <p role="status">正在载入附件…</p> : items.length ? (
              <div className="workspace-attachments" role="listbox" aria-label="当前会话附件">
                {items.map(item => (
                  <button
                    type="button"
                    role="option"
                    aria-selected={selectedId === item.attachmentId}
                    key={item.attachmentId}
                    onClick={() => setSelectedId(item.attachmentId)}
                  >
                    <b>{item.originalName}</b>
                    <small>{item.mime} · {fmtSize(item.size)}</small>
                  </button>
                ))}
              </div>
            ) : <p>当前会话没有附件。</p>}
            {error && <p role="alert">{error}</p>}
            {localDetail ? (
              <article className="workspace-document" style={{ fontSize: `${zoom}%` }}>
                <h3>{localDetail.path}</h3>
                <small>{fmtSize(localDetail.size)} · 本地工作区只读预览</small>
                <pre><SafeLinkedText text={localDetail.content} /></pre>
              </article>
            ) : detail && (
              <article className="workspace-document" style={{ fontSize: `${zoom}%` }}>
                <h3>{detail.originalName}</h3>
                <dl>
                  <dt>类型</dt><dd>{detail.mime}</dd>
                  <dt>大小</dt><dd>{fmtSize(detail.size)}</dd>
                  <dt>解析状态</dt><dd>{detail.parseStatus}</dd>
                </dl>
                {detail.mime.startsWith('image/') ? (
                  <p>此附件是图片。当前版本可将它交给视觉模型分析，但工作区尚未取得原图字节，暂不能在这里显示。</p>
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
            )}
            <ArtifactPanel sessionId={sessionId} artifacts={artifactCards} onRevise={onRevise} />
          </div>
        </>
      )}

      {tab === 'code' && (
        <CodePanel
          bridge={localBridge()}
          sessionId={sessionId}
          isolateRoot={isolateRoot}
          targetPath={targetPath}
          toolActivities={toolActivities}
        />
      )}

      {tab === 'changes' && <ChangesPanel toolActivities={toolActivities.filter(a => isChangeTool(a.name))} />}

      {tab === 'browser' && (
        <div className="workspace-browser">
          <div className="workspace-browser-chrome">
            <div className="workspace-browser-tabs" role="tablist" aria-label="浏览器页面">
              <div className="workspace-browser-tab" role="tab" aria-selected="true">
                <span aria-hidden="true">▣</span>
                <b>{browserTabLabel}</b>
              </div>
            </div>
            <div className="workspace-browser-toolbar">
              <div className="workspace-browser-nav" aria-hidden="true">
                <button type="button" tabIndex={-1} disabled>←</button>
                <button type="button" tabIndex={-1} disabled>→</button>
                <button type="button" tabIndex={-1} disabled>↻</button>
              </div>
              <label className="workspace-browser-address">
                <span aria-hidden="true">⌾</span>
                <input aria-label="浏览器地址" type="text" inputMode="url" value={browserURL} onChange={e => setBrowserURL(e.target.value)} />
              </label>
              <div className="workspace-browser-actions">
                <button className="workspace-browser-open" type="button" aria-label="打开独立浏览器" disabled={browserBusy || !browserURL.startsWith('https://')} onClick={() => void openBrowser()}>↗ 独立打开</button>
                <button className="workspace-browser-close" type="button" aria-label="关闭浏览器" title="关闭独立浏览器" disabled={browserBusy} onClick={() => void closeBrowser()}>关闭</button>
              </div>
            </div>
          </div>
          <div className="workspace-browser-meta">
            <output aria-label="浏览器状态" role="status"><i aria-hidden="true" />{browserStatus}</output>
            <small>隔离 WebView2 · 地址栏同步导航</small>
          </div>
          <div className="workspace-browser-viewport">
            {searchCards.hits.length ? (
              <section className="workspace-search-results" aria-label="搜索结果">
                <header>
                  <b>{searchCards.query ? `搜索结果 · ${searchCards.query}` : artifact?.path ?? '搜索结果'}</b>
                  <small>点击结果会更新地址栏</small>
                </header>
                <ol>
                  {searchCards.hits.map(hit => (
                    <li key={hit.url}>
                      <button type="button" onClick={() => void openSearchHit(hit.url)}>
                        <b>{hit.title}</b>
                        <small>{hit.url}</small>
                        {hit.snippet && <p>{hit.snippet}</p>}
                      </button>
                    </li>
                  ))}
                </ol>
              </section>
            ) : artifact ? (
              <section className="workspace-html-preview">
                <header>
                  <b>{isBrowserAddress(browserURL) ? browserURL : artifact.path}</b>
                  <small>安全摘录预览 · 原文点「独立打开」</small>
                </header>
                <iframe title={`HTML 预览 ${artifact.path}`} sandbox="" referrerPolicy="no-referrer" srcDoc={isolatedHTML(artifact.content)} />
              </section>
            ) : searchBusy ? (
              <div className="workspace-browser-empty">
                <span aria-hidden="true">⌕</span>
                <b>正在检索网页…</b>
                <p>搜索结果会显示在这个安全预览里，不会打开系统浏览器。</p>
              </div>
            ) : isBrowserAddress(browserURL) ? (
              <div className="workspace-browser-empty">
                <span aria-hidden="true">⧉</span>
                <b>该网页无法嵌进预览</b>
                <p>地址栏已同步到 {browserURL}。点「独立打开」在隔离窗口查看原文。</p>
              </div>
            ) : (
              <div className="workspace-browser-empty">
                <span aria-hidden="true">◫</span>
                <b>尚无 HTML 预览</b>
                <p>网页搜索和抓取的结果会安全显示在这里；输入 HTTPS 地址可在独立隔离窗口打开。</p>
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
