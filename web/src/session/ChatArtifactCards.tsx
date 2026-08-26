import React from 'react'
import { sessionFolderBridge, type StreamArtifact } from '../bridge/client'

export type ChatArtifact = StreamArtifact & { callId: string; toolName: string }

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
  if (!artifacts.length) return null
  const open = async (artifact: ChatArtifact) => {
    try {
      await sessionFolderBridge.open({ sessionId, relativePath: artifact.path })
    } catch (e) {
      onError?.(e instanceof Error ? e.message : '无法打开产物文件')
    }
  }
  return (
    <div className="chat-artifacts" role="list" aria-label="本次对话产物">
      {artifacts.map(artifact => (
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
