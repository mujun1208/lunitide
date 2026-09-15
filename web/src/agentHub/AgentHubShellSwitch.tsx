import React from 'react'
import { useZh } from '../i18n/language'
import './agentHub.css'

export function AgentHubShellSwitch({
  mode,
  onLunitide,
  onAgents,
  onToggleDrawer,
  drawerOpen = true,
}: {
  mode: 'lunitide' | 'agentHub'
  onLunitide: () => void
  onAgents: () => void
  onToggleDrawer?: () => void
  drawerOpen?: boolean
}): React.JSX.Element {
  const zh = useZh()
  return (
    <div className="shell-mode-switch" role="group" aria-label="Work / AgentHub">
      {onToggleDrawer ? (
        <button
          type="button"
          className="shell-drawer-toggle"
          aria-label={drawerOpen ? (zh ? '收起左侧栏' : 'Collapse sidebar') : (zh ? '展开左侧栏' : 'Expand sidebar')}
          aria-expanded={drawerOpen}
          onClick={onToggleDrawer}
        >
          <span aria-hidden="true">◧</span>
        </button>
      ) : null}
      <button type="button" aria-pressed={mode === 'lunitide'} onClick={onLunitide}>Work</button>
      <button type="button" aria-pressed={mode === 'agentHub'} onClick={onAgents}>AgentHub</button>
    </div>
  )
}
