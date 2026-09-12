import React from 'react'
import { useZh } from '../i18n/language'
import type { AgentHubPreview } from './agentHubApi'

export function AgentHubFileInspector({
  preview,
  onOpen,
}: {
  preview?: AgentHubPreview
  onOpen?: () => void
}): React.JSX.Element | null {
  const zh = useZh()
  if (!preview) return null
  return (
    <section className="agent-hub-inspector" aria-label={zh ? '文件预览' : 'File preview'}>
      <header className="agent-hub-actions">
        <strong>{preview.path.split(/[/\\]/).pop()}</strong>
        <button type="button" onClick={onOpen}>{zh ? '用本机打开' : 'Open locally'}</button>
      </header>
      {preview.notice && <p className="agent-hub-hint">{preview.notice}</p>}
      {preview.kind === 'image' && preview.content
        ? <img alt={preview.path} src={preview.content} />
        : preview.kind === 'md' || preview.kind === 'text'
          ? <pre>{preview.content || (zh ? '没有可展示的文本。' : 'No text preview.')}</pre>
          : <p className="agent-hub-hint">{zh ? '请用本机软件打开查看完整内容' : 'Open this file in a local app.'}</p>}
    </section>
  )
}
