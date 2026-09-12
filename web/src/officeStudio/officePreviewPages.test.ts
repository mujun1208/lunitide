import { describe, expect, test } from 'vitest'
import { officePreviewPages } from './officePreviewPages'
import type { OfficeNode } from './officeStudioApi'

const node = (id: string, location: string, text: string): OfficeNode => ({
  id,
  label: `${location} · ${id}`,
  text,
  location,
  editable: true,
})

describe('officePreviewPages', () => {
  test('keeps one Word node as one page', () => {
    const pages = officePreviewPages('docx', [node('n1', 'word/document.xml', '正文')])
    expect(pages).toHaveLength(1)
    expect(pages[0].nodes[0].id).toBe('n1')
  })

  test('groups PPT shapes on the same slide into one page', () => {
    const pages = officePreviewPages('pptx', [
      node('a', 'ppt/slides/slide1.xml', '封面标题'),
      node('b', 'ppt/slides/slide1.xml', '副标题'),
      node('c', 'ppt/slides/slide2.xml', '目录'),
    ])
    expect(pages.map((page) => page.label)).toEqual(['第 1 页', '第 2 页'])
    expect(pages[0].nodes.map((item) => item.id)).toEqual(['a', 'b'])
    expect(pages[1].nodes.map((item) => item.id)).toEqual(['c'])
  })

  test('groups PPT shapes from labels when location is missing', () => {
    const pages = officePreviewPages('pptx', [
      { id: 'a', label: 'ppt/slides/slide1.xml · t1', text: '封面标题', editable: true },
      { id: 'b', label: 'ppt/slides/slide1.xml · t2', text: '副标题', editable: true },
    ])
    expect(pages).toHaveLength(1)
    expect(pages[0].label).toBe('第 1 页')
  })
})
