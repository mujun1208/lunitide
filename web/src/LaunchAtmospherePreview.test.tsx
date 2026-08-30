import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { expect, it, vi } from 'vitest'

vi.mock('./session/companion/visual/webglSupport', () => ({
  canUseCompanionWebgl: () => true,
  prefersCompanionStillVisual: () => false,
}))

vi.mock('./session/companion/visual/Aurora', () => ({
  Aurora: (props: { lightMode?: boolean }) => <div data-testid="launch-aurora" data-light={String(!!props.lightMode)} />,
}))

import { LaunchAtmospherePreview } from './LaunchAtmospherePreview'

it('puts a theme-aware aurora behind the home stage', async () => {
  const user = userEvent.setup()
  render(<LaunchAtmospherePreview />)
  expect(document.querySelector('.atmosphere')?.getAttribute('data-aurora')).toBe('webgl')
  expect(screen.getByTestId('launch-aurora')).toHaveAttribute('data-light', 'false')
  await user.click(screen.getByRole('button', { name: '白天' }))
  expect(document.documentElement.dataset.theme).toBe('light')
  expect(screen.getByTestId('launch-aurora')).toHaveAttribute('data-light', 'true')
})
