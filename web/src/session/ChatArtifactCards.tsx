import React from 'react'
import { sessionFolderBridge, type StreamArtifact } from '../bridge/client'

export type ChatArtifact = StreamArtifact & { callId: string; toolName: string }

/** User-facing deliverables only — not intermediate web.search/fetch HTML. */
export function isChatDeliverableArtifact(artifact: Pick<ChatArtifact, 'toolName' | 'kind' | 'path'>): boolean {
  if (artifact.toolName === 'web.search' || artifact.toolName === 'web.fetch') return false
  if (['pptx.gen', 'docx.gen', 'excel.gen', 'pdf.gen', 'html.gen'].includes(artifact.toolName)) return true
  const base = artifact.path.split('/').pop()?.toLowerCase() ?? ''
  if (artifact.kind === 'html' && (base === 'search.html' || base === 'fetch.html')) return false
  if (artifact.kind === 'image') return true
  if (artifact.kind === 'html' && artifact.toolName === 'workspace.write') return true
  return false
}

export function filterChatDeliverables(artifacts: readonly ChatArtifact[]): ChatArtifact[] {
  return artifacts.filter(isChatDeliverableArtifact)
}

const KIND_LABEL: Record<string, string> = { html: 'HTML', xlsx: 'Excel', docx: 'Word', pptx: 'PPT', pdf: 'PDF', image: '截图' }
const KIND_ICON: Record<string, string> = { html: '◧', xlsx: '▤', docx: '▤', pptx: '◫', pdf: '▦', image: '▣' }

export function ChatArtifactCards({
  sessionId,
  artifacts,
  onError,
}: {
  sessionId: string
  artifacts: ChatArtifact[]
  onError?: (message: string) => void
}): React.JSX.Element | null {
  const visible = filterChatDeliverables(artifacts)
  if (!visible.length) return null
  const open = async (artifact: ChatArtifact) => {
    try {
      await sessionFolderBridge.open({ sessionId, relativePath: artifact.path })
    } catch (e) {
      onError?.(e instanceof Error ? e.message : '无法打开产物文件')
    }
  }
  return (
    <div className="chat-artifacts" role="list" aria-label="本次对话产物">
      {visible.map(artifact => (
        <button
          type="button"
          key={artifact.callId}
          className="chat-artifact-card"
          role="listitem"
          title={artifact.path}
          onClick={() => void open(artifact)}
        >
          <span className="chat-artifact-icon" aria-hidden="true">
            {KIND_ICON[artifact.kind] ?? '▣'}
          </span>
          <span className="chat-artifact-body">
            <b>{artifact.path.split('/').pop() ?? artifact.path}</b>
            <small>{KIND_LABEL[artifact.kind] ?? artifact.kind} · 点击打开</small>
          </span>
        </button>
      ))}
    </div>
  )
}
