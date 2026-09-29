import React from 'react'
import {useZh} from '../i18n/language'

export function laneWordFromGuidance(hint: string): string {
  for (const part of hint.split(' / ')) {
    const word = part.trim()
    if (word.startsWith('档位:')) {
      return word
    }
  }
  return ''
}

export function ComposerAccessChips({
  executionMode,
  lane,
}: {
  executionMode: 'approval' | 'auto-edit' | 'full-access'
  lane?: string
}): React.JSX.Element {
  const zh = useZh()
  const shell = executionMode === 'full-access' ? (zh ? '完全访问' : 'full access') : (zh ? '白名单命令' : 'allowlisted')
  return (
    <div className="composer-access-chips" role="group" aria-label={zh ? '编码权限' : 'Coding access'}>
      {lane ? <span className="composer-access-chip">{lane}</span> : null}
      <span className="composer-access-chip">{zh ? 'Git 只读' : 'Git read-only'}</span>
      <span className="composer-access-chip">Shell {shell}</span>
    </div>
  )
}
