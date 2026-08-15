import React from 'react'

export interface ActionSpec {
  id: string
  name: string
  description: string
  argvPreview: string[]
  envAllowlist: string[]
  cwdPolicy: string
}

export interface ActionCardProps {
  spec: ActionSpec
  onRun?: () => void
  disabled?: boolean
}

export const CWD_POLICY_LABELS: Record<string, string> = {
  workspace: '工作区内',
}

export function ActionCard({ spec, onRun, disabled = false }: ActionCardProps): React.JSX.Element {
  const cwdLabel = CWD_POLICY_LABELS[spec.cwdPolicy] ?? spec.cwdPolicy
  return (
    <article className="m5-action-card" data-action-id={spec.id} aria-label={`动作 ${spec.name}`}>
      <header className="m5-action-head">
        <strong className="m5-action-name">{spec.name}</strong>
        <span className="m5-badge m5-badge-cwd" data-policy={spec.cwdPolicy}>{cwdLabel}</span>
      </header>
      <p className="m5-action-desc">{spec.description}</p>
      {spec.envAllowlist.length > 0 && (
        <div className="m5-env-row">
          <span className="m5-env-label">环境变量白名单</span>
          <span className="m5-env-chips">
            {spec.envAllowlist.map(env => (
              <code key={env} className="m5-chip">{env}</code>
            ))}
          </span>
        </div>
      )}
      <pre className="m5-argv"><code>{spec.argvPreview.join(' ')}</code></pre>
      {onRun && (
        <button
          type="button"
          className="m5-btn m5-btn-primary"
          disabled={disabled}
          onClick={onRun}
          data-testid={`m5-run-${spec.id}`}
        >
          运行
        </button>
      )}
    </article>
  )
}

export default ActionCard
