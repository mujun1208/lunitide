import { cleanup, render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, expect, it, vi } from 'vitest'
import { BridgeClientError, type OCRRoutingBridge, type ProviderBridge } from '../bridge/client'
import type { ProviderDTO } from '../generated/bridge'
import { OCRRouting } from './OCRRouting'

afterEach(cleanup)

const now = new Date().toISOString()
const provider: ProviderDTO = {
  id: '01ARZ3NDEKTSV4RRFFQ69G5FAV', name: 'Demo', protocol: 'openai_compatible',
  baseUrl: 'https://example.com',
  models: [{ modelId: 'vision-1', displayName: 'Vision', isDefault: false, kind: 'vision' }],
  status: 'enabled', credentialState: 'configured', credentialBackupCount: 0,
  createdAt: now, updatedAt: now, version: 1,
}

it('does not show raw English load failures', async () => {
  const providers = { list: vi.fn().mockRejectedValue(new BridgeClientError('Failed to fetch', 'ENGINE_UNAVAILABLE', true, 'engine')) } as unknown as ProviderBridge
  const ocr = {
    get: vi.fn().mockResolvedValue({ preferProvider: true, revision: 'a'.repeat(64), appliedRevision: 'a'.repeat(64), state: 'applied' }),
    set: vi.fn(),
  } as unknown as OCRRoutingBridge
  render(<OCRRouting providers={providers} ocr={ocr} />)
  expect(await screen.findByRole('alert')).toHaveTextContent('OCR 路由载入失败')
  expect(screen.queryByText('Failed to fetch')).toBeNull()
})

it('keeps Chinese save conflicts and hides English save failures', async () => {
  const user = userEvent.setup()
  const providers = { list: vi.fn().mockResolvedValue({ items: [provider] }) } as unknown as ProviderBridge
  const ocr = {
    get: vi.fn().mockResolvedValue({ preferProvider: true, revision: 'a'.repeat(64), appliedRevision: 'a'.repeat(64), state: 'applied' }),
    set: vi.fn()
      .mockRejectedValueOnce(new BridgeClientError('OCR 路由已被修改，请载入最新版本', 'SETTINGS_VERSION_CONFLICT', false, 'engine'))
      .mockRejectedValueOnce(new BridgeClientError('Failed to fetch', 'ENGINE_UNAVAILABLE', true, 'engine')),
  } as unknown as OCRRoutingBridge
  render(<OCRRouting providers={providers} ocr={ocr} />)
  await user.click(await screen.findByRole('button', { name: '保存 OCR 路由' }))
  expect(await screen.findByRole('alert')).toHaveTextContent('OCR 路由已被修改，请载入最新版本')
  expect(screen.queryByText('SETTINGS_VERSION_CONFLICT')).toBeNull()
  await user.click(screen.getByRole('button', { name: '保存 OCR 路由' }))
  expect(await screen.findByRole('alert')).toHaveTextContent('OCR 路由保存失败')
  expect(screen.queryByText('Failed to fetch')).toBeNull()
})

it('saves OCR routing with provider-first preference', async () => {
  const user = userEvent.setup()
  const providers = { list: vi.fn().mockResolvedValue({ items: [provider] }) } as unknown as ProviderBridge
  const ocr = {
    get: vi.fn().mockResolvedValue({ preferProvider: true, revision: 'a'.repeat(64), appliedRevision: 'a'.repeat(64), state: 'applied' }),
    set: vi.fn().mockResolvedValue({ preferProvider: true, providerId: provider.id, modelId: 'vision-1', revision: 'b'.repeat(64), appliedRevision: 'b'.repeat(64), state: 'applied' }),
    install: vi.fn(),
  } as unknown as OCRRoutingBridge
  render(<OCRRouting providers={providers} ocr={ocr} />)
  expect(await screen.findByRole('heading', { name: 'OCR 路由' })).toBeInTheDocument()
  expect(screen.getAllByText(/Windows OCR/).length).toBeGreaterThan(0)
  await user.selectOptions(screen.getByLabelText('OCR 模型'), `${provider.id}\u0000vision-1`)
  await user.click(screen.getByRole('button', { name: '保存 OCR 路由' }))
  await waitFor(() => expect(ocr.set).toHaveBeenCalled())
  expect(await screen.findByRole('status')).toHaveTextContent('下一次识别生效')
})

it('shows last provider failure class and until without inventing zero counts', async () => {
  const providers = { list: vi.fn().mockResolvedValue({ items: [provider] }) } as unknown as ProviderBridge
  const ocr = {
    get: vi.fn().mockResolvedValue({
      preferProvider: true, revision: 'a'.repeat(64), appliedRevision: 'a'.repeat(64), state: 'applied',
      lastFailure: { class: 'auth', operation: 'image-ocr', until: '2026-09-09T12:01:00Z' },
      localReady: { pdf: true, image: true, backend: 'windows-ocr' },
    }),
    set: vi.fn(),
  } as unknown as OCRRoutingBridge
  render(<OCRRouting providers={providers} ocr={ocr} />)
  expect(await screen.findByText(/鉴权失败/)).toBeInTheDocument()
  expect(screen.queryByText(/\bauth\b/)).toBeNull()
  expect(screen.getByText(/2026-09-09T12:01:00Z/)).toBeInTheDocument()
  expect(screen.getByText(/图片识别/)).toBeInTheDocument()
  expect(screen.queryByText(/image-ocr/)).toBeNull()
  expect(screen.queryByText(/0 次失败/)).toBeNull()
  expect(screen.getByText(/windows-ocr/)).toBeInTheDocument()
  expect(screen.getAllByText(/Windows 兜底/).length).toBeGreaterThan(0)
})

it('lists provider OCR models first then PP-OCR then Windows fallback', async () => {
  const mixed: ProviderDTO = {
    ...provider,
    models: [
      { modelId: 'vision-1', displayName: 'Vision', isDefault: false, kind: 'vision' },
      { modelId: 'ocr-v2', displayName: '文档识别', isDefault: false, kind: 'vision' },
    ],
  }
  const providers = { list: vi.fn().mockResolvedValue({ items: [mixed] }) } as unknown as ProviderBridge
  const ocr = {
    get: vi.fn().mockResolvedValue({ preferProvider: true, revision: 'a'.repeat(64), appliedRevision: 'a'.repeat(64), state: 'applied', localEngine: 'auto' }),
    set: vi.fn(),
    install: vi.fn(),
  } as unknown as OCRRoutingBridge
  render(<OCRRouting providers={providers} ocr={ocr} />)
  const select = await screen.findByLabelText('OCR 模型')
  const groups = [...select.querySelectorAll('optgroup')].map(group => group.label)
  expect(groups).toEqual(['① 供应商 OCR', '② 本机 PP-OCR', '③ Windows 兜底'])
  const providerOptions = [...select.querySelectorAll('optgroup')[0].querySelectorAll('option')].map(option => option.textContent)
  expect(providerOptions[0]).toMatch(/文档识别/)
  expect(providerOptions[1]).toMatch(/Vision/)
})

it('lists a supplier OCR-named LLM even without the vision flag', async () => {
  const ocrOnly: ProviderDTO = {
    ...provider,
    models: [
      { modelId: 'paddleocr-vl', displayName: 'PaddleOCR 识别', isDefault: false, kind: 'llm' },
    ],
  }
  const providers = { list: vi.fn().mockResolvedValue({ items: [ocrOnly] }) } as unknown as ProviderBridge
  const ocr = {
    get: vi.fn().mockResolvedValue({ preferProvider: true, revision: 'a'.repeat(64), appliedRevision: 'a'.repeat(64), state: 'applied', localEngine: 'auto' }),
    set: vi.fn(),
    install: vi.fn(),
  } as unknown as OCRRoutingBridge
  render(<OCRRouting providers={providers} ocr={ocr} />)
  const select = await screen.findByLabelText('OCR 模型')
  expect([...select.querySelectorAll('optgroup')[0].querySelectorAll('option')].map(option => option.textContent).join()).toMatch(/PaddleOCR 识别/)
})

it('downloads PP-OCR on click then lets auto load it', async () => {
  const user = userEvent.setup()
  const providers = { list: vi.fn().mockResolvedValue({ items: [provider] }) } as unknown as ProviderBridge
  const install = vi.fn()
    .mockResolvedValueOnce({ state: 'downloading', percent: 40, doneBytes: 10, totalBytes: 28 })
    .mockResolvedValue({ state: 'ready', percent: 100, doneBytes: 28, totalBytes: 28 })
  const ocr = {
    get: vi.fn()
      .mockResolvedValueOnce({
        preferProvider: true, revision: 'a'.repeat(64), appliedRevision: 'a'.repeat(64), state: 'applied',
        localEngine: 'auto',
        localReady: { pdf: true, image: true, backend: 'windows-ocr' },
        pack: { available: false, status: 'missing_dependency', backend: 'ppocr-pack' },
        downloadBytes: 29234358,
      })
      .mockResolvedValue({
        preferProvider: true, revision: 'a'.repeat(64), appliedRevision: 'a'.repeat(64), state: 'applied',
        localEngine: 'auto',
        localReady: { pdf: true, image: true, backend: 'ppocr' },
        pack: { available: true, status: 'ready', backend: 'ppocr-pack' },
        downloadBytes: 29234358,
      }),
    set: vi.fn().mockResolvedValue({
      preferProvider: true, revision: 'b'.repeat(64), appliedRevision: 'b'.repeat(64), state: 'applied',
      localEngine: 'auto',
      localReady: { pdf: true, image: true, backend: 'ppocr' },
      pack: { available: true, status: 'ready', backend: 'ppocr-pack' },
    }),
    install,
  } as unknown as OCRRoutingBridge
  render(<OCRRouting providers={providers} ocr={ocr} />)
  const engine = await screen.findByLabelText('本机 OCR 引擎')
  expect(engine).toHaveDisplayValue('自动（推荐）')
  expect(screen.getByText(/改引擎、装完或点保存后/)).toBeInTheDocument()
  expect(screen.getByRole('option', { name: 'Windows OCR（内置）' })).toBeEnabled()
  expect(screen.getByRole('option', { name: 'PP-OCR（未安装）' })).toBeDisabled()
  expect(screen.queryByRole('button', { name: '安装 PP-OCR' })).toBeNull()
  await user.click(screen.getByRole('button', { name: '下载安装' }))
  await waitFor(() => expect(install).toHaveBeenCalled())
  expect(await screen.findByRole('button', { name: '已安装' }, { timeout: 3000 })).toBeDisabled()
  expect(await screen.findByRole('option', { name: 'PP-OCR' })).toBeEnabled()
  await waitFor(() => expect(ocr.set).toHaveBeenCalledWith(expect.objectContaining({
    localEngine: 'auto',
  }), expect.anything()))
  expect(await screen.findByRole('status')).toHaveTextContent('下一次识别生效')
})

it('saves the local engine as soon as it is changed', async () => {
  const user = userEvent.setup()
  const providers = { list: vi.fn().mockResolvedValue({ items: [provider] }) } as unknown as ProviderBridge
  const ocr = {
    get: vi.fn().mockResolvedValue({
      preferProvider: true, revision: 'a'.repeat(64), appliedRevision: 'a'.repeat(64), state: 'applied',
      localEngine: 'auto',
      pack: { available: true, status: 'ready', backend: 'ppocr-pack' },
    }),
    set: vi.fn().mockResolvedValue({
      preferProvider: true, revision: 'b'.repeat(64), appliedRevision: 'b'.repeat(64), state: 'applied',
      localEngine: 'ppocr',
      pack: { available: true, status: 'ready', backend: 'ppocr-pack' },
    }),
    install: vi.fn(),
  } as unknown as OCRRoutingBridge
  render(<OCRRouting providers={providers} ocr={ocr} />)
  await screen.findByLabelText('本机 OCR 引擎')
  await user.selectOptions(screen.getByLabelText('本机 OCR 引擎'), 'ppocr')
  await waitFor(() => expect(ocr.set).toHaveBeenCalledWith(expect.objectContaining({
    localEngine: 'ppocr',
  }), expect.anything()))
})

it('falls back unknown lastFailure class to Chinese', async () => {
  const providers = { list: vi.fn().mockResolvedValue({ items: [provider] }) } as unknown as ProviderBridge
  const ocr = {
    get: vi.fn().mockResolvedValue({
      preferProvider: true, revision: 'a'.repeat(64), appliedRevision: 'a'.repeat(64), state: 'applied',
      lastFailure: { class: 'timeout', operation: 'embed-ocr', until: '2026-09-09T12:01:00Z' },
    }),
    set: vi.fn(),
  } as unknown as OCRRoutingBridge
  render(<OCRRouting providers={providers} ocr={ocr} />)
  expect(await screen.findByText(/识别异常/)).toBeInTheDocument()
  expect(screen.queryByText(/\btimeout\b/)).toBeNull()
  expect(screen.queryByText(/embed-ocr/)).toBeNull()
})
