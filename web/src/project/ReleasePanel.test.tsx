import React from 'react'
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterEach, expect, it, vi } from 'vitest'
import type { DeliverableBridge, ProjectBridge, ReleaseBridge } from '../bridge/client'
import type { ProjectDTO } from '../generated/bridge'
import { ReleasePanel } from './ReleasePanel'
import { createProjectCrRevision } from './crRevision'

const project = { id: '01ARZ3NDEKTSV4RRFFQ69G5FAV', projectCode: 'ITM00001', name: 'Project', type: 'implementation', status: 'go_live_prep', version: 10 } as ProjectDTO
const digest = 'a'.repeat(64)
const documents = () => ({ list: vi.fn(async ({ phase }: { phase: number }) => ({ items: [{ documentType: phase === 3 ? 'db_design' : phase === 4 ? 'interface_list' : 'dev_checklist', status: 'approved', digest }] })) }) as unknown as DeliverableBridge
afterEach(() => { cleanup(); localStorage.clear() })

it('does not show raw English revision load failures', async () => {
  const release = { getRevision: vi.fn().mockRejectedValue(new Error('Failed to fetch')) } as unknown as ReleaseBridge
  render(<ReleasePanel project={project} bridge={release} deliverables={documents()} />)
  expect(await screen.findByRole('alert')).toHaveTextContent('请求失败')
  expect(screen.queryByText('Failed to fetch')).toBeNull()
})

it('creates a source-bound revision without invented members or size', async () => {
  const createRevision = vi.fn().mockResolvedValue({ crRevisionId: 'revision', revisionNo: 1, digest })
  await createProjectCrRevision(project, 'Release', documents(), { createRevision } as unknown as ReleaseBridge)
  const payload = createRevision.mock.calls[0][0]
  expect(payload.manifest).toMatchObject({ projectId: project.id, projectCode: project.projectCode })
  expect(payload.manifest).not.toHaveProperty('members')
  expect(JSON.stringify(payload)).not.toContain('manifest.stub')
  const empty = { list: vi.fn().mockResolvedValue({ items: [] }) } as unknown as DeliverableBridge
  await expect(createProjectCrRevision(project, 'Empty', empty, { createRevision } as unknown as ReleaseBridge)).rejects.toThrow('真实交付文件')
  expect(createRevision).toHaveBeenCalledTimes(1)
})

it('restores the server revision and completes preparation only after server verification', async () => {
  const revisionId = '01ARZ3NDEKTSV4RRFFQ69G5FAW'
  const release = { getRevision: vi.fn().mockResolvedValue({ revisions: [{ crRevisionId: revisionId, revisionNo: 1, status: 'submitted', digest, createdAt: '2026-09-06' }], manifest: {}, reviews: [] }) } as unknown as ReleaseBridge
  const advanceStatus = vi.fn().mockRejectedValueOnce(new Error('发布文件已改变')).mockResolvedValueOnce({ ...project, version: 11 })
  const updated = vi.fn()
  render(<ReleasePanel project={project} bridge={release} deliverables={documents()} projects={{ advanceStatus } as unknown as ProjectBridge} onProjectUpdated={updated} />)
  await waitFor(() => expect(screen.getByLabelText('CR Revision ID')).toHaveValue(revisionId))
  expect(screen.getByText(/当前发布生成本地制品/)).toBeInTheDocument()
  fireEvent.click(screen.getByRole('button', { name: '核验制品并完成发布准备' }))
  expect(await screen.findByRole('alert')).toHaveTextContent('发布文件已改变')
  expect(updated).not.toHaveBeenCalled()
  fireEvent.click(screen.getByRole('button', { name: '核验制品并完成发布准备' }))
  await waitFor(() => expect(updated).toHaveBeenCalledWith(expect.objectContaining({ status: 'go_live_prep', version: 11 })))
  expect(advanceStatus).toHaveBeenCalledWith({ id: project.id, version: 10, phase: 8 }, expect.anything())
})
