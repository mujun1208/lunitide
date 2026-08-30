import { cleanup, render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, beforeEach, describe, expect, test, vi } from 'vitest'

const tts = vi.hoisted(() => ({
  voices: vi.fn(),
  synthesize: vi.fn(),
  cancel: vi.fn(),
  refAudios: vi.fn(),
  ensureRefEngine: vi.fn(),
}))

vi.mock('../bridge/client', async () => {
  const actual = await vi.importActual<typeof import('../bridge/client')>('../bridge/client')
  return { ...actual, getTtsBridge: () => tts }
})

vi.mock('../session/companion/localAsr', () => ({
  localAsrStatus: () => Promise.resolve(undefined),
  installLocalAsr: vi.fn(),
  selectLocalAsrModel: vi.fn(),
}))

import { CompanionSection } from './SettingsPage'

const STORAGE_KEY = 'lunitide:companion'

describe('CompanionSection voice path', () => {
  beforeEach(() => {
    localStorage.clear()
    tts.voices.mockReset()
    tts.voices.mockResolvedValue({
      voices: [{ voice_id: 'zh-CN-XiaoxiaoNeural', display_name: '晓晓', lang: 'zh-CN', gender: 'female' }],
    })
  })

  afterEach(() => cleanup())

  test('settings offer three voice paths', async () => {
    render(<CompanionSection />)
    expect(await screen.findByRole('radiogroup', { name: '语音通道' })).toBeTruthy()
    expect(screen.getAllByRole('radio')).toHaveLength(3)
    expect(screen.getByRole('radio', { name: /云端/ })).toHaveAttribute('aria-checked', 'true')
    expect(screen.getByText('晓晓 · 微软 Neural')).toBeInTheDocument()
    expect(screen.getByText('火山听 · 晓晓读')).toBeInTheDocument()
    expect(screen.getByText('sherpa + GPT-SoVITS')).toBeInTheDocument()
    expect(document.querySelectorAll('.voice-path-card')).toHaveLength(3)
  })

  test('home wake word is on by default and can be turned off', async () => {
    const user = userEvent.setup()
    render(<CompanionSection />)
    const toggle = await screen.findByRole('switch', { name: '首页语音唤醒' })
    expect(toggle).toHaveAttribute('aria-checked', 'true')
    await user.click(toggle)
    expect(toggle).toHaveAttribute('aria-checked', 'false')
    expect(JSON.parse(localStorage.getItem(STORAGE_KEY) || '{}').wakeWord).toBe(false)
  })

  test('speaker-bleed wake gate is on by default and can be turned off', async () => {
    const user = userEvent.setup()
    render(<CompanionSection />)
    const toggle = await screen.findByRole('switch', { name: '挡扬声器误唤醒' })
    expect(toggle).toHaveAttribute('aria-checked', 'true')
    await user.click(toggle)
    expect(JSON.parse(localStorage.getItem(STORAGE_KEY) || '{}').wakeVad).toBe(false)
  })

  test('instant pad is on by default and barge-in is off', async () => {
    const user = userEvent.setup()
    render(<CompanionSection />)
    const pad = await screen.findByRole('switch', { name: '先应一声' })
    expect(pad).toHaveAttribute('aria-checked', 'true')
    const barge = screen.getByRole('switch', { name: '语音插话' })
    expect(barge).toHaveAttribute('aria-checked', 'false')
    await user.click(barge)
    expect(JSON.parse(localStorage.getItem(STORAGE_KEY) || '{}').voiceBargeIn).toBe(true)
    await user.click(pad)
    expect(JSON.parse(localStorage.getItem(STORAGE_KEY) || '{}').instantAck).toBe(false)
  })

  test('offers a correction table for ASR OOV', async () => {
    render(<CompanionSection />)
    const box = await screen.findByRole('textbox', { name: '识别纠错' })
    expect(box).toBeInTheDocument()
  })

  test('retired omni saves show cloud extras after migration', async () => {
    localStorage.setItem(STORAGE_KEY, JSON.stringify({
      enabled: true,
      voicePath: 'omni',
      engine: 'edge',
      rev: 9,
    }))
    render(<CompanionSection />)
    expect(await screen.findByRole('radio', { name: /云端/ })).toHaveAttribute('aria-checked', 'true')
    expect(screen.getAllByRole('radio')).toHaveLength(3)
    expect(screen.getByText('朗读音色')).toBeInTheDocument()
    expect(screen.getByText('回复自动朗读')).toBeInTheDocument()
    const stored = JSON.parse(localStorage.getItem(STORAGE_KEY) || '{}') as Record<string, unknown>
    expect(stored.voicePath).toBe('cloud')
    expect(stored).not.toHaveProperty('omniPersonaId')
  })

  test('volc path keeps Edge 晓晓 extras and turns barge-in on', async () => {
    const user = userEvent.setup()
    render(<CompanionSection />)
    await user.click(await screen.findByRole('radio', { name: /火山/ }))
    expect(screen.getByRole('radio', { name: /火山/ })).toHaveAttribute('aria-checked', 'true')
    expect(screen.getByText('朗读音色')).toBeInTheDocument()
    expect(screen.queryByLabelText('GPT-SoVITS 服务地址')).not.toBeInTheDocument()
    expect(JSON.parse(localStorage.getItem(STORAGE_KEY) || '{}').voiceBargeIn).toBe(true)
  })

  test('local path keeps three cards and shows GPT-SoVITS extras', async () => {
    const user = userEvent.setup()
    render(<CompanionSection />)
    await user.click(await screen.findByRole('radio', { name: /本地/ }))
    expect(screen.getAllByRole('radio')).toHaveLength(3)
    expect(screen.getByRole('radio', { name: /本地/ })).toHaveAttribute('aria-checked', 'true')
    expect(screen.getByLabelText('GPT-SoVITS 服务地址')).toBeInTheDocument()
    await waitFor(() => expect(screen.getByText(/50 种人生已内置/)).toBeInTheDocument())
  })
})
