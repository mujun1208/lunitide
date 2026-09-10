import { cleanup, fireEvent, render, screen } from '@testing-library/react'
import { afterEach, expect, it, vi } from 'vitest'
import { LanguageProvider } from '../i18n/language'
import { QuickForm } from './MroForms'

afterEach(cleanup)

it('does not show raw English save failures', async () => {
  render(
    <LanguageProvider value="zh-CN">
      <QuickForm spec={{ title: '登记', fields: [], submit: () => Promise.reject(new Error('Failed to fetch')) }} onClose={vi.fn()} />
    </LanguageProvider>,
  )
  fireEvent.click(screen.getByRole('button', { name: '保存' }))
  expect(await screen.findByRole('alert')).toHaveTextContent('保存失败')
  expect(screen.queryByText('Failed to fetch')).toBeNull()
})
