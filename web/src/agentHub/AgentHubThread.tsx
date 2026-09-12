import React, { useEffect, useState } from 'react'
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
    const timer = window.setInterval(() => {
      void Promise.all([
        agentHubApi.threadGet({ threadId }),
        agentHubApi.workspaceList({ threadId }),
      ]).then(([next, listed]) => {
        setDetail(next)
        setFiles(listed.items ?? [])
      }).catch(() => undefined)
    }, 400)
    return () => window.clearInterval(timer)
  }, [threadId, detail?.thread.status])
  const send = async () => {
    const text = draft.trim()
    if (!text) return
    try {
      const next = await agentHubApi.threadPrompt({ threadId, text })
      setDetail(next)
      setDraft('')
      setError('')
    } catch (err) {
      setError(err instanceof Error && /[\u4e00-\u9fff]/.test(err.message) ? err.message : (zh ? '消息没有发出。' : 'The message did not send.'))
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
          <AgentHubAskBar threadId={threadId} prompt={detail.prompt} onResponded={setDetail} />
        ) : null}
        <div className="agent-hub-console">
          <textarea
            value={draft}
            onChange={event => setDraft(event.target.value)}
            aria-label="消息"
          />
          <div className="agent-hub-console-bar">
            <button type="button" className="agent-hub-run" onClick={() => void send()}>{zh ? '发送' : 'Send'}</button>
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
