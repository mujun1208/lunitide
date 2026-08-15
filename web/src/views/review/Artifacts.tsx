import React from 'react'
import { formatBytes, formatDateTime, shortSha } from '../format'

export type ArtifactDownloadState = 'blocked' | 'allowed' | 'downloaded'

export interface ArtifactItem {
  id: string
  mime: string
  size: number
  sha256: string
  generator: string
  downloadState: ArtifactDownloadState
  createdAt: string
}

export interface ArtifactsProps {
  artifacts: ArtifactItem[]
  onAllowDownload(id: string): void
  onDownload(id: string): void
}

export const DOWNLOAD_STATE_LABELS: Record<ArtifactDownloadState, string> = {
  blocked: '已阻断',
  allowed: '已允许',
  downloaded: '已下载',
}

export type MimeCategory = 'text' | 'image' | 'archive' | 'other'

export function mimeCategory(mime: string): MimeCategory {
  if (mime.startsWith('text/')) return 'text'
  if (mime.startsWith('image/')) return 'image'
  if (/(zip|gzip|compress|tar|rar|7z)/i.test(mime)) return 'archive'
  return 'other'
}

const MIME_ICONS: Record<MimeCategory, string> = {
  text: '📄',
  image: '🖼️',
  archive: '🗜️',
  other: '📎',
}

export function isPreviewable(mime: string): boolean {
  return mime.startsWith('text/') || mime.startsWith('image/')
}

export function Artifacts({ artifacts, onAllowDownload, onDownload }: ArtifactsProps): React.JSX.Element {
  return (
    <section className="m5-artifacts" aria-label="产物列表">
      <header className="m5-artifacts-head"><h3>产物</h3></header>
      {artifacts.length === 0 ? (
        <p className="m5-empty">暂无产物。</p>
      ) : (
        <ul className="m5-artifact-list">
          {artifacts.map(a => {
            const category = mimeCategory(a.mime)
            return (
              <li key={a.id} className="m5-artifact-card" data-artifact-id={a.id}>
                <span className="m5-mime-icon" data-category={category} title={a.mime}>{MIME_ICONS[category]}</span>
                <div className="m5-artifact-body">
                  <div className="m5-artifact-row">
                    <code className="m5-sha" title={a.sha256}>{shortSha(a.sha256)}</code>
                    <span className="m5-badge m5-badge-download" data-state={a.downloadState}>
                      {DOWNLOAD_STATE_LABELS[a.downloadState]}
                    </span>
                  </div>
                  <div className="m5-artifact-meta">
                    <span>{a.mime}</span>
                    <span>{formatBytes(a.size)}</span>
                    <span>生成者 {a.generator}</span>
                    <span>{formatDateTime(a.createdAt)}</span>
                  </div>
                </div>
                <div className="m5-artifact-actions">
                  {isPreviewable(a.mime) && (
                    <button type="button" className="m5-btn" aria-label={`预览 ${shortSha(a.sha256)}`}>预览</button>
                  )}
                  {a.downloadState === 'blocked' && (
                    <button
                      type="button"
                      className="m5-btn m5-btn-danger"
                      onClick={() => {
                        if (window.confirm(`允许下载产物 ${shortSha(a.sha256)}？`)) onAllowDownload(a.id)
                      }}
                      data-testid={`m5-allow-${a.id}`}
                    >
                      允许下载
                    </button>
                  )}
                  {a.downloadState === 'allowed' && (
                    <button
                      type="button"
                      className="m5-btn m5-btn-primary"
                      onClick={() => onDownload(a.id)}
                      data-testid={`m5-download-${a.id}`}
                    >
                      下载
                    </button>
                  )}
                  {a.downloadState === 'downloaded' && (
                    <button type="button" className="m5-btn" disabled data-testid={`m5-download-${a.id}`}>下载</button>
                  )}
                </div>
              </li>
            )
          })}
        </ul>
      )}
    </section>
  )
}

export default Artifacts
