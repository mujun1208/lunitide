import { cleanup, render, screen } from '@testing-library/react'
import { afterEach, expect, it, vi } from 'vitest'
import { BridgeClientError, type DeliverableBridge, type ProjectAttachmentBridge, type ProjectBridge, type TemplateBridge } from '../bridge/client'
import type { ProjectDTO } from '../generated/bridge'
import { DeliverablePanel } from './DeliverablePanel'

afterEach(cleanup)
const project = { id: '01ARZ3NDEKTSV4RRFFQ69G5FAV', name: 'Project', projectCode: 'ITM00001', type: 'implementation', status: 'in_progress', version: 1 } as ProjectDTO

it('does not show raw English list failures', async () => {
  render(
    <DeliverablePanel
      project={project}
      phase={1}
      bridge={{} as ProjectBridge}
      deliverableBridge={{ list: vi.fn().mockRejectedValue(new Error('Failed to fetch')) } as unknown as DeliverableBridge}
      projectAttachments={{ list: vi.fn().mockResolvedValue({ items: [] }) } as unknown as ProjectAttachmentBridge}
      templates={{ list: vi.fn().mockResolvedValue({ items: [] }) } as unknown as TemplateBridge}
    />,
  )
  expect(await screen.findByRole('alert')).toHaveTextContent('请求失败')
  expect(screen.queryByText('Failed to fetch')).toBeNull()
})

it('does not leak BridgeClientError transport English from the deliverable list', async () => {
  render(
    <DeliverablePanel
      project={project}
      phase={1}
      bridge={{} as ProjectBridge}
      deliverableBridge={{ list: vi.fn().mockRejectedValue(new BridgeClientError('Failed to fetch', 'ENGINE_UNAVAILABLE', true, 'engine')) } as unknown as DeliverableBridge}
      projectAttachments={{ list: vi.fn().mockResolvedValue({ items: [] }) } as unknown as ProjectAttachmentBridge}
      templates={{ list: vi.fn().mockResolvedValue({ items: [] }) } as unknown as TemplateBridge}
    />,
  )
  expect(await screen.findByRole('alert')).toHaveTextContent('请求失败')
  expect(screen.queryByText('Failed to fetch')).toBeNull()
})
