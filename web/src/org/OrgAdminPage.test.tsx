import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterEach, expect, it, vi } from 'vitest'
import type { OrgBridge } from '../bridge/client'
import { OrgAdminPage } from './OrgAdminPage'

afterEach(cleanup)
const now = '2026-01-01T00:00:00Z'
const orgA = { orgId: '01JDORGAAAAAAAAAAAAAAAAA', name: '组织甲', state: 'active' as const }
const orgB = { orgId: '01JDORGBBBBBBBBBBBBBBB', name: '组织乙', state: 'draft' as const }
const orgDetail = { ...orgA, retentionDays: 2555, createdAt: now, updatedAt: now }
const spaceA = { spaceId: '01JDSPAAAAAAAAAAAAAAAAA', name: '航线维修空间', state: 'active' as const, createdAt: now, updatedAt: now }
const memberA = {
  principal: { principalId: '01JDPRAAAAAAAAAAAAAAAAA', displayName: '张工', state: 'active' as const, bindingVersion: 2, createdAt: now, updatedAt: now },
  bindings: [{ bindingId: '01JDBNAAAAAAAAAAAAAAAAA', principalId: '01JDPRAAAAAAAAAAAAAAAAA', scopeKey: 'org:' + orgA.orgId, role: 'org-admin' as const, expiresAt: '2027-01-01T00:00:00Z', state: 'active' as const, createdAt: now, updatedAt: now }],
}

const api = (o: Partial<OrgBridge> = {}): OrgBridge => ({
  summary: vi.fn().mockResolvedValue({ boundOrgId: '', orgs: [] }),
  create: vi.fn(), switch: vi.fn(), activate: vi.fn(), suspend: vi.fn(),
  spaceList: vi.fn().mockResolvedValue({ spaces: [] }),
  spaceCreate: vi.fn(), memberList: vi.fn().mockResolvedValue({ members: [] }), memberInvite: vi.fn(), memberRevoke: vi.fn(),
  ...o,
})
const gotoTab = (label: string) => fireEvent.click(screen.getByRole('button', { name: new RegExp(label) }))

it('renders empty state when no org exists', async () => {
  const bridge = api()
  render(<OrgAdminPage bridge={bridge} />)
  expect(await screen.findByText('暂无组织')).toBeInTheDocument()
  expect(bridge.summary).toHaveBeenCalledOnce()
  // 隔离门禁：未绑定组织时不读取空间/成员数据
  expect(bridge.spaceList).not.toHaveBeenCalled()
  expect(bridge.memberList).not.toHaveBeenCalled()
})

it('shows isolation gate (M9-003) when orgs exist but none is bound', async () => {
  const bridge = api({ summary: vi.fn().mockResolvedValue({ boundOrgId: '', orgs: [orgA, orgB] }) })
  render(<OrgAdminPage bridge={bridge} />)
  expect(await screen.findByText(/未绑定组织（M9-003）/)).toBeInTheDocument()
  expect(screen.getByText('组织甲')).toBeInTheDocument()
  expect(screen.getByText('组织乙')).toBeInTheDocument()
  expect(bridge.spaceList).not.toHaveBeenCalled()
  expect(bridge.memberList).not.toHaveBeenCalled()
})

it('loads scoped spaces and members only for the bound org', async () => {
  const bridge = api({
    summary: vi.fn().mockResolvedValue({ boundOrgId: orgA.orgId, org: orgDetail, orgs: [orgA, orgB] }),
    spaceList: vi.fn().mockResolvedValue({ spaces: [spaceA] }),
    memberList: vi.fn().mockResolvedValue({ members: [memberA] }),
  })
  render(<OrgAdminPage bridge={bridge} />)
  gotoTab('TeamSpace')
  expect(await screen.findByText('航线维修空间')).toBeInTheDocument()
  gotoTab('成员与身份')
  expect(screen.getByText('张工')).toBeInTheDocument()
  expect(screen.getByText(/组织管理员/)).toBeInTheDocument()
  expect(bridge.spaceList).toHaveBeenCalledOnce()
  expect(bridge.memberList).toHaveBeenCalledOnce()
})

it('revoking a member reloads member list immediately (role changes take effect instantly)', async () => {
  const memberList = vi.fn()
    .mockResolvedValueOnce({ members: [memberA] })
    .mockResolvedValueOnce({ members: [{ ...memberA, principal: { ...memberA.principal, state: 'revoked' as const } }] })
  const bridge = api({
    summary: vi.fn().mockResolvedValue({ boundOrgId: orgA.orgId, org: orgDetail, orgs: [orgA] }),
    memberList,
    memberRevoke: vi.fn().mockResolvedValue({ principalId: memberA.principal.principalId, state: 'revoked', bindingVersion: 3 }),
  })
  render(<OrgAdminPage bridge={bridge} />)
  gotoTab('成员与身份')
  expect(await screen.findByText('张工')).toBeInTheDocument()
  fireEvent.click(screen.getByRole('button', { name: '撤销' }))
  fireEvent.click(screen.getByRole('button', { name: '确认撤销' }))
  await waitFor(() => expect(bridge.memberRevoke).toHaveBeenCalledWith({ principalId: memberA.principal.principalId }, expect.anything()))
  expect(memberList).toHaveBeenCalledTimes(2)
  await waitFor(() => expect(screen.getByText(/即时失效/)).toBeInTheDocument())
})

it('switching org clears previous scoped data and reloads from the new org only', async () => {
  const summary = vi.fn()
    .mockResolvedValueOnce({ boundOrgId: orgA.orgId, org: orgDetail, orgs: [orgA, orgB] })
    .mockResolvedValueOnce({ boundOrgId: orgB.orgId, org: { ...orgDetail, ...orgB, retentionDays: 2555 }, orgs: [orgA, orgB] })
  const spaceList = vi.fn().mockResolvedValueOnce({ spaces: [spaceA] }).mockResolvedValueOnce({ spaces: [] })
  const bridge = api({ summary, spaceList, switch: vi.fn().mockResolvedValue({ ...orgDetail, ...orgB }) })
  render(<OrgAdminPage bridge={bridge} />)
  gotoTab('TeamSpace')
  expect(await screen.findByText('航线维修空间')).toBeInTheDocument()
  gotoTab('组织概览')
  fireEvent.click(screen.getByRole('button', { name: /组织乙/ }))
  await waitFor(() => expect(bridge.switch).toHaveBeenCalledWith({ orgId: orgB.orgId }, expect.anything()))
  // 旧组织空间数据被清除，仅按新绑定重新加载（跨组织数据零出现）
  await waitFor(() => expect(spaceList).toHaveBeenCalledTimes(2))
  gotoTab('TeamSpace')
  await waitFor(() => expect(screen.queryByText('航线维修空间')).not.toBeInTheDocument())
})

it('creates an org and binds it immediately', async () => {
  const summary = vi.fn()
    .mockResolvedValueOnce({ boundOrgId: '', orgs: [] })
    .mockResolvedValueOnce({ boundOrgId: '', orgs: [{ ...orgB, state: 'draft' as const }] })
    .mockResolvedValueOnce({ boundOrgId: orgB.orgId, org: { ...orgDetail, ...orgB }, orgs: [orgB] })
  const bridge = api({ summary, create: vi.fn().mockResolvedValue({ ...orgDetail, ...orgB }), switch: vi.fn() })
  render(<OrgAdminPage bridge={bridge} />)
  await screen.findByText('暂无组织')
  fireEvent.click(screen.getByRole('button', { name: '新建组织' }))
  fireEvent.change(screen.getByLabelText('组织名称'), { target: { value: '组织乙' } })
  fireEvent.click(screen.getByRole('button', { name: '创建并绑定' }))
  await waitFor(() => expect(bridge.create).toHaveBeenCalledWith({ name: '组织乙' }, expect.anything()))
  await waitFor(() => expect(bridge.switch).toHaveBeenCalledWith({ orgId: orgB.orgId }, expect.anything()))
  await waitFor(() => expect(screen.findAllByText('组织乙').then(items => items.length >= 1)).resolves.toBeTruthy())
})

it('activates a draft org through the lifecycle action', async () => {
  const summary = vi.fn()
    .mockResolvedValueOnce({ boundOrgId: orgB.orgId, org: { ...orgDetail, ...orgB }, orgs: [orgB] })
    .mockResolvedValueOnce({ boundOrgId: orgB.orgId, org: { ...orgDetail, ...orgB, state: 'active' as const }, orgs: [{ ...orgB, state: 'active' as const }] })
  const bridge = api({ summary, activate: vi.fn().mockResolvedValue({ ...orgDetail, ...orgB, state: 'active' }) })
  render(<OrgAdminPage bridge={bridge} />)
  expect(await screen.findByRole('button', { name: '激活组织' })).toBeInTheDocument()
  fireEvent.click(screen.getByRole('button', { name: '激活组织' }))
  await waitFor(() => expect(bridge.activate).toHaveBeenCalledWith({}, expect.anything()))
  await waitFor(() => expect(screen.getByText(/已激活/)).toBeInTheDocument())
})

it('keeps invite and space actions disabled until required input is provided', async () => {
  const bridge = api({ summary: vi.fn().mockResolvedValue({ boundOrgId: orgA.orgId, org: orgDetail, orgs: [orgA] }) })
  render(<OrgAdminPage bridge={bridge} />)
  gotoTab('成员与身份')
  await screen.findByPlaceholderText('成员名称')
  expect(screen.getByRole('button', { name: '邀请成员' })).toBeDisabled()
  gotoTab('TeamSpace')
  expect(screen.getByRole('button', { name: '创建空间' })).toBeDisabled()
  gotoTab('成员与身份')
  fireEvent.change(screen.getByPlaceholderText('成员名称'), { target: { value: '王工' } })
  fireEvent.click(screen.getByRole('button', { name: '邀请成员' }))
  await waitFor(() => expect(bridge.memberInvite).toHaveBeenCalledWith({ displayName: '王工' }, expect.anything()))
})

it('planned governance tabs render concept contracts without enableable controls', async () => {
  const bridge = api({ summary: vi.fn().mockResolvedValue({ boundOrgId: orgA.orgId, org: orgDetail, orgs: [orgA] }) })
  render(<OrgAdminPage bridge={bridge} />)
  gotoTab('PolicyCenter')
  expect(await screen.findByText(/依赖 M9 切片决策冻结/)).toBeInTheDocument()
  expect(document.querySelector('.screen-route[data-route="/org/:orgId/policies"]')).not.toBeNull()
  gotoTab('运营中心')
  expect(document.querySelector('.screen-route[data-route="/org/:orgId/operations"]')).not.toBeNull()
  expect(screen.getByText('low-sample')).toBeInTheDocument()
})
