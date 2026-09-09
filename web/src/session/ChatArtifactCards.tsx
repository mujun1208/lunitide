import React, { useState } from 'react'
import { sessionFolderBridge, type StreamArtifact } from '../bridge/client'
import { requestOfficeStudio } from '../officeStudio/officeNavigation'

export type ChatArtifact = StreamArtifact & { callId: string; toolName: string }

const OFFICE_KIND = new Set(['pptx', 'docx', 'xlsx', 'pdf'])
const OFFICE_EXT = /\.(pptx|docx|xlsx|pdf)$/i

/** Keep the actual path; the host resolves it against authorized roots. */
export function artifactOpenRelativePath(path: string): string {
  return path.trim().replace(/\\/g, '/')
}

/** User-facing deliverables only — not intermediate web.search/fetch HTML. */
export function isChatDeliverableArtifact(artifact: Pick<ChatArtifact, 'toolName' | 'kind' | 'path'>): boolean {
  if (artifact.toolName === 'web.search' || artifact.toolName === 'web.fetch') return false
  if (['pptx.gen', 'docx.gen', 'excel.gen', 'pdf.gen', 'html.gen'].includes(artifact.toolName)) return true
  const base = artifact.path.split(/[/\\]/).pop()?.toLowerCase() ?? ''
  if (artifact.kind === 'html' && (base === 'search.html' || base === 'fetch.html')) return false
  if (artifact.kind === 'image') return true
  if (OFFICE_KIND.has(artifact.kind) || OFFICE_EXT.test(base)) return true
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
  onInspect,
}: {
  sessionId: string
  artifacts: ChatArtifact[]
  onError?: (message: string) => void
  onInspect?: (artifact: ChatArtifact) => void
}): React.JSX.Element | null {
  const [openingOffice, setOpeningOffice] = useState(false)
  const visible = filterChatDeliverables(artifacts)
  if (!visible.length) return null
  const open = async (artifact: ChatArtifact) => {
    if (onInspect) { onInspect(artifact); return }
    try {
      await sessionFolderBridge.open({ sessionId, relativePath: artifactOpenRelativePath(artifact.path) })
    } catch (e) {
      onError?.(e instanceof Error ? e.message : '无法打开产物文件')
    }
  }
  return (
    <div className="chat-artifacts" role="list" aria-label="本次对话产物">
      {visible.map(artifact => (
        <React.Fragment key={`${artifact.callId}:${artifact.path}`}>
        <button
          type="button"
          key={`${artifact.callId}:${artifact.path}`}
          className="chat-artifact-card"
          role="listitem"
          title={artifact.path}
          onClick={() => void open(artifact)}
        >
          <span className="chat-artifact-icon" aria-hidden="true">
            {KIND_ICON[artifact.kind] ?? '▣'}
          </span>
          <span className="chat-artifact-body">
            <b>{artifact.path.split(/[/\\]/).pop() ?? artifact.path}</b>
            <small>{artifact.kind === 'image' && !artifact.toolName.startsWith('cc.') ? '图片' : KIND_LABEL[artifact.kind] ?? artifact.kind} · {onInspect ? '点击查看' : '点击打开'}</small>
          </span>
        </button>
        {(OFFICE_KIND.has(artifact.kind) || OFFICE_EXT.test(artifact.path)) && <button type="button" disabled={openingOffice} title={`在办公工作台查看 ${artifact.path.split(/[/\\]/).pop()}`} onClick={() => {
          if (openingOffice) return
          setOpeningOffice(true)
          void requestOfficeStudio(sessionId, artifact.path).catch(error => onError?.(error instanceof Error ? error.message : '办公工作台暂不可用，原对话仍可继续。')).finally(() => setOpeningOffice(false))
        }}>在办公工作台查看</button>}
        </React.Fragment>
      ))}
    </div>
  )
}
