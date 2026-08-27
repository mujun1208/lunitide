import React from 'react'

export function ComposerAccessChips({
  executionMode,
}: {
  executionMode: 'approval' | 'auto-edit' | 'full-access'
}): React.JSX.Element {
  const shell = executionMode === 'full-access' ? '完全访问' : '白名单命令'
  return (
    <div className="composer-access-chips" role="group" aria-label="编码权限">
      <span className="composer-access-chip">Git 只读</span>
      <span className="composer-access-chip">Shell {shell}</span>
    </div>
  )
}
