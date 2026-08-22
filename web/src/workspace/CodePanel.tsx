import React, { useEffect, useMemo, useState } from 'react'
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
  targetPath,
  toolActivities = [],
  onOpenPath,
}: {
  bridge?: LocalWorkspaceBridge
  sessionId: string
  isolateRoot?: boolean
  targetPath?: string
  toolActivities?: WorkspaceToolActivity[]
  onOpenPath?: (path: string) => void
}): React.JSX.Element {
  const [file, setFile] = useState<OpenFile | undefined>()
  const [recent, setRecent] = useState<string[]>([])
  const [openError, setOpenError] = useState('')
  const taskFiles = useMemo(() => extractTaskFiles(toolActivities), [toolActivities])

  const openFile = async (entry: TaskFileEntry) => {
    if (!bridge) return
    try {
      const read = await bridge.read(entry.path)
      setFile(read)
      setRecent(prev => [entry.path, ...prev.filter(p => p !== entry.path)].slice(0, 8))
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

  return (
    <div className="code-panel">
      <header className="code-panel-toolbar">
        <span className="code-panel-title">代码</span>
        <button
          type="button"
          className="code-panel-vscode"
          disabled={!bridge}
          title="在 VS Code 中打开当前文件或工作区根目录"
          onClick={() => {
            if (!bridge) return
            setOpenError('')
            void openWorkspaceInEditor(bridge, file?.path)
              .catch(() => setOpenError('无法在 VS Code 中打开；已尝试资源管理器回退'))
          }}
        >
          在 VS Code 中打开
        </button>
      </header>
      {openError && <p className="code-panel-open-error" role="alert">{openError}</p>}
      <div className="code-panel-split">
        <aside className="code-panel-tree" aria-label="任务相关文件">
          <h3>任务相关文件</h3>
          {taskFiles.length ? (
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
                    <span className="code-file-path">{entry.path}</span>
                  </button>
                </li>
              ))}
            </ul>
          ) : (
            <p className="code-panel-empty">本轮还没有代码变更；Agent 写入后会出现在这里。</p>
          )}
          {bridge ? (
            <>
              <h4>工作区目录</h4>
              <LocalExplorer
                bridge={bridge}
                sessionId={sessionId}
                isolateRoot={isolateRoot}
                targetPath={targetPath ?? file?.path}
                onPreview={next => {
                  setFile(next)
                  setRecent(prev => [next.path, ...prev.filter(p => p !== next.path)].slice(0, 8))
                  onOpenPath?.(next.path)
                }}
              />
            </>
          ) : (
            <p className="code-panel-empty">本地工作区暂不可用；代码浏览需要绑定本地文件夹。</p>
          )}
          {recent.length > 0 && (
            <div className="code-recent">
              <h4>最近打开</h4>
              <ul>{recent.map(path => (
                <li key={path}>
                  <button type="button" onClick={() => void bridge?.read(path).then(setFile).catch(() => {})}>
                    {path}
                  </button>
                </li>
              ))}</ul>
            </div>
          )}
        </aside>
        <section className="code-panel-editor" aria-label="代码编辑器">
          {file ? <CodeEditorView file={file} /> : (
            <div className="code-panel-placeholder">
              <b>选择左侧文件开始浏览</b>
              <p>支持本地工作区只读预览；Agent 变更会标记 M/A/D 状态。</p>
            </div>
          )}
        </section>
      </div>
    </div>
  )
}
