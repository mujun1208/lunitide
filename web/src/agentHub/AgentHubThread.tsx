import React, { useEffect, useRef, useState } from 'react'
import { useZh } from '../i18n/language'
import { AgentHubAskBar } from './AgentHubAskBar'
import { AgentHubFileInspector } from './AgentHubFileInspector'
import {
  agentHubApi,
  type AgentHubPreview,
  type AgentHubThreadDetail,
  type AgentHubWorkspaceItem,
} from './agentHubApi'
import { parentWorkspacePath, statusLabel, threadPptMissing } from './agentHubCopy'

function liveStatus(status: string | undefined): boolean {
  return status === 'running' || status === 'waiting_user'
}

export function AgentHubThread({
  threadId,
}: {
  threadId: string
}): React.JSX.Element {
  const zh = useZh()
  const [detail, setDetail] = useState<AgentHubThreadDetail>()
  const [files, setFiles] = useState<AgentHubWorkspaceItem[]>([])
  const [folder, setFolder] = useState('')
  const [preview, setPreview] = useState<AgentHubPreview>()
  const [draft, setDraft] = useState('')
  const [error, setError] = useState('')
  const threadIdRef = useRef(threadId)
  threadIdRef.current = threadId
  const wasLive = useRef(false)
  useEffect(() => { wasLive.current = false; setFolder('') }, [threadId])
  const refreshFiles = async (id: string, relativePath = folder) => {
    const listed = await agentHubApi.workspaceList({ threadId: id, ...(relativePath ? { relativePath } : {}) })
    if (threadIdRef.current !== id) return
    setFiles(listed.items ?? [])
  }
  const applyDetail = async (next: AgentHubThreadDetail) => {
    if (threadIdRef.current !== next.thread.threadId) return
    setDetail(next)
    await refreshFiles(next.thread.threadId)
  }
  useEffect(() => {
    let alive = true
    const load = async () => {
      try {
        const [next, listed] = await Promise.all([
          agentHubApi.threadGet({ threadId }),
          agentHubApi.workspaceList({ threadId }),
        ])
        if (!alive) return
        setDetail(next)
        setFiles(listed.items ?? [])
        setError('')
      } catch (err) {
        if (alive) setError(err instanceof Error && /[\u4e00-\u9fff]/.test(err.message) ? err.message : (zh ? '会话读取失败。' : 'Could not load this thread.'))
      }
    }
    void load()
    return () => { alive = false }
  }, [threadId, zh])
  useEffect(() => {
    const live = liveStatus(detail?.thread.status)
    let alive = true
    const timer = window.setInterval(() => {
      void Promise.all([
        agentHubApi.threadGet({ threadId }),
        agentHubApi.workspaceList({ threadId, ...(folder ? { relativePath: folder } : {}) }),
      ]).then(([next, listed]) => {
        if (!alive) return
        setDetail(next)
        setFiles(listed.items ?? [])
      }).catch(() => undefined)
    }, live ? 400 : 4000)
    return () => { alive = false; window.clearInterval(timer) }
  }, [threadId, detail?.thread.status, folder])
  useEffect(() => {
    const live = liveStatus(detail?.thread.status)
    if (wasLive.current && !live && detail) {
      void refreshFiles(detail.thread.threadId)
    }
    wasLive.current = live
  }, [detail])
  const live = liveStatus(detail?.thread.status)
  const send = async () => {
    const text = draft.trim()
    if (!text || live) return
    try {
      const next = await agentHubApi.threadPrompt({ threadId, text })
      await applyDetail(next)
      setDraft('')
      setError('')
    } catch (err) {
      setError(err instanceof Error && /[\u4e00-\u9fff]/.test(err.message) ? err.message : (zh ? '消息没有发出。' : 'The message did not send.'))
    }
  }
  const cancel = async () => {
    try {
      const next = await agentHubApi.threadCancel({ threadId })
      await applyDetail(next)
      setError('')
    } catch (err) {
      setError(err instanceof Error && /[\u4e00-\u9fff]/.test(err.message) ? err.message : (zh ? '没有取消。' : 'Could not cancel.'))
    }
  }
  const openPreview = async (path: string) => {
    setPreview(await agentHubApi.preview({ threadId, path }))
  }
  const openItem = async (item: AgentHubWorkspaceItem) => {
    if (item.isDir) {
      setFolder(item.path)
      await refreshFiles(threadId, item.path)
      return
    }
    await openPreview(item.path)
  }
  const goUp = async () => {
    const next = parentWorkspacePath(folder)
    setFolder(next)
    await refreshFiles(threadId, next)
  }
  const tokens = (detail?.tokensUsed ?? 0) > 0
    ? String(detail?.tokensUsed)
    : (zh ? 'CLI 未回报' : 'CLI did not report tokens')
  const noDeck = detail?.thread.status === 'success' && threadPptMissing(detail.thread.scene, detail.files)
  return (
    <div className="workspace-layout workspace-is-open">
      <section className="message-panel" aria-label={zh ? `${detail?.thread.title || '会话'} 消息` : 'messages'}>
        {detail ? (
          <p className="agent-hub-hint">
            {statusLabel(detail.thread.status, zh)} · {tokens} · {zh ? '消耗的是该 CLI 自己的会员额度' : 'Usage comes from that CLI subscription'}
          </p>
        ) : null}
        {noDeck ? (
          <p className="agent-hub-hint" role="status">{zh ? '没有文稿。打开目录查看本轮文件，或看时间线说明。' : 'No deck was produced. Open the folder or read the timeline.'}</p>
        ) : null}
        {(detail?.messages ?? []).map(item => (
          <div key={item.id} className="conversation-row" data-role={item.role}>
            <p>{item.content}</p>
          </div>
        ))}
        {detail?.prompt ? (
          <AgentHubAskBar threadId={threadId} prompt={detail.prompt} onResponded={next => { void applyDetail(next) }} />
        ) : null}
        <div className="agent-hub-console">
          <textarea
            value={draft}
            onChange={event => setDraft(event.target.value)}
            aria-label="消息"
          />
          <div className="agent-hub-console-bar">
            {live ? (
              <button type="button" className="agent-hub-chip" onClick={() => void cancel()}>{zh ? '取消' : 'Cancel'}</button>
            ) : null}
            <button type="button" className="agent-hub-run" disabled={live} onClick={() => void send()}>{zh ? '发送' : 'Send'}</button>
          </div>
        </div>
        {error && <p className="agent-hub-error" role="alert">{error}</p>}
      </section>
      <div className="workspace-resizer" />
      <aside className="workspace-column">
        <div className="workspace">
          {folder ? (
            <button type="button" className="agent-hub-art" onClick={() => void goUp()}>{zh ? '上一级' : 'Up'}</button>
          ) : null}
          {files.map(item => (
            <button key={item.path} type="button" className="agent-hub-art" onClick={() => void openItem(item)}>
              {item.name}
            </button>
          ))}
          <AgentHubFileInspector
            preview={preview}
            onOpen={() => { if (preview) void agentHubApi.open({ threadId, path: preview.path }) }}
          />
        </div>
      </aside>
    </div>
  )
}
