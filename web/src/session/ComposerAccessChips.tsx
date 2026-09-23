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
  lane,
}: {
  executionMode?: 'approval' | 'auto-edit' | 'full-access'
  lane?: string
}): React.JSX.Element {
  const zh = useZh()
  return (
    <div className="composer-access-chips" role="group" aria-label={zh ? '编码权限' : 'Coding access'}>
      {lane ? <span className="composer-access-chip">{lane}</span> : null}
    </div>
  )
}
