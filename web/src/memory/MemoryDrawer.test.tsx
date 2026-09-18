import { cleanup, fireEvent, render, screen } from '@testing-library/react'
import { afterEach, expect, it } from 'vitest'
import { MemoryDrawer } from './MemoryDrawer'

afterEach(cleanup)

it('renders a single complementary drawer and closes on Escape', () => {
  const closed: Array<string> = []
  render(
    <MemoryDrawer state={{ kind: 'settings' }} onClose={() => closed.push('close')}>
      <p>设置内容</p>
    </MemoryDrawer>,
  )
  expect(screen.getAllByRole('complementary')).toHaveLength(1)
  expect(screen.getByText('设置内容')).toBeInTheDocument()
  fireEvent.keyDown(window, { key: 'Escape' })
  expect(closed).toEqual(['close'])
})
