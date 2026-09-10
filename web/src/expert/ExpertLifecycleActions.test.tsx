import { cleanup, fireEvent, render, screen } from '@testing-library/react'
import { afterEach, expect, it, vi } from 'vitest'
import type { ExpertBridge } from '../bridge/client'
import type { ExpertListResult } from '../generated/bridge'
import { ExpertLifecycleActions } from './ExpertLifecycleActions'

afterEach(cleanup)
const item = {
  expertId: '01ARZ3NDEKTSV4RRFFQ69G5FAA',
  name: '短剧创作专家',
  semver: '1.0.0',
  state: 'enabled',
  creationOrigin: 'manual',
  isOwn: true,
} as ExpertListResult['experts'][number]

it('does not show raw English trial failures', async () => {
  const bridge = { try: vi.fn().mockRejectedValue(new Error('Failed to fetch')) } as unknown as ExpertBridge
  render(<ExpertLifecycleActions item={item} versionId="v1" bridge={bridge} disabled={false} onDeleted={vi.fn()} />)
  fireEvent.click(screen.getByRole('button', { name: /试用/ }))
  fireEvent.change(screen.getByRole('textbox'), { target: { value: '试一题' } })
  fireEvent.click(screen.getByRole('button', { name: '开始试答' }))
  expect(await screen.findByRole('alert')).toHaveTextContent('操作失败，请重试')
  expect(screen.queryByText('Failed to fetch')).toBeNull()
})
