import { cleanup, fireEvent, render, screen } from '@testing-library/react'
import { afterEach, expect, it } from 'vitest'
import { OfficeStudioPreview } from './OfficeStudioPreview'

afterEach(cleanup)

it('switches between Office tasks and work modes', () => {
  render(<OfficeStudioPreview />)
  expect(screen.getByRole('heading', { name: '董事会年度经营汇报' })).toBeInTheDocument()
  fireEvent.click(screen.getByRole('button', { name: /供应商评估报告/ }))
  expect(screen.getByRole('heading', { name: '供应商评估报告' })).toBeInTheDocument()
  fireEvent.click(screen.getByRole('button', { name: '检查' }))
  expect(screen.getByText('整体质量良好')).toBeInTheDocument()
})

it('shows version history and export feedback', () => {
  render(<OfficeStudioPreview />)
  fireEvent.click(screen.getByRole('button', { name: '版本' }))
  expect(screen.getByText(/v3 · 当前版本/)).toBeInTheDocument()
  fireEvent.click(screen.getByRole('button', { name: /导出/ }))
  expect(screen.getByRole('status')).toHaveTextContent('年度经营汇报_v3.pptx 已准备导出')
})
