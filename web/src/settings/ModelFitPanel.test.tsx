import { cleanup, render, screen } from '@testing-library/react'
import { afterEach, expect, it } from 'vitest'
import { ModelFitPanel } from './ModelFitPanel'

afterEach(cleanup)

it('TestModelFitAndOfficeOutcomeUI: fixture probe phases never show live qualified', () => {
  for (const phase of ['in_progress', 'failed', 'expired', 'activated'] as const) {
    cleanup()
    render(<ModelFitPanel phase={phase} source="fixture" status="qualified" />)
    expect(screen.queryByText(/qualified/i)).toBeNull()
    expect(screen.queryByText('高端商用')).toBeNull()
    expect(screen.getByLabelText('模型适配')).not.toHaveAttribute('data-live-qualified', 'yes')
  }
})
