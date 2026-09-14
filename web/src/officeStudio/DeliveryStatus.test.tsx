import { cleanup, render, screen } from '@testing-library/react'
import { afterEach, expect, it } from 'vitest'
import { DeliveryStatus } from './DeliveryStatus'

afterEach(cleanup)

it('TestModelFitAndOfficeOutcomeUI: renders FormalDecision not quality passed', () => {
  render(
    <DeliveryStatus
      decision={{
        decisionId: '01ARZ3NDEKTSV4RRFFQ69G5FAD',
        allowed: false,
        state: 'needs_review',
        missingChecks: ['actual-render', 'file-integrity'],
      }}
      quality="passed"
    />,
  )
  expect(screen.getByText('needs_review')).toBeInTheDocument()
  expect(screen.getByText('01ARZ3NDEKTSV4RRFFQ69G5FAD')).toBeInTheDocument()
  expect(screen.getByText('actual-render')).toBeInTheDocument()
  expect(screen.getByText('file-integrity')).toBeInTheDocument()
  expect(screen.queryByText('verified')).toBeNull()
  expect(screen.queryByText(/可作为正式交付/)).toBeNull()
})
