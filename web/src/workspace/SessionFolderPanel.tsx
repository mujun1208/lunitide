import React, { useCallback, useEffect, useState } from 'react'
import { sessionFolderBridge, type SessionFolderBridge } from '../bridge/client'

type Node = { name: string; path: string; directory: boolean }

export function SessionFolderPanel({
  sessionId,
  bridge = sessionFolderBridge,
  onPreview,
}: {
  sessionId: string
  bridge?: SessionFolderBridge
  onPreview?: (file: { path: string; content: string; size: number }) => void
}): React.JSX.Element {
  const [rootPath, setRootPath] = useState('')
  const [children, setChildren] = useState<Record<string, Node[]>>({})
  const [open, setOpen] = useState<Set<string>>(new Set())
  const [error, setError] = useState('')
  const [loading, setLoading] = useState(true)

  const loadDir = useCallback(
    async (relativePath = '') => {
      const listed = await bridge.list({ sessionId, ...(relativePath ? { relativePath } : {}) })
      return listed.items
    },
    [bridge, sessionId],
  )

  const refresh = useCallback(async () => {
    setLoading(true)
    setError('')
    try {
      const info = await bridge.get({ sessionId })
      setRootPath(info.path)
      const items = await loadDir('')
      setChildren({ '': items })
      setOpen(new Set())
    } catch (e) {
      setError(e instanceof Error ? e.message : '会话目录载入失败')
    } finally {
      setLoading(false)
    }
  }, [bridge, loadDir, sessionId])

  useEffect(() => {
    let alive = true
    void refresh().then(() => {
      if (!alive) void 0
    })
    return () => {
      alive = false
    }
  }, [refresh])

  const activate = async (node: Node) => {
    try {
      setError('')
      if (node.directory) {
        const next = new Set(open)
        if (next.has(node.path)) next.delete(node.path)
        else {
          next.add(node.path)
          if (!children[node.path]) {
            const items = await loadDir(node.path)
            setChildren(v => ({ ...v, [node.path]: items }))
          }
        }
        setOpen(next)
        return
      }
      if (onPreview) {
        onPreview({ path: node.path, content: '（会话产物文件，请在资源管理器中打开查看）', size: 0 })
      }
      await bridge.open({ sessionId, relativePath: node.path })
    } catch (e) {
      setError(e instanceof Error ? e.message : '无法打开文件')
    }
  }

  const rows = (path = '', depth = 0): React.ReactNode =>
    (children[path] ?? []).map(node => (
      <React.Fragment key={node.path}>
        <button
          type="button"
          role="treeitem"
          aria-expanded={node.directory ? open.has(node.path) : undefined}
          className="local-tree-row"
          style={{ paddingLeft: 12 + depth * 16 }}
          onClick={() => void activate(node)}
        >
          <span aria-hidden="true">{node.directory ? (open.has(node.path) ? '▾' : '▸') : '·'}</span>
          {node.name}
        </button>
        {node.directory && open.has(node.path) && rows(node.path, depth + 1)}
      </React.Fragment>
    ))

  return (
    <section className="session-folder-panel" aria-label="会话产物目录">
      <header>
        <div>
          <b>对话产物目录</b>
          <small>本对话生成的文件与附件会保存在这里</small>
        </div>
        <button type="button" onClick={() => void bridge.open({ sessionId }).catch(e => setError(e instanceof Error ? e.message : '打开失败'))}>
          在资源管理器中打开
        </button>
      </header>
      {rootPath && <small title={rootPath}>{rootPath}</small>}
      {loading ? (
        <p role="status">正在载入目录…</p>
      ) : (
        <div role="tree">{children['']?.length ? rows() : <p>此对话还没有产物文件。</p>}</div>
      )}
      {error && <p role="alert">{error}</p>}
    </section>
  )
}
