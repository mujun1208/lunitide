import { cleanup, fireEvent, render, screen } from '@testing-library/react'
import { afterEach, expect, it, vi } from 'vitest'
import { PendingMemoryBanner } from './PendingMemoryBanner'

afterEach(() => cleanup())

it('lets the user confirm or park a pending preference in the session', () => {
  const onConfirm = vi.fn()
  const onLater = vi.fn()
  render(<PendingMemoryBanner item={{ candidateId: '01ARZ3NDEKTSV4RRFFQ69G5FAA', confirmationToken: 'tok', content: '以后回答默认用中文' }} onConfirm={onConfirm} onLater={onLater} />)
  expect(screen.getByRole('status', { name: '待确认偏好' })).toHaveTextContent('确认后才进长期记忆')
  fireEvent.click(screen.getByRole('button', { name: '确认沉淀' }))
  fireEvent.click(screen.getByRole('button', { name: '以后再说' }))
  expect(onConfirm).toHaveBeenCalledOnce()
  expect(onLater).toHaveBeenCalledOnce()
})

it('floats above the companion stage when overlay is on', () => {
  render(<PendingMemoryBanner overlay item={{ candidateId: '01ARZ3NDEKTSV4RRFFQ69G5FAA', confirmationToken: 'tok', content: '以后回答默认用中文' }} onConfirm={() => {}} onLater={() => {}} />)
  expect(screen.getByRole('status', { name: '待确认偏好' })).toHaveClass('companion-float')
})
