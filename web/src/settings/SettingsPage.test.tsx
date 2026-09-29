import { cleanup, render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, expect, it, vi } from 'vitest'
import { LanguageProvider } from '../i18n/language'
import type { CapabilityRolesBridge, ProviderBridge } from '../bridge/client'
import type { ProviderDTO } from '../generated/bridge'
import { SettingsPage } from './SettingsPage'

afterEach(() => {
  cleanup()
  localStorage.removeItem('lunitide:general')
  localStorage.removeItem('lunitide:appearance')
})

const now = new Date().toISOString()
const provider: ProviderDTO = {
  id: '01ARZ3NDEKTSV4RRFFQ69G5FAV', name: 'Demo', protocol: 'openai_compatible',
  baseUrl: 'https://example.com', models: [{ modelId: 'chat-l', displayName: 'Chat', isDefault: true, kind: 'llm' }],
  status: 'enabled', credentialState: 'configured', credentialBackupCount: 0,
  createdAt: now, updatedAt: now, version: 1,
}
const emptyRoles = ['chat', 'flash', 'vision', 'embed', 'judge', 'gui'].map(role => ({ role, allowJudgeEqChat: false })) as Awaited<ReturnType<CapabilityRolesBridge['get']>>['roles']

function bridges() {
  const providers = {
    list: vi.fn().mockResolvedValue({ items: [provider] }),
    get: vi.fn().mockResolvedValue(provider),
    create: vi.fn(), update: vi.fn(), delete: vi.fn(), revealCredential: vi.fn(),
    submitCredential: vi.fn(), syncModels: vi.fn(), test: vi.fn(), backupAdd: vi.fn(), backupRemove: vi.fn(),
  } as unknown as ProviderBridge
  const roles = {
    get: vi.fn().mockResolvedValue({ roles: emptyRoles }),
    set: vi.fn().mockResolvedValue({ roles: emptyRoles }),
  } as unknown as CapabilityRolesBridge
  return { providers, roles }
}

function open(category: React.ComponentProps<typeof SettingsPage>['initialCategory'] = 'general') {
  const { providers, roles } = bridges()
  render(
    <LanguageProvider value="zh-CN">
      <SettingsPage initialCategory={category} providers={providers} roles={roles} onBack={vi.fn()} />
    </LanguageProvider>,
  )
  return { providers, roles }
}

it('persists general settings and shows a save indicator', async () => {
  const user = userEvent.setup()
  open('general')
  expect(await screen.findByText('启动时打开')).toBeInTheDocument()
  await user.selectOptions(screen.getByDisplayValue('新对话'), 'last')
  expect(await screen.findByText('✓ 已保存')).toBeInTheDocument()
  expect(JSON.parse(localStorage.getItem('lunitide:general') ?? '{}')).toMatchObject({ startupPage: 'last' })
  await user.click(screen.getByRole('switch', { name: '恢复未完成运行' }))
  await waitFor(() => expect(JSON.parse(localStorage.getItem('lunitide:general') ?? '{}').restoreUnfinished).toBe(false))
})

it('searches 能力路由 and opens providers with routing above the catalog', async () => {
  const user = userEvent.setup()
  const { roles } = open('general')
  await user.type(screen.getByLabelText('搜索设置'), '能力路由')
  expect(screen.getByRole('button', { name: /模型与供应商/ })).toBeInTheDocument()
  expect(screen.queryByRole('button', { name: /^常规$/ })).not.toBeInTheDocument()
  await user.click(screen.getByRole('button', { name: /模型与供应商/ }))
  expect(await screen.findByRole('heading', { name: '能力路由' })).toBeInTheDocument()
  expect(roles.get).toHaveBeenCalled()
  expect(screen.getByRole('tab', { name: 'LLM' })).toBeInTheDocument()
  expect(screen.getByRole('tab', { name: '向量模型' })).toBeInTheDocument()
  expect(screen.getByRole('tab', { name: 'GUI 模型' })).toBeInTheDocument()
  expect(screen.getByRole('button', { name: /Demo/ })).toBeInTheDocument()
})

it('saves capability routing from the providers page', async () => {
  const user = userEvent.setup()
  const { roles } = open('providers')
  await screen.findByRole('heading', { name: '能力路由' })
  await user.selectOptions(screen.getByLabelText('对话缺省'), `${provider.id}\u0000chat-l`)
  await user.click(screen.getByRole('button', { name: '保存能力路由' }))
  await waitFor(() => expect(roles.set).toHaveBeenCalled())
  expect(await screen.findByText('能力路由已保存')).toBeInTheDocument()
  expect(vi.mocked(roles.set).mock.calls[0][0].roles.find((row: { role: string }) => row.role === 'chat')).toMatchObject({
    providerId: provider.id, modelId: 'chat-l',
  })
})
