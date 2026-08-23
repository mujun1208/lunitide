import { act, cleanup, fireEvent, render, waitFor } from '@testing-library/react'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import { BridgeClientError, type ChatBridge, type MessageBridge, type ProviderBridge, type SessionBridge } from '../bridge/client'
import type { ProjectDTO, ProviderDTO, SessionDTO } from '../generated/bridge'
import { SessionPage, TURN_RESUME_PROMPT } from './SessionPage'
import { resetLiveChatForTests } from './liveChat'

const speech = vi.hoisted(() => ({
  start: vi.fn(),
  callbacks: undefined as { onFinal: (transcript: string) => void } | undefined,
  handle: () => ({
    stop: vi.fn(),
    setAssistantPlayback: vi.fn(),
    setCommitPaused: vi.fn(),
    setBargeInActive: vi.fn(),
  }),
}))

vi.mock('../bridge/client', async importOriginal => {
  const actual = await importOriginal<typeof import('../bridge/client')>()
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
  }
})

vi.mock('./companion/ensureCompanionCapabilities', () => ({
  ensureCompanionCapabilities: vi.fn().mockResolvedValue({ fullAccess: true, ccEnabled: true }),
}))

vi.mock('./companion/speech', () => ({
  ECHO_GUARD_MS: 700,
  INTERRUPT_ECHO_MS: 160,
  startCompanionSpeech: (callbacks: { onFinal: (transcript: string) => void }) => {
    speech.callbacks = callbacks
    return speech.start(callbacks)
  },
}))

vi.mock('./companion/ttsPlayer', () => ({
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
  resetLiveChatForTests()
  localStorage.clear()
})

beforeEach(() => {
  speech.callbacks = undefined
  speech.start.mockReset()
  speech.start.mockResolvedValue(speech.handle())
})

const P = '01ARZ3NDEKTSV4RRFFQ69G5FAV'
const S = '01ARZ3NDEKTSV4RRFFQ69G5FAA'
const NOW = '2025-01-01T00:00:00Z'
const project: ProjectDTO = { id: P, name: 'Runtime', projectCode: 'ITM00001', type: 'implementation', status: 'active', createdAt: NOW, updatedAt: NOW, version: 1 }
const session: SessionDTO = { id: S, projectId: P, title: '月伴对话', pinned: false, status: 'active', createdAt: NOW, updatedAt: NOW, version: 1 }
const sessionBridge: SessionBridge = { list: vi.fn().mockResolvedValue({ items: [session] }), create: vi.fn(), update: vi.fn(), delete: vi.fn() }
const provider: ProviderDTO = { id: '01ARZ3NDEKTSV4RRFFQ69G5FAB', name: 'Ready', protocol: 'openai_compatible', baseUrl: 'https://example.test', models: [{ modelId: 'model', displayName: 'Model', isDefault: true }], status: 'enabled', credentialState: 'configured', createdAt: NOW, updatedAt: NOW, version: 1 }

it('does not auto-resume a retryable companion chat.start failure with the work prompt', async () => {
  const start = vi.fn().mockRejectedValue(new BridgeClientError('上下文装配暂时不可用', 'CONTEXT_ASSEMBLY_FAILED', true, 'engine'))
  const chat: ChatBridge = { start, approve: vi.fn(), dispose: vi.fn() }
  const messages: MessageBridge = { list: vi.fn().mockResolvedValue({ items: [], hasMore: false, nextCursor: null, snapshotSequence: 0 }), append: vi.fn().mockResolvedValue({}) }
  render(
    <SessionPage
      project={project}
      bridge={sessionBridge}
      messages={messages}
      onBack={vi.fn()}
      personal
      initialSession={session}
      initialCompanion
      chat={chat}
      providers={{ list: vi.fn().mockResolvedValue({ items: [provider] }) } as unknown as ProviderBridge}
    />,
  )
  const stage = await waitFor(() => {
    const node = document.querySelector('.companion-stage') as HTMLElement | null
    expect(node).toBeTruthy()
    return node!
  })
  await waitFor(() => expect(stage.getAttribute('data-state')).toBe('listening'), { timeout: 3000 })
  await act(async () => {
    speech.callbacks!.onFinal('今晚月色如何')
  })
  await waitFor(() => expect(start).toHaveBeenCalledOnce())
  expect(start.mock.calls[0][0]).toMatchObject({ companion: true, messages: [{ role: 'user', content: '今晚月色如何' }] })
  await act(async () => {
    await new Promise(resolve => setTimeout(resolve, 50))
  })
  expect(start).toHaveBeenCalledOnce()
  expect(JSON.stringify(start.mock.calls)).not.toContain(TURN_RESUME_PROMPT)
  fireEvent.keyDown(stage, { key: 'Escape' })
})
