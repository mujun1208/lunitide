export type ComposerPickerStatus = 'idle' | 'loading' | 'ready' | 'failed'

export type ComposerPickerState = {
  status: ComposerPickerStatus
  message: string
  retryable: boolean
}

export function composerPickerIdle(): ComposerPickerState {
  return {status: 'idle', message: '', retryable: false}
}

export function composerPickerLoading(): ComposerPickerState {
  return {status: 'loading', message: '正在载入…', retryable: false}
}

export function composerPickerReady(): ComposerPickerState {
  return {status: 'ready', message: '', retryable: false}
}

export function composerPickerFailed(kind: 'at' | 'skill' | 'expert', code = ''): ComposerPickerState {
  const message = kind === 'skill'
    ? '技能列表暂时读不到，请再试一次。'
    : kind === 'expert'
      ? '专家列表暂时读不到，请再试一次。'
      : '可引用的上下文暂时读不到，请再试一次。'
  return {status: 'failed', message: code ? `${message} ${code}` : message, retryable: true}
}

export function composerPickerEmpty(kind: 'at' | 'skill' | 'expert'): string {
  if (kind === 'skill') return '还没有已发布技能。'
  if (kind === 'expert') return '还没有已启用专家。'
  return '这一会话还没有可引用的附件、专家或同事。'
}
