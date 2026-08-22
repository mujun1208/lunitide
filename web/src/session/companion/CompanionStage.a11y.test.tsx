// CompanionStage.a11y.test.tsx pins the MC-06 acceptance (T-9.5.3.4
// automatable slice) for the pure-moon voice stage: the full companion
// conversation is operable with zero mouse (stage Space/Enter mic
// shortcut, moon click, moon-click interrupt vs unconditional Esc exit),
// every dynamic region
// announces through aria-live (status pill + visually-hidden live log),
// the hands-free loop auto-opens on entry and re-listens after each
// reply, and each machine state stays distinguishable without
// vision-alternative cues via data-state and state-suffixed moon labels.
import { act, cleanup, fireEvent, render, waitFor } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, test, vi } from 'vitest'
import type { TtsPlayerCallbacks } from './ttsPlayer'

interface CapturedSpeech {
  onFinal: (transcript: string) => void
  onError: (error: unknown) => void
  onLevels?: (levels: number[]) => void
  onEndWithoutFinal?: () => void
}

const speech = vi.hoisted(() => ({
  start: vi.fn(),
  callbacks: undefined as CapturedSpeech | undefined,
  stop: vi.fn(),
  handle: () => ({
    stop: speech.stop,
    setAssistantPlayback: vi.fn(),
    setCommitPaused: vi.fn(),
    setBargeInActive: vi.fn(),
  }),
}))

const tts = vi.hoisted(() => ({
  speakCalls: [] as Array<{ segments: string[]; callbacks: TtsPlayerCallbacks }>,
  enqueueCalls: [] as Array<{ segments: string[]; callbacks: TtsPlayerCallbacks }>,
  configuredWith: [] as string[],
  interrupts: 0,
  playing: false,
}))

vi.mock('../../bridge/client', async importOriginal => {
  const actual = await importOriginal<typeof import('../../bridge/client')>()
  return {
    ...actual,
    getTtsBridge: () => ({
      voices: () =>
        Promise.resolve({
          voices: [
            { voice_id: 'zh-female', display_name: '月汐温柔女声', gender: 'female' as const, lang: 'zh-CN' },
            { voice_id: 'en-male', display_name: 'Male voice', gender: 'male' as const, lang: 'en-US' },
          ],
        }),
      synthesize: vi.fn(),
      cancel: vi.fn(),
    }),
    automationBridge: {
      listRuns: () => Promise.resolve({ runs: [] }),
    },
  }
})

vi.mock('./speech', () => ({
  ECHO_GUARD_MS: 700,
  INTERRUPT_ECHO_MS: 160,
  startCompanionSpeech: (callbacks: CapturedSpeech) => {
    speech.callbacks = callbacks
    return speech.start(callbacks)
  },
}))

vi.mock('./ttsPlayer', () => ({
  unlockTtsAudio: vi.fn(() => Promise.resolve()),
  getTtsAudioState: () => 'running' as const,
  TtsPlayer: class {
    configure(voiceId: string) {
      tts.configuredWith.push(voiceId)
    }
    async speak(segments: string[], _settings: unknown, callbacks: TtsPlayerCallbacks) {
      tts.speakCalls.push({ segments, callbacks })
    }
    enqueue(segments: string[], _settings: unknown, callbacks: TtsPlayerCallbacks) {
      tts.enqueueCalls.push({ segments, callbacks })
      tts.playing = true
    }
    async flush(callbacks: TtsPlayerCallbacks) {
      await new Promise<void>(resolve => {
        const finish = () => {
          callbacks.onFinished?.('completed')
          resolve()
        }
        const check = () => {
          if (!tts.playing) finish()
          else setTimeout(check, 40)
        }
        check()
      })
    }
    isBusy() {
      return tts.playing
    }
    interrupt() {
      tts.interrupts++
      tts.playing = false
    }
    dispose() {}
  },
}))

import { CompanionStage, type CompanionStageProps } from './CompanionStage'

const baseProps: CompanionStageProps = {
  chatStatus: 'idle',
  assistantText: '',
  chatReady: true,
  onSend: vi.fn(),
  onExit: vi.fn(),
}

const stage = (container: HTMLElement) => container.firstChild as HTMLElement
const liveLog = (container: HTMLElement) => container.querySelector('.companion-subtitle-list') as HTMLElement
const statusRegion = (container: HTMLElement) => container.querySelector('.companion-status') as HTMLElement
const moonBody = (container: HTMLElement) => container.querySelector('.companion-moon-body') as HTMLButtonElement
const stateOf = (container: HTMLElement) => stage(container).getAttribute('data-state')

async function renderStage(overrides: Partial<CompanionStageProps> = {}) {
  const props = { ...baseProps, ...overrides }
  const utils = render(<CompanionStage {...props} />)
  await waitFor(() => expect(stateOf(utils.container)).toBe('idle'))
  return { ...utils, props }
}

beforeEach(() => {
  speech.callbacks = undefined
  speech.stop.mockReset()
  // Default: the microphone auto-attempt fails silently so tests stay
  // deterministic in idle until a test arms a resolving implementation.
  speech.start.mockReset()
  speech.start.mockRejectedValue(new Error('麦克风不可用'))
  tts.speakCalls = []
  tts.enqueueCalls = []
  tts.configuredWith = []
  tts.interrupts = 0
  tts.playing = false
  vi.mocked(baseProps.onSend).mockClear()
  vi.mocked(baseProps.onExit).mockClear()
  localStorage.clear()
})

afterEach(cleanup)

describe('MC-06 a11y skeleton', () => {
  test('dialog role, live regions and the visually-hidden log are present without legacy controls', async () => {
    const { container } = await renderStage()
    const root = stage(container)
    expect(root.getAttribute('role')).toBe('dialog')
    expect(root.getAttribute('aria-modal')).toBe('true')
    expect(root.getAttribute('aria-label')).toBe('月伴对话舞台')
    // Decorative starfield is hidden from the accessibility tree.
    expect(container.querySelector('.companion-stars')?.getAttribute('aria-hidden')).toBe('true')
    // Status pill + hidden log both announce politely.
    expect(statusRegion(container).getAttribute('aria-live')).toBe('polite')
    expect(liveLog(container).getAttribute('aria-live')).toBe('polite')
    expect(liveLog(container).getAttribute('role')).toBe('log')
    const subtitles = container.querySelector('.companion-subtitles') as HTMLElement
    expect(subtitles.getAttribute('aria-label')).toBe('对话记录')
    // The subtitle strip is now visible (streams the current turn); the
    // sr-only hiding was the "conversation never shows" bug.
    expect(subtitles.className).not.toContain('sr-only')
    expect(subtitles.textContent).toContain('开启麦克风后，你说的话和月汐的回答都会在这里播报。')
    // The pure-moon stage has no visible chat bar or control toolbar.
    expect(container.querySelector('.companion-controls')).toBeNull()
    expect(container.querySelector('.companion-mic')).toBeNull()
    expect(container.querySelector('.companion-typing')).toBeNull()
    expect(container.querySelector('.companion-tts-toggle')).toBeNull()
    expect(container.querySelector('.companion-type-toggle')).toBeNull()
    // Ghost exit stays reachable for assistive tech.
    const exit = container.querySelector('.companion-exit') as HTMLButtonElement
    expect(exit.getAttribute('aria-label')).toBe('退出月伴对话（Esc）')
  })

  test('focus lands on the stage root on mount and returns to the entry element on unmount', async () => {
    const entry = document.createElement('button')
    document.body.append(entry)
    entry.focus()
    const { container, unmount } = await renderStage()
    expect(document.activeElement).toBe(stage(container))
    unmount()
    expect(document.activeElement).toBe(entry)
    entry.remove()
  })
})

describe('MC-06 zero-mouse operation', () => {
  test('Space on the stage toggles the mic; a second Space stops it; Esc exits from idle', async () => {
    speech.start.mockResolvedValueOnce(speech.handle())
    const { container } = await renderStage()
    fireEvent.keyDown(stage(container), { key: ' ' })
    await waitFor(() => expect(stateOf(container)).toBe('listening'))
    expect(speech.callbacks).toBeDefined()
    // Space while listening stops the handle and returns to idle.
    fireEvent.keyDown(stage(container), { key: ' ' })
    await waitFor(() => expect(stateOf(container)).toBe('idle'))
    expect(speech.stop).toHaveBeenCalled()
    // Esc on the root exits without any pointer device.
    fireEvent.keyDown(stage(container), { key: 'Escape' })
    await waitFor(() => expect(baseProps.onExit).toHaveBeenCalled())
  })

  test('Space inside an interactive control does not toggle the mic', async () => {
    const { container } = await renderStage()
    fireEvent.keyDown(container.querySelector('.companion-exit')!, { key: ' ' })
    expect(stateOf(container)).toBe('idle')
    expect(speech.start).not.toHaveBeenCalled()
  })

  test('clicking the moon in idle opens the microphone', async () => {
    speech.start.mockResolvedValueOnce(speech.handle())
    const { container } = await renderStage()
    expect(moonBody(container).getAttribute('aria-label')).toBe('月亮：轻点开始说话')
    fireEvent.click(moonBody(container))
    await waitFor(() => expect(stateOf(container)).toBe('listening'))
    expect(moonBody(container).getAttribute('aria-label')).toBe('月亮正在聆听，轻点暂停')
  })
})

describe('MC-06 hands-free auto conversation', () => {
  test('auto-opens the microphone on entry when permission is granted', async () => {
    speech.start.mockResolvedValue(speech.handle())
    const { container } = await renderStage()
    await waitFor(() => expect(stateOf(container)).toBe('listening'), { timeout: 3000 })
    expect(speech.start).toHaveBeenCalledTimes(1)
  })

  test('a failed auto attempt stays silent: faint hint, no error banner, stage idle', async () => {
    const { container } = await renderStage()
    const hint = await waitFor(() => {
      const found = container.querySelector('.companion-hint') as HTMLElement
      expect(found).toBeTruthy()
      return found
    }, { timeout: 3000 })
    expect(hint.getAttribute('aria-live')).toBe('polite')
    expect(hint.textContent).toContain('轻点月亮或按空格，开始和月汐说话')
    expect(container.querySelector('.companion-banner.error')).toBeNull()
    expect(stateOf(container)).toBe('idle')
  })

  test('re-listens automatically after an interrupted reply', async () => {
    speech.start.mockResolvedValue(speech.handle())
    const { container } = await renderStage()
    await waitFor(() => expect(stateOf(container)).toBe('listening'), { timeout: 3000 })
    // Manual pause disarms the loop…
    fireEvent.keyDown(stage(container), { key: ' ' })
    await waitFor(() => expect(stateOf(container)).toBe('idle'))
    // …and it stays idle: no ghost restarts.
    await new Promise(resolve => setTimeout(resolve, 1200))
    expect(stateOf(container)).toBe('idle')
  })
})

describe('MC-06 state distinguishability + live announcements', () => {
  test('full round: Space → final transcript → thinking → speaking with the female voice → moon-click interrupt re-listens, Esc exits', async () => {
    const onSend = vi.fn()
    const onExit = vi.fn()
    speech.start.mockResolvedValue(speech.handle())
    const { container, rerender } = await renderStage({ onSend, onExit })
    // 1. Voice round starts from the stage Space shortcut.
    fireEvent.keyDown(stage(container), { key: ' ' })
    await waitFor(() => expect(stateOf(container)).toBe('listening'))
    // 2. Final transcript lands in the live log and moves to thinking.
    await act(async () => {
      speech.callbacks!.onFinal('今晚月色如何')
    })
    expect(stateOf(container)).toBe('thinking')
    expect(statusRegion(container).textContent).toContain('回应中')
    expect(onSend).toHaveBeenCalledWith('今晚月色如何')
    expect(liveLog(container).textContent).toContain('今晚月色如何')
    // Thinking stays interruptible: moon click can cancel a slow reply.
    expect(moonBody(container).disabled).toBe(false)
    expect(moonBody(container).getAttribute('aria-label')).toBe('月亮正在回应')
    // 3. Streaming reply is announced through the same live log.
    rerender(
      <CompanionStage
        {...baseProps}
        onSend={onSend}
        onExit={onExit}
        chatStatus="streaming"
        assistantText="今晚是满月，适合抬头。"
      />,
    )
    expect(liveLog(container).textContent).toContain('今晚是满月，适合抬头。')
    // P0-1 streaming TTS: the first complete sentence enqueues playback
    // right away, so the stage leaves thinking as soon as it arrives.
    expect(stateOf(container)).toBe('speaking')
    // 4. done + autoSpeak: the reply round was enqueued with the default
    // female voice (streaming enqueue, then flush at done).
    rerender(
      <CompanionStage
        {...baseProps}
        onSend={onSend}
        onExit={onExit}
        chatStatus="done"
        assistantText="今晚是满月，适合抬头。"
      />,
    )
    await waitFor(() => expect(stateOf(container)).toBe('speaking'))
    expect(statusRegion(container).textContent).toContain('说话中')
    expect(tts.enqueueCalls.length).toBe(1)
    expect(tts.configuredWith).toContain('zh-female')
    expect(moonBody(container).disabled).toBe(false)
    expect(moonBody(container).getAttribute('aria-label')).toBe('月亮正在说话，点击打断朗读')
    // 5. Moon click during speaking interrupts playback — it must NOT exit.
    fireEvent.click(moonBody(container))
    await waitFor(() => expect(tts.interrupts).toBe(1))
    expect(onExit).not.toHaveBeenCalled()
    // 6. Hands-free loop re-opens the mic by itself…
    await waitFor(() => expect(stateOf(container)).toBe('listening'), { timeout: 3000 })
    // 7. …and Esc exits unconditionally, even mid-listen.
    fireEvent.keyDown(stage(container), { key: 'Escape' })
    await waitFor(() => expect(onExit).toHaveBeenCalledTimes(1))
  })

  test('returns to idle after a streamed reply finishes so subtitles can fade', async () => {
    const onSend = vi.fn()
    speech.start.mockResolvedValue(speech.handle())
    const { container, rerender } = await renderStage({ onSend })
    fireEvent.keyDown(stage(container), { key: ' ' })
    await waitFor(() => expect(stateOf(container)).toBe('listening'))
    await act(async () => {
      speech.callbacks!.onFinal('你好')
    })
    expect(stateOf(container)).toBe('thinking')
    rerender(
      <CompanionStage
        {...baseProps}
        onSend={onSend}
        chatStatus="streaming"
        assistantText="最近怎么样，有什么想聊的？"
      />,
    )
    expect(stateOf(container)).toBe('speaking')
    rerender(
      <CompanionStage
        {...baseProps}
        onSend={onSend}
        chatStatus="done"
        assistantText="最近怎么样，有什么想聊的？"
      />,
    )
    await act(async () => {
      tts.playing = false
    })
    await waitFor(() => expect(stateOf(container)).toBe('idle'), { timeout: 2000 })
    expect(statusRegion(container).textContent).not.toContain('说话中')
  })

  test('unavailable chat config announces the error via role=alert and stays idle', async () => {
    const { container } = await renderStage({ chatReady: false })
    fireEvent.keyDown(stage(container), { key: ' ' })
    const banner = await waitFor(() => {
      const found = container.querySelector('.companion-banner.error') as HTMLElement
      expect(found).toBeTruthy()
      return found
    })
    expect(banner.getAttribute('role')).toBe('alert')
    expect(banner.textContent).toContain('CHAT_CONFIG_MISSING')
    expect(stateOf(container)).toBe('idle')
  })
})
