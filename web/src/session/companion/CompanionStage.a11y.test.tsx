// CompanionStage.a11y.test.tsx pins the MC-06 acceptance (T-9.5.3.4
// automatable slice): the full companion conversation is operable with
// zero mouse (stage Space/Enter mic shortcut, Esc interrupt-then-exit,
// keyboard composer), every dynamic region announces through aria-live
// (status label + subtitle log), and each machine state stays
// distinguishable without vision-alternative cues via data-state,
// aria-pressed and state-suffixed labels.
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
  callbacks: undefined as CapturedSpeech | undefined,
  stop: vi.fn(),
}))

const tts = vi.hoisted(() => ({
  speakCalls: [] as Array<{ segments: string[]; callbacks: TtsPlayerCallbacks }>,
  interrupts: 0,
}))

vi.mock('../../bridge/client', async importOriginal => {
  const actual = await importOriginal<typeof import('../../bridge/client')>()
  return {
    ...actual,
    getTtsBridge: () => ({
      voices: () =>
        Promise.resolve({
          voices: [{ voice_id: 'zh-1', display_name: '月汐', gender: 'female' as const, lang: 'zh-CN' }],
        }),
      synthesize: vi.fn(),
      cancel: vi.fn(),
    }),
  }
})

vi.mock('./speech', () => ({
  startCompanionSpeech: vi.fn((callbacks: CapturedSpeech) => {
    speech.callbacks = callbacks
    return Promise.resolve({ stop: speech.stop })
  }),
}))

vi.mock('./ttsPlayer', () => ({
  TtsPlayer: class {
    configure() {}
    async speak(segments: string[], _settings: unknown, callbacks: TtsPlayerCallbacks) {
      tts.speakCalls.push({ segments, callbacks })
    }
    interrupt() {
      tts.interrupts++
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
const subtitleBox = (container: HTMLElement) => container.querySelector('.companion-subtitles') as HTMLElement
const subtitleLog = (container: HTMLElement) => container.querySelector('.companion-subtitle-list') as HTMLElement
const statusRegion = (container: HTMLElement) => container.querySelector('.companion-status') as HTMLElement
const micButton = (container: HTMLElement) => container.querySelector('.companion-mic') as HTMLButtonElement
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
  tts.speakCalls = []
  tts.interrupts = 0
  vi.mocked(baseProps.onSend).mockClear()
  vi.mocked(baseProps.onExit).mockClear()
  localStorage.clear()
})

afterEach(cleanup)

describe('MC-06 a11y skeleton', () => {
  test('dialog/toolbar/log roles and aria-live regions are present', async () => {
    const { container } = await renderStage()
    const root = stage(container)
    expect(root.getAttribute('role')).toBe('dialog')
    expect(root.getAttribute('aria-modal')).toBe('true')
    expect(root.getAttribute('aria-label')).toBe('月伴对话舞台')
    // Decorative starfield is hidden from the accessibility tree.
    expect(container.querySelector('.companion-stars')?.getAttribute('aria-hidden')).toBe('true')
    // Status line + subtitle log both announce politely.
    expect(statusRegion(container).getAttribute('aria-live')).toBe('polite')
    expect(subtitleLog(container).getAttribute('aria-live')).toBe('polite')
    expect(subtitleLog(container).getAttribute('role')).toBe('log')
    expect(subtitleBox(container).getAttribute('aria-label')).toBe('对话字幕')
    const toolbar = container.querySelector('.companion-controls')
    expect(toolbar?.getAttribute('role')).toBe('toolbar')
    expect(toolbar?.getAttribute('aria-label')).toBe('月伴控制')
  })

  test('focus lands on the mic on mount and returns to the entry element on unmount', async () => {
    const entry = document.createElement('button')
    document.body.append(entry)
    entry.focus()
    const { unmount } = await renderStage()
    expect(document.activeElement).toBe(micButton(document.body as unknown as HTMLElement))
    unmount()
    expect(document.activeElement).toBe(entry)
    entry.remove()
  })
})

describe('MC-06 zero-mouse operation', () => {
  test('Space on the subtitle viewer toggles the mic (stage shortcut is not dead code)', async () => {
    const { container } = await renderStage()
    fireEvent.keyDown(subtitleBox(container), { key: ' ' })
    await waitFor(() => expect(stateOf(container)).toBe('listening'))
    expect(speech.callbacks).toBeDefined()
    // Esc on the root exits from idle without any pointer device.
    fireEvent.keyDown(stage(container), { key: 'Escape' })
    await waitFor(() => expect(baseProps.onExit).toHaveBeenCalled())
  })

  test('Space inside the composer input types a space instead of toggling the mic', async () => {
    const { container } = await renderStage()
    // Open the folded composer from its focused native button (Enter
    // activation is a browser default action; jsdom needs the click).
    const typeToggle = container.querySelector('.companion-type-toggle') as HTMLButtonElement
    typeToggle.focus()
    fireEvent.click(typeToggle)
    const input = await waitFor(() => {
      const found = container.querySelector('.companion-typing input') as HTMLInputElement
      expect(found).toBeTruthy()
      return found
    })
    expect(document.activeElement).toBe(input)
    fireEvent.change(input, { target: { value: '你好' } })
    fireEvent.keyDown(input, { key: ' ' })
    expect(stateOf(container)).toBe('idle')
    expect(speech.callbacks).toBeUndefined()
    // Esc inside the composer only collapses it — never exits the stage.
    fireEvent.keyDown(input, { key: 'Escape' })
    expect(container.querySelector('.companion-typing')).toBeNull()
    expect(baseProps.onExit).not.toHaveBeenCalled()
  })

  test('typed send is fully keyboard-driven and reaches thinking', async () => {
    const onSend = vi.fn()
    const { container } = await renderStage({ onSend })
    const typeToggle = container.querySelector('.companion-type-toggle') as HTMLButtonElement
    fireEvent.click(typeToggle)
    const input = container.querySelector('.companion-typing input') as HTMLInputElement
    fireEvent.change(input, { target: { value: '帮我总结手册' } })
    fireEvent.submit(input.form!)
    await waitFor(() => expect(stateOf(container)).toBe('thinking'))
    expect(onSend).toHaveBeenCalledWith('帮我总结手册')
    expect(subtitleLog(container).textContent).toContain('帮我总结手册')
  })
})

describe('MC-06 state distinguishability + live announcements', () => {
  test('listening flips aria-pressed, the mic label and the announced status text', async () => {
    const { container } = await renderStage()
    const mic = micButton(container)
    expect(mic.getAttribute('aria-pressed')).toBe('false')
    expect(mic.getAttribute('aria-label')).toBe('语音输入（空格）')
    expect(statusRegion(container).textContent).toContain('待机')
    fireEvent.keyDown(subtitleBox(container), { key: 'Enter' })
    await waitFor(() => expect(stateOf(container)).toBe('listening'))
    expect(mic.getAttribute('aria-pressed')).toBe('true')
    expect(mic.getAttribute('aria-label')).toBe('取消语音输入')
    expect(statusRegion(container).textContent).toContain('聆听中')
    expect(mic.className).toContain('state-listening')
  })

  test('full round: final transcript → thinking → streaming subtitle → speaking → Esc interrupts, second Esc exits', async () => {
    const onSend = vi.fn()
    const onExit = vi.fn()
    const { container, rerender } = await renderStage({ onSend, onExit })
    // 1. Voice round starts from the stage Space shortcut.
    fireEvent.keyDown(subtitleBox(container), { key: ' ' })
    await waitFor(() => expect(stateOf(container)).toBe('listening'))
    // 2. Final transcript lands in the live log and moves to thinking.
    await act(async () => {
      speech.callbacks!.onFinal('今晚月色如何')
    })
    expect(stateOf(container)).toBe('thinking')
    expect(statusRegion(container).textContent).toContain('思考中')
    expect(onSend).toHaveBeenCalledWith('今晚月色如何')
    expect(subtitleLog(container).textContent).toContain('今晚月色如何')
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
    expect(subtitleLog(container).textContent).toContain('今晚是满月，适合抬头。')
    expect(stateOf(container)).toBe('thinking')
    // 4. done + autoSpeak → speaking with an interruptible moon.
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
    expect(tts.speakCalls.length).toBe(1)
    const moon = container.querySelector('.companion-moon-body') as HTMLButtonElement
    expect(moon.disabled).toBe(false)
    expect(moon.getAttribute('aria-label')).toBe('月亮正在说话，点击打断朗读')
    // 5. Esc during speaking interrupts playback — it must NOT exit.
    fireEvent.keyDown(stage(container), { key: 'Escape' })
    await waitFor(() => expect(stateOf(container)).toBe('idle'))
    expect(tts.interrupts).toBe(1)
    expect(onExit).not.toHaveBeenCalled()
    // 6. Esc from idle exits the stage.
    fireEvent.keyDown(stage(container), { key: 'Escape' })
    await waitFor(() => expect(onExit).toHaveBeenCalled())
  })

  test('unavailable chat config disables the mic and announces the error via role=alert', async () => {
    const { container } = await renderStage({ chatReady: false })
    const mic = micButton(container)
    expect(mic.disabled).toBe(true)
    // The stage shortcut still surfaces the config error politely.
    fireEvent.keyDown(subtitleBox(container), { key: ' ' })
    const banner = await waitFor(() => {
      const found = container.querySelector('.companion-banner.error') as HTMLElement
      expect(found).toBeTruthy()
      return found
    })
    expect(banner.getAttribute('role')).toBe('alert')
    expect(banner.textContent).toContain('CHAT_CONFIG_MISSING')
    expect(stateOf(container)).toBe('idle')
  })

  test('auto-speak toggle exposes its pressed state to assistive tech', async () => {
    const { container } = await renderStage()
    const toggle = container.querySelector('.companion-tts-toggle') as HTMLButtonElement
    await waitFor(() => expect(toggle.disabled).toBe(false))
    expect(toggle.getAttribute('aria-pressed')).toBe('true')
    expect(toggle.getAttribute('aria-label')).toBe('关闭自动朗读')
    fireEvent.click(toggle)
    expect(toggle.getAttribute('aria-pressed')).toBe('false')
    expect(toggle.getAttribute('aria-label')).toBe('开启自动朗读')
  })
})
