import { afterEach, expect, it, vi } from 'vitest'
import { applyLiveChatEvent, resetLiveChatForTests, startLiveChat } from './liveChat'
import { presentModelFit } from '../settings/modelFitUi'
import { presentFormalDecision } from '../officeStudio/officeQualityUi'
import {
  applyTaskOutcomeEvent,
  presentTaskOutcome,
  type TaskOutcome,
  type TaskOutcomeEvent,
} from './taskOutcome'

afterEach(resetLiveChatForTests)

const taskId = '01ARZ3NDEKTSV4RRFFQ69G5FAV'

function outcome(patch: Partial<TaskOutcome> = {}): TaskOutcome {
  return {
    taskId,
    goalRevision: 1,
    version: 1,
    state: 'incomplete',
    completion: 'unverified',
    reasonCode: 'missing_artifact',
    requiredSteps: ['deliver-pptx'],
    passedSteps: [],
    remainingSteps: ['deliver-pptx'],
    evidenceRefs: [],
    artifactRefs: [],
    ...patch,
  }
}

it('TestModelFitAndOfficeOutcomeUI: probe fixture states never show live qualified', () => {
  for (const phase of ['in_progress', 'failed', 'expired', 'activated'] as const) {
    const view = presentModelFit({ phase, source: 'fixture', status: 'qualified' })
    expect(view.liveQualified).toBe(false)
    expect(view.label.toLowerCase()).not.toContain('qualified')
    expect(view.label).not.toContain('高端商用')
  }
})

it('TestModelFitAndOfficeOutcomeUI: missing renderer decision is needs_review with checks, not verified', () => {
  const view = presentFormalDecision({
    decisionId: '01ARZ3NDEKTSV4RRFFQ69G5FAD',
    allowed: false,
    state: 'needs_review',
    missingChecks: ['actual-render'],
  })
  expect(view.verified).toBe(false)
  expect(view.state).toBe('needs_review')
  expect(view.missingChecks).toContain('actual-render')
  expect(view.label).not.toBe('verified')
})

it('TestModelFitAndOfficeOutcomeUI: quality passed is not a second formal verdict', () => {
  const view = presentFormalDecision(undefined, { quality: 'passed', validations: [] })
  expect(view.verified).toBe(false)
  expect(view.allowed).toBe(false)
})

it('TestModelFitAndOfficeOutcomeUI: missing artifact is not green complete and spinner ends', () => {
  const activity = vi.fn()
  const entry = startLiveChat('outcome-session', 'outcome-turn', activity)
  applyLiveChatEvent(entry, {
    v: '1.0',
    kind: 'event',
    id: '01ARZ3NDEKTSV4RRFFQ69G5FAA',
    streamId: '01ARZ3NDEKTSV4RRFFQ69G5FAB',
    sequence: 1,
    type: 'completed',
    completed: {
      taskOutcome: outcome({ state: 'succeeded', completion: 'verified', artifactRefs: [''] }),
    },
  })
  expect(entry.terminal).toBe(true)
  expect(activity).toHaveBeenCalledWith(false)
  const view = presentTaskOutcome({
    outcome: entry.state.taskOutcome ?? outcome({ state: 'succeeded', completion: 'verified', artifactRefs: [''] }),
    streamStatus: 'done',
    artifactPaths: [''],
    requiredFiles: true,
  })
  expect(view.spinner).toBe(false)
  expect(view.greenComplete).toBe(false)
  expect(view.taskComplete).toBe(false)
  expect(view.tone).toBe('partial')
  expect(view.label).toBe('部分完成')
})

it('does not treat succeeded+verified with an empty artifactRefs list as complete', () => {
  const view = presentTaskOutcome({
    outcome: outcome({ state: 'succeeded', completion: 'verified', reasonCode: 'ok', remainingSteps: [], artifactRefs: [] }),
    streamStatus: 'done',
    artifactPaths: [],
  })
  expect(view.greenComplete).toBe(false)
  expect(view.taskComplete).toBe(false)
  expect(view.tone).toBe('partial')
  expect(view.label).toBe('部分完成')
  expect(view.spinner).toBe(false)
})

it('does not treat an artifact ref id without a filesystem path as complete', () => {
  const view = presentTaskOutcome({
    outcome: outcome({ state: 'succeeded', completion: 'verified', reasonCode: 'ok', remainingSteps: [], artifactRefs: ['art-1'] }),
    streamStatus: 'done',
    artifactPaths: [],
  })
  expect(view.greenComplete).toBe(false)
  expect(view.taskComplete).toBe(false)
  expect(view.tone).toBe('partial')
  expect(view.spinner).toBe(false)
})

it('does not treat bridge success with an empty path as complete', () => {
  const view = presentTaskOutcome({
    outcome: outcome({ state: 'succeeded', completion: 'verified', reasonCode: 'ok', remainingSteps: [], artifactRefs: [''] }),
    streamStatus: 'done',
    artifactPaths: [''],
    requiredFiles: true,
  })
  expect(view.greenComplete).toBe(false)
  expect(view.taskComplete).toBe(false)
  expect(view.tone).not.toBe('complete')
  expect(view.spinner).toBe(false)
})

it('stops the spinner when the run fails', () => {
  const activity = vi.fn()
  const entry = startLiveChat('fail-session', 'fail-turn', activity)
  applyLiveChatEvent(entry, {
    v: '1.0',
    kind: 'event',
    id: '01ARZ3NDEKTSV4RRFFQ69G5FAC',
    streamId: '01ARZ3NDEKTSV4RRFFQ69G5FAD',
    sequence: 1,
    type: 'failed',
    error: { code: 'STREAM_FAILED', message: '模型中断', retryable: false },
  })
  expect(entry.terminal).toBe(true)
  expect(activity).toHaveBeenCalledWith(false)
  const view = presentTaskOutcome({ outcome: outcome({ state: 'failed' }), streamStatus: 'failed', artifactPaths: [] })
  expect(view.spinner).toBe(false)
  expect(view.greenComplete).toBe(false)
})

it('keeps reply-ended separate from verified task complete', () => {
  const replied = presentTaskOutcome({
    outcome: outcome({ state: 'succeeded', completion: 'not_required', remainingSteps: [], reasonCode: '' }),
    streamStatus: 'done',
    artifactPaths: [],
    requiredFiles: false,
  })
  expect(replied.replyEnded).toBe(true)
  expect(replied.greenComplete).toBe(false)
  expect(replied.label).toBe('已回复')

  const verified = presentTaskOutcome({
    outcome: outcome({
      state: 'succeeded',
      completion: 'verified',
      remainingSteps: [],
      passedSteps: ['deliver-pptx'],
      artifactRefs: ['deck.pptx'],
      reasonCode: '',
    }),
    streamStatus: 'done',
    artifactPaths: ['deck.pptx'],
    requiredFiles: true,
  })
  expect(verified.greenComplete).toBe(true)
  expect(verified.label).toBe('已验证完成')
})

it('marks old sessions without TaskOutcome as unverified history', () => {
  const view = presentTaskOutcome({ outcome: undefined, streamStatus: 'idle', artifactPaths: ['notes.txt'], requiredFiles: false })
  expect(view.tone).toBe('unverified')
  expect(view.label).toBe('历史结果未验证')
  expect(view.greenComplete).toBe(false)
})

it('applies task_outcome events by goalRevision then outcomeVersion', () => {
  const first: TaskOutcomeEvent = {
    taskId,
    goalRevision: 1,
    outcomeVersion: 1,
    lastSequence: 1,
    outcome: outcome({ version: 1, state: 'incomplete' }),
  }
  let state = applyTaskOutcomeEvent(undefined, first)
  expect(state.outcome?.version).toBe(1)
  expect(state.needsGet).toBe(false)

  state = applyTaskOutcomeEvent(state, { ...first, lastSequence: 1 })
  expect(state.ignored).toBe('duplicate')

  state = applyTaskOutcomeEvent(state, {
    taskId,
    goalRevision: 1,
    outcomeVersion: 0,
    lastSequence: 1,
    outcome: outcome({ version: 0, state: 'succeeded', completion: 'verified' }),
  })
  expect(state.ignored).toBe('reverse')
  expect(state.outcome?.state).toBe('incomplete')

  state = applyTaskOutcomeEvent(state, {
    taskId,
    goalRevision: 2,
    outcomeVersion: 1,
    lastSequence: 4,
    outcome: outcome({ goalRevision: 2, version: 1, state: 'paused', reasonCode: 'TASK_BUDGET_EXHAUSTED' }),
  })
  expect(state.outcome?.goalRevision).toBe(2)
  expect(state.needsGet).toBe(true)
})
