import React, { useEffect, useMemo, useRef, useState } from 'react'
import type { LocalWorkspaceBridge } from '../bridge/client'
import { LocalExplorer } from './LocalExplorer'
import type { WorkspaceToolActivity } from './Workspace'
import { openWorkspaceInEditor } from '../project/projectWorkbenchNav'
import { CodeWorkbench, type CodeProblem } from './CodeWorkbench'
import {
  acceptSourceLine,
  extractTaskFiles,
  latestFileDiff,
  statusBadge,
  suggestSourceLine,
  type TaskFileEntry,
} from './codePanelUtils'

type OpenFile = { path: string; content: string; size: number }

function CodeEditorView({
  file,
  projectRoot,
  sessionId,
  bridge,
  diff,
  onContent,
}: {
  file: OpenFile
  projectRoot?: string
  sessionId: string
  bridge?: LocalWorkspaceBridge
  diff: string
  onContent: (content: string) => void
}): React.JSX.Element {
  const [problems, setProblems] = useState<CodeProblem[]>([])
  const [suggestion, setSuggestion] = useState('')
  const [stoppedLine, setStoppedLine] = useState(0)
  const lineIndex = file.content.split('\n').findIndex((_, index) => suggestSourceLine(file.content, index) !== '')
  useEffect(() => {
    setSuggestion(lineIndex >= 0 ? suggestSourceLine(file.content, lineIndex) : '')
    setProblems([])
    setStoppedLine(0)
    if (!bridge?.code || !projectRoot) return
    let cancel = false
    void bridge.code({
      action: 'diagnostics',
      root: projectRoot,
      path: file.path,
      content: file.content,
      sessionId,
    }).then((result) => {
      if (!cancel) setProblems(result.diagnostics ?? [])
    }).catch(() => {})
    if (lineIndex >= 0) {
      void bridge.code({
        action: 'complete',
        root: projectRoot,
        path: file.path,
        line: lineIndex + 1,
        content: file.content,
        sessionId,
      }).then((result) => {
        if (!cancel && result.suggestion) setSuggestion(result.suggestion)
      }).catch(() => {})
    }
    return () => { cancel = true }
  }, [bridge, file.content, file.path, lineIndex, projectRoot, sessionId])
  return (
    <CodeWorkbench
      path={file.path}
      content={file.content}
      problems={problems}
      suggestion={suggestion}
      stoppedLine={stoppedLine}
      diff={diff}
      onAccept={() => {
        if (!suggestion || lineIndex < 0) return
        const next = acceptSourceLine(file.content, lineIndex, suggestion)
        onContent(next)
        if (bridge?.code && projectRoot) {
          void bridge.code({
            action: 'complete',
            root: projectRoot,
            path: file.path,
            line: lineIndex + 1,
            content: file.content,
            accept: true,
            sessionId,
          })
        }
      }}
      onDefine={() => {
        if (!bridge?.code || !projectRoot) return
        const sourceLine = file.content.split('\n')[Math.max(lineIndex, 0)] ?? ''
        const column = Math.max(sourceLine.search(/\S/) + 1, 1)
        void bridge.code({
          action: 'definition',
          root: projectRoot,
          path: file.path,
          line: Math.max(lineIndex + 1, 1),
          column,
          content: file.content,
          sessionId,
        }).then((result) => {
          if (result.line) setStoppedLine(result.line)
        }).catch(() => {})
      }}
      onDebug={() => {
        if (!bridge?.code || !projectRoot) return
        void bridge.code({
          action: 'debug',
          root: projectRoot,
          path: file.path,
          line: Math.max(lineIndex + 1, 1),
          sessionId,
        }).then((result) => {
          if (result.stopped) setStoppedLine(result.stopped)
        }).catch(() => {})
      }}
      onAcceptDiff={() => {
        if (!bridge?.code || !projectRoot) return
        void bridge.code({ action: 'accept', root: projectRoot, sessionId })
      }}
      onRestore={() => {
        if (!bridge?.code || !projectRoot) return
        void bridge.code({ action: 'restore', root: projectRoot, sessionId }).then(() => bridge.read(file.path)).then((read) => {
          onContent(read.content)
        }).catch(() => {})
      }}
      onReferences={() => {
        if (!bridge?.code || !projectRoot) return
        const sourceLine = file.content.split('\n')[Math.max(lineIndex, 0)] ?? ''
        const column = Math.max(sourceLine.search(/\S/) + 1, 1)
        void bridge.code({
          action: 'references',
          root: projectRoot,
          path: file.path,
          line: Math.max(lineIndex + 1, 1),
          column,
          content: file.content,
          sessionId,
        }).then((result) => {
          const hit = result.references?.find((item) => item.line > 0)
          if (hit) setStoppedLine(hit.line)
        }).catch(() => {})
      }}
    />
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
  refreshKey = 0,
}: {
  bridge?: LocalWorkspaceBridge
  sessionId: string
  isolateRoot?: boolean
  projectRoot?: string
  targetPath?: string
  toolActivities?: WorkspaceToolActivity[]
  onOpenPath?: (path: string) => void
  refreshKey?: number
}): React.JSX.Element {
  const [file, setFile] = useState<OpenFile | undefined>()
  const [openError, setOpenError] = useState('')
  const [treeOpen, setTreeOpen] = useState(true)
  const [treeWidth, setTreeWidth] = useState(260)
  const splitRef = useRef<HTMLDivElement>(null)
  const dragCleanup = useRef<(() => void) | undefined>(undefined)
  const taskFiles = useMemo(() => extractTaskFiles(toolActivities), [toolActivities])
  useEffect(() => () => dragCleanup.current?.(), [])
  useEffect(() => {
    if (!bridge?.code || !projectRoot || !sessionId) return
    void bridge.code({ action: 'diff', root: projectRoot, sessionId }).catch(() => {})
  }, [bridge, projectRoot, sessionId])

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
          {file ? (
            <CodeEditorView
              file={file}
              projectRoot={projectRoot}
              sessionId={sessionId}
              bridge={bridge}
              diff={latestFileDiff(toolActivities.map((item) => item.summary ?? ''))}
              onContent={(content) => setFile({ ...file, content })}
            />
          ) : (
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
                  refreshKey={refreshKey}
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
