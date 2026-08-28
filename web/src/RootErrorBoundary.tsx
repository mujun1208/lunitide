// A render-time throw anywhere in the tree unmounts the whole React root,
// which in a WebView2 window looks like the app crashed to a blank screen
// with no way back. Event-handler throws (send click) and unhandled
// rejections are not caught by getDerivedStateFromError — subscribe to
// window so those become this recovery shell instead of a native exit.
import React from 'react'

interface Props {
  children: React.ReactNode
}

interface State {
  error: Error | null
}

function asError(value: unknown, fallback: string): Error | null {
  if (value instanceof Error) return value
  if (typeof value === 'string' && value) return new Error(value)
  if (value && typeof value === 'object' && 'message' in value && typeof (value as { message: unknown }).message === 'string') {
    return new Error((value as { message: string }).message)
  }
  if (fallback) return new Error(fallback)
  return null
}

function isBridgeShaped(value: unknown): boolean {
  return !!value && typeof value === 'object' && 'code' in value && 'retryable' in value
}

function ignoreNoise(err: Error): boolean {
  return /ResizeObserver|Script error\.?$/i.test(err.message)
}

export class RootErrorBoundary extends React.Component<Props, State> {
  state: State = { error: null }
  private handling = false
  private onWindowError?: (event: ErrorEvent) => void
  private onUnhandledRejection?: (event: PromiseRejectionEvent) => void

  static getDerivedStateFromError(error: Error): State {
    return { error }
  }

  private capture(err: Error, log: string, event?: { preventDefault(): void }): void {
    if (this.handling || this.state.error) return
    this.handling = true
    event?.preventDefault()
    this.setState({ error: err })
    try {
      console.error(log, err)
    } catch {
      /* host loggers that re-throw must not re-enter this handler */
    } finally {
      this.handling = false
    }
  }

  componentDidMount(): void {
    this.onWindowError = (event: ErrorEvent) => {
      // Resource load failures have no .error; only script exceptions should replace the shell.
      if (!event.error) return
      const err = asError(event.error, event.message || '界面运行时错误')
      if (!err || ignoreNoise(err)) return
      this.capture(err, '[lunitide] 未捕获界面错误', event)
    }
    this.onUnhandledRejection = (event: PromiseRejectionEvent) => {
      if (isBridgeShaped(event.reason)) return
      const err = asError(event.reason, '')
      if (!err || ignoreNoise(err)) return
      this.capture(err, '[lunitide] 未处理的 Promise 失败', event)
    }
    window.addEventListener('error', this.onWindowError)
    window.addEventListener('unhandledrejection', this.onUnhandledRejection)
  }

  componentWillUnmount(): void {
    if (this.onWindowError) window.removeEventListener('error', this.onWindowError)
    if (this.onUnhandledRejection) window.removeEventListener('unhandledrejection', this.onUnhandledRejection)
  }

  componentDidCatch(error: Error, info: React.ErrorInfo): void {
    if (this.handling) return
    try {
      console.error('[lunitide] 界面渲染失败', error, info.componentStack)
    } catch {
      /* see capture() */
    }
  }

  private reload = (): void => {
    this.setState({ error: null })
    window.location.reload()
  }

  render(): React.ReactNode {
    const { error } = this.state
    if (!error) return this.props.children
    return (
      <div className="root-error-shell">
        <div className="root-error" role="alert">
          <h1>界面遇到了一个错误</h1>
          <p>月汐已经把细节写进日志。重新载入通常就能恢复。</p>
          <pre>{error.message}</pre>
          <button type="button" onClick={this.reload}>
            重新载入
          </button>
        </div>
      </div>
    )
  }
}
