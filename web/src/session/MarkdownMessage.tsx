import React, { useEffect, useState } from 'react'
import ReactMarkdown, { defaultUrlTransform, type Components } from 'react-markdown'
import remarkGfm from 'remark-gfm'
import { MermaidBlock } from './markdown/MermaidBlock'
import { RichCodeBlock, codeBlockLanguage } from './markdown/RichCodeBlock'

const allowedElements = ['p','h1','h2','h3','h4','h5','h6','strong','em','del','ul','ol','li','table','thead','tbody','tr','th','td','blockquote','pre','code','a','br','hr','input']
const firstCjkPunctuation = /[，。！？、；：]/u

type MarkdownNode = {
  type?: string
  url?: string
  value?: string
  children?: MarkdownNode[]
  position?: {start?: {offset?: number}; end?: {offset?: number}}
}

const remarkTrimBareUrlPunctuation = () => (tree: MarkdownNode, file: {value?: unknown}) => {
  const source = typeof file.value === 'string' ? file.value : String(file.value ?? '')
  const visit = (node: MarkdownNode) => {
    node.children?.forEach((child,index) => {
      if(child.type === 'link' && child.url?.startsWith('https://')) {
        const start=child.position?.start?.offset,end=child.position?.end?.offset
        const raw=start === undefined || end === undefined ? '' : source.slice(start,end)
        if(raw.startsWith('https://')) {
          const match=firstCjkPunctuation.exec(child.url)
          if(match) {
            const punctuationAndSuffix=child.url.slice(match.index)
            child.url=child.url.slice(0,match.index)
            const label=child.children?.[0]
            if(label?.type === 'text' && label.value?.endsWith(punctuationAndSuffix))label.value=label.value.slice(0,-punctuationAndSuffix.length)
            node.children?.splice(index+1,0,{type:'text',value:punctuationAndSuffix})
          }
        }
      }
      visit(child)
    })
  }
  visit(tree)
}

export const safeMarkdownUrl = (url: string): string => {
  try {
    const parsed = new URL(url)
    return parsed.protocol === 'https:' ? defaultUrlTransform(url) : ''
  } catch {
    return ''
  }
}

function markdownComponents(onCopy?: (value: string) => void | Promise<void>): Components {
  return {
    a: ({ children, href, ...props }) => {
      if (!href) return <>{children}</>
      return <a {...props} href={href} target="_blank" rel="noopener noreferrer">{children}</a>
    },
    input: ({ type, ...props }) => type === 'checkbox' ? <input {...props} type="checkbox" disabled readOnly /> : null,
    pre: ({ children }) => <>{children}</>,
    table: ({ children }) => <div className="md-table-wrap"><table className="md-table">{children}</table></div>,
    code: ({ className, children, ...props }) => {
      const lang = codeBlockLanguage(className)
      const text = String(children).replace(/\n$/, '')
      if (lang === 'mermaid') return <MermaidBlock source={text} onCopy={onCopy} />
      if (lang) return <RichCodeBlock lang={lang} code={text} onCopy={onCopy} />
      return <code className={className} {...props}>{children}</code>
    },
  }
}

export function MarkdownMessage({ text, onCopy }: { text: string; onCopy?: (value: string) => void | Promise<void> }) {
  return <ReactMarkdown
    remarkPlugins={[remarkGfm, remarkTrimBareUrlPunctuation]}
    allowedElements={allowedElements}
    unwrapDisallowed
    urlTransform={safeMarkdownUrl}
    components={markdownComponents(onCopy)}
  >{text}</ReactMarkdown>
}

/** Last sentence (or tail) so the live thinking row does not type the full chain. */
export function compressThinking(text: string, maxRunes = 36): string {
  const t = text.replace(/\s+/g, ' ').trim()
  if (!t) return ''
  const parts = t.split(/(?<=[。！？.!?])\s*/).filter(Boolean)
  const last = parts[parts.length - 1] ?? t
  const runes = Array.from(last)
  if (runes.length <= maxRunes) return last
  return `…${runes.slice(-maxRunes).join('')}`
}

export function formatTaskElapsed(ms: number): string {
  const total = Math.max(0, Math.floor(ms / 1000))
  const m = Math.floor(total / 60)
  const s = total % 60
  return m > 0 ? `${m}m ${s}s` : `${s}s`
}

function TaskElapsedChip({elapsed, startedAt, streaming}: {elapsed?: string; startedAt?: number; streaming?: boolean}) {
  const [now, setNow] = useState(() => Date.now())
  useEffect(() => {
    if (!streaming || !startedAt) return
    setNow(Date.now())
    const id = window.setInterval(() => setNow(Date.now()), 1000)
    return () => window.clearInterval(id)
  }, [streaming, startedAt])
  const label = elapsed ?? (startedAt ? formatTaskElapsed((streaming ? now : Date.now()) - startedAt) : '')
  if (!label) return null
  return <span className="thinking-summary-chip">耗时 {label}</span>
}

export function ThinkingPanel({
  text, open, onToggle, status, elapsed, startedAt, skillCount, searchCount, streaming, children, onCopy,
}: {
  text: string
  open: boolean
  onToggle: (open: boolean) => void
  status?: string
  elapsed?: string
  startedAt?: number
  skillCount?: number
  searchCount?: number
  streaming?: boolean
  children?: React.ReactNode
  onCopy?: (value: string) => void | Promise<void>
}) {
  const [reasonOpen, setReasonOpen] = useState(false)
  if (!text && !children) return null
  const preview = compressThinking(text)
  return <details className="thinking-panel" open={open} onToggle={event => onToggle(event.currentTarget.open)}>
    <summary>
      <span className="thinking-summary-label">任务过程</span>
      <TaskElapsedChip elapsed={elapsed} startedAt={startedAt} streaming={streaming} />
      {!!skillCount && <span className="thinking-summary-chip">已调用 {skillCount} 次技能</span>}
      {!!searchCount && <span className="thinking-summary-chip">已搜索 {searchCount} 次网页</span>}
      {status && <span className="thinking-summary-status">{status}</span>}
    </summary>
    <div className="thinking-content">
      {text && <details className="thinking-reasoning-fold" open={reasonOpen} onToggle={event => setReasonOpen(event.currentTarget.open)}>
        <summary>思考{preview && <em>{streaming ? preview : compressThinking(text, 48)}</em>}</summary>
        <div className={`thinking-reasoning${streaming ? ' is-live' : ''}`}>
          {reasonOpen ? (streaming ? <pre className="thinking-live-text">{text}</pre> : <MarkdownMessage text={text} onCopy={onCopy} />) : null}
        </div>
      </details>}
      {children}
    </div>
  </details>
}
