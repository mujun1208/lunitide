import { cleanup, render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, expect, it, vi } from 'vitest'
import { BridgeClientError, type CapabilityRolesBridge, type ProviderBridge } from '../bridge/client'
import type { ProviderDTO } from '../generated/bridge'
import { CapabilityRouting } from './CapabilityRouting'
import { SETTINGS_CATEGORIES } from './settingsNav'

afterEach(cleanup)

const provider: ProviderDTO = {
  id: '01ARZ3NDEKTSV4RRFFQ69G5FAV', name: 'Demo', protocol: 'openai_compatible',
  baseUrl: 'https://example.com', models: [
    { modelId: 'chat-l', displayName: 'Chat', isDefault: true, kind: 'llm' },
    { modelId: 'gui-m', displayName: 'GUI', isDefault: false, kind: 'gui' },
  ],
  status: 'enabled', credentialState: 'configured', credentialBackupCount: 0,
  createdAt: new Date().toISOString(), updatedAt: new Date().toISOString(), version: 1,
}

const emptyRoles = ['chat', 'flash', 'vision', 'embed', 'judge', 'gui'].map(role => ({ role, allowJudgeEqChat: false })) as Awaited<ReturnType<CapabilityRolesBridge['get']>>['roles']

function api(setImpl?: CapabilityRolesBridge['set']) {
  const providers = { list: vi.fn().mockResolvedValue({ items: [provider] }) } as unknown as ProviderBridge
  const roles = {
    get: vi.fn().mockResolvedValue({ roles: emptyRoles }),
    set: setImpl ?? vi.fn().mockResolvedValue({ roles: emptyRoles }),
  } as unknown as CapabilityRolesBridge
  return { providers, roles }
}

it('renders six auto rows without a new settings category', async () => {
  const { providers, roles } = api()
  render(<CapabilityRouting providers={providers} roles={roles} />)
  expect(await screen.findByRole('heading', { name: '能力路由' })).toBeInTheDocument()
  expect(screen.getAllByRole('combobox')).toHaveLength(6)
  expect(SETTINGS_CATEGORIES.map(c => c.id)).not.toContain('capability-routing')
})

it('clears a binding back to auto', async () => {
  const set = vi.fn().mockResolvedValue({ roles: emptyRoles })
  const { providers, roles } = api(set)
  const user = userEvent.setup()
  render(<CapabilityRouting providers={providers} roles={roles} />)
  await screen.findByRole('heading', { name: '能力路由' })
  const chat = screen.getByLabelText('对话缺省')
  await user.selectOptions(chat, `${provider.id}\u0000chat-l`)
  await user.selectOptions(chat, '')
  await user.click(screen.getByRole('button', { name: '保存能力路由' }))
  await waitFor(() => expect(set).toHaveBeenCalled())
  const payload = set.mock.calls[0][0] as { roles: Array<{ role: string; providerId?: string; modelId?: string }> }
  const chatRow = payload.roles.find(r => r.role === 'chat')
  expect(chatRow?.providerId).toBeUndefined()
  expect(chatRow?.modelId).toBeUndefined()
})

it('rejects judge=chat unless allowed', async () => {
  const set = vi.fn().mockRejectedValue(new BridgeClientError('judge 与 chat 相同必须勾选允许', 'CAPABILITY_ROLE_JUDGE_EQ_CHAT', false, 't'))
  const { providers, roles } = api(set)
  const user = userEvent.setup()
  render(<CapabilityRouting providers={providers} roles={roles} />)
  await screen.findByRole('heading', { name: '能力路由' })
  const chat = screen.getByLabelText('对话缺省')
  await user.selectOptions(chat, `${provider.id}\u0000chat-l`)
  const judge = screen.getByLabelText('Judge')
  await user.selectOptions(judge, `${provider.id}\u0000chat-l`)
  await user.click(screen.getByRole('button', { name: '保存能力路由' }))
  expect(await screen.findByRole('alert')).toHaveTextContent('judge 与 chat 相同必须勾选允许')
})

it('saves judge=chat after the allow checkbox and reloads the binding', async () => {
  let store = emptyRoles
  const set = vi.fn().mockImplementation(async payload => {
    store = payload.roles.map((row: { role: string; providerId?: string; modelId?: string; allowJudgeEqChat?: boolean }) => ({
      role: row.role, providerId: row.providerId, modelId: row.modelId, allowJudgeEqChat: !!row.allowJudgeEqChat,
    }))
    return { roles: store }
  })
  const providers = { list: vi.fn().mockResolvedValue({ items: [provider] }) } as unknown as ProviderBridge
  const roles = { get: vi.fn().mockImplementation(async () => ({ roles: store })), set } as unknown as CapabilityRolesBridge
  const user = userEvent.setup()
  const view = render(<CapabilityRouting providers={providers} roles={roles} />)
  await screen.findByRole('heading', { name: '能力路由' })
  await user.selectOptions(screen.getByLabelText('对话缺省'), `${provider.id}\u0000chat-l`)
  await user.selectOptions(screen.getByLabelText('Judge'), `${provider.id}\u0000chat-l`)
  await user.click(screen.getByLabelText('允许 judge 与 chat 相同'))
  await user.click(screen.getByRole('button', { name: '保存能力路由' }))
  expect(await screen.findByRole('status')).toHaveTextContent('能力路由已保存')
  const judgeRow = set.mock.calls[0][0].roles.find((row: { role: string }) => row.role === 'judge')
  expect(judgeRow).toMatchObject({ providerId: provider.id, modelId: 'chat-l', allowJudgeEqChat: true })
  view.unmount()
  render(<CapabilityRouting providers={providers} roles={roles} />)
  await screen.findByRole('heading', { name: '能力路由' })
  expect(screen.getByLabelText('对话缺省')).toHaveValue(`${provider.id}\u0000chat-l`)
  expect(screen.getByLabelText('Judge')).toHaveValue(`${provider.id}\u0000chat-l`)
  expect(screen.getByLabelText('允许 judge 与 chat 相同')).toBeChecked()
})

it('lists only matching kinds on each role', async () => {
  const mixed: ProviderDTO = {
    ...provider,
    models: [
      { modelId: 'chat-l', displayName: 'Chat', isDefault: true, kind: 'llm' },
      { modelId: 'see', displayName: 'See', isDefault: false, kind: 'vision' },
      { modelId: 'emb', displayName: 'Emb', isDefault: false, kind: 'embedding' },
      { modelId: 'gui-m', displayName: 'GUI', isDefault: false, kind: 'gui' },
      { modelId: 'vlm', displayName: 'VLM', isDefault: false, kind: 'llm', supportsVision: true },
    ],
  }
  const providers = { list: vi.fn().mockResolvedValue({ items: [mixed] }) } as unknown as ProviderBridge
  const roles = { get: vi.fn().mockResolvedValue({ roles: emptyRoles }), set: vi.fn() } as unknown as CapabilityRolesBridge
  render(<CapabilityRouting providers={providers} roles={roles} />)
  await screen.findByRole('heading', { name: '能力路由' })
  const labels = (select: HTMLElement) => [...select.querySelectorAll('option')].map(o => o.textContent)
  expect(labels(screen.getByLabelText('对话缺省'))).toEqual(['自动', 'Demo / Chat', 'Demo / VLM'])
  expect(labels(screen.getByLabelText('视觉'))).toEqual(['自动', 'Demo / See', 'Demo / VLM'])
  expect(labels(screen.getByLabelText('向量'))).toEqual(['自动', 'Demo / Emb'])
  expect(labels(screen.getByLabelText('GUI'))).toEqual(['自动', 'Demo / GUI'])
})
