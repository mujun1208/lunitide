import React from 'react'
import ReactMarkdown, { defaultUrlTransform } from 'react-markdown'
import remarkGfm from 'remark-gfm'

const allowedElements = ['p','h1','h2','h3','h4','h5','h6','strong','em','del','ul','ol','li','table','thead','tbody','tr','th','td','blockquote','pre','code','a','br','hr','input']
const firstCjkPunctuation = /[，。！？、；：]/u

type MarkdownNode = {
  type?: string
  url?: string
  value?: string
  children?: MarkdownNode[]
  position?: {start?: {offset?: number}; end?: {offset?: number}}
}

// GFM's bare URL autolinker can include adjacent CJK punctuation. Fix only
// autolinks identified from their source span; explicit [labels](targets) are
// deliberately never rewritten.
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

export function MarkdownMessage({text}:{text:string}) {
  return <ReactMarkdown
    remarkPlugins={[remarkGfm,remarkTrimBareUrlPunctuation]}
    allowedElements={allowedElements}
    unwrapDisallowed
    urlTransform={safeMarkdownUrl}
    components={{a:({children,href,...props})=>{
      if(!href)return <>{children}</>
      return <a {...props} href={href} target="_blank" rel="noopener noreferrer">{children}</a>
    },input:({type,...props})=>type === 'checkbox' ? <input {...props} type="checkbox" disabled readOnly/> : null}}
  >{text}</ReactMarkdown>
}

export function ThinkingPanel({text,open,onToggle}:{text:string;open:boolean;onToggle:(open:boolean)=>void}) {
  if(!text)return null
  return <details className="thinking-panel" open={open} onToggle={event=>onToggle(event.currentTarget.open)}>
    <summary>思考过程</summary>
    <div className="thinking-content"><MarkdownMessage text={text}/></div>
  </details>
}
