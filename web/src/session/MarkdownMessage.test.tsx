import { cleanup, fireEvent, render, screen } from '@testing-library/react'
import { afterEach, expect, it, vi } from 'vitest'
import { MarkdownMessage, ThinkingPanel, compressThinking, formatTaskElapsed, safeMarkdownUrl } from './MarkdownMessage'

afterEach(cleanup)

it('renders GFM structures and secure HTTPS links', () => {
  render(<MarkdownMessage text={'# 标题\n\n**粗体**\n\n- 列表\n\n> 引用\n\n| A | B |\n|---|---|\n| 1 | 2 |\n\n```ts\nconst x=1\n```\n\n[安全](https://example.com)'} />)
  expect(screen.getByRole('heading', { name: '标题' })).toBeInTheDocument()
  expect(screen.getByText('粗体').tagName).toBe('STRONG')
  expect(screen.getByRole('table')).toBeInTheDocument()
  expect(screen.getByRole('link', { name: '安全' })).toHaveAttribute('rel', 'noopener noreferrer')
})

it('keeps raw HTML inert and blocks unsafe URLs and images', () => {
  const { container } = render(<MarkdownMessage text={'<img src="https://remote.test/x.png" onerror="alert(1)">\n\n<script>alert(2)</script>\n\n[bad](javascript:alert(3)) ![remote](https://remote.test/x.png)'} />)
  expect(container.querySelector('img,script')).toBeNull()
  expect(screen.queryByRole('link', { name: 'bad' })).toBeNull()
  expect(container).toHaveTextContent('alert(2)')
  expect(safeMarkdownUrl('http://example.com')).toBe('')
})

it('does not rewrite an explicit Markdown link whose label looks like a punctuated URL', () => {
  render(<MarkdownMessage text={'[https://label.test/path，说明](https://target.test/kept)'} />)
  const link = screen.getByRole('link', { name: 'https://label.test/path，说明' })
  expect(link).toHaveAttribute('href', 'https://target.test/kept')
})

it('moves trailing CJK punctuation outside a bare HTTPS autolink', () => {
  const { container } = render(<MarkdownMessage text={'访问 https://example.com/path，继续。'} />)
  const link = screen.getByRole('link', { name: 'https://example.com/path' })
  expect(link).toHaveAttribute('href', 'https://example.com/path')
  expect(container).toHaveTextContent('https://example.com/path，继续。')
})

it('renders GFM deletion and only disabled read-only task checkboxes', () => {
  const { container } = render(<MarkdownMessage text={'~~删除~~\n\n- [x] 完成\n- [ ] 待办\n\n<input type="text" value="unsafe">'} />)
  expect(screen.getByText('删除').tagName).toBe('DEL')
  const checkboxes = screen.getAllByRole('checkbox')
  expect(checkboxes).toHaveLength(2)
  for (const checkbox of checkboxes) {
    expect(checkbox).toBeDisabled()
    expect(checkbox).toHaveAttribute('readonly')
  }
  expect(container.querySelector('input:not([type="checkbox"])')).toBeNull()
})

it('uses standard Markdown soft breaks while preserving code whitespace', () => {
  const { container } = render(<MarkdownMessage text={'第一行\n第二行\n\n```txt\n  indented\nnext  line\n```'} />)
  const paragraph = container.querySelector('p')!
  expect(paragraph.querySelector('br')).toBeNull()
  expect(paragraph.textContent).toBe('第一行\n第二行')
  expect(container.querySelector('.rich-code-pre code')?.textContent).toBe('  indented\nnext  line')
})

it('renders copyable fenced code blocks with language labels', () => {
  const onCopy = vi.fn()
  render(<MarkdownMessage onCopy={onCopy} text={'```powershell\nGet-ChildItem\n```'} />)
  expect(screen.getByText('PowerShell')).toBeInTheDocument()
  fireEvent.click(screen.getByRole('button', { name: '复制PowerShell代码' }))
  expect(onCopy).toHaveBeenCalledWith('Get-ChildItem')
})

it('renders mermaid blocks with copy fallback', () => {
  const onCopy = vi.fn()
  render(<MarkdownMessage onCopy={onCopy} text={'```mermaid\nflowchart TD\nA-->B\n```'} />)
  expect(screen.getByText('Mermaid')).toBeInTheDocument()
  fireEvent.click(screen.getByRole('button', { name: '复制 Mermaid 源码' }))
  expect(onCopy).toHaveBeenCalledWith('flowchart TD\nA-->B')
})

it('compresses thinking to the last short sentence instead of the full chain', () => {
  expect(compressThinking('先列出步骤。然后核对来源。最后给出结论。')).toBe('最后给出结论。')
  expect(compressThinking('x'.repeat(80)).startsWith('…')).toBe(true)
  expect(formatTaskElapsed(174_000)).toBe('2m 54s')
  expect(formatTaskElapsed(9_000)).toBe('9s')
})

it('does not parse markdown while thinking is streaming and the reasoning fold is closed', () => {
  render(<ThinkingPanel text={'**内部推理** 然后给出结论。'} open streaming onToggle={() => {}} />)
  expect(document.querySelector('.thinking-live-text')).toBeNull()
  expect(document.querySelector('.thinking-reasoning strong')).toBeNull()
  expect(document.querySelector('.message-body')).toBeNull()
})
