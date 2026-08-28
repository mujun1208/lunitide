import { act, cleanup, render, screen } from '@testing-library/react'
import { afterEach, expect, it, vi } from 'vitest'
import { RootErrorBoundary } from './RootErrorBoundary'

afterEach(cleanup)

it('keeps a recovery shell when a child render throws', () => {
  const spy = vi.spyOn(console, 'error').mockImplementation(() => {})
  const Boom = () => {
    throw new Error('workbench send boom')
  }
  render(
    <RootErrorBoundary>
      <Boom />
    </RootErrorBoundary>,
  )
  expect(screen.getByRole('alert')).toHaveTextContent('workbench send boom')
  expect(screen.getByRole('button', { name: '重新载入' })).toBeInTheDocument()
  spy.mockRestore()
})

it('turns a window error from send into the recovery shell instead of a blank host', () => {
  const spy = vi.spyOn(console, 'error').mockImplementation(() => {})
  render(
    <RootErrorBoundary>
      <p>工作台</p>
    </RootErrorBoundary>,
  )
  expect(screen.getByText('工作台')).toBeInTheDocument()
  act(() => {
    window.dispatchEvent(new ErrorEvent('error', { error: new Error('send threw'), message: 'send threw' }))
  })
  expect(screen.getByRole('alert')).toHaveTextContent('send threw')
  expect(screen.queryByText('工作台')).toBeNull()
  spy.mockRestore()
})
