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
