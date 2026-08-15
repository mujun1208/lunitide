import React, { useState } from 'react'
import { formatBytes } from '../format'

export interface ConvertFileEntry {
  path: string
  change: string
  size: number
}

export interface ConvertPreview {
  sourceWorkspaceId: string
  targetPath: string
  files: ConvertFileEntry[]
  gitStatus?: string
  conflicts: string[]
  previewDigest: string
}

export interface ConvertScope {
  paths: string[]
}

export interface ConvertWizardProps {
  preview: ConvertPreview
  onConfirm(scope: ConvertScope): Promise<void>
  onCancel(): void
  busy?: boolean
}

export function ConvertWizard({ preview, onConfirm, onCancel, busy = false }: ConvertWizardProps): React.JSX.Element {
  const [confirming, setConfirming] = useState(false)
  const doConfirm = async () => {
    if (busy || confirming) return
    setConfirming(true)
    try {
      await onConfirm({ paths: preview.files.map(f => f.path) })
    } finally {
      setConfirming(false)
    }
  }
  return (
    <section className="m5-convert-wizard" aria-label="转为正式项目确认">
      <header className="m5-convert-head">
        <h2>转为正式项目</h2>
        <code className="m5-preview-digest" title={preview.previewDigest}>摘要 {preview.previewDigest.slice(0, 12)}</code>
      </header>

      <div className="m5-convert-step" data-testid="m5-step-target">
        <h3>① 目标路径</h3>
        <code className="m5-target-path">{preview.targetPath}</code>
      </div>

      <div className="m5-convert-step" data-testid="m5-step-files">
        <h3>② 文件清单（{preview.files.length} 个）</h3>
        {preview.files.length > 0 ? (
          <ul className="m5-file-list">
            {preview.files.map(f => (
              <li key={f.path} className="m5-file-row">
                <code>{f.path}</code>
                <span className="m5-file-change">{f.change}</span>
                <span className="m5-file-size">{formatBytes(f.size)}</span>
              </li>
            ))}
          </ul>
        ) : (
          <p className="m5-empty">没有待复制的文件。</p>
        )}
      </div>

      <div className="m5-convert-step" data-testid="m5-step-git">
        <h3>③ Git 状态与冲突策略</h3>
        <p className="m5-git-status">{preview.gitStatus ?? '目标路径尚未初始化 Git 仓库'}</p>
        {preview.conflicts.length > 0 ? (
          <div className="m5-conflict-warning" role="alert" data-testid="m5-conflict-warning">
            <p>以下 {preview.conflicts.length} 个文件与目标项目冲突，将备份后覆盖：</p>
            <ul>
              {preview.conflicts.map(c => (
                <li key={c}><code>{c}</code></li>
              ))}
            </ul>
          </div>
        ) : (
          <p className="m5-no-conflict" data-testid="m5-no-conflict">无冲突，可直接复制。</p>
        )}
      </div>

      <footer className="m5-convert-foot">
        <button
          type="button"
          className="m5-btn m5-btn-primary"
          disabled={busy || confirming}
          onClick={() => void doConfirm()}
          data-testid="m5-confirm"
        >
          将复制 {preview.files.length} 个文件到目标项目
        </button>
        <button
          type="button"
          className="m5-btn"
          disabled={busy}
          onClick={onCancel}
          data-testid="m5-cancel"
        >
          取消
        </button>
      </footer>
    </section>
  )
}

export default ConvertWizard
