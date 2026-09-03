import { act, cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import { BridgeClientError, type ChatBridge, type FeedbackBridge, type MemoryBridge, type MessageBridge, type ProviderBridge, type SessionBridge, type StreamEvent } from '../bridge/client'
import type { ProjectDTO, ProviderDTO, SessionDTO } from '../generated/bridge'
import type { CompanionSpeechHandle } from './companion/speech'
import { SessionPage, TURN_RESUME_PROMPT } from './SessionPage'
import { ensureCompanionCapabilities } from './companion/ensureCompanionCapabilities'
import { resetLiveChatForTests } from './liveChat'

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

vi.mock('../bridge/client', async importOriginal => {
  const actual = await importOriginal<typeof import('../bridge/client')>()
  return {
    ...actual,
    getTtsBridge: () => ({
      voices: () => Promise.resolve({ voices: [{ voice_id: 'zh-female', display_name: '月汐温柔女声', gender: 'female' as const, lang: 'zh-CN' }] }),
      synthesize: vi.fn(),
      cancel: vi.fn(),
      installOnnxEngine: () => Promise.resolve({ state: 'ready', percent: 100, doneBytes: 0, totalBytes: 0 }),
    }),
    automationBridge: { listRuns: () => Promise.resolve({ runs: [] }) },
    sessionFolderBridge: { get: vi.fn().mockResolvedValue({ path: '' }), list: vi.fn(), open: vi.fn() },
    toolsPolicyBridge: { getCommandPolicy: vi.fn().mockResolvedValue({ commands: [], fullAccess: true }), setCommandPolicy: vi.fn() },
    ccBridge: { getConfig: vi.fn().mockResolvedValue({ enabled: true }), updateConfig: vi.fn(), getAuditLog: vi.fn(), emergencyStop: vi.fn() },
  }
})

vi.mock('./companion/ensureCompanionCapabilities', () => ({
  ensureCompanionCapabilities: vi.fn().mockResolvedValue({ fullAccess: true, ccEnabled: true }),
  CC_CONFIG_EVENT: 'lunitide:cc-config',
  notifyCcConfigChanged: vi.fn(),
}))

vi.mock('./companion/speech', () => ({
  ECHO_GUARD_MS: 700,
  FORCE_COMMIT_MS: 1800,
  stageForceCommitMayBeginTurn: () => false,
  INTERRUPT_ECHO_MS: 160,
  shouldShowSpeechSetupHint: () => false,
  startCompanionSpeech: (options: { onFinal: (transcript: string) => void }) => {
    speech.callbacks = options
    return speech.start(options)
  },
}))

vi.mock('./companion/ttsPlayer', () => ({
  unlockTtsAudio: vi.fn(() => Promise.resolve()),
  playCompanionAckPcm: vi.fn(),
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
  vi.mocked(ensureCompanionCapabilities).mockResolvedValue({ fullAccess: true, ccEnabled: true })
})

const P = '01ARZ3NDEKTSV4RRFFQ69G5FAV'
const S = '01ARZ3NDEKTSV4RRFFQ69G5FAA'
const NOW = '2025-01-01T00:00:00Z'
const project: ProjectDTO = { id: P, name: 'Runtime', projectCode: 'ITM00001', type: 'implementation', status: 'active', createdAt: NOW, updatedAt: NOW, version: 1 }
const session: SessionDTO = { id: S, projectId: P, title: '月伴对话', pinned: false, status: 'active', createdAt: NOW, updatedAt: NOW, version: 1 }
const sessionBridge: SessionBridge = { list: vi.fn().mockResolvedValue({ items: [session] }), create: vi.fn(), update: vi.fn(), delete: vi.fn() }
const provider: ProviderDTO = {
  id: '01ARZ3NDEKTSV4RRFFQ69G5FAB',
  name: 'Ready',
  protocol: 'openai_compatible',
  baseUrl: 'https://example.test',
  models: [{ modelId: 'model', displayName: 'Model', isDefault: true }],
  status: 'enabled',
  credentialState: 'configured',
  createdAt: NOW,
  updatedAt: NOW,
  version: 1,
}

it('starts companion chat even when persist append fails, and keeps the persist banner', async () => {
  const start = vi.fn()
  const append = vi.fn().mockRejectedValue(new BridgeClientError('write failed', 'WRITE_FAILED', true, 'engine'))
  const chat: ChatBridge = { start, approve: vi.fn(), dispose: vi.fn() }
  const messages: MessageBridge = { list: vi.fn().mockResolvedValue({ items: [], hasMore: false, nextCursor: null, snapshotSequence: 0 }), append }
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
  await waitFor(() => expect(append).toHaveBeenCalled())
  await waitFor(() => expect(start).toHaveBeenCalled())
  expect(start.mock.calls[0][0].messages?.[0]?.content).toBe('今晚月色如何')
  expect(await screen.findByRole('alert')).toHaveTextContent('这句话没记下')
})

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

it('starts companion turns with the model selected on the home page', async () => {
  const start = vi.fn().mockResolvedValue({ streamId: '01ARZ3NDEKTSV4RRFFQ69G5FAY', cancel: vi.fn(), dispose: vi.fn() })
  const chat: ChatBridge = { start, approve: vi.fn(), dispose: vi.fn() }
  const messages: MessageBridge = { list: vi.fn().mockResolvedValue({ items: [], hasMore: false, nextCursor: null, snapshotSequence: 0 }), append: vi.fn().mockResolvedValue({}) }
  const chosen: ProviderDTO = {
    ...provider,
    models: [
      { modelId: 'm-one', displayName: 'Model One', isDefault: true },
      { modelId: 'm-two', displayName: 'Model Two', isDefault: false },
    ],
  }
  render(
    <SessionPage
      project={project}
      bridge={sessionBridge}
      messages={messages}
      onBack={vi.fn()}
      personal
      initialSession={session}
      initialCompanion
      initialProviderId={chosen.id}
      initialModelId="m-two"
      chat={chat}
      providers={{ list: vi.fn().mockResolvedValue({ items: [chosen] }) } as unknown as ProviderBridge}
    />,
  )
  const stage = await waitFor(() => {
    const node = document.querySelector('.companion-stage') as HTMLElement | null
    expect(node).toBeTruthy()
    return node!
  })
  await waitFor(() => expect(stage.getAttribute('data-state')).toBe('listening'), { timeout: 3000 })
  await act(async () => {
    speech.callbacks!.onFinal('你好月汐')
  })
  await waitFor(() => expect(start).toHaveBeenCalledOnce())
  expect(start.mock.calls[0][0]).toMatchObject({ companion: true, providerId: chosen.id, modelId: 'm-two' })
})

it('companion idle chat silently prefers a flash model already on that provider', async () => {
  const start = vi.fn().mockResolvedValue({ streamId: '01ARZ3NDEKTSV4RRFFQ69G5FAY', cancel: vi.fn(), dispose: vi.fn() })
  const chat: ChatBridge = { start, approve: vi.fn(), dispose: vi.fn() }
  const messages: MessageBridge = { list: vi.fn().mockResolvedValue({ items: [], hasMore: false, nextCursor: null, snapshotSequence: 0 }), append: vi.fn().mockResolvedValue({}) }
  const chosen: ProviderDTO = {
    ...provider,
    models: [
      { modelId: 'glm-4-plus', displayName: 'Plus', isDefault: true, kind: 'llm' },
      { modelId: 'glm-4-flash', displayName: 'Flash', isDefault: false, kind: 'llm' },
    ],
  }
  render(
    <SessionPage
      project={project}
      bridge={sessionBridge}
      messages={messages}
      onBack={vi.fn()}
      personal
      initialSession={session}
      initialCompanion
      initialProviderId={chosen.id}
      initialModelId="glm-4-plus"
      chat={chat}
      providers={{ list: vi.fn().mockResolvedValue({ items: [chosen] }) } as unknown as ProviderBridge}
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
  expect(start.mock.calls[0][0]).toMatchObject({ companion: true, providerId: chosen.id, modelId: 'glm-4-flash' })
})

it('interrupt then a new spoken line starts a fresh companion chat.start', async () => {
  const start = vi.fn().mockImplementation(async (_payload: unknown, onEvent: (event: { type: string }) => void) => ({
    streamId: '01ARZ3NDEKTSV4RRFFQ69G5FAZ',
    cancel: async () => { onEvent({ type: 'cancelled' }) },
    dispose: vi.fn(),
  }))
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
    speech.callbacks!.onFinal('打开桌面协议')
  })
  await waitFor(() => expect(start).toHaveBeenCalledOnce())
  await waitFor(() => expect(stage.querySelector('.companion-interrupt')).not.toBeDisabled())
  fireEvent.click(stage.querySelector('.companion-interrupt') as HTMLButtonElement)
  await waitFor(() => expect(stage.getAttribute('data-state')).toBe('listening'), { timeout: 3000 })
  await act(async () => {
    speech.callbacks!.onFinal('有没有打开协议')
  })
  await waitFor(() => expect(start).toHaveBeenCalledTimes(2))
  expect(start.mock.calls[1][0]).toMatchObject({ companion: true, messages: [{ role: 'user', content: '有没有打开协议' }] })
})

it('does not restart companion chat.start for the same in-flight sentence', async () => {
  const start = vi.fn().mockImplementation(() => new Promise(() => {}))
  const append = vi.fn().mockResolvedValue({})
  const chat: ChatBridge = { start, approve: vi.fn(), dispose: vi.fn() }
  const messages: MessageBridge = { list: vi.fn().mockResolvedValue({ items: [], hasMore: false, nextCursor: null, snapshotSequence: 0 }), append }
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
    speech.callbacks!.onFinal('打开记事本')
  })
  await waitFor(() => expect(start).toHaveBeenCalledOnce())
  await act(async () => {
    speech.callbacks!.onFinal('打开记事本')
  })
  await act(async () => {
    await new Promise(resolve => setTimeout(resolve, 80))
  })
  expect(start).toHaveBeenCalledOnce()
  expect(append).toHaveBeenCalledOnce()
})

it('retries companion chat.start after HOST_BUSY without speaking 无法执行', async () => {
  const busy = new BridgeClientError('桌面主机正忙，请稍后重试', 'HOST_BUSY', true, 'host')
  const start = vi.fn()
    .mockRejectedValueOnce(busy)
    .mockResolvedValue({ streamId: '01ARZ3NDEKTSV4RRFFQ69G5FAZ', cancel: vi.fn(), dispose: vi.fn() })
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
    speech.callbacks!.onFinal('你告诉我你刚才怎么处事了')
  })
  await waitFor(() => expect(start.mock.calls.length).toBeGreaterThanOrEqual(2), { timeout: 4000 })
  expect(document.body.textContent).not.toMatch(/无法执行/)
})

it('shows the companion computer-control-off banner without enabling CC', async () => {
  vi.mocked(ensureCompanionCapabilities).mockResolvedValue({ fullAccess: true, ccEnabled: false })
  const chat: ChatBridge = { start: vi.fn(), approve: vi.fn(), dispose: vi.fn() }
  render(
    <SessionPage
      project={project}
      bridge={sessionBridge}
      messages={{ list: vi.fn().mockResolvedValue({ items: [], hasMore: false, nextCursor: null, snapshotSequence: 0 }), append: vi.fn() } as MessageBridge}
      onBack={vi.fn()}
      personal
      initialSession={session}
      initialCompanion
      chat={chat}
      providers={{ list: vi.fn().mockResolvedValue({ items: [provider] }) } as unknown as ProviderBridge}
    />,
  )
  expect(await screen.findByText(/电脑控制未启用/)).toBeInTheDocument()
  expect(document.body.textContent).toMatch(/第一次控桌面请到设置里打开/)
  expect(ensureCompanionCapabilities).toHaveBeenCalled()
})

it('shows persist-retry on the companion stage from the server turn', async () => {
  const inspectTurn = vi.fn().mockResolvedValue({ status: 'completed', persistFailed: true, persistDraft: '已经生成但没落库' })
  const chat: ChatBridge = { start: vi.fn(), approve: vi.fn(), inspectTurn, dispose: vi.fn() }
  render(
    <SessionPage
      project={project}
      bridge={sessionBridge}
      messages={{ list: vi.fn().mockResolvedValue({ items: [], hasMore: false, nextCursor: null, snapshotSequence: 0 }), append: vi.fn() } as MessageBridge}
      onBack={vi.fn()}
      personal
      initialSession={session}
      initialCompanion
      chat={chat}
      providers={{ list: vi.fn().mockResolvedValue({ items: [provider] }) } as unknown as ProviderBridge}
    />,
  )
  expect(await screen.findByRole('button', { name: '只重试写入' })).toBeInTheDocument()
  expect(screen.queryByRole('button', { name: '继续上次' })).toBeNull()
  expect(inspectTurn).toHaveBeenCalledWith({ sessionId: S })
})

it('restores a running companion draft without persist-failed or auto-send', async () => {
  const inspectTurn = vi.fn().mockResolvedValue({ status: 'running', persistFailed: false, persistDraft: '流到一半还没写完' })
  const start = vi.fn()
  const chat: ChatBridge = { start, approve: vi.fn(), inspectTurn, dispose: vi.fn() }
  render(
    <SessionPage
      project={project}
      bridge={sessionBridge}
      messages={{ list: vi.fn().mockResolvedValue({ items: [], hasMore: false, nextCursor: null, snapshotSequence: 0 }), append: vi.fn() } as MessageBridge}
      onBack={vi.fn()}
      personal
      initialSession={session}
      initialCompanion
      chat={chat}
      providers={{ list: vi.fn().mockResolvedValue({ items: [provider] }) } as unknown as ProviderBridge}
    />,
  )
  expect(await screen.findByRole('button', { name: '继续上次' })).toBeInTheDocument()
  expect(screen.queryByRole('button', { name: '只重试写入' })).toBeNull()
  await act(async () => { await new Promise(resolve => setTimeout(resolve, 80)) })
  expect(start).not.toHaveBeenCalled()
})

it('shows resume on the companion stage when the server turn is interrupted', async () => {
  const inspectTurn = vi.fn().mockResolvedValue({ status: 'interrupted', persistFailed: false, persistDraft: '' })
  const chat: ChatBridge = { start: vi.fn(), approve: vi.fn(), inspectTurn, dispose: vi.fn() }
  render(
    <SessionPage
      project={project}
      bridge={sessionBridge}
      messages={{ list: vi.fn().mockResolvedValue({ items: [], hasMore: false, nextCursor: null, snapshotSequence: 0 }), append: vi.fn() } as MessageBridge}
      onBack={vi.fn()}
      personal
      initialSession={session}
      initialCompanion
      chat={chat}
      providers={{ list: vi.fn().mockResolvedValue({ items: [provider] }) } as unknown as ProviderBridge}
    />,
  )
  expect(await screen.findByRole('button', { name: '继续上次' })).toBeInTheDocument()
  expect(screen.queryByRole('button', { name: '只重试写入' })).toBeNull()
})

it('parks a UAC computer.act result as user.ask on the companion stage', async () => {
  let onEvent: (event: StreamEvent) => void = () => {}
  const start = vi.fn().mockImplementation(async (_payload: unknown, onStreamEvent: (event: StreamEvent) => void) => {
    onEvent = onStreamEvent
    return { streamId: '01ARZ3NDEKTSV4RRFFQ69G5FAD', cancel: vi.fn(), dispose: vi.fn() }
  })
  const chat: ChatBridge = { start, approve: vi.fn(), dispose: vi.fn() }
  render(
    <SessionPage
      project={project}
      bridge={sessionBridge}
      messages={{ list: vi.fn().mockResolvedValue({ items: [], hasMore: false, nextCursor: null, snapshotSequence: 0 }), append: vi.fn().mockResolvedValue({}) } as MessageBridge}
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
    speech.callbacks!.onFinal('点一下那个确认')
  })
  await waitFor(() => expect(start).toHaveBeenCalled())
  await act(async () => {
    onEvent({
      v: '1.0',
      kind: 'event',
      id: '01ARZ3NDEKTSV4RRFFQ69G5FAE',
      streamId: '01ARZ3NDEKTSV4RRFFQ69G5FAD',
      sequence: 1,
      type: 'tool_completed',
      tool: {
        callId: 'cc-uac',
        name: 'computer.act',
        argsDigest: 'a'.repeat(64),
        summary: 'needs_user: 这是系统提权对话框，我不能代点「是」。请你自己确认或取消。',
      },
    })
  })
  const wizard = await screen.findByRole('form', { name: '系统提权' })
  expect(wizard).toHaveTextContent(/不能代点「是」/)
  expect(screen.getByRole('radio', { name: /我已经处理完了/ })).toBeInTheDocument()
})

it('keeps the pending preference banner above the companion stage', async () => {
  const confirmCandidate = vi.fn().mockResolvedValue({ candidateId: '01ARZ3NDEKTSV4RRFFQ69G5FAC', state: 'confirmed' })
  const feedback = {
    record: vi.fn(),
    candidates: vi.fn().mockResolvedValue({
      items: [{ candidateId: '01ARZ3NDEKTSV4RRFFQ69G5FAC', content: '以后回答默认用中文', scopeId: 'learning', confirmationToken: 'tok', createdAt: NOW, expiresAt: NOW }],
    }),
  } as unknown as FeedbackBridge
  const memory = { confirmCandidate } as unknown as MemoryBridge
  render(
    <SessionPage
      project={project}
      bridge={sessionBridge}
      messages={{ list: vi.fn().mockResolvedValue({ items: [], hasMore: false, nextCursor: null, snapshotSequence: 0 }), append: vi.fn() } as MessageBridge}
      onBack={vi.fn()}
      personal
      initialSession={session}
      initialCompanion
      chat={{ start: vi.fn(), approve: vi.fn(), dispose: vi.fn() }}
      providers={{ list: vi.fn().mockResolvedValue({ items: [provider] }) } as unknown as ProviderBridge}
      feedback={feedback}
      memory={memory}
    />,
  )
  await waitFor(() => expect(document.querySelector('.companion-stage')).toBeTruthy())
  const banner = await screen.findByRole('status', { name: '待确认偏好' })
  expect(banner).toHaveClass('companion-float')
  fireEvent.click(screen.getByRole('button', { name: '确认沉淀' }))
  await waitFor(() => expect(confirmCandidate).toHaveBeenCalledWith(expect.objectContaining({
    candidateId: '01ARZ3NDEKTSV4RRFFQ69G5FAC',
    confirmationToken: 'tok',
    action: 'confirm',
  })))
})

it('does not delete a companion session after a spoken turn on exit', async () => {
  const del = vi.fn().mockResolvedValue(undefined)
  const update = vi.fn().mockImplementation(async (payload: { title?: string }) => ({
    ...session, title: payload.title ?? session.title, version: session.version + 1,
  }))
  const start = vi.fn().mockResolvedValue({ streamId: '01ARZ3NDEKTSV4RRFFQ69G5FAD', cancel: vi.fn(), dispose: vi.fn() })
  render(
    <SessionPage
      project={project}
      bridge={{ ...sessionBridge, delete: del, update }}
      messages={{ list: vi.fn().mockResolvedValue({ items: [], hasMore: false, nextCursor: null, snapshotSequence: 0 }), append: vi.fn().mockResolvedValue({}) } as MessageBridge}
      onBack={vi.fn()}
      personal
      initialSession={session}
      initialCompanion
      chat={{ start, approve: vi.fn(), dispose: vi.fn() }}
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
    speech.callbacks!.onFinal('今天天气怎么样')
  })
  await waitFor(() => expect(start).toHaveBeenCalled())
  fireEvent.click(screen.getByRole('button', { name: /退出月伴对话/ }))
  await act(async () => { await new Promise(resolve => setTimeout(resolve, 80)) })
  expect(del).not.toHaveBeenCalled()
})
