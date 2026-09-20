import React from 'react'
import { useZh } from '../i18n/language'
import './agentHub.css'

function ChatGlyph(): React.JSX.Element {
  return (
    <svg viewBox="0 0 16 16" width="13" height="13" aria-hidden="true">
      <path fill="currentColor" d="M2.4 3.2h11.2A1.2 1.2 0 0 1 14.8 4.4v6.2a1.2 1.2 0 0 1-1.2 1.2H8.1L5 13.8v-2H3.6A1.2 1.2 0 0 1 2.4 10.6Z" />
    </svg>
  )
}

function WorkGlyph(): React.JSX.Element {
  return (
    <svg viewBox="0 0 16 16" width="13" height="13" aria-hidden="true">
      <path fill="currentColor" d="M5.2 3.6h5.6l.6 1.4h2.2A1.4 1.4 0 0 1 15 6.4v6.2A1.4 1.4 0 0 1 13.6 14H2.4A1.4 1.4 0 0 1 1 12.6V6.4a1.4 1.4 0 0 1 1.4-1.4h2.2Zm1.1.9v.5h3.4v-.5Z" />
    </svg>
  )
}

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
    <div className="shell-mode-switch" role="group" aria-label="Chat / Work">
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
      <div className="shell-mode-pills">
        <button type="button" aria-pressed={mode === 'lunitide'} onClick={onLunitide}><ChatGlyph />Chat</button>
        <button type="button" aria-pressed={mode === 'agentHub'} onClick={onAgents}><WorkGlyph />Work</button>
      </div>
    </div>
  )
}
