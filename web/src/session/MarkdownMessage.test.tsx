import {cleanup,render,screen} from '@testing-library/react'
import {afterEach,expect,it} from 'vitest'
import {MarkdownMessage,safeMarkdownUrl} from './MarkdownMessage'

afterEach(cleanup)

it('renders GFM structures and secure HTTPS links',()=>{
 render(<MarkdownMessage text={'# 标题\n\n**粗体**\n\n- 列表\n\n> 引用\n\n| A | B |\n|---|---|\n| 1 | 2 |\n\n```ts\nconst x=1\n```\n\n[安全](https://example.com)'}/>)
 expect(screen.getByRole('heading',{name:'标题'})).toBeInTheDocument()
 expect(screen.getByText('粗体').tagName).toBe('STRONG')
 expect(screen.getByRole('table')).toBeInTheDocument()
 expect(screen.getByRole('link',{name:'安全'})).toHaveAttribute('rel','noopener noreferrer')
})

it('keeps raw HTML inert and blocks unsafe URLs and images',()=>{
 const {container}=render(<MarkdownMessage text={'<img src="https://remote.test/x.png" onerror="alert(1)">\n\n<script>alert(2)</script>\n\n[bad](javascript:alert(3)) ![remote](https://remote.test/x.png)'}/>)
 expect(container.querySelector('img,script')).toBeNull()
 expect(screen.queryByRole('link',{name:'bad'})).toBeNull()
 expect(container).toHaveTextContent('alert(2)')
 expect(safeMarkdownUrl('http://example.com')).toBe('')
})

it('does not rewrite an explicit Markdown link whose label looks like a punctuated URL',()=>{
 render(<MarkdownMessage text={'[https://label.test/path，说明](https://target.test/kept)'}/>)
 const link=screen.getByRole('link',{name:'https://label.test/path，说明'})
 expect(link).toHaveAttribute('href','https://target.test/kept')
})

it('moves trailing CJK punctuation outside a bare HTTPS autolink',()=>{
 const {container}=render(<MarkdownMessage text={'访问 https://example.com/path，继续。'}/>)
 const link=screen.getByRole('link',{name:'https://example.com/path'})
 expect(link).toHaveAttribute('href','https://example.com/path')
 expect(container).toHaveTextContent('https://example.com/path，继续。')
})

it('renders GFM deletion and only disabled read-only task checkboxes',()=>{
 const {container}=render(<MarkdownMessage text={'~~删除~~\n\n- [x] 完成\n- [ ] 待办\n\n<input type="text" value="unsafe">'}/>)
 expect(screen.getByText('删除').tagName).toBe('DEL')
 const checkboxes=screen.getAllByRole('checkbox')
 expect(checkboxes).toHaveLength(2)
 for(const checkbox of checkboxes){
  expect(checkbox).toBeDisabled()
  expect(checkbox).toHaveAttribute('readonly')
 }
 expect(container.querySelector('input:not([type="checkbox"])')).toBeNull()
})

it('uses standard Markdown soft breaks while preserving code whitespace',()=>{
 const {container}=render(<MarkdownMessage text={'第一行\n第二行\n\n```txt\n  indented\nnext  line\n```'}/>)
 const paragraph=container.querySelector('p')!
 expect(paragraph.querySelector('br')).toBeNull()
 expect(paragraph.textContent).toBe('第一行\n第二行')
 expect(container.querySelector('pre code')?.textContent).toBe('  indented\nnext  line\n')
})
