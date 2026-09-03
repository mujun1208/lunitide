import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterEach, expect, it, vi } from 'vitest'
import { LanguageProvider } from '../i18n/language'
import { MroCiteList } from './MroCiteList'

vi.mock('../bridge/client', async importOriginal => {
  const actual = await importOriginal<typeof import('../bridge/client')>()
  return { ...actual, mroBridge: { ...actual.mroBridge, checklistBuild: vi.fn() } }
})

afterEach(cleanup)

it('shows revision and expert name on a cite chip', () => {
  render(
    <LanguageProvider value="zh-CN">
      <MroCiteList cites={[{docType: 'AMM', revision: '42', locator: '{}', quote: 'Gear retraction', expertName: '航空机务专家'}]} />
    </LanguageProvider>,
  )
  expect(screen.getByText(/修订 42/)).toBeInTheDocument()
  expect(screen.getByText('航空机务专家')).toBeInTheDocument()
})

it('reports discarded effectivity chunks and restored appendix', () => {
  render(
    <LanguageProvider value="zh-CN">
      <MroCiteList cites={[]} discarded={3} restored gate="ungrounded" />
    </LanguageProvider>,
  )
  expect(screen.getByText('3 块因机尾不适用已丢弃')).toBeInTheDocument()
  expect(screen.getByText('已补回机务引用')).toBeInTheDocument()
  expect(screen.queryByRole('button', { name: '下载检查单 JSON' })).not.toBeInTheDocument()
})

it('downloads a cited checklist built from the answer citations', async () => {
  const { mroBridge } = await import('../bridge/client')
  const build = vi.mocked(mroBridge.checklistBuild)
  build.mockResolvedValue({ banner: '辅助建议，不构成放行', steps: [{ n: 1, text: 'Gear retraction', revision: '42', ata: '32' }] })
  const createObjectURL = vi.fn().mockReturnValue('blob:checklist')
  vi.stubGlobal('URL', { createObjectURL, revokeObjectURL: vi.fn() })
  vi.spyOn(HTMLAnchorElement.prototype, 'click').mockImplementation(() => undefined)
  render(
    <LanguageProvider value="zh-CN">
      <MroCiteList cites={[{ docType: 'AMM', revision: '42', locator: 'mro://AMM/42?ata=32', quote: 'Gear retraction', expertName: '航空机务专家' }]} />
    </LanguageProvider>,
  )
  fireEvent.click(screen.getByRole('button', { name: '下载检查单 JSON' }))
  await waitFor(() => expect(build).toHaveBeenCalledWith({
    steps: ['Gear retraction'],
    cites: [{ revision: '42', locator: 'mro://AMM/42?ata=32', quote: 'Gear retraction', expertName: '航空机务专家', ata: '32' }],
  }))
  expect(createObjectURL).toHaveBeenCalled()
  vi.unstubAllGlobals()
  vi.restoreAllMocks()
})
