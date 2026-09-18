export const OCR_SETTINGS_TARGET = {
  settingsCategory: 'personal',
  settingsIntelligenceView: 'ocr',
} as const

export type OCRActivityPhase =
  | 'queued'
  | 'awaiting_approval'
  | 'running'
  | 'verifying'
  | 'succeeded'
  | 'uncertain'
  | 'failed'
  | 'cancelled'

export type OCRActivityProjection = {
  domain: 'ocr'
  activityId: string
  kind: 'pack' | 'run'
  phase: OCRActivityPhase
  terminal: boolean
  successClaimable: boolean
  unread: boolean
  verificationStatus: 'not_applicable' | 'not_started' | 'pending' | 'confirmed' | 'unconfirmed'
  recoveryAction: 'none' | 'retry' | 'cancel' | 'open_settings'
  recoveryTarget: typeof OCR_SETTINGS_TARGET
  errorCode: string | null
  retryable: boolean
  completedUnits: number | null
  totalUnits: number | null
}

const PACK_PHASES = {
  requested: 'queued',
  preflighting: 'running',
  downloading: 'running',
  verifying: 'verifying',
  installing: 'running',
  self_testing: 'verifying',
  succeeded: 'succeeded',
  failed: 'failed',
  cancelled: 'cancelled',
} as const satisfies Record<string, OCRActivityPhase>

const RUN_PHASES = {
  queued: 'queued',
  running: 'running',
  verifying: 'verifying',
  succeeded: 'succeeded',
  failed: 'failed',
  cancelled: 'cancelled',
} as const satisfies Record<string, OCRActivityPhase>

function closedUnknown(activityId: string, kind: 'pack' | 'run', errorCode: string | null, completedUnits: number | null, totalUnits: number | null): OCRActivityProjection {
  return {
    domain: 'ocr',
    activityId,
    kind,
    phase: 'uncertain',
    terminal: true,
    successClaimable: false,
    unread: true,
    verificationStatus: 'unconfirmed',
    recoveryAction: 'open_settings',
    recoveryTarget: OCR_SETTINGS_TARGET,
    errorCode: errorCode ?? 'OCR_PHASE_UNKNOWN',
    retryable: false,
    completedUnits,
    totalUnits,
  }
}

function projectMapped(
  activityId: string,
  kind: 'pack' | 'run',
  phase: OCRActivityPhase,
  errorCode: string | null,
  retryable: boolean,
  completedUnits: number | null,
  totalUnits: number | null,
  cancelRequested: boolean,
): OCRActivityProjection {
  const terminal = phase === 'succeeded' || phase === 'failed' || phase === 'cancelled'
  const successClaimable = phase === 'succeeded'
  return {
    domain: 'ocr',
    activityId,
    kind,
    phase,
    terminal,
    successClaimable,
    unread: phase === 'failed',
    verificationStatus: successClaimable ? 'confirmed' : phase === 'failed' || phase === 'uncertain' ? 'unconfirmed' : phase === 'verifying' ? 'pending' : 'not_started',
    recoveryAction: phase === 'failed' ? 'open_settings' : !terminal && !cancelRequested ? 'cancel' : 'none',
    recoveryTarget: OCR_SETTINGS_TARGET,
    errorCode,
    retryable: retryable && phase === 'failed',
    completedUnits,
    totalUnits,
  }
}

export function mapOCRPackOperation(op: {
  operationId: string
  phase: string
  errorCode?: string | null
  retryable?: boolean
  completedBytes?: number
  totalBytes?: number
  cancelRequested?: boolean
}): OCRActivityProjection {
  const mapped = PACK_PHASES[op.phase as keyof typeof PACK_PHASES]
  if (!mapped) {
    return closedUnknown(op.operationId, 'pack', op.errorCode ?? null, op.completedBytes ?? null, op.totalBytes ?? null)
  }
  return projectMapped(
    op.operationId,
    'pack',
    mapped,
    op.errorCode ?? null,
    Boolean(op.retryable),
    op.completedBytes ?? null,
    op.totalBytes ?? null,
    Boolean(op.cancelRequested),
  )
}

export function mapOCRRun(run: {
  runId: string
  phase?: string
  status?: string
  errorCode?: string | null
  retryable?: boolean
  completedPages?: number
  totalPages?: number
}): OCRActivityProjection {
  const raw = run.phase || run.status || ''
  const mapped = RUN_PHASES[raw as keyof typeof RUN_PHASES]
  if (!mapped) {
    return closedUnknown(run.runId, 'run', run.errorCode ?? null, run.completedPages ?? null, run.totalPages ?? null)
  }
  return projectMapped(
    run.runId,
    'run',
    mapped,
    run.errorCode ?? null,
    Boolean(run.retryable),
    run.completedPages ?? null,
    run.totalPages ?? null,
    false,
  )
}
