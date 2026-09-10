import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterEach, expect, it, vi } from 'vitest'
import { BridgeClientError, type SkillImportBridge } from '../bridge/client'
import { SkillImportWizard } from './SkillImportWizard'

afterEach(cleanup)
const id = '01ARZ3NDEKTSV4RRFFQ69G5FAA'
const sha = '0123456789abcdef0123456789abcdef01234567'
const summary = { name: 'notes-helper', description: 'Summarize supplied notes', license: 'unknown', archiveHash: 'b'.repeat(64), skippedFiles: 2 }
const api = (overrides: Partial<SkillImportBridge> = {}): SkillImportBridge => ({
  discover: vi.fn().mockResolvedValue({ candidateId: id, state: 'discovered', version: 1, summary }),
  inspect: vi.fn().mockResolvedValue({ candidateId: id, state: 'inspected', version: 3, summary }),
  submit: vi.fn().mockResolvedValue({ candidateId: id, state: 'awaiting_approval', version: 6, summary }),
  approve: vi.fn().mockResolvedValue({ candidateId: id, state: 'approved', version: 7, summary, skillId: id }),
  reject: vi.fn(), revoke: vi.fn(), ...overrides,
})

async function discover() {
  fireEvent.change(screen.getByLabelText('GitHub 仓库或技能目录 URL'), { target: { value: 'https://github.com/acme/notes' } })
  fireEvent.change(screen.getByLabelText('固定提交 SHA'), { target: { value: sha } })
  fireEvent.click(screen.getByRole('button', { name: '读取技能' }))
  await screen.findByText('notes-helper')
}

it('does not leak BridgeClientError transport English from discover', async () => {
  render(<SkillImportWizard open onClose={vi.fn()} bridge={api({ discover: vi.fn().mockRejectedValue(new BridgeClientError('Failed to fetch', 'ENGINE_UNAVAILABLE', true, 'engine')) })} />)
  fireEvent.change(screen.getByLabelText('GitHub 仓库或技能目录 URL'), { target: { value: 'https://github.com/acme/notes' } })
  fireEvent.change(screen.getByLabelText('固定提交 SHA'), { target: { value: sha } })
  fireEvent.click(screen.getByRole('button', { name: '读取技能' }))
  expect(await screen.findByRole('alert')).toHaveTextContent('请求失败')
  expect(screen.queryByText('Failed to fetch')).toBeNull()
  cleanup()
  render(<SkillImportWizard open onClose={vi.fn()} bridge={api({ discover: vi.fn().mockRejectedValue(new BridgeClientError('FEATURE_DISABLED: catalog inspect', 'FEATURE_DISABLED', false, 'engine')) })} />)
  fireEvent.change(screen.getByLabelText('GitHub 仓库或技能目录 URL'), { target: { value: 'https://github.com/acme/notes' } })
  fireEvent.change(screen.getByLabelText('固定提交 SHA'), { target: { value: sha } })
  fireEvent.click(screen.getByRole('button', { name: '读取技能' }))
  expect(await screen.findByRole('alert')).toHaveTextContent('FEATURE_DISABLED: catalog inspect')
})

it('does not show raw English discover failures', async () => {
  render(<SkillImportWizard open onClose={vi.fn()} bridge={api({ discover: vi.fn().mockRejectedValue(new Error('Failed to fetch')) })} />)
  fireEvent.change(screen.getByLabelText('GitHub 仓库或技能目录 URL'), { target: { value: 'https://github.com/acme/notes' } })
  fireEvent.change(screen.getByLabelText('固定提交 SHA'), { target: { value: sha } })
  fireEvent.click(screen.getByRole('button', { name: '读取技能' }))
  expect(await screen.findByRole('alert')).toHaveTextContent('请求失败')
  expect(screen.queryByText('Failed to fetch')).toBeNull()
})

it('sends source identity only and imports a draft using backend evidence', async () => {
  const bridge = api(), onApproved = vi.fn()
  render(<SkillImportWizard open onClose={vi.fn()} onApproved={onApproved} bridge={bridge} />)
  await discover()
  expect(bridge.discover).toHaveBeenCalledWith({ assetType: 'skill', sourceUrl: 'https://github.com/acme/notes', immutableCommit: sha }, expect.anything())
  expect(screen.getByText(/未知（仓库未提供/)).toBeTruthy()
  expect(screen.getByText('未导入的其他文件：2 个')).toBeTruthy()
  fireEvent.click(screen.getByRole('button', { name: '校验固定文件' }))
  fireEvent.click(await screen.findByRole('button', { name: '运行静态检查' }))
  await screen.findByRole('button', { name: '批准导入草稿' })
  expect(bridge.submit).toHaveBeenCalledWith({ candidateId: id, expectedVersion: 3 }, expect.anything())
  expect(onApproved).not.toHaveBeenCalled()
  fireEvent.click(screen.getByRole('button', { name: '批准导入草稿' }))
  await screen.findByText(/技能已导入为草稿/)
  expect(bridge.approve).toHaveBeenCalledWith({ candidateId: id, expectedVersion: 6, approval: { source: 'github-import', scope: 'instructions-only-draft' } }, expect.anything())
  expect(onApproved).toHaveBeenCalledOnce()
  expect(onApproved).toHaveBeenCalledWith(id)
})

it('keeps a failed real scan before approval and displays the error', async () => {
  const bridge = api({ submit: vi.fn().mockRejectedValue(new BridgeClientError('正文检查未通过', 'SKILL_IMPORT_SCAN_REJECTED', false, 'engine')) })
  const onApproved = vi.fn()
  render(<SkillImportWizard open onClose={vi.fn()} onApproved={onApproved} bridge={bridge} />)
  await discover()
  fireEvent.click(screen.getByRole('button', { name: '校验固定文件' }))
  fireEvent.click(await screen.findByRole('button', { name: '运行静态检查' }))
  expect(await screen.findByRole('alert')).toHaveTextContent('正文检查未通过')
  expect(screen.queryByRole('button', { name: '批准导入草稿' })).toBeNull()
  expect(bridge.approve).not.toHaveBeenCalled()
  expect(onApproved).not.toHaveBeenCalled()
})

it('resumes the committed awaiting-approval step after reopening', async () => {
  const bridge = api({ discover: vi.fn().mockResolvedValue({ candidateId: id, state: 'awaiting_approval', version: 6, summary }) })
  render(<SkillImportWizard open onClose={vi.fn()} bridge={bridge} />)
  await discover()
  await waitFor(() => expect(screen.getByRole('button', { name: '批准导入草稿' })).toBeEnabled())
  expect(bridge.inspect).not.toHaveBeenCalled()
  expect(bridge.submit).not.toHaveBeenCalled()
})


it('retries a lost approval ACK with the same mutation attempt', async () => {
  const approve = vi.fn().mockRejectedValueOnce(new BridgeClientError('响应丢失', 'TIMEOUT', true, 'renderer')).mockResolvedValueOnce({ candidateId: id, state: 'approved', version: 7, summary, skillId: id })
  const bridge = api({ discover: vi.fn().mockResolvedValue({ candidateId: id, state: 'awaiting_approval', version: 6, summary }), approve })
  render(<SkillImportWizard open onClose={vi.fn()} bridge={bridge} />)
  await discover()
  fireEvent.click(screen.getByRole('button', { name: '批准导入草稿' }))
  await screen.findByRole('alert')
  fireEvent.click(screen.getByRole('button', { name: '批准导入草稿' }))
  await screen.findByText(/技能已导入为草稿/)
  expect(approve.mock.calls[1][0]).toEqual(approve.mock.calls[0][0])
  expect(approve.mock.calls[1][1].attempt).toBe(approve.mock.calls[0][1].attempt)
})

it('shows an already committed import as completed after reopening', async () => {
  const bridge = api({ discover: vi.fn().mockResolvedValue({ candidateId: id, state: 'approved', version: 7, summary, skillId: id }) })
  const onApproved = vi.fn()
  render(<SkillImportWizard open onClose={vi.fn()} onApproved={onApproved} bridge={bridge} />)
  await discover()
  await screen.findByText(/技能已导入为草稿/)
  expect(screen.queryByRole('button', { name: '批准导入草稿' })).toBeNull()
  expect(bridge.approve).not.toHaveBeenCalled()
  expect(onApproved).toHaveBeenCalledOnce()
  expect(onApproved).toHaveBeenCalledWith(id)
})
