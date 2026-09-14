import { cleanup, fireEvent, render, screen } from '@testing-library/react'
import { afterEach, expect, it, vi } from 'vitest'
import { TaskOutcomePanel } from './TaskOutcomePanel'
import type { TaskOutcome } from './taskOutcome'

afterEach(cleanup)

const taskId = '01ARZ3NDEKTSV4RRFFQ69G5FAV'

const missing: TaskOutcome = {
  taskId,
  goalRevision: 1,
  version: 1,
  state: 'succeeded',
  completion: 'verified',
  reasonCode: 'ok',
  requiredSteps: ['deliver-pptx'],
  passedSteps: ['deliver-pptx'],
  remainingSteps: [],
  evidenceRefs: ['layout'],
  artifactRefs: [''],
}

it('TestModelFitAndOfficeOutcomeUI: ended run with empty path shows partial, not a green complete mark', () => {
  const { container } = render(
    <TaskOutcomePanel
      streamStatus="done"
      outcome={missing}
      artifactPaths={['']}
      requiredFiles
    />,
  )
  expect(screen.queryByLabelText('任务进行中')).toBeNull()
  expect(screen.getByText('已回复')).toBeInTheDocument()
  expect(screen.getByText('部分完成')).toBeInTheDocument()
  expect(container.querySelector('[data-complete="green"]')).toBeNull()
  expect(screen.queryByText('已验证完成')).toBeNull()
})

it('does not show a green complete mark for succeeded+verified with empty artifactRefs', () => {
  const { container } = render(
    <TaskOutcomePanel
      streamStatus="done"
      outcome={{ ...missing, artifactRefs: [] }}
      artifactPaths={[]}
    />,
  )
  expect(screen.getByText('部分完成')).toBeInTheDocument()
  expect(container.querySelector('[data-complete="green"]')).toBeNull()
  expect(screen.queryByText('已验证完成')).toBeNull()
  expect(screen.queryByLabelText('任务进行中')).toBeNull()
})

it('shows unverified history and keeps read/download when TaskOutcome is absent', () => {
  render(
    <TaskOutcomePanel
      streamStatus="idle"
      artifactPaths={['report.docx']}
      requiredFiles={false}
    />,
  )
  expect(screen.getByText('历史结果未验证')).toBeInTheDocument()
  expect(screen.getByText('report.docx')).toBeInTheDocument()
})

it('TestModelFitAndOfficeOutcomeUI: missing renderer checks stay visible and copy is not hidden', () => {
  render(
    <TaskOutcomePanel
      streamStatus="done"
      outcome={{
        ...missing,
        completion: 'unverified',
        remainingSteps: ['render'],
        evidenceRefs: ['actual-render'],
        artifactRefs: ['draft.docx'],
      }}
      artifactPaths={['draft.docx']}
      requiredFiles
    />,
  )
  expect(screen.getByText('部分完成')).toBeInTheDocument()
  expect(screen.getByText('actual-render')).toBeInTheDocument()
  expect(screen.queryByText('已验证完成')).toBeNull()
  expect(screen.getByText('draft.docx')).toBeInTheDocument()
})

it('offers continue with the same TaskID when budget is exhausted', () => {
  const onContinue = vi.fn()
  render(
    <TaskOutcomePanel
      streamStatus="done"
      outcome={{
        ...missing,
        state: 'paused',
        completion: 'unverified',
        reasonCode: 'TASK_BUDGET_EXHAUSTED',
        artifactRefs: ['draft.pptx'],
      }}
      artifactPaths={['draft.pptx']}
      requiredFiles
      onContinue={onContinue}
    />,
  )
  fireEvent.click(screen.getByRole('button', { name: '继续此任务' }))
  expect(onContinue).toHaveBeenCalledWith(taskId)
})
