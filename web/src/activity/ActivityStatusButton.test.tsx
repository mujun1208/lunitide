import { cleanup, render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, expect, it } from 'vitest'
import type { ActivitySnapshotDTO } from '../generated/bridge'
import { ActivityCenter } from './ActivityCenter'
import { ActivityStatusButton } from './ActivityStatusButton'
import { OCR_SETTINGS_TARGET } from '../settings/ocrActivityAdapter'

afterEach(() => cleanup())

const item = (overrides: Partial<ActivitySnapshotDTO>): ActivitySnapshotDTO => ({
  activityId: '01ARZ3NDEKTSV4RRFFQ69G5FAV',
  domain: 'ocr',
  kind: 'pack',
  phase: 'succeeded',
  terminal: true,
  verificationStatus: 'confirmed',
  verificationSource: 'none',
  title: 'paddleocr-vl-1.6',
  completedUnits: null,
  totalUnits: null,
  errorCode: null,
  retryable: false,
  recoveryAction: 'none',
  scopeKind: 'user',
  scopeId: null,
  createdAt: '2026-01-01T00:00:00Z',
  updatedAt: '2026-01-01T00:00:00Z',
  rootOperationId: '01ARZ3NDEKTSV4RRFFQ69G5FAV',
  ...overrides,
})

it('TestActivityCenter: keeps the top entry quiet for ordinary success and alerts on uncertain', async () => {
  const user = userEvent.setup()
  const succeeded = item({ phase: 'succeeded', terminal: true })
  const failed = item({ activityId: '01ARZ3NDEKTSV4RRFFQ69G5FAW', phase: 'uncertain', terminal: true, verificationStatus: 'unconfirmed', recoveryAction: 'open_settings', errorCode: 'NO_VERIFIED_RUNTIME_PROFILE' })
  const { rerender } = render(<ActivityStatusButton items={[succeeded]} open={false} onToggle={() => {}} />)
  expect(screen.getByRole('button', { name: '运行状态' })).toHaveClass('is-quiet')
  rerender(<ActivityStatusButton items={[failed]} open={false} onToggle={() => {}} />)
  expect(screen.getByRole('button', { name: '有任务需要处理' })).toHaveClass('is-alert')
  render(<ActivityCenter items={[failed]} open onClose={() => {}} onRecover={() => {}} />)
  expect(screen.getByRole('dialog', { name: '活动' })).toBeInTheDocument()
  expect(screen.getByRole('button', { name: '打开设置' })).toBeInTheDocument()
  await user.click(screen.getByRole('button', { name: '打开设置' }))
  expect(OCR_SETTINGS_TARGET).toEqual({ settingsCategory: 'personal', settingsIntelligenceView: 'ocr' })
})

it('TestActivityCenter: retry and open-player recoveries stay explicit', async () => {
  const user = userEvent.setup()
  const recovered: string[] = []
  const failed = item({
    activityId: '01ARZ3NDEKTSV4RRFFQ69G5FAX',
    domain: 'media',
    kind: 'play',
    phase: 'failed',
    terminal: true,
    retryable: true,
    recoveryAction: 'retry',
    title: '夜曲',
  })
  const player = item({
    activityId: '01ARZ3NDEKTSV4RRFFQ69G5FAY',
    domain: 'media',
    kind: 'play',
    phase: 'failed',
    terminal: true,
    recoveryAction: 'open_player',
    title: '夜曲',
  })
  render(<ActivityCenter items={[failed, player]} open onClose={() => {}} onRecover={item => recovered.push(item.recoveryAction)} />)
  expect(screen.getByRole('button', { name: '重试' })).toBeInTheDocument()
  expect(screen.getByRole('button', { name: '打开播放器' })).toBeInTheDocument()
  await user.click(screen.getByRole('button', { name: '重试' }))
  await user.click(screen.getByRole('button', { name: '打开播放器' }))
  expect(recovered).toEqual(['retry', 'open_player'])
})
