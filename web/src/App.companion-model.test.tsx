import { act, cleanup, render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import type { ChatBridge, MessageBridge, ProjectBridge, ProviderBridge, SessionBridge } from './bridge/client'
import type { ProjectDTO } from './generated/bridge'
import type { CompanionSpeechHandle } from './session/companion/speech'
import { App, PERSONAL_CHAT_PROJECT } from './App'

const speech = vi.hoisted(() => ({
  start: vi.fn(),
  callbacks: undefined as { onFinal: (transcript: string) => void } | undefined,
  // Typed against the real handle so adding a method to
  // CompanionSpeechHandle fails typecheck here instead of throwing
  // "not a function" deep inside a React effect at runtime.
  handle: (): CompanionSpeechHandle => ({
    stop: vi.fn(),
    setAssistantPlayback: vi.fn(),
    setCommitPaused: vi.fn(),
    pulseRecognition: vi.fn(),
    forceCommit: vi.fn(),
    resumeCapture: vi.fn(),
  }),
}))

const peopleHost = vi.hoisted(() => {
  const identityDTO = {
    subjectId: '01ARZ3NDEKTSV4RRFFQ69G5FAV', nickname: 'mu', avatar: '', status: 'online' as const,
    department: '', title: '', orgName: '', bio: '', publicKey: 'aa'.repeat(32), pairingCode: '111111',
    passwordSet: false, locked: false, discoveryEnabled: false,
    createdAt: '2026-01-01T00:00:00Z', updatedAt: '2026-01-01T00:00:00Z',
  }
  const identity = { get: vi.fn().mockResolvedValue(identityDTO), update: vi.fn(), passwordSet: vi.fn(), unlock: vi.fn() }
  const people = {
    list: vi.fn().mockResolvedValue({ items: [] }), pair: vi.fn(),
    discoveryGet: vi.fn().mockResolvedValue({ enabled: false, pairingCode: '111111' }),
    discoverySet: vi.fn(), threadList: vi.fn().mockResolvedValue({ items: [] }),
    threadOpen: vi.fn(), threadSend: vi.fn(), groupCreate: vi.fn(), fileDecide: vi.fn(),
    threadTyping: vi.fn(), fileStage: vi.fn(), filePick: vi.fn(), peerAdd: vi.fn(), contactUpdate: vi.fn(),
  }
  return { identity, people }
})

vi.mock('./bridge/client', async importOriginal => {
  const actual = await importOriginal<typeof import('./bridge/client')>()
  return {
    ...actual,
    getTtsBridge: () => ({
      voices: () => Promise.resolve({ voices: [{ voice_id: 'zh-female', display_name: '月汐温柔女声', gender: 'female' as const, lang: 'zh-CN' }] }),
      synthesize: vi.fn(),
      cancel: vi.fn(),
    }),
    automationBridge: { listRuns: () => Promise.resolve({ runs: [] }) },
    sessionFolderBridge: { get: vi.fn().mockResolvedValue({ path: '' }), list: vi.fn(), open: vi.fn() },
    toolsPolicyBridge: { getCommandPolicy: vi.fn().mockResolvedValue({ commands: [], fullAccess: true }), setCommandPolicy: vi.fn() },
    ccBridge: { getConfig: vi.fn().mockResolvedValue({ enabled: true }), updateConfig: vi.fn(), getAuditLog: vi.fn(), emergencyStop: vi.fn() },
    getIdentityBridge: () => peopleHost.identity,
    getPeopleBridge: () => peopleHost.people,
    getMeetingsBridge: () => ({ list: vi.fn().mockResolvedValue({ items: [] }), start: vi.fn(), append: vi.fn(), stop: vi.fn(), get: vi.fn(), summarize: vi.fn(), exportMeeting: vi.fn(), update: vi.fn(), delete: vi.fn() }),
    meetingsBridge: { list: () => Promise.resolve({ items: [] }), start: vi.fn(), append: vi.fn(), stop: vi.fn(), get: vi.fn(), summarize: vi.fn(), exportMeeting: vi.fn(), update: vi.fn(), delete: vi.fn() },
  }
})

vi.mock('./session/companion/ensureCompanionCapabilities', () => ({
  ensureCompanionCapabilities: vi.fn().mockResolvedValue({ fullAccess: true, ccEnabled: true }),
}))

vi.mock('./session/companion/speech', () => ({
  ECHO_GUARD_MS: 700,
  FORCE_COMMIT_MS: 1800,
  INTERRUPT_ECHO_MS: 160,
  shouldShowSpeechSetupHint: () => false,
  startCompanionSpeech: (options: { onFinal: (transcript: string) => void }) => {
    speech.callbacks = options
    return speech.start(options)
  },
}))

vi.mock('./session/companion/ttsPlayer', () => ({
  unlockTtsAudio: vi.fn(() => Promise.resolve()),
  getTtsAudioState: () => 'running' as const,
  TtsPlayer: class {
    configure() {}
    async speak() {}
    enqueue() {}
    async flush() {}
    isBusy() {
      return false
    }
    interrupt() {}
    dispose() {}
  },
}))

afterEach(() => {
  cleanup()
  localStorage.clear()
})

beforeEach(() => {
  speech.callbacks = undefined
  speech.start.mockReset()
  speech.start.mockResolvedValue(speech.handle())
})

const now = '2026-01-01T00:00:00Z'
const personal: ProjectDTO = { id: '01ARZ3NDEKTSV4RRFFQ69G5FAV', name: PERSONAL_CHAT_PROJECT, projectCode: 'ITM00000', type: 'implementation', status: 'active', createdAt: now, updatedAt: now, version: 1 }
const session = { id: '01ARZ3NDEKTSV4RRFFQ69G5FAW', projectId: personal.id, title: '月伴对话', pinned: false, status: 'active' as const, createdAt: now, updatedAt: now, version: 1 }
const provider = {
  id: '01ARZ3NDEKTSV4RRFFQ69G5FAX',
  name: 'Ready',
  protocol: 'openai_compatible' as const,
  baseUrl: 'https://example.test',
  models: [
    { modelId: 'm-one', displayName: 'Model One', isDefault: true },
    { modelId: 'm-two', displayName: 'Model Two', isDefault: false },
  ],
  status: 'enabled' as const,
  credentialState: 'configured' as const,
  createdAt: now,
  updatedAt: now,
  version: 1,
}

it('uses the home-page model when opening companion talk', async () => {
  const start = vi.fn().mockResolvedValue({ streamId: '01ARZ3NDEKTSV4RRFFQ69G5FAY', cancel: vi.fn(), dispose: vi.fn() })
  const chat: ChatBridge = { start, approve: vi.fn(), dispose: vi.fn() }
  const messages: MessageBridge = { list: vi.fn().mockResolvedValue({ items: [], hasMore: false, nextCursor: null, snapshotSequence: 0 }), append: vi.fn().mockResolvedValue({}) }
  const projects: ProjectBridge = {
    list: vi.fn().mockResolvedValue({ items: [personal] }),
    create: vi.fn(),
    update: vi.fn(),
    publish: vi.fn(),
    close: vi.fn(),
    reopen: vi.fn(),
    advanceStatus: vi.fn(),
    delete: vi.fn(),
  }
  const sessions: SessionBridge = { list: vi.fn().mockResolvedValue({ items: [] }), create: vi.fn().mockResolvedValue(session), update: vi.fn(), delete: vi.fn() }
  const providers = { list: vi.fn().mockResolvedValue({ items: [provider] }) } as unknown as ProviderBridge
  const user = userEvent.setup()
  render(<App projects={projects} sessions={sessions} providers={providers} messages={messages} chat={chat} />)
  await user.click(await screen.findByRole('button', { name: /Model One/ }))
  await user.click(await screen.findByRole('button', { name: /Model Two/ }))
  await user.click(await screen.findByRole('button', { name: /月伴对话|Companion talk/ }))
  const stage = await waitFor(() => {
    const node = document.querySelector('.companion-stage') as HTMLElement | null
    expect(node).toBeTruthy()
    return node!
  })
  await waitFor(() => expect(stage.getAttribute('data-state')).toBe('listening'), { timeout: 3000 })
  await act(async () => {
    speech.callbacks!.onFinal('你好月汐')
  })
  await waitFor(() => expect(start).toHaveBeenCalled())
  expect(start.mock.calls[0][0]).toMatchObject({ companion: true, providerId: provider.id, modelId: 'm-two' })
})
