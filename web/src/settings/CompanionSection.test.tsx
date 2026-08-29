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

  test('settings still offer two voice paths and never MiniCPM-o', async () => {
    render(<CompanionSection />)
    expect(await screen.findByRole('radiogroup', { name: '语音通道' })).toBeTruthy()
    expect(screen.getAllByRole('radio')).toHaveLength(2)
    expect(screen.getByRole('radio', { name: /云端/ })).toHaveAttribute('aria-checked', 'true')
    expect(screen.getByText('晓晓 · 微软 Neural')).toBeInTheDocument()
    expect(screen.getByText('sherpa + GPT-SoVITS')).toBeInTheDocument()
    expect(screen.queryByText(/MiniCPM/i)).not.toBeInTheDocument()
    expect(screen.queryByText(/omni/i)).not.toBeInTheDocument()
    expect(document.querySelectorAll('.voice-path-card')).toHaveLength(2)
  })

  test('leftover MiniCPM-o settings open the 云端 extras', async () => {
    localStorage.setItem(STORAGE_KEY, JSON.stringify({
      enabled: true,
      voicePath: 'omni',
      engine: 'edge',
      rev: 9,
    }))
    render(<CompanionSection />)
    expect(await screen.findByRole('radio', { name: /云端/ })).toHaveAttribute('aria-checked', 'true')
    expect(screen.getAllByRole('radio')).toHaveLength(2)
    expect(screen.queryByText(/MiniCPM/i)).not.toBeInTheDocument()
    expect(screen.getByText('朗读音色')).toBeInTheDocument()
    expect(screen.getByText('回复自动朗读')).toBeInTheDocument()
  })

  test('local path keeps two cards and shows GPT-SoVITS extras', async () => {
    const user = userEvent.setup()
    render(<CompanionSection />)
    await user.click(await screen.findByRole('radio', { name: /本地/ }))
    expect(screen.getAllByRole('radio')).toHaveLength(2)
    expect(screen.getByRole('radio', { name: /本地/ })).toHaveAttribute('aria-checked', 'true')
    expect(screen.getByLabelText('GPT-SoVITS 服务地址')).toBeInTheDocument()
    expect(screen.queryByText(/MiniCPM/i)).not.toBeInTheDocument()
    await waitFor(() => expect(screen.getByText(/50 种人生已内置/)).toBeInTheDocument())
  })
})
