import { cleanup, render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, expect, it, vi } from 'vitest'
import { PersonalIntelligencePage } from './PersonalIntelligencePage'

afterEach(cleanup)
vi.mock('../memory/MemoryPage', () => ({ MemoryPage: () => <div data-testid="memory-page-stub" /> }))
vi.mock('./PrivacyConsole', () => ({ PrivacyConsole: () => <div data-testid="privacy-console-stub" /> }))
vi.mock('../settings/OCRSettingsPanel', () => ({ OCRSettingsPanel: () => <div data-testid="ocr-settings-stub" /> }))
vi.mock('../settings/SmartCapabilitiesPanel', () => ({
  SmartCapabilitiesPanel: ({ onOpenMemory, onOpenOCR }: { onOpenMemory?: () => void; onOpenOCR?: () => void }) => (
    <div className="smart-cap">
      <section className="smart-cap-card" aria-labelledby="smart-mem-title">
        <h3 id="smart-mem-title">自动记忆</h3>
        <button type="button" onClick={onOpenMemory}>管理已保存记忆与隐私</button>
      </section>
      <section className="smart-cap-card" aria-labelledby="smart-ocr-title">
        <h3 id="smart-ocr-title">文字识别</h3>
        <button type="button" onClick={onOpenOCR}>打开文字识别</button>
      </section>
    </div>
  ),
}))

it('renders exactly two overview cards and one OCR detail', async () => {
  const user = userEvent.setup()
  const onViewChange = vi.fn()
  render(<PersonalIntelligencePage onViewChange={onViewChange} />)
  expect(screen.getByRole('heading', { name: '智能能力' })).toBeInTheDocument()
  expect(screen.getByRole('heading', { name: '自动记忆' })).toBeInTheDocument()
  expect(screen.getByRole('heading', { name: '文字识别' })).toBeInTheDocument()
  expect(document.querySelectorAll('.smart-cap-card')).toHaveLength(2)
  expect(screen.queryByTestId('ocr-settings-stub')).toBeNull()
  await user.click(screen.getByRole('button', { name: '打开文字识别' }))
  expect(onViewChange).toHaveBeenCalledWith('ocr')
  expect(screen.getByTestId('ocr-settings-stub')).toBeInTheDocument()
  expect(screen.getAllByTestId('ocr-settings-stub')).toHaveLength(1)
  expect(screen.queryByRole('heading', { name: 'OCR 路由' })).toBeNull()
  expect(screen.getByRole('button', { name: '返回智能能力' })).toBeInTheDocument()
  await user.click(screen.getByRole('button', { name: '返回智能能力' }))
  expect(screen.getByRole('heading', { name: '自动记忆' })).toBeInTheDocument()
  expect(document.querySelectorAll('.smart-cap-card')).toHaveLength(2)
})
