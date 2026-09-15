import React, { useEffect, useMemo, useRef, useState } from 'react'
import type { LocalWorkspaceBridge } from '../bridge/client'
import { LocalExplorer } from './LocalExplorer'
import type { WorkspaceToolActivity } from './Workspace'
import { openWorkspaceInEditor } from '../project/projectWorkbenchNav'
import {
  extractTaskFiles,
  languageFromPath,
  statusBadge,
  type TaskFileEntry,
} from './codePanelUtils'

type OpenFile = { path: string; content: string; size: number }

function CodeEditorView({ file }: { file: OpenFile }): React.JSX.Element {
  const lines = file.content.split('\n')
  const lang = languageFromPath(file.path)
  return (
    <div className="code-editor">
      <header className="code-editor-head">
        <b>{file.path.split(/[/\\]/).pop()}</b>
        <small>{file.path} · {lang} · UTF-8</small>
      </header>
      <div className="code-editor-body">
        <pre className="code-editor-gutter" aria-hidden="true">
          {lines.map((_, index) => `${index + 1}\n`).join('')}
        </pre>
        <pre className="code-editor-content"><code>{file.content || ' '}</code></pre>
      </div>
      <footer className="code-editor-foot" role="status">
        <span>✓ 0 问题</span>
        <span>Ln 1, Col 1</span>
        <span>Agent 变更 · 尚未提交</span>
      </footer>
    </div>
  )
}

export function CodePanel({
  bridge,
  sessionId,
  isolateRoot = false,
  projectRoot,
  targetPath,
  toolActivities = [],
  onOpenPath,
}: {
  bridge?: LocalWorkspaceBridge
  sessionId: string
  isolateRoot?: boolean
  projectRoot?: string
  targetPath?: string
  toolActivities?: WorkspaceToolActivity[]
  onOpenPath?: (path: string) => void
}): React.JSX.Element {
  const [file, setFile] = useState<OpenFile | undefined>()
  const [openError, setOpenError] = useState('')
  const [treeOpen, setTreeOpen] = useState(true)
  const [treeWidth, setTreeWidth] = useState(260)
  const splitRef = useRef<HTMLDivElement>(null)
  const dragCleanup = useRef<(() => void) | undefined>(undefined)
  const taskFiles = useMemo(() => extractTaskFiles(toolActivities), [toolActivities])
  useEffect(() => () => dragCleanup.current?.(), [])

  const openFile = async (entry: TaskFileEntry) => {
    if (!bridge) return
    try {
      const read = await bridge.read(entry.path)
      setFile(read)
      onOpenPath?.(entry.path)
    } catch {
      setFile({ path: entry.path, content: entry.summary ?? `# ${entry.path}\n\n（无法读取文件内容，仅显示工具摘要）`, size: 0 })
    }
  }

  useEffect(() => {
    if (!targetPath || !bridge) return
    void bridge.read(targetPath).then(setFile).catch(() => {})
  }, [bridge, targetPath])

  useEffect(() => {
    if (file || !taskFiles.length || !bridge) return
    void openFile(taskFiles[taskFiles.length - 1]!)
  }, [taskFiles.length, bridge])

  const dragTree = (event: React.PointerEvent<HTMLButtonElement>) => {
    event.preventDefault()
    const box = splitRef.current?.getBoundingClientRect()
    if (!box) return
    dragCleanup.current?.()
    const onMove = (move: PointerEvent) => {
      setTreeWidth(Math.min(480, Math.max(180, box.right - move.clientX)))
    }
    const onUp = () => {
      window.removeEventListener('pointermove', onMove)
      window.removeEventListener('pointerup', onUp)
      dragCleanup.current = undefined
    }
    window.addEventListener('pointermove', onMove)
    window.addEventListener('pointerup', onUp)
    dragCleanup.current = onUp
  }

  return (
    <div className={`code-panel${treeOpen ? '' : ' is-tree-hidden'}`}>
      <header className="code-panel-toolbar">
        <span className="code-panel-title">{file?.path.split(/[/\\]/).pop() || '代码'}</span>
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
          <button
            type="button"
            className="artifact-icon-btn code-panel-vscode"
            disabled={!bridge}
            aria-label="在 VS Code 中打开"
            title="在 VS Code 中打开当前文件或工作区根目录"
            onClick={() => {
              if (!bridge) return
              setOpenError('')
              void openWorkspaceInEditor(bridge, file?.path)
                .catch(() => setOpenError('无法在 VS Code 中打开；已尝试资源管理器回退'))
            }}
          >
            ↗
          </button>
        </div>
      </header>
      {openError && <p className="code-panel-open-error" role="alert">{openError}</p>}
      <div className="code-panel-split" ref={splitRef} style={{'--tree-width': `${treeWidth}px`} as React.CSSProperties}>
        <section className="code-panel-editor" aria-label="代码编辑器">
          {file ? <CodeEditorView file={file} /> : (
            <div className="code-panel-placeholder">
              <b>代码</b>
              <p>从文件树选择文件即可在此处预览</p>
            </div>
          )}
        </section>
        {treeOpen && (
          <>
            <button type="button" className="workspace-tree-resizer" role="separator" aria-orientation="vertical" aria-label="调整文件树宽度" onPointerDown={dragTree} />
            <aside className="code-panel-tree" aria-label="文件树">
              {taskFiles.length > 0 && (
                <ul className="code-task-files">
                  {taskFiles.map(entry => (
                    <li key={entry.path}>
                      <button
                        type="button"
                        className={file?.path === entry.path ? 'on' : ''}
                        onClick={() => void openFile(entry)}
                      >
                        <span className={`code-file-badge status-${entry.status}`} aria-hidden="true">
                          {statusBadge(entry.status)}
                        </span>
                        <span className="code-file-path" title={entry.path}>{entry.path.replace(/\\/g, '/')}</span>
                      </button>
                    </li>
                  ))}
                </ul>
              )}
              {bridge ? (
                <LocalExplorer
                  bridge={bridge}
                  sessionId={sessionId}
                  isolateRoot={isolateRoot && !projectRoot}
                  projectRoot={projectRoot}
                  targetPath={targetPath ?? file?.path}
                  onPreview={next => {
                  setFile(next)
                  onOpenPath?.(next.path)
                  }}
                />
              ) : null}
            </aside>
          </>
        )}
      </div>
    </div>
  )
}
