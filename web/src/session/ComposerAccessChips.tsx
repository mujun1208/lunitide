import React from 'react'
import {useZh} from '../i18n/language'

export function ComposerAccessChips({
  executionMode,
}: {
  executionMode: 'approval' | 'auto-edit' | 'full-access'
}): React.JSX.Element {
  const zh = useZh()
  const shell = executionMode === 'full-access' ? (zh ? '完全访问' : 'full access') : (zh ? '白名单命令' : 'allowlisted')
  return (
    <div className="composer-access-chips" role="group" aria-label={zh ? '编码权限' : 'Coding access'}>
      <span className="composer-access-chip">{zh ? 'Git 只读' : 'Git read-only'}</span>
      <span className="composer-access-chip">Shell {shell}</span>
    </div>
  )
}
