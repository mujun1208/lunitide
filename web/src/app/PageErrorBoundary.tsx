// Per-page recovery boundary: an in-page render throw becomes a "this page
// failed" panel with a retry, leaving the sidebar and other pages usable.
// Root-level window/unhandledrejection handling stays in RootErrorBoundary;
// this boundary only catches render-time throws for the page it wraps.
import React from 'react'
import { bridgeTransportUserError } from '../bridge/bridgeUserError'

interface Props {
  label?: string
  onReset?: () => void
  children: React.ReactNode
}

interface State {
  error: Error | null
}

export class PageErrorBoundary extends React.Component<Props, State> {
  state: State = { error: null }

  static getDerivedStateFromError(error: Error): State {
    return { error }
  }

  componentDidCatch(error: Error, info: React.ErrorInfo): void {
    try {
      console.error('[lunitide] 页面渲染失败', this.props.label ?? '', error, info.componentStack)
    } catch {
      /* host loggers that re-throw must not re-enter this handler */
    }
  }

  private reset = (): void => {
    this.setState({ error: null })
    this.props.onReset?.()
  }

  render(): React.ReactNode {
    const { error } = this.state
    if (!error) return this.props.children
    return (
      <div className="page-error-shell">
        <div className="page-error" role="alert">
          <h2>本页出错了</h2>
          <p>其它页面仍然可用。重试通常就能恢复本页。</p>
          <pre>{bridgeTransportUserError(error, '本页渲染失败')}</pre>
          <button type="button" onClick={this.reset}>
            重试
          </button>
        </div>
      </div>
    )
  }
}