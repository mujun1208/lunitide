export type TaskOutcomeState = 'succeeded' | 'incomplete' | 'paused' | 'failed' | 'cancelled' | 'outcome_unknown'
export type TaskCompletion = 'verified' | 'not_required' | 'unverified'

export type TaskOutcome = {
  taskId: string
  goalRevision: number
  version: number
  state: TaskOutcomeState
  completion: TaskCompletion
  reasonCode: string
  requiredSteps: string[]
  passedSteps: string[]
  remainingSteps: string[]
  evidenceRefs: string[]
  artifactRefs: string[]
}

export type TaskOutcomeEvent = {
  taskId: string
  goalRevision: number
  outcomeVersion: number
  lastSequence: number
  outcome: TaskOutcome
}

export type TaskOutcomeStore = {
  outcome?: TaskOutcome
  lastSequence?: number
  needsGet: boolean
  ignored?: 'duplicate' | 'reverse'
}

export type TaskOutcomeView = {
  spinner: boolean
  greenComplete: boolean
  replyEnded: boolean
  taskComplete: boolean
  tone: 'complete' | 'partial' | 'failed' | 'unverified' | 'replied' | 'paused' | 'running'
  label: string
}

export type StreamStatus = 'idle' | 'streaming' | 'done' | 'failed' | 'cancelled'

function missingArtifact(outcome: TaskOutcome | undefined, artifactPaths: string[] | undefined, requiredFiles: boolean | undefined): boolean {
  const paths = artifactPaths ?? []
  if (paths.some(path => !String(path).trim())) return true
  if (outcome?.artifactRefs.some(ref => !String(ref).trim())) return true
  if ((outcome?.remainingSteps.length ?? 0) > 0) return true
  const hasPath = paths.some(path => String(path).trim())
  if (requiredFiles && !hasPath) return true
  if (outcome?.state === 'succeeded' && outcome.completion === 'verified' && !hasPath) return true
  return false
}

export function presentTaskOutcome(opts: {
  outcome?: TaskOutcome
  streamStatus: StreamStatus
  artifactPaths?: string[]
  requiredFiles?: boolean
}): TaskOutcomeView {
  const ended = opts.streamStatus === 'done' || opts.streamStatus === 'failed' || opts.streamStatus === 'cancelled'
  const spinner = opts.streamStatus === 'streaming'
  const missing = missingArtifact(opts.outcome, opts.artifactPaths, opts.requiredFiles)
  if (!opts.outcome) {
    if (opts.streamStatus === 'streaming') {
      return { spinner: true, greenComplete: false, replyEnded: false, taskComplete: false, tone: 'running', label: '进行中' }
    }
    return {
      spinner: false,
      greenComplete: false,
      replyEnded: ended,
      taskComplete: false,
      tone: 'unverified',
      label: '历史结果未验证',
    }
  }
  if (opts.streamStatus === 'failed' || opts.outcome.state === 'failed') {
    return { spinner: false, greenComplete: false, replyEnded: ended || opts.outcome.state === 'failed', taskComplete: false, tone: 'failed', label: '失败' }
  }
  if (missing) {
    return { spinner: false, greenComplete: false, replyEnded: ended, taskComplete: false, tone: 'partial', label: '部分完成' }
  }
  if (opts.outcome.state === 'paused') {
    return { spinner: false, greenComplete: false, replyEnded: ended, taskComplete: false, tone: 'paused', label: '已暂停' }
  }
  if (opts.outcome.state === 'succeeded' && opts.outcome.completion === 'verified' && opts.outcome.remainingSteps.length === 0) {
    return { spinner: false, greenComplete: true, replyEnded: ended, taskComplete: true, tone: 'complete', label: '已验证完成' }
  }
  if (opts.outcome.state === 'succeeded' && opts.outcome.completion === 'not_required') {
    return { spinner: false, greenComplete: false, replyEnded: true, taskComplete: false, tone: 'replied', label: '已回复' }
  }
  return { spinner, greenComplete: false, replyEnded: ended, taskComplete: false, tone: spinner ? 'running' : 'partial', label: spinner ? '进行中' : '部分完成' }
}

export function applyTaskOutcomeEvent(prev: TaskOutcomeStore | undefined, event: TaskOutcomeEvent): TaskOutcomeStore {
  const current = prev?.outcome
  if (current) {
    if (event.goalRevision === current.goalRevision && event.outcomeVersion === current.version) {
      return { ...prev, ignored: 'duplicate', needsGet: false }
    }
    if (event.goalRevision < current.goalRevision || (event.goalRevision === current.goalRevision && event.outcomeVersion < current.version)) {
      return { ...prev, ignored: 'reverse', needsGet: false }
    }
  }
  const gap = prev?.lastSequence != null && event.lastSequence > prev.lastSequence + 1
  return { outcome: event.outcome, lastSequence: event.lastSequence, needsGet: gap }
}

export function isTaskOutcome(value: unknown): value is TaskOutcome {
  if (!value || typeof value !== 'object') return false
  const v = value as Record<string, unknown>
  return typeof v.taskId === 'string' && Number.isInteger(v.goalRevision) && Number.isInteger(v.version) && typeof v.state === 'string' && typeof v.completion === 'string'
}

export function coerceTaskOutcome(value: unknown): TaskOutcome | undefined {
  if (!isTaskOutcome(value)) return
  const v = value as TaskOutcome & { reasonCode?: string; requiredSteps?: string[]; passedSteps?: string[]; remainingSteps?: string[]; evidenceRefs?: string[]; artifactRefs?: string[] }
  return {
    taskId: v.taskId,
    goalRevision: v.goalRevision,
    version: v.version,
    state: v.state,
    completion: v.completion,
    reasonCode: v.reasonCode ?? '',
    requiredSteps: v.requiredSteps ?? [],
    passedSteps: v.passedSteps ?? [],
    remainingSteps: v.remainingSteps ?? [],
    evidenceRefs: v.evidenceRefs ?? [],
    artifactRefs: v.artifactRefs ?? [],
  }
}
