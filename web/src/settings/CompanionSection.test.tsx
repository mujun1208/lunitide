import { cleanup, render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, beforeEach, describe, expect, test, vi } from 'vitest'

const tts = vi.hoisted(() => ({
  voices: vi.fn(),
  synthesize: vi.fn(),
  cancel: vi.fn(),
  refAudios: vi.fn(),
  ensureRefEngine: vi.fn(),
  installRefEngine: vi.fn(),
}))

const providers = vi.hoisted(() => ({
  list: vi.fn(),
}))

vi.mock('../bridge/client', async () => {
  const actual = await vi.importActual<typeof import('../bridge/client')>('../bridge/client')
  return { ...actual, getTtsBridge: () => tts, getProviderBridge: () => providers }
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
    tts.synthesize.mockReset()
    tts.ensureRefEngine.mockReset()
    providers.list.mockReset()
    providers.list.mockResolvedValue({ items: [] })
    tts.ensureRefEngine.mockResolvedValue({ state: 'launching', host_script: '', endpoint: 'http://127.0.0.1:9880' })
    tts.installRefEngine.mockReset()
    tts.installRefEngine.mockResolvedValue({ state: 'ready', percent: 100, doneBytes: 0, totalBytes: 0 })
    tts.voices.mockImplementation(async (payload?: { engine?: string }) => {
      if (payload?.engine === 'volc') {
        return {
          voices: [{ voice_id: 'zh_female_xiaohe_uranus_bigtts', display_name: '小何', lang: 'zh-CN', gender: 'female', group: '通用女声' }],
        }
      }
      return {
        voices: [{ voice_id: 'zh-CN-XiaoxiaoNeural', display_name: '晓晓', lang: 'zh-CN', gender: 'female' }],
      }
    })
  })

  afterEach(() => cleanup())

  test('settings offer three voice paths', async () => {
    render(<CompanionSection />)
    expect(await screen.findByRole('radiogroup', { name: '语音通道' })).toBeTruthy()
    expect(screen.getAllByRole('radio')).toHaveLength(3)
    expect(screen.getByRole('radio', { name: /云端/ })).toHaveAttribute('aria-checked', 'true')
    expect(screen.getByText('晓晓 · 微软 Neural')).toBeInTheDocument()
    expect(screen.getByText('火山听 · 晓晓读（未配朗读）')).toBeInTheDocument()
    expect(screen.getByText('sherpa + GPT-SoVITS')).toBeInTheDocument()
    expect(document.querySelectorAll('.voice-path-card')).toHaveLength(3)
  })

  test('home wake controls are gone from settings', async () => {
    render(<CompanionSection />)
    expect(screen.queryByRole('switch', { name: '首页语音唤醒' })).not.toBeInTheDocument()
    expect(screen.queryByRole('switch', { name: '挡扬声器误唤醒' })).not.toBeInTheDocument()
  })

  test('instant pad is off by default and cloud explains button interrupt', async () => {
    const user = userEvent.setup()
    render(<CompanionSection />)
    const pad = await screen.findByRole('switch', { name: '先应一声' })
    expect(pad).toHaveAttribute('aria-checked', 'false')
    expect(screen.queryByRole('switch', { name: '语音插话' })).not.toBeInTheDocument()
    expect(screen.getByRole('note')).toHaveTextContent(/打断/)
    expect(screen.getByRole('note')).toHaveTextContent(/三种通道统一/)
    expect(screen.getByRole('note')).toHaveTextContent(/误打断/)
    await user.click(pad)
    expect(JSON.parse(localStorage.getItem(STORAGE_KEY) || '{}').instantAck).toBe(true)
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

  test('volc path without TTS keeps 晓晓 extras', async () => {
    const user = userEvent.setup()
    render(<CompanionSection />)
    await user.click(await screen.findByRole('radio', { name: /火山/ }))
    expect(screen.getByRole('radio', { name: /火山/ })).toHaveAttribute('aria-checked', 'true')
    expect(screen.getByText('朗读音色')).toBeInTheDocument()
    expect(screen.queryByRole('searchbox', { name: '查找火山音色' })).not.toBeInTheDocument()
    const stored = JSON.parse(localStorage.getItem(STORAGE_KEY) || '{}') as Record<string, unknown>
    expect(stored.engine).toBe('edge')
    expect(stored.voicePath).toBe('volc')
    expect(stored.voiceBargeIn).toBe(false)
  })

  test('volc path lists official seed-tts speakers after TTS is configured', async () => {
    providers.list.mockResolvedValue({
      items: [{
        id: '01ARZ3NDEKTSV4RRFFQ69G5FAV',
        name: 'Volc',
        protocol: 'volc_speech',
        baseUrl: 'https://openspeech.bytedance.com',
        status: 'enabled',
        credentialState: 'configured',
        createdAt: '2026-01-01T00:00:00Z',
        updatedAt: '2026-01-01T00:00:00Z',
        version: 1,
        models: [
          { modelId: 'seed-asr-2.0', displayName: 'seed-asr', isDefault: true, kind: 'asr' },
          { modelId: 'zh_female_xiaohe_uranus_bigtts', displayName: '小何', isDefault: false, kind: 'tts', kindDefault: true },
        ],
      }],
    })
    const user = userEvent.setup()
    render(<CompanionSection />)
    await waitFor(() => expect(screen.getByText('火山听 · 火山读')).toBeInTheDocument())
    expect(screen.getByText(/已配火山朗读/)).toBeInTheDocument()
    await user.click(await screen.findByRole('radio', { name: /火山/ }))
    expect(screen.queryByText(/已配火山朗读/)).not.toBeInTheDocument()
    expect(screen.getByRole('radio', { name: /火山/ })).toHaveAttribute('aria-checked', 'true')
    expect(screen.queryByText('显示外语音色')).not.toBeInTheDocument()
    expect(screen.queryByLabelText('GPT-SoVITS 服务地址')).not.toBeInTheDocument()
    expect(await screen.findByText('小何')).toBeInTheDocument()
    expect(screen.getByRole('searchbox', { name: '查找火山音色' })).toBeInTheDocument()
    const stored = JSON.parse(localStorage.getItem(STORAGE_KEY) || '{}') as Record<string, unknown>
    expect(stored.engine).toBe('volc')
    expect(stored.voiceId).toBe('zh_female_xiaohe_uranus_bigtts')
    expect(stored.voiceBargeIn).toBe(false)
    expect(await screen.findByRole('button', { name: '试听' })).toBeEnabled()
  })

  test('local preview stays disabled while the hosted engine is launching', async () => {
    tts.voices.mockResolvedValue({
      voices: [{ voice_id: 'refpack:温暖御姐.wav', display_name: '温暖御姐', lang: 'zh-CN', gender: 'female', group: '温柔御姐' }],
      ref_meta: {
        endpoint: 'http://127.0.0.1:9880',
        pack_dir: 'pack',
        server_online: false,
        pack_exists: true,
        missing_files: [],
        host_state: 'launching',
        host_script: 'E:\\GPT-SoVITS\\start-api-cpu.bat',
      },
    })
    const user = userEvent.setup()
    render(<CompanionSection />)
    await user.click(await screen.findByRole('radio', { name: /本地/ }))
    expect(await screen.findByText(/语音引擎启动中/)).toBeInTheDocument()
    expect(await screen.findByRole('button', { name: '启动中…' })).toBeDisabled()
    expect(tts.synthesize).not.toHaveBeenCalled()
  })

  test('local preview is clickable after the host dies', async () => {
    tts.voices.mockResolvedValue({
      voices: [{ voice_id: 'refpack:温暖御姐.wav', display_name: '温暖御姐', lang: 'zh-CN', gender: 'female', group: '温柔御姐' }],
      ref_meta: {
        endpoint: 'http://127.0.0.1:9880',
        pack_dir: 'pack',
        server_online: false,
        pack_exists: true,
        missing_files: [],
        host_state: 'offline',
        host_script: 'E:\\GPT-SoVITS\\start-api-cpu.bat',
        host_last_err: 'jieba_fast dict.txt missing',
      },
    })
    const user = userEvent.setup()
    render(<CompanionSection />)
    await user.click(await screen.findByRole('radio', { name: /本地/ }))
    expect(await screen.findByText(/jieba_fast/)).toBeInTheDocument()
    expect(await screen.findByRole('button', { name: '试听' })).toBeEnabled()
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
