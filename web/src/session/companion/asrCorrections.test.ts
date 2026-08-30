import { afterEach, describe, expect, test } from 'vitest'
import {
  applyAsrCorrections,
  correctAsrText,
  formatAsrCorrectionTable,
  loadUserAsrCorrectionText,
  parseAsrCorrectionTable,
  saveUserAsrCorrectionText,
} from './asrCorrections'

const STORAGE_KEY = 'lunitide:asr-corrections'

afterEach(() => {
  localStorage.removeItem(STORAGE_KEY)
})

describe('parseAsrCorrectionTable', () => {
  test('reads 误识别 : 正确 lines and skips comments', () => {
    expect(
      parseAsrCorrectionTable(`
# ignore
open cloud : OpenClaw
GPT so vits:GPT-SoVITS

桌面儿 : 桌面
a
`),
    ).toEqual([
      { from: 'open cloud', to: 'OpenClaw' },
      { from: 'GPT so vits', to: 'GPT-SoVITS' },
      { from: '桌面儿', to: '桌面' },
    ])
  })

  test('drops empty, too-short, and identity rows', () => {
    expect(parseAsrCorrectionTable('x : y\nok : ok\n : missing')).toEqual([])
  })
})

describe('applyAsrCorrections', () => {
  test('repairs spaced English OOV without touching surrounding CJK', () => {
    expect(applyAsrCorrections('用 gpt so vits 克隆音色')).toBe('用 GPT-SoVITS 克隆音色')
    expect(applyAsrCorrections('打开 Web View 2 窗口')).toBe('打开 WebView2 窗口')
    expect(applyAsrCorrections('luna tide 工作台')).toBe('Lunitide 工作台')
  })

  test('applies user extras after builtins, longer key first', () => {
    expect(
      applyAsrCorrections('open cloud 网关', [{ from: 'open cloud', to: 'OpenClaw' }]),
    ).toBe('OpenClaw 网关')
  })

  test('leaves already-correct product names alone', () => {
    expect(applyAsrCorrections('打开桌面上的协议文档')).toBe('打开桌面上的协议文档')
    expect(applyAsrCorrections('GPT-SoVITS 已就绪')).toBe('GPT-SoVITS 已就绪')
  })
})

describe('user correction storage', () => {
  test('round-trips the table the settings textarea edits', () => {
    saveUserAsrCorrectionText(formatAsrCorrectionTable([{ from: '贾维尔', to: '贾维斯' }]))
    expect(loadUserAsrCorrectionText()).toMatch(/贾维尔/)
    expect(correctAsrText('叫一声贾维尔')).toBe('叫一声贾维斯')
  })
})
