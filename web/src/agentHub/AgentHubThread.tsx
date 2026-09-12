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
  const [preview, setPreview] = useState<AgentHubPreview>()
  const [draft, setDraft] = useState('')
  const [error, setError] = useState('')
  const threadIdRef = useRef(threadId)
  threadIdRef.current = threadId
  const wasLive = useRef(false)
  useEffect(() => { wasLive.current = false }, [threadId])
  const refreshFiles = async (id: string) => {
    const listed = await agentHubApi.workspaceList({ threadId: id })
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
    if (!liveStatus(detail?.thread.status)) return
    let alive = true
    const timer = window.setInterval(() => {
      void Promise.all([
        agentHubApi.threadGet({ threadId }),
        agentHubApi.workspaceList({ threadId }),
      ]).then(([next, listed]) => {
        if (!alive) return
        setDetail(next)
        setFiles(listed.items ?? [])
      }).catch(() => undefined)
    }, 400)
    return () => { alive = false; window.clearInterval(timer) }
  }, [threadId, detail?.thread.status])
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
  return (
    <div className="workspace-layout workspace-is-open">
      <section className="message-panel" aria-label={zh ? `${detail?.thread.title || '会话'} 消息` : 'messages'}>
        {(detail?.messages ?? []).map(item => (
          <div key={item.id} className="conversation-row">
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
          {files.map(item => (
            <button key={item.path} type="button" className="agent-hub-art" onClick={() => void openPreview(item.path)}>
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
