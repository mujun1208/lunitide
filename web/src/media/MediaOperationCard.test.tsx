import { cleanup, render, screen } from '@testing-library/react'
import { afterEach, expect, it } from 'vitest'
import type { MediaOperationDTO } from '../generated/bridge'
import { MediaOperationCard } from './MediaOperationCard'

afterEach(() => cleanup())

const operation = (phase: MediaOperationDTO['phase']): MediaOperationDTO => ({
  operationId: '01ARZ3NDEKTSV4RRFFQ69G5FAX',
  mediaSessionId: '01ARZ3NDEKTSV4RRFFQ69G5FAV',
  parentOperationId: null,
  rootOperationId: '01ARZ3NDEKTSV4RRFFQ69G5FAX',
  action: 'play',
  phase,
  verificationStatus: phase === 'succeeded' ? 'confirmed' : 'unconfirmed',
  verificationSource: 'owned_runtime',
  errorCode: phase === 'uncertain' ? 'MEDIA_UNVERIFIED' : null,
  revision: 1,
})

it('TestTruthfulOperationStates: does not paint dispatched or uncertain as confirmed success', () => {
  render(<MediaOperationCard operation={operation('uncertain')} />)
  const card = screen.getByLabelText('媒体操作')
  expect(card).toHaveClass('is-error')
  expect(card).toHaveTextContent('未确认')
  expect(card).not.toHaveTextContent('已确认')
  expect(screen.getByRole('status')).toHaveTextContent('MEDIA_UNVERIFIED')
  cleanup()
  render(<MediaOperationCard operation={operation('dispatching')} />)
  expect(screen.getByLabelText('媒体操作')).toHaveTextContent('命令已发送，待核验')
  expect(screen.getByLabelText('媒体操作')).not.toHaveTextContent('已确认')
  cleanup()
  render(<MediaOperationCard operation={operation('succeeded')} />)
  expect(screen.getByLabelText('媒体操作')).toHaveTextContent('已确认')
})
