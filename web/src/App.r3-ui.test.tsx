import { cleanup, fireEvent, render, screen } from '@testing-library/react'
import { afterEach, expect, it, vi } from 'vitest'
import { LanguageProvider } from './i18n/language'
import type { CapabilityRolesBridge, IdentityBridge, MemoryBridge, MemoryOpsBridge, OCRPackBridge, OCRRoutingBridge, OCRRunBridge, ProviderBridge } from './bridge/client'
import type { OcrPackGetResult, OcrRoutingGetResult, ProviderDTO } from './generated/bridge'
import { ActivityStatusButton } from './activity/ActivityStatusButton'
import { MediaCenterPage } from './media/MediaCenterPage'
import { MediaMiniPlayer } from './media/MediaMiniPlayer'
import { MemoryPage } from './memory/MemoryPage'
import { OCRSettingsPanel } from './settings/OCRSettingsPanel'
import { SettingsPage } from './settings/SettingsPage'
import { SmartCapabilitiesPanel } from './settings/SmartCapabilitiesPanel'

afterEach(cleanup)

const now = '2026-01-01T00:00:00Z'
const version = 'a'.repeat(64)
const revision = 'a'.repeat(64)

function routing(): OcrRoutingGetResult {
  return {
    requestedScope: { scopeKind: 'user', scopeId: null },
    policySource: { scopeKind: 'user', scopeId: null, inherited: false },
    policy: { mode: 'auto', complexDocumentEngine: 'paddleocr-vl-1.6', fallbackOrder: ['ppocr', 'windows-ocr'], sendToCloud: 'never' },
    revision,
    windowsProbe: { state: 'ready', available: true, languages: ['zh-Hans-CN'], checkedAt: '2026-09-18T12:00:00.000Z' },
    legacy: { engineId: 'ppocr', registered: true, state: 'registered_unwired', available: false, markerDetected: false },
  }
}

function pack(): OcrPackGetResult {
  return {
    pack: { packId: 'paddleocr-vl-1.6', availability: 'not_installed', currentVersion: null, previousVersion: null, manifestDigest: null, engineVersion: null, deviceKind: null, lastHealthAt: null, lastErrorCode: null, revision: 1 },
    gate: { installAllowed: false, autoRouteAllowed: false, reasonCode: 'NO_VERIFIED_RUNTIME_PROFILE', verifiedRuntimeProfileDigest: null, revision: 1 },
    operation: null,
    release: null,
  }
}

it('TestR3InformationArchitecture: two cards, one drawer, truthful OCR, media empty, quiet activity', async () => {
  const memoryOps = {
    getSettings: vi.fn().mockResolvedValue({
      subjectId: 'local-user', memoryEnabled: true, autoNominate: false, growthDays: 14, updatedAt: now, version,
      captureMode: 'auto', revision: 1, personalMemoryEnabled: true, projectMemoryEnabled: true,
    }),
    updateSettings: vi.fn(),
  } as unknown as MemoryOpsBridge
  const identity = { get: vi.fn().mockResolvedValue({ subjectId: 'local-user' }) } as unknown as IdentityBridge
  render(
    <LanguageProvider value="zh-CN">
      <SmartCapabilitiesPanel memoryOps={memoryOps} identity={identity} onOpenMemory={() => {}} onOpenOCR={() => {}} />
    </LanguageProvider>,
  )
  expect(await screen.findByRole('heading', { name: '自动记忆' })).toBeInTheDocument()
  expect(screen.getAllByRole('heading', { name: '文字识别' })).toHaveLength(1)
  expect(screen.getByText(/已配置的视觉模型能用就先用，然后走本机 RapidOCR，最后用 Windows OCR 兜底/)).toBeInTheDocument()
  expect(document.querySelectorAll('.smart-cap-card')).toHaveLength(2)
  expect(screen.queryByRole('button', { name: '安装（不可用）' })).toBeNull()
  cleanup()

  const item = { factId: '01ARZ3NDEKTSV4RRFFQ69G5FAV', version: 1, revision: 1, scopeKind: 'user' as const, kind: 'preference' as const, text: '我喜欢简洁的回答', forgotten: false, updatedAt: now }
  const memory = {
    itemList: vi.fn().mockResolvedValue({ items: [item], databaseRevision: 1 }),
    itemCreate: vi.fn(), itemForget: vi.fn(),
    reviewList: vi.fn().mockResolvedValue({ items: [], databaseRevision: 1 }),
  } as unknown as MemoryBridge
  render(
    <LanguageProvider value="zh-CN">
      <MemoryPage bridge={memory} ops={memoryOps} />
    </LanguageProvider>,
  )
  expect(await screen.findByText(/自动记忆 · 个人开 · 项目开/)).toBeInTheDocument()
  expect(screen.queryByRole('tab')).toBeNull()
  fireEvent.click(await screen.findByRole('button', { name: '记忆设置' }))
  expect(screen.getAllByRole('complementary')).toHaveLength(1)
  expect(screen.getAllByRole('radio')).toHaveLength(3)
  cleanup()

  const ocr = { get: vi.fn().mockResolvedValue(routing()), set: vi.fn(), install: vi.fn().mockResolvedValue({ state: 'idle', percent: 0, doneBytes: 0, totalBytes: 1 }) } as unknown as OCRRoutingBridge
  const packApi = {
    get: vi.fn().mockResolvedValue(pack()),
    install: vi.fn(), cancel: vi.fn(), uninstall: vi.fn(),
    noticeList: vi.fn().mockResolvedValue({ items: [], nextCursor: null }),
    noticeRead: vi.fn(),
  } as unknown as OCRPackBridge
  const providers = { list: vi.fn().mockResolvedValue({ items: [] }) } as unknown as ProviderBridge
  const runs = { list: vi.fn().mockResolvedValue({ items: [], nextCursor: null }), get: vi.fn(), readArtifact: vi.fn() } as unknown as OCRRunBridge
  render(
    <LanguageProvider value="zh-CN">
      <OCRSettingsPanel ocr={ocr} packApi={packApi} providers={providers} runs={runs} />
    </LanguageProvider>,
  )
  expect(await screen.findByRole('status')).toHaveTextContent('文字识别：自动')
  expect(screen.queryByRole('switch')).toBeNull()
  expect(screen.queryByRole('heading', { name: 'PaddleOCR-VL-1.6' })).toBeNull()
  expect(screen.queryByRole('button', { name: '安装（不可用）' })).toBeNull()
  expect(screen.queryByRole('heading', { name: 'OCR 路由' })).toBeNull()
  cleanup()

  render(
    <LanguageProvider value="zh-CN">
      <MediaCenterPage snapshot={null} assets={[]} operation={null} playbackUrl={null} notice="" disabledReason="媒体会话未启用。当前只能选择文件，还不能创建播放会话。" busy={false} onPick={() => {}} onPlayPause={() => {}} onPrevious={() => {}} onNext={() => {}} onJump={() => {}} onRemove={() => {}} onClear={() => {}} onSeek={() => {}} onVolume={() => {}} />
      <MediaMiniPlayer phase="hidden" snapshot={null} title="" error="" onOpen={() => {}} onPlayPause={() => {}} onClose={() => {}} onRetryClose={() => {}} />
      <ActivityStatusButton items={[]} open={false} onToggle={() => {}} />
    </LanguageProvider>,
  )
  expect(screen.getByRole('heading', { name: '媒体中心' })).toBeInTheDocument()
  expect(screen.getByLabelText('空状态')).toBeInTheDocument()
  expect(screen.queryByLabelText('迷你播放器')).toBeNull()
  expect(screen.getByRole('button', { name: '运行状态' })).toHaveClass('is-quiet')
  cleanup()

  const provider: ProviderDTO = {
    id: '01ARZ3NDEKTSV4RRFFQ69G5FAV', name: 'Demo', protocol: 'openai_compatible',
    baseUrl: 'https://example.com', models: [{ modelId: 'chat-l', displayName: 'Chat', isDefault: true, kind: 'llm' }],
    status: 'enabled', credentialState: 'configured', credentialBackupCount: 0,
    createdAt: now, updatedAt: now, version: 1,
  }
  const roles = {
    get: vi.fn().mockResolvedValue({ roles: ['chat', 'flash', 'vision', 'embed', 'judge', 'gui'].map(role => ({ role, allowJudgeEqChat: false })), revision, appliedRevision: revision, state: 'applied' }),
    set: vi.fn(),
  } as unknown as CapabilityRolesBridge
  render(
    <LanguageProvider value="zh-CN">
      <SettingsPage initialCategory="routing" providers={{ list: vi.fn().mockResolvedValue({ items: [provider] }), get: vi.fn(), create: vi.fn(), update: vi.fn(), delete: vi.fn(), revealCredential: vi.fn(), submitCredential: vi.fn(), syncModels: vi.fn(), test: vi.fn(), backupAdd: vi.fn(), backupRemove: vi.fn() } as unknown as ProviderBridge} roles={roles} ocr={ocr} onBack={vi.fn()} />
    </LanguageProvider>,
  )
  expect(await screen.findByRole('heading', { name: '能力路由' })).toBeInTheDocument()
  expect(screen.queryByRole('heading', { name: '文字识别' })).not.toBeInTheDocument()
  expect(screen.getByRole('button', { name: /智能能力/ })).toBeInTheDocument()
})
