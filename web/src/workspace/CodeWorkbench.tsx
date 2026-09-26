import React, { useEffect, useRef } from 'react'
import { codeStatusLabel } from './codePanelUtils'

export type CodeProblem = { line: number; message: string }

export function CodeWorkbench({
  path,
  content,
  problems,
  suggestion,
  stoppedLine,
  diff,
  onAccept,
  onDefine,
  onDebug,
  onAcceptDiff,
  onRestore,
  onReferences,
  onCursor,
}: {
  path: string
  content: string
  problems: CodeProblem[] | null
  suggestion: string
  stoppedLine: number
  diff: string
  onAccept: () => void
  onDefine: () => void
  onDebug: () => void
  onAcceptDiff: () => void
  onRestore: () => void
  onReferences: () => void
  onCursor?: (line: number) => void
}): React.JSX.Element {
  const host = useRef<HTMLDivElement>(null)
  const cursorRef = useRef(onCursor)
  cursorRef.current = onCursor
  const editorRef = useRef<{ dispose: () => void; setPosition: (position: { lineNumber: number; column: number }) => void; revealLineInCenter: (lineNumber: number) => void } | null>(null)
  const lines = content.split('\n')
  useEffect(() => {
    if (import.meta.env.VITEST || !host.current) return
    let editor: { dispose: () => void } | undefined
    let cancelled = false
    const landed = stoppedLine
    void import('monaco-editor/esm/vs/editor/editor.api').then((monaco) => {
      if (cancelled || !host.current) return
      const worker = new Worker(new URL('../../node_modules/monaco-editor/esm/vs/editor/editor.worker.js', import.meta.url), { type: 'module' })
      self.MonacoEnvironment = { getWorker: () => worker }
      const created = monaco.editor.create(host.current, {
        value: content,
        language: path.endsWith('.go') ? 'go' : 'plaintext',
        minimap: { enabled: false },
        glyphMargin: true,
        automaticLayout: true,
        scrollBeyondLastLine: false,
      })
      created.onDidChangeCursorPosition((event) => cursorRef.current?.(event.position.lineNumber))
      editorRef.current = created
      editor = created
      if (landed > 0) {
        created.setPosition({ lineNumber: landed, column: 1 })
        created.revealLineInCenter(landed)
      }
    }).catch(() => {})
    return () => {
      cancelled = true
      editorRef.current = null
      editor?.dispose()
    }
  }, [path, content])
  useEffect(() => {
    if (stoppedLine < 1) return
    editorRef.current?.setPosition({ lineNumber: stoppedLine, column: 1 })
    editorRef.current?.revealLineInCenter(stoppedLine)
  }, [stoppedLine])

  return (
    <div className="code-editor">
      <header className="code-editor-head">
        <b>{path.split(/[/\\]/).pop()}</b>
        <small>{path}</small>
      </header>
      <div ref={host} className="code-monaco" />
      {import.meta.env.VITEST && (
        <pre className="code-editor-content"><code>{content || ' '}</code></pre>
      )}
      {problems != null && problems.length > 0 && (
        <ul className="code-problems" aria-label="问题">
          {problems.map((item) => (
            <li key={`${item.line}:${item.message}`}>
              <button type="button" onClick={onDefine}>{item.line}: {item.message}</button>
            </li>
          ))}
        </ul>
      )}
      {stoppedLine > 0 && <p role="status">断点停在第 {stoppedLine} 行</p>}
      {diff && <pre className="code-diff">{diff}</pre>}
      <footer className="code-editor-foot" role="status">
        <span>{codeStatusLabel(problems == null ? null : problems.length)}</span>
        <span>{suggestion ? `建议 ${suggestion.trim()}` : '未定位光标'}</span>
        <span>Ln {stoppedLine || lines.length}, 共 {lines.length} 行</span>
        {suggestion && <button type="button" onClick={onAccept}>接受补全</button>}
        {diff && <button type="button" onClick={onAcceptDiff}>接受差异</button>}
        {diff && <button type="button" onClick={onRestore}>还原差异</button>}
        <button type="button" onClick={onDefine}>转到定义</button>
        <button type="button" onClick={onReferences}>查找引用</button>
        <button type="button" onClick={onDebug}>在此行停下</button>
      </footer>
    </div>
  )
}
