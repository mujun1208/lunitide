export const OCR_PACK_ID = 'paddleocr-vl-1.6' as const
export const OCR_NOTICE_READ_LIMIT = 65_536
export const OCR_ARTIFACT_PREVIEW_LIMIT = 1_048_576
export const OCR_ARTIFACT_CHUNK = 65_536

export function ocrUserError(err: unknown, fallback: string): string {
  const detail = err instanceof Error ? err.message.trim() : ''
  return /[\u4e00-\u9fff]/.test(detail) ? detail : fallback
}

export function windowsProbeSummary(state: string | undefined): string {
  switch (state) {
    case 'ready': return 'Windows OCR 可用'
    case 'unsupported_os': return '当前系统不支持 Windows OCR'
    case 'initialization_failed': return 'Windows OCR 初始化失败'
    case 'language_unavailable': return '需要安装 OCR 语言包'
    case 'sample_failed': return 'Windows OCR 自检失败'
    case 'timed_out': return 'Windows OCR 检查超时'
    default: return 'Windows OCR 状态未知'
  }
}

export function windowsProbeCanRepair(state: string | undefined): boolean {
  return state === 'initialization_failed' || state === 'language_unavailable' || state === 'sample_failed' || state === 'timed_out'
}

export function windowsProbeHint(state: string | undefined): string {
  switch (state) {
    case 'sample_failed':
      return '本机已经有 Windows OCR 时，多半是自检读图失败。一键修复会重新接通，并补齐中文和英文识别语言包。'
    case 'language_unavailable':
      return '系统还没有 OCR 语言包。一键修复会安装中文和英文识别包，可能弹出系统权限确认。'
    case 'initialization_failed':
      return 'Windows OCR 组件没有就绪。一键修复会补语言包并重新检查。'
    case 'timed_out':
      return '上次检查超时。可以重新检查，或一键修复后再测。'
    default:
      return ''
  }
}

export function formatCheckedAt(iso: string | undefined): string {
  if (!iso) return ''
  const date = new Date(iso)
  if (Number.isNaN(date.getTime())) return ''
  return date.toLocaleString('zh-CN')
}

export function isLegacyUnwired(legacy: unknown): legacy is {
  engineId: 'ppocr'
  registered: true
  state: 'registered_unwired'
  available: false
  markerDetected: boolean
} {
  if (!legacy || typeof legacy !== 'object') return false
  const row = legacy as { engineId?: string; registered?: boolean; state?: string; available?: boolean }
  return row.engineId === 'ppocr' && row.registered === true && row.state === 'registered_unwired' && row.available === false
}
