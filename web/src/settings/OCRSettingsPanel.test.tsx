import { cleanup, render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, expect, it, vi } from 'vitest'
import type { OCRPackBridge, OCRRoutingBridge, OCRRunBridge, ProviderBridge } from '../bridge/client'
import type { OcrPackGetResult, OcrRoutingGetResult } from '../generated/bridge'
import { OCRSettingsPanel } from './OCRSettingsPanel'

afterEach(cleanup)

const revision = 'a'.repeat(64)

function routing(state: OcrRoutingGetResult['windowsProbe']['state'] = 'ready'): OcrRoutingGetResult {
  return {
    requestedScope: { scopeKind: 'user', scopeId: null },
    policySource: { scopeKind: 'user', scopeId: null, inherited: false },
    policy: {
      mode: 'auto',
      complexDocumentEngine: 'paddleocr-vl-1.6',
      fallbackOrder: ['ppocr', 'windows-ocr'],
      sendToCloud: 'never',
    },
    revision,
    windowsProbe: {
      state,
      available: state === 'ready',
      languages: ['zh-Hans-CN', 'en-US'],
      checkedAt: '2026-09-18T12:00:00.000Z',
    },
    legacy: { engineId: 'ppocr', registered: true, state: 'registered_unwired', available: false, markerDetected: false },
  }
}

function pack(): OcrPackGetResult {
  return {
    pack: {
      packId: 'paddleocr-vl-1.6',
      availability: 'not_installed',
      currentVersion: null,
      previousVersion: null,
      manifestDigest: null,
      engineVersion: null,
      deviceKind: null,
      lastHealthAt: null,
      lastErrorCode: null,
      revision: 1,
    },
    gate: {
      installAllowed: false,
      autoRouteAllowed: false,
      reasonCode: 'NO_VERIFIED_RUNTIME_PROFILE',
      verifiedRuntimeProfileDigest: null,
      revision: 1,
    },
    operation: null,
    release: null,
  }
}

function renderPanel(overrides: {
  get?: OCRRoutingBridge['get']
  list?: ProviderBridge['list']
  install?: OCRRoutingBridge['install']
} = {}) {
  const get = overrides.get ?? vi.fn().mockResolvedValue(routing())
  const ocr = { get, set: vi.fn(), install: overrides.install ?? vi.fn().mockResolvedValue({ state: 'idle', percent: 0, doneBytes: 0, totalBytes: 1 }) } as unknown as OCRRoutingBridge
  const packApi = {
    get: vi.fn().mockResolvedValue(pack()),
    install: vi.fn(),
    cancel: vi.fn(),
    uninstall: vi.fn(),
    noticeList: vi.fn().mockResolvedValue({ items: [], nextCursor: null }),
    noticeRead: vi.fn(),
  } as unknown as OCRPackBridge
  const list = overrides.list ?? vi.fn().mockResolvedValue({ items: [] })
  const providers = { list } as unknown as ProviderBridge
  const runs = {
    list: vi.fn().mockResolvedValue({ items: [], nextCursor: null }),
    get: vi.fn(),
    readArtifact: vi.fn(),
  } as unknown as OCRRunBridge
  render(<OCRSettingsPanel ocr={ocr} packApi={packApi} providers={providers} runs={runs} />)
  return { get, list, packApi, ocr }
}

it('TestWindowsOCRTruthfulStates: renders automatic as read only and omits demo actions', async () => {
  const { packApi } = renderPanel()
  expect(await screen.findByRole('status')).toHaveTextContent('文字识别：自动')
  expect(await screen.findByText('Windows OCR 可用')).toBeInTheDocument()
  expect(screen.queryByRole('switch')).toBeNull()
  expect(document.querySelector('input[type=checkbox]')).toBeNull()
  expect(document.querySelector('input[type=file]')).toBeNull()
  expect(screen.queryByRole('button', { name: '保存 OCR 路由' })).toBeNull()
  expect(screen.queryByText(/截图识别|选择文件/)).toBeNull()
  expect(screen.queryByRole('heading', { name: 'PaddleOCR-VL-1.6' })).toBeNull()
  expect(screen.queryByRole('button', { name: '安装（不可用）' })).toBeNull()
  expect(packApi.get).not.toHaveBeenCalled()
})

it('TestWindowsOCRTruthfulStates: maps every probe state to a real reason and retry', async () => {
  const cases: Array<[OcrRoutingGetResult['windowsProbe']['state'], string, boolean]> = [
    ['ready', 'Windows OCR 可用', false],
    ['unsupported_os', '当前系统不支持 Windows OCR', true],
    ['initialization_failed', 'Windows OCR 初始化失败', true],
    ['language_unavailable', '需要安装 OCR 语言包', true],
    ['sample_failed', 'Windows OCR 自检失败', true],
    ['timed_out', 'Windows OCR 检查超时', true],
  ]
  for (const [state, label, retry] of cases) {
    cleanup()
    renderPanel({ get: vi.fn().mockResolvedValue(routing(state)) })
    expect(await screen.findByText(label)).toBeInTheDocument()
    expect(screen.queryByText('Windows OCR 就绪')).toBeNull()
    if (retry) {
      expect(screen.getByRole('button', { name: '重新检查' })).toBeInTheDocument()
      if (state === 'unsupported_os') {
        expect(screen.queryByRole('button', { name: '一键修复' })).toBeNull()
      } else {
        expect(screen.getByRole('button', { name: '一键修复' })).toBeInTheDocument()
      }
    } else {
      expect(screen.queryByRole('button', { name: '重新检查' })).toBeNull()
      expect(screen.queryByRole('button', { name: '一键修复' })).toBeNull()
    }
  }
})

it('repairs Windows OCR through routing get', async () => {
  const user = userEvent.setup()
  const get = vi.fn()
    .mockResolvedValueOnce(routing('sample_failed'))
    .mockResolvedValueOnce(routing('ready'))
  renderPanel({ get })
  expect(await screen.findByText('Windows OCR 自检失败')).toBeInTheDocument()
  await user.click(screen.getByRole('button', { name: '一键修复' }))
  await waitFor(() => expect(get).toHaveBeenCalledWith({ scopeKind: 'user', refreshProbe: true, repairWindows: true }))
  expect(await screen.findByText('Windows OCR 可用')).toBeInTheDocument()
})

it('provider failure does not block core OCR snapshot', async () => {
  const user = userEvent.setup()
  const { list } = renderPanel({
    list: vi.fn().mockRejectedValue(new Error('Failed to fetch')),
  })
  expect(await screen.findByText('Windows OCR 可用')).toBeInTheDocument()
  expect(screen.queryByRole('heading', { name: 'PaddleOCR-VL-1.6' })).toBeNull()
  expect(screen.queryByText('尚无经验证的 Windows 运行包')).toBeNull()
  expect(list).not.toHaveBeenCalled()
  await user.click(screen.getByRole('button', { name: '高级' }))
  expect(screen.getByText('回退顺序：视觉模型（能力路由） → RapidOCR → Windows OCR')).toBeInTheDocument()
  expect(screen.queryByText('从不')).toBeNull()
  expect(await screen.findByRole('alert')).toHaveTextContent('云端识别目录载入失败')
  expect(screen.queryByText('Failed to fetch')).toBeNull()
  expect(screen.getByText('Windows OCR 可用')).toBeInTheDocument()
  expect(screen.getByText('已登记，尚未接入')).toBeInTheDocument()
})

it('loads providers only after advanced opens', async () => {
  const user = userEvent.setup()
  const { list, get } = renderPanel()
  await screen.findByRole('status')
  await waitFor(() => expect(get).toHaveBeenCalledWith({ scopeKind: 'user' }))
  expect(list).not.toHaveBeenCalled()
  await user.click(screen.getByRole('button', { name: '高级' }))
  await waitFor(() => expect(list).toHaveBeenCalledTimes(1))
  await user.click(screen.getByRole('button', { name: '高级' }))
  await user.click(screen.getByRole('button', { name: '高级' }))
  expect(list).toHaveBeenCalledTimes(1)
})

it('TestOCRProgressiveDisclosure: core snapshot stays visible before advanced opens', async () => {
  renderPanel()
  expect(await screen.findByText('Windows OCR 可用')).toBeInTheDocument()
  expect(screen.getByRole('button', { name: '高级' })).toHaveAttribute('aria-expanded', 'false')
  expect(screen.queryByText('云端识别目录')).toBeNull()
})

it('installs RapidOCR through ocr.install', async () => {
  const user = userEvent.setup()
  const { ocr } = renderPanel()
  expect(await screen.findByRole('button', { name: '安装 RapidOCR' })).toBeEnabled()
  await waitFor(() => expect(ocr.install).toHaveBeenCalledWith({ probe: true }))
  await user.click(screen.getByRole('button', { name: '安装 RapidOCR' }))
  await waitFor(() => expect(ocr.install).toHaveBeenCalledWith({}))
})

it('shows RapidOCR as installed after a probe without starting download', async () => {
  const install = vi.fn().mockImplementation(async (payload?: { probe?: boolean }) => {
    if (payload?.probe) {
      return { state: 'ready', percent: 100, doneBytes: 1, totalBytes: 1 }
    }
    throw new Error('must not download when already ready')
  })
  renderPanel({ get: vi.fn().mockResolvedValue(routing()), install })
  expect(await screen.findByRole('button', { name: '已安装 RapidOCR' })).toBeDisabled()
  expect(install).toHaveBeenCalledWith({ probe: true })
  expect(install).not.toHaveBeenCalledWith({})
})
