import type { ComponentProps } from 'react'
import { cleanup, render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, expect, it, vi } from 'vitest'
import { LanguageProvider } from '../i18n/language'
import type { CapabilityRolesBridge, OCRRoutingBridge, ProviderBridge } from '../bridge/client'
import { conversationsBridge, getAppUpdateBridge, getCollabGateBridge, getSystemHealthBridge, systemSettingsBridge } from '../bridge/client'
import type { ProviderDTO } from '../generated/bridge'
import { SettingsPage } from './SettingsPage'

vi.mock('../bridge/client', async (importOriginal) => {
  const actual = await importOriginal<typeof import('../bridge/client')>()
  return {
    ...actual,
    conversationsBridge: {
      get: vi.fn().mockResolvedValue({ path: '', configured: false, legacyPath: '' }),
      select: vi.fn(),
      set: vi.fn(),
    },
    getCollabGateBridge: vi.fn(() => ({
      status: vi.fn().mockResolvedValue({ capability: 'disabled' }),
      evaluate: vi.fn(),
      confirm: vi.fn(),
    })),
    getAppUpdateBridge: vi.fn(() => ({
      check: vi.fn().mockResolvedValue({}),
      install: vi.fn(),
    })),
    getSystemHealthBridge: vi.fn(() => ({
      health: vi.fn().mockResolvedValue({ version: '0.1.0' }),
      diagnostics: vi.fn(),
    })),
    getDiagnosticsBridge: vi.fn(() => ({
      exportDiagnostics: vi.fn(),
    })),
    systemSettingsBridge: {
      open: vi.fn().mockResolvedValue(undefined),
    },
    getTtsBridge: vi.fn(() => ({
      voices: vi.fn().mockResolvedValue({ voices: [] }),
      synthesize: vi.fn(),
      ensureRefEngine: vi.fn(),
    })),
    getProviderBridge: vi.fn(() => ({
      list: vi.fn().mockResolvedValue({ items: [] }),
    })),
  }
})

afterEach(() => {
  cleanup()
  localStorage.removeItem('lunitide:general')
  localStorage.removeItem('lunitide:appearance')
  vi.mocked(conversationsBridge.get).mockReset().mockResolvedValue({ path: '', configured: false, legacyPath: '' })
})

const now = new Date().toISOString()
const provider: ProviderDTO = {
  id: '01ARZ3NDEKTSV4RRFFQ69G5FAV', name: 'Demo', protocol: 'openai_compatible',
  baseUrl: 'https://example.com', models: [{ modelId: 'chat-l', displayName: 'Chat', isDefault: true, kind: 'llm' }],
  status: 'enabled', credentialState: 'configured', credentialBackupCount: 0,
  createdAt: now, updatedAt: now, version: 1,
}
const emptyRoles = ['chat', 'flash', 'vision', 'embed', 'judge', 'gui'].map(role => ({ role, allowJudgeEqChat: false })) as Awaited<ReturnType<CapabilityRolesBridge['get']>>['roles']

function open(category: ComponentProps<typeof SettingsPage>['initialCategory']) {
  const providers = { list: vi.fn().mockResolvedValue({ items: [provider] }) } as unknown as ProviderBridge
  const roles = {
    get: vi.fn().mockResolvedValue({ roles: emptyRoles, revision: 'a'.repeat(64), appliedRevision: 'a'.repeat(64), state: 'applied' }),
    set: vi.fn(),
  } as unknown as CapabilityRolesBridge
  const ocr = {
    get: vi.fn().mockResolvedValue({ preferProvider: true, revision: 'a'.repeat(64), appliedRevision: 'a'.repeat(64), state: 'applied' }),
    set: vi.fn(),
  } as unknown as OCRRoutingBridge
  render(
    <LanguageProvider value="zh-CN">
      <SettingsPage initialCategory={category} providers={providers} roles={roles} ocr={ocr} onBack={vi.fn()} />
    </LanguageProvider>,
  )
}

it('does not show raw English conversation storage failures', async () => {
  vi.mocked(conversationsBridge.get).mockRejectedValue(new Error('Failed to fetch'))
  open('general')
  expect(await screen.findByText('无法读取对话存储路径')).toBeInTheDocument()
  expect(screen.queryByText('Failed to fetch')).toBeNull()
})

it('does not show raw English collab gate failures', async () => {
  vi.mocked(getCollabGateBridge).mockReturnValue({
    status: vi.fn().mockRejectedValue(new Error('Failed to fetch')),
    evaluate: vi.fn(),
    confirm: vi.fn(),
  })
  open('collab')
  expect(await screen.findByText('门禁状态查询失败')).toBeInTheDocument()
  expect(screen.queryByText('Failed to fetch')).toBeNull()
})

it('does not show raw English microphone detect or system-settings failures', async () => {
  Object.defineProperty(navigator, 'mediaDevices', {
    configurable: true,
    value: {
      enumerateDevices: vi.fn().mockRejectedValue(new Error('Failed to fetch')),
      getUserMedia: vi.fn(),
    },
  })
  open('voice')
  await userEvent.click(await screen.findByRole('button', { name: '刷新设备' }))
  expect(await screen.findByText('无法检测麦克风')).toBeInTheDocument()
  expect(screen.queryByText('Failed to fetch')).toBeNull()
  cleanup()
  vi.mocked(navigator.mediaDevices.enumerateDevices).mockResolvedValue([])
  vi.mocked(systemSettingsBridge.open).mockRejectedValue(new Error('Failed to fetch'))
  open('voice')
  await userEvent.click(await screen.findByRole('button', { name: '打开 Windows 麦克风设置' }))
  expect(await screen.findByText('无法打开 Windows 麦克风设置')).toBeInTheDocument()
  expect(screen.queryByText('Failed to fetch')).toBeNull()
})

it('does not show raw English update check failures', async () => {
  vi.mocked(getSystemHealthBridge).mockReturnValue({
    health: vi.fn().mockResolvedValue({ version: '0.1.0' }),
    diagnostics: vi.fn(),
  })
  vi.mocked(getAppUpdateBridge).mockReturnValue({
    check: vi.fn().mockRejectedValue(new Error('Failed to fetch')),
    install: vi.fn(),
  })
  open('diagnostics')
  await userEvent.click(await screen.findByRole('button', { name: '检查更新' }))
  expect(await screen.findByText('检查更新失败')).toBeInTheDocument()
  expect(screen.queryByText('Failed to fetch')).toBeNull()
})
