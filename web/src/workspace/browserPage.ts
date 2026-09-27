/** A resolved Windows file the side browser can open as its own document. */
export function filePageURL(absolutePath?: string): string {
  const slash = (absolutePath ?? '').trim().replace(/\\/g, '/')
  if (!/^[A-Za-z]:\/[^?#\u0000]+$/.test(slash)) return ''
  const parts = slash.split('/')
  if (parts.length < 2 || parts.some(part => part === '' || part === '.' || part === '..')) return ''
  const drive = `${parts[0].slice(0, 1).toUpperCase()}:`
  const rest = parts.slice(1).map(part => encodeURIComponent(part)).join('/')
  return `file:///${drive}/${rest}`
}

/** The side browser is one frame. Chat can read the page that is open there,
 *  and a short “打开/点击 …” follows that same page. */

/** The Windows path of a file:// page the side browser is showing. */
export function filePathFromPageURL(url: string): string {
  const match = /^file:\/\/\/([A-Za-z]):\/([^?#\s]+)$/.exec(url.trim())
  if (!match) return ''
  let rest = ''
  try {
    rest = decodeURIComponent(match[2])
  } catch {
    return ''
  }
  rest = rest.replace(/\//g, '\\')
  if (!rest || rest.split('\\').some(part => part === '' || part === '.' || part === '..')) return ''
  return `${match[1].toUpperCase()}:\\${rest}`
}

export function openBrowserFilePath(): string {
  if (typeof document === 'undefined') return ''
  const url = document.querySelector('[data-lunitide-pane="browser"]')?.getAttribute('data-lunitide-page-url') ?? ''
  return filePathFromPageURL(url)
}

export const openPageFileInstruction = '要改这个页面或给它加数据，先 workspace.read 读这个文件，再用 workspace.edit 改这个文件本身。不要另写一份，不要只在页面上点击。改完这个文件后页面会自动刷新。'
export const openPageReadInstruction = '这是当前打开的文件。要看页面上的内容，用 workspace.read 读这个文件，不要把页面原文抄进对话。'

function pageChangeAsked(prompt: string): boolean {
  return /新增|添加|加上|加一条|加个|加数据|加入|加到|加进|修改|改一下|写入|删掉|删除|去掉|模块/.test(prompt)
    || (/加/.test(prompt) && /数据|商机|客户/.test(prompt))
}

export function promptWithBrowserPage(prompt: string, pageText: string, filePath = ''): string {
  const page = pageText.replace(/\s+/g, ' ').trim().slice(0, 1500)
  const file = filePath.trim()
  if (!page && !file) return prompt
  let out = prompt
  if (file) {
    const line = pageChangeAsked(prompt) ? openPageFileInstruction : openPageReadInstruction
    out += `\n\n[正在看的页面文件]\n${file}\n${line}`
    return out
  }
  if (page) out += `\n\n[浏览器页面]\n${page}`
  return out
}

export function previewShouldReload(name: string, status: string, summary = ''): boolean {
  if (status !== 'tool_completed') return false
  if (name !== 'workspace.edit' && name !== 'workspace.write') return false
  return !/ok:false/.test(summary)
}

export function browserClickTarget(text: string): string {
  const match = /(?:打开|点击|点开|切到)\s*([^\s，。！？]{1,24})/.exec(text.trim())
  const target = match?.[1]?.trim() ?? ''
  if (!target || /网页|网站|浏览器|链接|https?:/.test(target)) return ''
  return target
}

/** An address the side browser should open because the user asked to look it up. */
export function browserFollowURL(text: string): string {
  const raw = text.trim()
  if (!raw || raw.length > 300) return ''
  const explicit = /https?:\/\/[^\s<>，。！？]+/i.exec(raw)?.[0]?.replace(/[)）]+$/, '') ?? ''
  if (explicit && /打开|访问|看看|浏览|去/.test(raw)) return explicit
  if (/^https?:\/\//i.test(raw)) return explicit
  if (/打开\s*百度/.test(raw)) return 'https://www.baidu.com/'
  const query = /(?:搜索|搜一下|查一下|查一查|帮我查|查找)\s*([^，。！？\n]{1,40})/.exec(raw)?.[1]?.trim() ?? ''
  if (!query || /网页|网站|浏览器|页面/.test(query)) return ''
  return `https://www.baidu.com/s?wd=${encodeURIComponent(query)}`
}

const previewOrigin = 'https://preview.lunitide.local/'

export function openPreviewFrame(): HTMLIFrameElement | null {
  if (typeof document === 'undefined') return null
  for (const frame of document.querySelectorAll('iframe')) {
    if (frame.getAttribute('src')?.startsWith(previewOrigin)) return frame
  }
  return null
}

/** Posts read and click into the open preview document, and forwards the
 *  text it sends back. The page stays on its own origin. The getter is read
 *  when a message arrives, because the iframe mounts after the first render. */
export function bindPreviewFrame(frame: () => HTMLIFrameElement | null): () => void {
  const post = (message: { type: string; text?: string }) => {
    frame()?.contentWindow?.postMessage({ source: 'lunitide-host', ...message }, '*')
  }
  const onRead = () => post({ type: 'read' })
  const onAct = (event: Event) => {
    const text = String((event as CustomEvent<{ text?: string }>).detail?.text ?? '').trim()
    if (text) post({ type: 'click', text })
  }
  const onMessage = (event: MessageEvent) => {
    const node = frame()?.contentWindow
    if (!node || event.source !== node) return
    if (event.origin !== 'https://preview.lunitide.local' && event.origin !== 'null' && event.origin !== '') return
    const data = event.data as { source?: string; type?: string; text?: string } | null
    if (!data || data.source !== 'lunitide-preview' || typeof data.text !== 'string') return
    const text = data.text.trim()
    if (!text) return
    if (data.type === 'cite') window.dispatchEvent(new CustomEvent('lunitide:preview-cite', { detail: { text } }))
    if (data.type === 'text') window.dispatchEvent(new CustomEvent('lunitide:preview-text', { detail: { text } }))
  }
  window.addEventListener('lunitide:preview-read', onRead)
  window.addEventListener('lunitide:preview-act', onAct)
  window.addEventListener('message', onMessage)
  return () => {
    window.removeEventListener('lunitide:preview-read', onRead)
    window.removeEventListener('lunitide:preview-act', onAct)
    window.removeEventListener('message', onMessage)
  }
}

export function readOpenBrowserPage(timeoutMs = 800): Promise<string> {
  const frame = openPreviewFrame()
  const pane = typeof document !== 'undefined' ? document.querySelector('[data-lunitide-pane="browser"]') : null
  if (!frame && !pane) {
    return Promise.resolve('')
  }
  return new Promise(resolve => {
    let settled = false
    const finish = (text: string) => {
      if (settled) return
      settled = true
      window.removeEventListener('lunitide:preview-text', onText)
      window.clearTimeout(timer)
      resolve(text.replace(/\s+/g, ' ').trim().slice(0, 1500))
    }
    const onText = (event: Event) => {
      finish(String((event as CustomEvent<{ text?: string }>).detail?.text ?? ''))
    }
    const timer = window.setTimeout(() => finish(''), timeoutMs)
    window.addEventListener('lunitide:preview-text', onText)
    window.dispatchEvent(new Event('lunitide:preview-read'))
  })
}
