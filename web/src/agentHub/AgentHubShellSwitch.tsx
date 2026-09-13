import React from 'react'
import { useZh } from '../i18n/language'
import './agentHub.css'

export function AgentHubShellSwitch({
  mode,
  onLunitide,
  onAgents,
}: {
  mode: 'lunitide' | 'agentHub'
  onLunitide: () => void
  onAgents: () => void
}): React.JSX.Element {
  const zh = useZh()
  return (
    <div className="shell-mode-switch" role="group" aria-label={zh ? '月汐 / 外接 Agent' : 'Lunitide / Agents'}>
      <button type="button" aria-pressed={mode === 'lunitide'} onClick={onLunitide}>{zh ? '月汐' : 'Lunitide'}</button>
      <button type="button" aria-pressed={mode === 'agentHub'} onClick={onAgents}>{zh ? '外接 Agent' : 'Agents'}</button>
    </div>
  )
}
