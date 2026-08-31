// CompanionStage.a11y.test.tsx pins the MC-06 acceptance (T-9.5.3.4
// automatable slice) for the voice stage: the full companion
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
  onInterim?: (transcript: string) => void
  onSpeechStart?: () => void
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
    pulseRecognition: vi.fn(),
    forceCommit: vi.fn(),
    resumeCapture: vi.fn(),
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
      ensureRefEngine: vi.fn().mockResolvedValue({ state: 'online' }),
    }),
    getProviderBridge: () => ({
      list: () => Promise.resolve({
        items: [{
          id: '01ARZ3NDEKTSV4RRFFQ69G5FAW',
          name: 'Chat',
          protocol: 'openai_compatible',
          status: 'enabled',
          credentialState: 'configured',
          models: [{ modelId: 'chat', displayName: 'Chat', isDefault: true, kind: 'llm', kindDefault: true }],
        }],
      }),
    }),
    automationBridge: {
      listRuns: () => Promise.resolve({ runs: [] }),
    },
  }
})

vi.mock('./speech', () => ({
  ECHO_GUARD_MS: 700,
  FORCE_COMMIT_MS: 1800,
  INTERRUPT_ECHO_MS: 160,
  shouldShowSpeechSetupHint: (input: {
    listening: boolean
    hasInterim: boolean
    listenSeconds: number
    heardThisVisit: boolean
    hasUserRound: boolean
  }) =>
    input.listening &&
    !input.hasInterim &&
    input.listenSeconds >= 20 &&
    !input.heardThisVisit &&
    !input.hasUserRound,
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
    interrupt(_options?: { cancelEngine?: boolean }) {
      tts.interrupts++
      tts.playing = false
    }
    dispose() {}
  },
}))

import { CompanionStage, type CompanionStageProps } from './CompanionStage'
import { COMPANION_PAD_SPEECH } from './companionText'

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
    // Decorative aurora is hidden from the accessibility tree.
    expect(container.querySelector('.companion-aurora')?.getAttribute('aria-hidden')).toBe('true')
    // Status pill + hidden log both announce politely.
    expect(statusRegion(container).getAttribute('aria-live')).toBe('polite')
    expect(liveLog(container).getAttribute('aria-live')).toBe('polite')
    expect(liveLog(container).getAttribute('role')).toBe('log')
    const subtitles = container.querySelector('.companion-subtitles') as HTMLElement
    expect(subtitles.getAttribute('aria-label')).toBe('对话记录')
    // The subtitle strip is now visible (streams the current turn); the
    // sr-only hiding was the "conversation never shows" bug.
    expect(subtitles.className).not.toContain('sr-only')
    expect(subtitles.textContent).toContain('进入后即可说话')
    // The voice stage has no toolbar, typing chrome, or text ask bar.
    expect(container.querySelector('.companion-controls')).toBeNull()
    expect(container.querySelector('.companion-mic')).toBeNull()
    expect(container.querySelector('.companion-typing')).toBeNull()
    expect(container.querySelector('.companion-tts-toggle')).toBeNull()
    expect(container.querySelector('.companion-type-toggle')).toBeNull()
    expect(container.querySelector('.companion-ask')).toBeNull()
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
  test('Space on the stage while listening does not cancel the mic; Esc exits', async () => {
    speech.start.mockResolvedValue(speech.handle())
    const { container } = await renderStage()
    await waitFor(() => expect(stateOf(container)).toBe('listening'))
    expect(speech.callbacks).toBeDefined()
    fireEvent.keyDown(stage(container), { key: ' ' })
    expect(stateOf(container)).toBe('listening')
    expect(speech.stop).not.toHaveBeenCalled()
    fireEvent.keyDown(stage(container), { key: 'Escape' })
    await waitFor(() => expect(baseProps.onExit).toHaveBeenCalled())
  })

  test('Space inside an interactive control does not toggle the mic', async () => {
    const { container } = await renderStage()
    await waitFor(() => expect(speech.start).toHaveBeenCalled())
    speech.start.mockClear()
    fireEvent.keyDown(container.querySelector('.companion-exit')!, { key: ' ' })
    expect(stateOf(container)).toBe('idle')
    expect(speech.start).not.toHaveBeenCalled()
  })

  test('clicking the moon in idle opens the microphone', async () => {
    const { container } = await renderStage()
    await waitFor(() => expect(speech.start).toHaveBeenCalled())
    await waitFor(() => expect(stateOf(container)).toBe('idle'))
    expect(moonBody(container).getAttribute('aria-label')).toBe('月亮：轻点开始说话')
    speech.start.mockResolvedValueOnce(speech.handle())
    fireEvent.click(moonBody(container))
    await waitFor(() => expect(stateOf(container)).toBe('listening'))
    expect(moonBody(container).getAttribute('aria-label')).toBe('月亮正在聆听')
  })
})

describe('MC-06 hands-free auto conversation', () => {
  test('auto-opens the microphone on entry when permission is granted', async () => {
    speech.start.mockResolvedValue(speech.handle())
    const { container } = await renderStage()
    await waitFor(() => expect(stateOf(container)).toBe('listening'), { timeout: 3000 })
    expect(speech.start).toHaveBeenCalledTimes(1)
  })

  test('paints the first interim words immediately', async () => {
    speech.start.mockResolvedValue(speech.handle())
    const { container } = await renderStage()
    await waitFor(() => expect(stateOf(container)).toBe('listening'), { timeout: 3000 })
    await act(async () => {
      speech.callbacks!.onInterim?.('你好月汐')
    })
    expect(liveLog(container).textContent).toContain('你好月汐')
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

  test('explicit pause disarms the loop; Space while listening does not', async () => {
    speech.start.mockResolvedValue(speech.handle())
    const { container } = await renderStage()
    await waitFor(() => expect(stateOf(container)).toBe('listening'), { timeout: 3000 })
    fireEvent.keyDown(stage(container), { key: ' ' })
    expect(stateOf(container)).toBe('listening')
    fireEvent.click(container.querySelector('.companion-pause') as HTMLButtonElement)
    await waitFor(() => expect(stateOf(container)).toBe('idle'))
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
    await act(async () => {
      await new Promise(resolve => setTimeout(resolve, 450))
    })
    // 1. Auto-start already opened the microphone on entry.
    await waitFor(() => expect(stateOf(container)).toBe('listening'), { timeout: 3000 })
    // 2. Final transcript lands in the live log and moves to thinking.
    await act(async () => {
      speech.callbacks!.onFinal('今晚月色如何')
    })
    expect(stateOf(container)).toBe('thinking')
    expect(statusRegion(container).textContent).toContain('对答中')
    expect(onSend).toHaveBeenCalledWith('今晚月色如何')
    expect(liveLog(container).textContent).toContain('今晚月色如何')
    expect(liveLog(container).textContent).not.toContain('嗯')
    expect(tts.enqueueCalls.map(call => call.segments.join(''))).toEqual([])
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
    expect(tts.enqueueCalls.filter(call => call.segments.join('') !== COMPANION_PAD_SPEECH).length).toBe(1)
    expect(tts.configuredWith).toContain('zh-female')
    expect(moonBody(container).disabled).toBe(false)
    expect(moonBody(container).getAttribute('aria-label')).toBe('月亮正在说话，点击打断朗读')
    // 5. Moon click during speaking interrupts playback — it must NOT exit.
    fireEvent.click(moonBody(container))
    await waitFor(() => expect(tts.interrupts).toBeGreaterThanOrEqual(1))
    expect(onExit).not.toHaveBeenCalled()
    // 6. Hands-free loop re-opens the mic by itself…
    await waitFor(() => expect(stateOf(container)).toBe('listening'), { timeout: 3000 })
    // 7. …and Esc exits unconditionally, even mid-listen.
    fireEvent.keyDown(stage(container), { key: 'Escape' })
    await waitFor(() => expect(onExit).toHaveBeenCalledTimes(1))
  })

  test('interrupt button and Tab shortcut stop speaking without exiting', async () => {
    const onSend = vi.fn()
    const onExit = vi.fn()
    const handle = speech.handle()
    speech.start.mockResolvedValue(handle)
    const { container, rerender } = await renderStage({ onSend, onExit })
    await waitFor(() => expect(stateOf(container)).toBe('listening'), { timeout: 3000 })
    await act(async () => {
      speech.callbacks!.onFinal('打断我一下')
    })
    rerender(
      <CompanionStage
        {...baseProps}
        onSend={onSend}
        onExit={onExit}
        chatStatus="done"
        assistantText="好，我先说到这里。"
      />,
    )
    await waitFor(() => expect(stateOf(container)).toBe('speaking'))
    const interruptBtn = container.querySelector('.companion-interrupt') as HTMLButtonElement
    expect(interruptBtn.disabled).toBe(false)
    fireEvent.click(interruptBtn)
    await waitFor(() => expect(tts.interrupts).toBeGreaterThanOrEqual(1))
    expect(onExit).not.toHaveBeenCalled()
    await waitFor(() => expect(stateOf(container)).toBe('listening'), { timeout: 3000 })
    expect(statusRegion(container).textContent).toContain('聆听中')
    expect(handle.resumeCapture).toHaveBeenCalled()
    await act(async () => {
      speech.callbacks!.onFinal('下一句你好吗')
    })
    expect(onSend).toHaveBeenLastCalledWith('下一句你好吗')
  })

  test('voice does not interrupt speaking; only the interrupt control does', async () => {
    const onSend = vi.fn()
    const onExit = vi.fn()
    const onCancel = vi.fn()
    speech.start.mockResolvedValue(speech.handle())
    const { container, rerender } = await renderStage({ onSend, onExit, onCancel })
    await waitFor(() => expect(stateOf(container)).toBe('listening'), { timeout: 3000 })
    await act(async () => {
      speech.callbacks!.onFinal('先听我说完')
    })
    rerender(
      <CompanionStage
        {...baseProps}
        onSend={onSend}
        onExit={onExit}
        onCancel={onCancel}
        chatStatus="done"
        assistantText="好，我先说到这里。"
      />,
    )
    await waitFor(() => expect(stateOf(container)).toBe('speaking'))
    const interruptsBefore = tts.interrupts
    await act(async () => {
      speech.callbacks!.onFinal('我想打断你')
    })
    expect(tts.interrupts).toBe(interruptsBefore)
    expect(onCancel).not.toHaveBeenCalled()
    expect(onSend).toHaveBeenCalledTimes(1)
    expect(onSend).toHaveBeenCalledWith('先听我说完')
    expect(stateOf(container)).toBe('speaking')
    expect(onExit).not.toHaveBeenCalled()
  })

  test('queues a real next sentence while she is speaking and sends it after this turn', async () => {
    const onSend = vi.fn()
    speech.start.mockResolvedValue(speech.handle())
    const { container, rerender } = await renderStage({ onSend })
    await waitFor(() => expect(stateOf(container)).toBe('listening'), { timeout: 3000 })
    await act(async () => {
      speech.callbacks!.onFinal('先听我说完')
    })
    rerender(
      <CompanionStage
        {...baseProps}
        onSend={onSend}
        chatStatus="done"
        assistantText="好，我先说到这里。"
      />,
    )
    await waitFor(() => expect(stateOf(container)).toBe('speaking'))
    await act(async () => {
      speech.callbacks!.onFinal('帮我打开桌面')
    })
    expect(onSend).toHaveBeenCalledTimes(1)
    expect(container.textContent).toMatch(/记下/)
    rerender(
      <CompanionStage
        {...baseProps}
        onSend={onSend}
        chatStatus="done"
        assistantText="好，我先说到这里。"
      />,
    )
    await act(async () => {
      tts.playing = false
    })
    await waitFor(() => expect(onSend).toHaveBeenLastCalledWith('帮我打开桌面'), { timeout: 3000 })
  })

  test('keeps a queued sentence when onSend says the previous turn is still busy', async () => {
    const onSend = vi.fn().mockReturnValueOnce(true).mockReturnValueOnce(false).mockReturnValue(true)
    speech.start.mockResolvedValue(speech.handle())
    const { container, rerender } = await renderStage({ onSend })
    await waitFor(() => expect(stateOf(container)).toBe('listening'), { timeout: 3000 })
    await act(async () => {
      speech.callbacks!.onFinal('先听我说完')
    })
    rerender(
      <CompanionStage
        {...baseProps}
        onSend={onSend}
        chatStatus="done"
        assistantText="好，我先说到这里。"
      />,
    )
    await waitFor(() => expect(stateOf(container)).toBe('speaking'))
    await act(async () => {
      speech.callbacks!.onFinal('帮我打开桌面')
    })
    await act(async () => {
      tts.playing = false
    })
    rerender(
      <CompanionStage
        {...baseProps}
        onSend={onSend}
        chatStatus="done"
        assistantText="好，我先说到这里。"
      />,
    )
    await waitFor(() => expect(container.textContent).toMatch(/还在发送/))
    expect(onSend).toHaveBeenCalledWith('帮我打开桌面')
    rerender(
      <CompanionStage
        {...baseProps}
        onSend={onSend}
        chatStatus="idle"
        assistantText="好，我先说到这里。"
      />,
    )
    await waitFor(() => expect(onSend.mock.calls.filter(call => call[0] === '帮我打开桌面').length).toBeGreaterThanOrEqual(2))
  })

  test('a new utterance clears the previous round and shows only this turn', async () => {
    const onSend = vi.fn()
    const handle = speech.handle()
    speech.start.mockResolvedValue(handle)
    const { container, rerender } = await renderStage({ onSend })
    await waitFor(() => expect(stateOf(container)).toBe('listening'), { timeout: 3000 })
    await act(async () => {
      speech.callbacks!.onFinal('今晚月色如何')
    })
    rerender(
      <CompanionStage
        {...baseProps}
        onSend={onSend}
        chatStatus="streaming"
        assistantText="今晚是满月，适合抬头。"
      />,
    )
    expect(liveLog(container).textContent).toContain('今晚月色如何')
    expect(liveLog(container).textContent).toContain('今晚是满月，适合抬头。')
    fireEvent.click(moonBody(container))
    await waitFor(() => expect(stateOf(container)).toBe('listening'), { timeout: 3000 })
    expect(handle.resumeCapture).toHaveBeenCalled()
    await act(async () => {
      speech.callbacks!.onInterim?.('下一句')
    })
    expect(liveLog(container).textContent).not.toContain('今晚月色如何')
    expect(liveLog(container).textContent).not.toContain('今晚是满月')
    expect(liveLog(container).textContent).toContain('下一句')
    await act(async () => {
      speech.callbacks!.onFinal('下一句你好吗')
    })
    expect(onSend).toHaveBeenLastCalledWith('下一句你好吗')
    expect(liveLog(container).textContent).toContain('下一句你好吗')
    expect(liveLog(container).textContent).not.toContain('今晚月色如何')
  })

  test('returns to idle after a streamed reply finishes and keeps this round visible', async () => {
    const onSend = vi.fn()
    speech.start.mockResolvedValue(speech.handle())
    const { container, rerender } = await renderStage({ onSend })
    await waitFor(() => expect(stateOf(container)).toBe('listening'), { timeout: 3000 })
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
    expect(liveLog(container).textContent).toContain('你好')
    expect(liveLog(container).textContent).toContain('最近怎么样')
  })

  test('does not open the microphone before the chat model is ready', async () => {
    speech.start.mockResolvedValue(speech.handle())
    const { container } = await renderStage({ chatReady: false })
    await waitFor(() => expect(stateOf(container)).toBe('idle'), { timeout: 3000 })
    expect(speech.start).not.toHaveBeenCalled()
    expect(container.textContent).toMatch(/不会空听/)
  })
})

describe('subtitle strip: this round only', () => {
  test('eight hands-free rounds stay listening, ignore TTS echo, and still accept 打断', async () => {
    const onSend = vi.fn()
    const onExit = vi.fn()
    const handle = speech.handle()
    speech.start.mockResolvedValue(handle)
    const { container, rerender } = await renderStage({ onSend, onExit })
    await waitFor(() => expect(stateOf(container)).toBe('listening'), { timeout: 3000 })
    const lines = [
      '你好月汐',
      '今晚天气怎么样',
      '帮我打开桌面',
      '打开网易云音乐',
      '搜索周杰伦放一首',
      '下一句你好吗',
      '谢谢',
      '再见',
    ]
    for (const [index, line] of lines.entries()) {
      await act(async () => {
        speech.callbacks!.onInterim?.(line)
        speech.callbacks!.onFinal(line)
      })
      expect(onSend).toHaveBeenLastCalledWith(line)
      expect(liveLog(container).textContent).toContain(line)
      const reply = `好的，${line}`
      rerender(
        <CompanionStage
          {...baseProps}
          onSend={onSend}
          onExit={onExit}
          chatStatus="streaming"
          assistantText={reply}
        />,
      )
      expect(stateOf(container)).toBe('speaking')
      await act(async () => {
        speech.callbacks!.onFinal(reply)
      })
      expect(onSend).toHaveBeenCalledTimes(index + 1)
      rerender(
        <CompanionStage
          {...baseProps}
          onSend={onSend}
          onExit={onExit}
          chatStatus="done"
          assistantText={reply}
        />,
      )
      await act(async () => {
        tts.playing = false
        tts.enqueueCalls.at(-1)?.callbacks.onFinished?.('completed')
      })
      await waitFor(() => expect(stateOf(container)).toBe('listening'), { timeout: 4000 })
    }
    expect(onSend).toHaveBeenCalledTimes(lines.length)
    fireEvent.click(container.querySelector('.companion-interrupt') as HTMLButtonElement)
    expect(onExit).not.toHaveBeenCalled()
    fireEvent.keyDown(stage(container), { key: 'Escape' })
    await waitFor(() => expect(onExit).toHaveBeenCalledTimes(1))
  })

  test('a new user utterance replaces the previous user line', async () => {
    speech.start.mockResolvedValue(speech.handle())
    const { container } = await renderStage()
    await waitFor(() => expect(stateOf(container)).toBe('listening'), { timeout: 3000 })
    await act(async () => {
      speech.callbacks!.onInterim?.('你好月汐')
    })
    expect(liveLog(container).textContent).toContain('你好月汐')
    await act(async () => {
      speech.callbacks!.onFinal('帮我打开桌面')
    })
    expect(liveLog(container).textContent).toContain('帮我打开桌面')
    expect(liveLog(container).textContent).not.toContain('你好月汐')
  })

  test('does not paint the MiniCPM-o clone label as a user or assistant line', async () => {
    speech.start.mockResolvedValue(speech.handle())
    const onSend = vi.fn()
    const { container, rerender } = await renderStage({ onSend })
    await waitFor(() => expect(stateOf(container)).toBe('listening'), { timeout: 3000 })
    await act(async () => {
      speech.callbacks!.onInterim?.('人生：优质台湾腔')
      speech.callbacks!.onFinal('人生：优质台湾腔')
    })
    expect(onSend).not.toHaveBeenCalled()
    expect(liveLog(container).textContent).not.toContain('人生：优质台湾腔')
    rerender(
      <CompanionStage
        {...baseProps}
        onSend={onSend}
        chatStatus="streaming"
        assistantText="人生：优质台湾腔"
      />,
    )
    expect(liveLog(container).textContent).not.toContain('人生：')
  })

  test('strips 我做完了 from the assistant subtitle', async () => {
    speech.start.mockResolvedValue(speech.handle())
    const { container, rerender } = await renderStage()
    await waitFor(() => expect(stateOf(container)).toBe('listening'), { timeout: 3000 })
    await act(async () => {
      speech.callbacks!.onFinal('建一个文件夹')
    })
    rerender(
      <CompanionStage
        {...baseProps}
        chatStatus="done"
        assistantText="文件夹建好了。我已经做完了。"
      />,
    )
    expect(liveLog(container).textContent).toContain('文件夹建好了')
    expect(liveLog(container).textContent).not.toContain('我已经做完了')
    expect(liveLog(container).textContent).not.toContain('任务已完成')
  })
})
