import { cleanup, fireEvent, render, screen } from '@testing-library/react'
import { afterEach, expect, it } from 'vitest'
import { LanguageProvider } from '../i18n/language'
import { ExpertDetailTabs } from './ExpertDetailTabs'

afterEach(cleanup)

it('switches overview knowledge and growth tabs', () => {
  render(
    <LanguageProvider value="zh-CN">
      <ExpertDetailTabs overview={<p>概览正文</p>} knowledge={<p>知识正文</p>} growth={<p>路径正文</p>} />
    </LanguageProvider>,
  )
  expect(screen.getByText('概览正文')).toBeInTheDocument()
  fireEvent.click(screen.getByRole('tab', { name: '知识' }))
  expect(screen.getByText('知识正文')).toBeInTheDocument()
  fireEvent.click(screen.getByRole('tab', { name: '路径' }))
  expect(screen.getByText('路径正文')).toBeInTheDocument()
})
