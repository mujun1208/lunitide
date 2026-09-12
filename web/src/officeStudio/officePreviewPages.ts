import type { OfficeKind, OfficeNode } from './officeStudioApi'

export type OfficePreviewPage = {
  id: string
  label: string
  location?: string
  nodes: OfficeNode[]
}

export function officePreviewPages(kind: OfficeKind | undefined, nodes: OfficeNode[]): OfficePreviewPage[] {
  if (kind !== 'pptx') {
    return nodes.map((node, index) => ({
      id: node.id,
      label: node.label || `内容 ${index + 1}`,
      location: node.location,
      nodes: [node],
    }))
  }
  const pages: OfficePreviewPage[] = []
  const index = new Map<string, OfficePreviewPage>()
  for (const node of nodes) {
    const part = node.location || node.label
    const key = slideKey(part) || node.id
    let page = index.get(key)
    if (!page) {
      page = {
        id: key,
        label: slideLabel(part, pages.length + 1),
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

export function officePreviewThumb(page: OfficePreviewPage): { title: string; excerpt: string } {
  const texts = page.nodes.map((node) => node.text?.trim()).filter((text): text is string => Boolean(text))
  const title = texts[0]?.split('\n')[0] || page.label
  return { title, excerpt: texts.join(' · ') }
}

function slideKey(location?: string) {
  const match = location?.match(/ppt\/slides\/slide(\d+)\.xml/i)
  return match ? `slide-${match[1]}` : location
}

function slideLabel(location: string | undefined, fallback: number) {
  const match = location?.match(/ppt\/slides\/slide(\d+)\.xml/i)
  return match ? `第 ${Number(match[1])} 页` : `第 ${fallback} 页`
}
