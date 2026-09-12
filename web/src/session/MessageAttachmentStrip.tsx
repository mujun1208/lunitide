import React, {useEffect, useRef, useState} from 'react'
import {attachmentBridge, type AttachmentBridge} from '../bridge/client'
import type {AttachmentGetResult} from '../generated/bridge'
import {attachmentPreview, isImageAttachmentName} from './attachments'
import {attachmentOperation} from './attachmentOperation'
import type {AttachmentMention} from './composerParser'
import './attachmentPreview.css'

function stripUserError(err: unknown, fallback: string): string {
  const detail = err instanceof Error ? err.message.trim() : ''
  return /[\u4e00-\u9fff]/.test(detail) ? detail : fallback
}

export function AttachmentViewer({item, attachments=attachmentBridge, onClose}:{item:AttachmentMention;attachments?:AttachmentBridge;onClose:()=>void}) {
 const [result,setResult]=useState<AttachmentGetResult>(),[error,setError]=useState(''),[revision,setRevision]=useState(0)
 const closeBtn=useRef<HTMLButtonElement>(null)
 useEffect(()=>{const controller=new AbortController();setResult(undefined);setError('');void attachmentOperation(Promise.resolve().then(()=>attachments.get({attachmentId:item.id})),controller.signal,10_000,'附件读取超时，请重试').then(setResult).catch(e=>{if(!controller.signal.aborted)setError(stripUserError(e,'附件读取失败'))});return()=>controller.abort()},[item.id,attachments,revision])
 useEffect(()=>{const close=(e:KeyboardEvent)=>{if(e.key==='Escape')onClose()};window.addEventListener('keydown',close);return()=>window.removeEventListener('keydown',close)},[onClose])
 useEffect(()=>{closeBtn.current?.focus({preventScroll:true})},[])
 const image=result?.contentBase64&&/^image\/(?:png|jpeg|webp)$/.test(result.mime)?`data:${result.mime};base64,${result.contentBase64}`:undefined
 return <div className="attachment-viewer-backdrop" onClick={onClose}><section className="attachment-viewer" role="dialog" aria-modal="true" aria-label={`查看附件 ${item.label}`} onClick={e=>e.stopPropagation()}><header><b>{result?.originalName||item.label}</b><button ref={closeBtn} type="button" onClick={onClose} aria-label="关闭附件预览">关闭</button></header>{error?<p role="alert">{error} <button type="button" onClick={()=>setRevision(x=>x+1)}>重试</button></p>:!result?<p role="status">正在读取附件…</p>:image?<img className="attachment-viewer-image" src={image} alt={result.originalName}/>:result.parsedText!==undefined?<pre>{result.parsedText||'这是一个空文件。'}</pre>:<p role="status">{result.parseStatus==='failed'?`文件已保存，暂时无法提取内容（${result.parseErrorCode||'解析失败'}）。`:'文件已保存，内容尚未解析。'}</p>}</section></div>
}

function AttachmentButton({item,onOpen,attachments}:{item:AttachmentMention;onOpen:()=>void;attachments:AttachmentBridge}) {
 const [url,setUrl]=useState(()=>attachmentPreview(item.id)?.url),[broken,setBroken]=useState(false)
 const image=isImageAttachmentName(item.label)||!!url
 useEffect(()=>{if(!image||url)return;const controller=new AbortController();void attachmentOperation(Promise.resolve().then(()=>attachments.get({attachmentId:item.id})),controller.signal,10_000).then(result=>{if(result.contentBase64&&/^image\/(?:png|jpeg|webp)$/.test(result.mime))setUrl(`data:${result.mime};base64,${result.contentBase64}`)}).catch(()=>{});return()=>controller.abort()},[image,url,item.id,attachments])
 return <button type="button" className="message-attachment-button" onClick={onOpen} aria-label={`查看附件 ${item.label}`}>{image&&url&&!broken?<img src={url} alt={item.label} onError={()=>setBroken(true)}/>:<span className="message-att-chip">{image?'🖼':'📎'} {item.label}</span>}</button>
}

export function MessageAttachmentStrip({mentions,attachments=attachmentBridge}:{mentions:AttachmentMention[];attachments?:AttachmentBridge}){
 const [opened,setOpened]=useState<AttachmentMention>()
 if(!mentions.length)return null
 return <><div className="message-att-strip" aria-label="附件">{mentions.map(item=><AttachmentButton key={item.id} item={item} attachments={attachments} onOpen={()=>setOpened(item)}/>)}</div>{opened&&<AttachmentViewer item={opened} attachments={attachments} onClose={()=>setOpened(undefined)}/>}</>
}
