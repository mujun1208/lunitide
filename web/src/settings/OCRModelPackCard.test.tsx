import { cleanup, render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, expect, it, vi } from 'vitest'
import type { OCRPackBridge } from '../bridge/client'
import type { OcrPackGetResult } from '../generated/bridge'
import { OCRModelPackCard } from './OCRModelPackCard'

afterEach(cleanup)

function pack(overrides: Partial<OcrPackGetResult['gate']> = {}): OcrPackGetResult {
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
      ...overrides,
    },
    operation: null,
    release: null,
  }
}

it('no verified runtime disables install with zero mutations', async () => {
  const user = userEvent.setup()
  const install = vi.fn()
  const api = {
    get: vi.fn().mockResolvedValue(pack()),
    install,
    cancel: vi.fn(),
    uninstall: vi.fn(),
    noticeList: vi.fn(),
    noticeRead: vi.fn(),
  } as unknown as OCRPackBridge
  render(<OCRModelPackCard pack={pack()} api={api} />)
  const button = screen.getByRole('button', { name: '安装（不可用）' })
  expect(button).toBeDisabled()
  expect(screen.getByText('尚无经验证的 Windows 运行包')).toBeInTheDocument()
  await user.click(button)
  await user.click(screen.getByRole('heading', { name: 'PaddleOCR-VL-1.6' }))
  expect(install).not.toHaveBeenCalled()
  expect(api.cancel).not.toHaveBeenCalled()
  expect(api.uninstall).not.toHaveBeenCalled()
})

it('TestOCRManifestConsent: license remains readable while install stays gated', async () => {
  const user = userEvent.setup()
  const noticeList = vi.fn().mockResolvedValue({
    items: [{
      packId: 'paddleocr-vl-1.6',
      manifestDigest: 'b'.repeat(64),
      version: '1.6.0',
      noticeDigest: 'c'.repeat(64),
      noticeBytes: 21,
      verifiedAt: '2026-09-01T00:00:00Z',
      installedAt: '2026-09-01T00:00:00Z',
      uninstalledAt: '2026-09-18T00:00:00Z',
    }],
    nextCursor: null,
  })
  const noticeRead = vi.fn().mockResolvedValue({
    base64: btoa('NOTICE retained offline'),
    nextOffset: 21,
    eof: true,
    sha256: 'd'.repeat(64),
    totalBytes: 21,
  })
  const install = vi.fn()
  const api = {
    get: vi.fn().mockResolvedValue(pack()),
    install,
    cancel: vi.fn(),
    uninstall: vi.fn(),
    noticeList,
    noticeRead,
  } as unknown as OCRPackBridge
  render(<OCRModelPackCard pack={pack()} api={api} />)
  expect(screen.getByRole('button', { name: '安装（不可用）' })).toBeDisabled()
  await user.click(screen.getByRole('button', { name: '许可证与声明' }))
  await waitFor(() => expect(noticeList).toHaveBeenCalled())
  await user.click(screen.getByRole('button', { name: /1\.6\.0/ }))
  expect(await screen.findByText('NOTICE retained offline')).toBeInTheDocument()
  expect(install).not.toHaveBeenCalled()
})

it('TestOCRLicenseLink', async () => {
  const user = userEvent.setup()
  const noticeList = vi.fn().mockResolvedValue({
    items: [{
      packId: 'paddleocr-vl-1.6',
      manifestDigest: 'b'.repeat(64),
      version: '1.6.0',
      noticeDigest: 'c'.repeat(64),
      noticeBytes: 21,
      verifiedAt: '2026-09-01T00:00:00Z',
      installedAt: '2026-09-01T00:00:00Z',
      uninstalledAt: '2026-09-18T00:00:00Z',
    }],
    nextCursor: null,
  })
  const noticeRead = vi.fn().mockResolvedValue({
    base64: btoa('NOTICE retained offline'),
    nextOffset: 21,
    eof: true,
    sha256: 'd'.repeat(64),
    totalBytes: 21,
  })
  const api = {
    get: vi.fn().mockResolvedValue(pack()),
    install: vi.fn(),
    cancel: vi.fn(),
    uninstall: vi.fn(),
    noticeList,
    noticeRead,
  } as unknown as OCRPackBridge
  render(<OCRModelPackCard pack={pack()} api={api} />)
  await user.click(screen.getByRole('button', { name: '许可证与声明' }))
  await waitFor(() => expect(noticeList).toHaveBeenCalledWith({ packId: 'paddleocr-vl-1.6' }))
  expect(screen.getByText(/已卸载，仍可离线查看/)).toBeInTheDocument()
  await user.click(screen.getByRole('button', { name: /1\.6\.0/ }))
  await waitFor(() => expect(noticeRead).toHaveBeenCalledWith({
    packId: 'paddleocr-vl-1.6',
    manifestDigest: 'b'.repeat(64),
    offset: 0,
    limit: 65_536,
  }))
  expect(await screen.findByText('NOTICE retained offline')).toBeInTheDocument()
})
