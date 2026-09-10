import { act, cleanup, render, screen } from '@testing-library/react'
import { afterEach, expect, it, vi } from 'vitest'
import { RootErrorBoundary } from './RootErrorBoundary'

afterEach(cleanup)

it('does not leak raw English transport failures on the crash screen', () => {
  const spy = vi.spyOn(console, 'error').mockImplementation(() => {})
  const Boom = () => {
    throw new Error('Failed to fetch')
  }
  render(
    <RootErrorBoundary>
      <Boom />
    </RootErrorBoundary>,
  )
  expect(screen.getByRole('alert')).toHaveTextContent('界面运行时错误')
  expect(screen.queryByText('Failed to fetch')).toBeNull()
  cleanup()
  const Chinese = () => {
    throw new Error('核心引擎暂时不可用')
  }
  render(
    <RootErrorBoundary>
      <Chinese />
    </RootErrorBoundary>,
  )
  expect(screen.getByRole('alert')).toHaveTextContent('核心引擎暂时不可用')
  spy.mockRestore()
})

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

it('does not recurse when console.error re-dispatches a window error', () => {
  const spy = vi.spyOn(console, 'error').mockImplementation(() => {
    window.dispatchEvent(new ErrorEvent('error', { error: new Error('log boom'), message: 'log boom' }))
  })
  render(
    <RootErrorBoundary>
      <p>工作台</p>
    </RootErrorBoundary>,
  )
  act(() => {
    window.dispatchEvent(new ErrorEvent('error', { error: new Error('first'), message: 'first' }))
  })
  expect(screen.getByRole('alert')).toHaveTextContent('first')
  expect(screen.queryByText('工作台')).toBeNull()
  spy.mockRestore()
})

it('does not leak window-error transport English on the crash screen', () => {
  const spy = vi.spyOn(console, 'error').mockImplementation(() => {})
  render(
    <RootErrorBoundary>
      <p>工作台</p>
    </RootErrorBoundary>,
  )
  act(() => {
    window.dispatchEvent(new ErrorEvent('error', { error: new Error('Failed to fetch'), message: 'Failed to fetch' }))
  })
  expect(screen.getByRole('alert')).toHaveTextContent('界面运行时错误')
  expect(screen.queryByText('Failed to fetch')).toBeNull()
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
