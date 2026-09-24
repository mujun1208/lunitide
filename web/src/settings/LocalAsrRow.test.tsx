import { cleanup, render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, beforeEach, describe, expect, test, vi } from 'vitest'

import { defaultCompanionSettings } from '../session/companion/companionSettings'

const asr = vi.hoisted(() => ({
  localAsrStatus: vi.fn(),
  installLocalAsr: vi.fn(),
  selectLocalAsrModel: vi.fn(),
  selectLocalAsrRefiner: vi.fn(),
}))

vi.mock('../session/companion/localAsr', () => asr)

import { LocalAsrRow } from './LocalAsrRow'

const mb = (n: number) => n * 1024 * 1024

const ready = {
  supported: true,
  ready: true,
  modelId: 'streaming-zipformer-zh-14m',
  modelTitle: '中文精确识别模型',
  refinerId: 'offline-paraformer-zh',
  downloadBytes: mb(300),
  backend: 'sherpa-onnx',
  models: [
    { id: 'streaming-zipformer-zh-14m', title: '中文轻量识别模型', sizeBytes: mb(24), installed: true },
    { id: 'streaming-paraformer-zh-en', title: '中英流式', sizeBytes: mb(226), installed: false },
  ],
  refiners: [
    { id: 'offline-paraformer-zh', title: '中文精确识别模型', sizeBytes: mb(232), installed: true },
    { id: 'sense-voice-zh-en-ja-ko-yue', title: 'SenseVoice 多语听写', sizeBytes: mb(228), installed: false },
  ],
}

describe('LocalAsrRow refiner slot', () => {
  beforeEach(() => {
    asr.localAsrStatus.mockReset()
    asr.installLocalAsr.mockReset()
    asr.selectLocalAsrModel.mockReset()
    asr.selectLocalAsrRefiner.mockReset()
    asr.localAsrStatus.mockResolvedValue(ready)
    asr.selectLocalAsrRefiner.mockResolvedValue({ modelId: ready.modelId, ready: false })
    asr.installLocalAsr.mockResolvedValue({ state: 'downloading', percent: 0, doneBytes: 0, totalBytes: mb(228) })
  })

  afterEach(() => cleanup())

  test('installs the chosen refiner without touching the caption model', async () => {
    const user = userEvent.setup()
    render(<LocalAsrRow companion={defaultCompanionSettings()} save={() => {}} />)
    const refiner = await screen.findByRole('combobox', { name: '听写模型' })
    expect(refiner).toHaveValue('offline-paraformer-zh')
    expect(screen.getByRole('combobox', { name: '字幕模型' })).toHaveValue('streaming-zipformer-zh-14m')

    asr.localAsrStatus.mockResolvedValue({
      ...ready,
      ready: false,
      modelTitle: 'SenseVoice 多语听写',
      refinerId: 'sense-voice-zh-en-ja-ko-yue',
    })
    await user.selectOptions(refiner, 'sense-voice-zh-en-ja-ko-yue')

    await waitFor(() => {
      expect(asr.selectLocalAsrRefiner).toHaveBeenCalledWith('sense-voice-zh-en-ja-ko-yue')
      expect(asr.installLocalAsr).toHaveBeenCalledWith('sense-voice-zh-en-ja-ko-yue')
    })
    expect(asr.selectLocalAsrModel).not.toHaveBeenCalled()
    expect(screen.getByRole('combobox', { name: '字幕模型' })).toHaveValue('streaming-zipformer-zh-14m')
  })
})
