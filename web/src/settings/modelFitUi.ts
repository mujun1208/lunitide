export type ModelFitPhase = 'in_progress' | 'failed' | 'expired' | 'activated'

const PHASE_LABEL: Record<ModelFitPhase, string> = {
  in_progress: '探测进行中',
  failed: '探测失败',
  expired: '探测已过期',
  activated: '已激活',
}

export function presentModelFit(opts: { phase: ModelFitPhase; source: 'fixture' | 'live'; status?: string }): {
  phase: ModelFitPhase
  label: string
  liveQualified: boolean
} {
  return {
    phase: opts.phase,
    label: PHASE_LABEL[opts.phase],
    liveQualified: false,
  }
}
