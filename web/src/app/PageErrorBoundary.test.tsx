import { act, cleanup, render, screen, fireEvent } from '@testing-library/react'
import { afterEach, expect, it, vi } from 'vitest'
import { PageErrorBoundary } from './PageErrorBoundary'

afterEach(cleanup)

it('does not leak raw English transport failures on the page crash screen', () => {
  const spy = vi.spyOn(console, 'error').mockImplementation(() => {})
  const Boom = () => {
    throw new Error('Failed to fetch')
  }
  render(
    <PageErrorBoundary label="providers">
      <Boom />
    </PageErrorBoundary>,
  )
  expect(screen.getByRole('alert')).toHaveTextContent('本页渲染失败')
  expect(screen.queryByText('Failed to fetch')).toBeNull()
  cleanup()
  const Chinese = () => {
    throw new Error('核心引擎暂时不可用')
  }
  render(
    <PageErrorBoundary label="providers">
      <Chinese />
    </PageErrorBoundary>,
  )
  expect(screen.getByRole('alert')).toHaveTextContent('核心引擎暂时不可用')
  spy.mockRestore()
})

it('shows a per-page retry shell when a child render throws and does not escape to the root', () => {
  const spy = vi.spyOn(console, 'error').mockImplementation(() => {})
  const Boom = () => {
    throw new Error('providers page boom')
  }
  // If the throw escaped this boundary it would reach the test-runner and fail the render call.
  expect(() =>
    render(
      <PageErrorBoundary label="providers">
        <Boom />
      </PageErrorBoundary>,
    ),
  ).not.toThrow()
  expect(screen.getByRole('alert')).toHaveTextContent('providers page boom')
  expect(screen.getByRole('button', { name: '重试' })).toBeInTheDocument()
  spy.mockRestore()
})

it('recovers the page after clicking retry once the child stops throwing', () => {
  const spy = vi.spyOn(console, 'error').mockImplementation(() => {})
  let shouldThrow = true
  const Flaky = () => {
    if (shouldThrow) throw new Error('transient boom')
    return <p>页面已恢复</p>
  }
  render(
    <PageErrorBoundary label="skill">
      <Flaky />
    </PageErrorBoundary>,
  )
  expect(screen.getByRole('alert')).toHaveTextContent('transient boom')
  shouldThrow = false
  act(() => {
    fireEvent.click(screen.getByRole('button', { name: '重试' }))
  })
  expect(screen.getByText('页面已恢复')).toBeInTheDocument()
  expect(screen.queryByRole('alert')).toBeNull()
  spy.mockRestore()
})