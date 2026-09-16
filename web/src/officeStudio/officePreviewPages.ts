import type { OfficeKind, OfficeNode } from './officeStudioApi'

export type OfficePreviewPage = {
  id: string
  label: string
  location?: string
  nodes: OfficeNode[]
}

export function officePreviewPages(kind: OfficeKind | undefined, nodes: OfficeNode[]): OfficePreviewPage[] {
  if (kind === 'pptx') {
    return groupPages(nodes, slideKey, (part, index) => slideLabel(part, index + 1))
  }
  if (kind === 'docx' || kind === 'xlsx') {
    return groupPages(nodes, xmlPartKey, (part, index) => partLabel(kind, part, index + 1))
  }
  return nodes.map((node, index) => ({
    id: node.id,
    label: node.label || `内容 ${index + 1}`,
    location: node.location,
    nodes: [node],
  }))
}

export function officePreviewThumb(page: OfficePreviewPage): { title: string; excerpt: string } {
  const texts = page.nodes.map((node) => node.text?.trim()).filter((text): text is string => Boolean(text))
  const title = texts[0]?.split('\n')[0] || page.label
  return { title, excerpt: texts.join(' · ') }
}

export function officeNodeHeading(node: OfficeNode): string {
  if (node.label && !/\.xml/i.test(node.label)) return node.label
  return '段落'
}

function groupPages(
  nodes: OfficeNode[],
  keyOf: (location?: string, fallback?: string) => string | undefined,
  labelOf: (part: string | undefined, index: number) => string,
): OfficePreviewPage[] {
  const pages: OfficePreviewPage[] = []
  const index = new Map<string, OfficePreviewPage>()
  for (const node of nodes) {
    const part = node.location || node.label
    const key = keyOf(part, node.id) || node.id
    let page = index.get(key)
    if (!page) {
      page = {
        id: key,
        label: labelOf(part, pages.length),
        location: node.location,
        nodes: [],
      }
      index.set(key, page)
      pages.push(page)
    }
    page.nodes.push(node)
  }
  return pages
}

function slideKey(location?: string) {
  const match = location?.match(/ppt\/slides\/slide(\d+)\.xml/i)
  return match ? `slide-${match[1]}` : location
}

function slideLabel(location: string | undefined, fallback: number) {
  const match = location?.match(/ppt\/slides\/slide(\d+)\.xml/i)
  return match ? `第 ${Number(match[1])} 页` : `第 ${fallback} 页`
}

function xmlPartKey(location?: string) {
  const match = location?.match(/[\w./-]+\.xml/i)
  return match ? match[0].toLowerCase() : location
}

function partLabel(kind: OfficeKind, location: string | undefined, fallback: number) {
  const part = xmlPartKey(location) || ''
  if (kind === 'docx') {
    if (part.includes('document.xml')) return '正文'
    if (part.includes('header')) return '页眉'
    if (part.includes('footer')) return '页脚'
    if (part.includes('footnotes')) return '脚注'
  }
  if (kind === 'xlsx') {
    const sheet = part.match(/sheet(\d+)\.xml/i)
    if (sheet) return `工作表 ${sheet[1]}`
  }
  return part || `内容 ${fallback}`
}
