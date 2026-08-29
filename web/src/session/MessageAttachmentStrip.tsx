import React from 'react'
import{attachmentPreview,isImageAttachmentName}from'./attachments'
import type{AttachmentMention}from'./composerParser'

export function MessageAttachmentStrip({mentions}:{mentions:AttachmentMention[]}){
 if(!mentions.length)return null
 return <div className="message-att-strip" aria-label="附件">{mentions.map(item=>{
  const preview=attachmentPreview(item.id)
  const image=!!preview||isImageAttachmentName(item.label)
  if(image&&preview?.url)return <img key={item.id} src={preview.url} alt={item.label}/>
  return <span key={item.id} className="message-att-chip">{image?'🖼':'📎'} {item.label}</span>
 })}</div>
}
