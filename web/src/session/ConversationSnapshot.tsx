import React,{useRef,useState}from'react'
import{toBlob}from'html-to-image'
import type{MessageDTO}from'../generated/bridge'
import{useZh}from'../i18n/language'
import{AssistantMessageBody,MarkdownMessage}from'./MarkdownMessage'

export const selectedMessages=(items:readonly MessageDTO[],ids:ReadonlySet<string>)=>items.filter(item=>ids.has(item.id)).sort((a,b)=>a.sequence-b.sequence)
const safeName=(value:string)=>value.trim().replace(/[<>:"/\\|?*\u0000-\u001f]/g,'-').replace(/[. ]+$/,'').slice(0,80)||'lunitide-chat'
const download=(blob:Blob,name:string)=>{const url=URL.createObjectURL(blob),link=document.createElement('a');link.href=url;link.download=name;document.body.appendChild(link);link.click();link.remove();setTimeout(()=>URL.revokeObjectURL(url),0)}

export function ConversationSnapshot({open,title,items,onClose}:{open:boolean;title:string;items:readonly MessageDTO[];onClose:()=>void}){
 const zh=useZh()
 const node=useRef<HTMLDivElement>(null),[busy,setBusy]=useState(false),[notice,setNotice]=useState('')
 if(!open)return null
 const make=async()=>{if(!node.current)throw new Error('快照内容尚未就绪');setBusy(true);setNotice('正在生成图片…');try{await document.fonts?.ready;const blob=await toBlob(node.current,{backgroundColor:'#080d16',pixelRatio:2,cacheBust:true});if(!blob)throw new Error('图片生成失败');return blob}finally{setBusy(false)}}
 const save=async()=>{try{const blob=await make();download(blob,`${safeName(title)}.png`);setNotice('图片已下载。')}catch(e){setNotice(e instanceof Error?e.message:'图片生成失败')}}
 const copy=async()=>{try{const blob=await make();if(!navigator.clipboard?.write||typeof ClipboardItem==='undefined')throw new Error('当前环境不支持复制图片，请使用“下载图片”');await navigator.clipboard.write([new ClipboardItem({'image/png':blob})]);setNotice('图片已复制到剪贴板。')}catch(e){setNotice(e instanceof Error?e.message:'复制图片失败')}}
 const share=async()=>{try{const blob=await make(),file=new File([blob],`${safeName(title)}.png`,{type:'image/png'});if(!navigator.share||!navigator.canShare?.({files:[file]}))throw new Error('当前环境不支持分享图片，请先下载图片');await navigator.share({title,files:[file]});setNotice('已打开系统分享。')}catch(e){if(e instanceof DOMException&&e.name==='AbortError')return;setNotice(e instanceof Error?e.message:'分享图片失败')}}
 return <div className="snapshot-dialog" role="dialog" aria-modal="true" aria-label="生成对话图片"><div className="snapshot-dialog-head"><div><h2>生成对话图片</h2><p>已选择 {items.length} 条消息。图片只保存在本机或交给系统分享，不会生成虚假的本地分享链接。</p></div><button onClick={onClose} disabled={busy}>取消</button></div><div className="snapshot-preview"><div className="snapshot-canvas" ref={node}><header><b>{zh?'月汐':'LUNITIDE'}</b><h1>{title}</h1><time>{new Date().toLocaleString()}</time></header>{items.map(item=><article key={item.id} className={item.role}><div><b>{item.role==='user'?(zh?'我':'Me'):item.role==='assistant'?(zh?'月汐':'Lunitide'):'Tool'}</b><time>{new Date(item.createdAt).toLocaleString()}</time></div><section>{item.role==='assistant'?<AssistantMessageBody text={item.text}/>:<MarkdownMessage text={item.text}/>}</section></article>)}</div></div>{notice&&<p className="snapshot-notice" role="status">{notice}</p>}<div className="snapshot-actions"><button onClick={()=>void save()} disabled={busy}>下载图片</button><button onClick={()=>void copy()} disabled={busy}>复制图片</button><button className="primary" onClick={()=>void share()} disabled={busy}>{busy?'生成中…':'系统分享'}</button></div></div>
}
