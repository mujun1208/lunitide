// A render-time throw anywhere in the tree unmounts the whole React root,
// which in a WebView2 window looks like the app crashed to a blank screen
// with no way back. This boundary keeps the window usable: it shows what
// broke and offers a reload, instead of nothing.
import React from 'react'

interface Props {
  children: React.ReactNode
}

interface State {
  error: Error | null
}

export class RootErrorBoundary extends React.Component<Props, State> {
  state: State = { error: null }

  static getDerivedStateFromError(error: Error): State {
    return { error }
  }

  componentDidCatch(error: Error, info: React.ErrorInfo): void {
    console.error('[lunitide] 界面渲染失败', error, info.componentStack)
  }

  private reload = (): void => {
    this.setState({ error: null })
    window.location.reload()
  }

  render(): React.ReactNode {
    const { error } = this.state
    if (!error) return this.props.children
    return (
      <div className="root-error" role="alert">
        <h1>界面遇到了一个错误</h1>
        <p>月汐已经把细节写进日志。重新载入通常就能恢复。</p>
        <pre>{error.message}</pre>
        <button type="button" onClick={this.reload}>
          重新载入
        </button>
      </div>
    )
  }
}
