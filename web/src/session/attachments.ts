import {BridgeClientError,type AttachmentBridge} from '../bridge/client'
import type{AttachmentIngestResult}from'../generated/bridge'
import {readBoundedFile} from '../files/readBoundedFile'
import {attachmentOperation,attachmentCancelled} from './attachmentOperation'

export const ATTACHMENT_FILE_MAX=10*1024*1024
export const ATTACHMENT_BATCH_MAX=20
export const VISION_IMAGE_MAX=4
export const ATTACHMENT_BATCH_BYTES=20*1024*1024
export const VISION_IMAGE_BYTES=180*1024
export const TEXT_EXTENSIONS=['.txt','.md','.json','.csv','.html','.xml','.js','.ts','.py','.go','.java','.c','.cpp','.rs','.yaml','.yml','.sh','.sql'] as const
export const IMAGE_MIME_BY_EXTENSION:Record<string,string>={'.png':'image/png','.jpg':'image/jpeg','.jpeg':'image/jpeg','.webp':'image/webp'}
export const ALLOWED_EXTENSIONS=[...TEXT_EXTENSIONS,...Object.keys(IMAGE_MIME_BY_EXTENSION)]
export const ATTACHMENT_ACCEPT=[...ALLOWED_EXTENSIONS,'image/png','image/jpeg','image/webp'].join(',')

const extension=(name:string)=>{const dot=name.lastIndexOf('.');return dot<0?'':name.slice(dot).toLowerCase()}
const imageExtensionByMIME=(mime:string)=>mime==='image/png'?'.png':mime==='image/jpeg'?'.jpg':mime==='image/webp'?'.webp':''
const readFile=(file:File,signal?:AbortSignal)=>attachmentOperation(readBoundedFile(file,ATTACHMENT_FILE_MAX),signal,12_000,'读取文件超时，请重新选择')
export const fileToBase64=async(file:File):Promise<string>=>{const bytes=new Uint8Array(await readFile(file)),chunk=0x8000;let binary='';for(let i=0;i<bytes.length;i+=chunk)binary+=String.fromCharCode(...bytes.subarray(i,i+chunk));return btoa(binary)}
const bytesToBase64=(bytes:Uint8Array)=>{let binary='';for(let i=0;i<bytes.length;i+=0x8000)binary+=String.fromCharCode(...bytes.subarray(i,i+0x8000));return btoa(binary)}
const hex=(bytes:ArrayBuffer)=>Array.from(new Uint8Array(bytes),x=>x.toString(16).padStart(2,'0')).join('')

async function compressVisionImage(file:File,signal?:AbortSignal):Promise<File>{
 if(file.size<=VISION_IMAGE_BYTES)return file
 let expired=false
 const decoding=createImageBitmap(file).then(bitmap=>{if(expired){bitmap.close();throw attachmentCancelled()}return bitmap})
 const bitmap=await attachmentOperation(decoding,signal,10_000,'图片解码超时，请重试').catch(error=>{expired=true;throw error});try{let width=bitmap.width,height=bitmap.height
 const canvas=document.createElement('canvas'),context=canvas.getContext('2d')
 if(!context){throw new Error(`${file.name}（无法压缩图片）`)}
 const scale=Math.min(1,1600/Math.max(width,height));width=Math.max(1,Math.round(width*scale));height=Math.max(1,Math.round(height*scale))
 for(const quality of[.86,.74,.62,.5,.4]){canvas.width=width;canvas.height=height;context.fillStyle='#fff';context.fillRect(0,0,width,height);context.drawImage(bitmap,0,0,width,height);const blob=await attachmentOperation(new Promise<Blob|null>(resolve=>canvas.toBlob(resolve,'image/webp',quality)),signal,10_000,'图片压缩超时，请重试');if(blob&&blob.size<=VISION_IMAGE_BYTES){return new File([blob],file.name.replace(/\.[^.]+$/,'')+'.webp',{type:'image/webp',lastModified:file.lastModified})}width=Math.max(1,Math.round(width*.82));height=Math.max(1,Math.round(height*.82))}
 throw new Error(`${file.name}（自动压缩后仍超过 180 KiB）`)
 }finally{bitmap.close()}
}

export async function prepareAttachmentFiles(files:readonly File[],signal?:AbortSignal):Promise<{files:File[];failed:string[]}>{
 const prepared:File[]=[],failed:string[]=[]
 let total=0
 for(const file of files.slice(0,ATTACHMENT_BATCH_MAX)){
  if(signal?.aborted)throw attachmentCancelled()
  const ext=extension(file.name),imageMIME=IMAGE_MIME_BY_EXTENSION[ext]
  if(!ALLOWED_EXTENSIONS.includes(ext)){failed.push(`${file.name}（不支持的类型）`);continue}
  if(file.size>ATTACHMENT_FILE_MAX){failed.push(`${file.name}（超过 10 MiB）`);continue}
  if((total+=file.size)>ATTACHMENT_BATCH_BYTES){failed.push(`${file.name}（本批原文件合计超过 20 MiB）`);continue}
  try{prepared.push(imageMIME?await compressVisionImage(file,signal):file)}catch(e){if(signal?.aborted)throw e;failed.push(e instanceof Error?e.message:`${file.name}（图片处理失败）`)}
 }
 if(files.length>ATTACHMENT_BATCH_MAX)failed.push(`超过 20 个的 ${files.length-ATTACHMENT_BATCH_MAX} 个文件`)
 return{files:prepared,failed}
}

export function normalizePastedImages(files:readonly File[],now=new Date()):File[]{
 const stamp=now.toISOString().replace(/[-:]/g,'').replace(/\.\d{3}Z$/,'Z').replace('T','-')
 return files.map((file,index)=>{const ext=imageExtensionByMIME(file.type),generic=!file.name||/^(?:image(?:\.(?:png|jpe?g|webp))?|blob)$/i.test(file.name);return ext&&generic?new File([file],`clipboard-${stamp}-${index+1}${ext}`,{type:file.type,lastModified:file.lastModified||now.getTime()}):file})
}

export function clipboardImages(data:DataTransfer):File[]{
 const fromItems=Array.from(data.items??[]).filter(item=>item.kind==='file'&&item.type.startsWith('image/')).map(item=>item.getAsFile()).filter((file):file is File=>!!file)
 return fromItems.length?fromItems:Array.from(data.files??[]).filter(file=>file.type.startsWith('image/'))
}

export function validateAttachmentBatch(files:readonly File[]):{accepted:File[];skipped:string[]}{
 const selected=files.slice(0,ATTACHMENT_BATCH_MAX),skipped:string[]=[]
 if(files.length>ATTACHMENT_BATCH_MAX)skipped.push(`超过 20 个的 ${files.length-ATTACHMENT_BATCH_MAX} 个文件`)
 let imageCount=0
 const accepted=selected.filter(file=>{const ext=extension(file.name),imageMIME=IMAGE_MIME_BY_EXTENSION[ext];if(!ALLOWED_EXTENSIONS.includes(ext)){skipped.push(`${file.name}（不支持的类型）`);return false}if(imageMIME&&file.type&&file.type!==imageMIME){skipped.push(`${file.name}（图片类型与扩展名不匹配）`);return false}if(imageMIME&&++imageCount>VISION_IMAGE_MAX){skipped.push(`${file.name}（每次最多 4 张图片）`);return false}if(imageMIME&&file.size>VISION_IMAGE_BYTES){skipped.push(`${file.name}（视觉图片超过 180 KiB）`);return false}if(file.size>ATTACHMENT_FILE_MAX){skipped.push(`${file.name}（超过 10 MiB）`);return false}return true})
 if(accepted.reduce((total,file)=>total+file.size,0)>ATTACHMENT_BATCH_BYTES)throw new BridgeClientError('本批支持文件合计不能超过 20 MiB','ATTACHMENT_BATCH_LIMIT',false,'renderer')
 return{accepted,skipped}
}

export type AttachmentProgress={key:string;status:'queued'|'reading'|'uploading'|'processing'|'complete'|'failed'|'cancelled';percent:number;name:string;size:number;attachmentId?:string;error?:string;previewUrl?:string;file?:File}
export type AttachmentProgressHandler=(progress:AttachmentProgress)=>void
export type AttachmentBatchResult={uploaded:number;skipped:string[];attachmentIds:string[];items:AttachmentIngestResult[];failed:Array<{name:string;error:string;file:File}>}
export type AttachmentPreview={url:string;name:string;mime:string}

const PREVIEW_PREFIX='lunitide:att-preview:'
const previewMemory=new Map<string,AttachmentPreview>()
export const isImageAttachmentName=(name:string)=>!!IMAGE_MIME_BY_EXTENSION[extension(name)]
export const isImageFile=(file:File)=>file.type.startsWith('image/')||isImageAttachmentName(file.name)
const previewKey=(id:string)=>PREVIEW_PREFIX+id
function readStoredPreview(id:string):AttachmentPreview|undefined{
 try{const raw=sessionStorage.getItem(previewKey(id))??localStorage.getItem(previewKey(id));if(!raw)return;const parsed=JSON.parse(raw) as{data?:string;name?:string;mime?:string};if(!parsed.data||!/^data:image\/(?:png|jpeg|webp);base64,[A-Za-z0-9+/]+=*$/.test(parsed.data))return;return{url:parsed.data,name:parsed.name||'图片',mime:parsed.mime||'image/png'}}catch{return}
}
export function attachmentPreview(id:string):AttachmentPreview|undefined{
 const hit=previewMemory.get(id)
 if(hit)return hit
 const stored=readStoredPreview(id)
 if(stored)previewMemory.set(id,stored)
 return stored
}
function persistPreview(id:string,dataUrl:string,name:string,mime:string){
 const payload=JSON.stringify({data:dataUrl,name,mime,t:Date.now()})
 try{sessionStorage.setItem(previewKey(id),payload)}catch{/* quota */}
 try{localStorage.setItem(previewKey(id),payload)}catch{
  const keys:string[]=[]
  for(let i=0;i<localStorage.length;i++){const key=localStorage.key(i);if(key?.startsWith(PREVIEW_PREFIX))keys.push(key)}
  keys.sort((a,b)=>{const stamp=(key:string)=>{try{return JSON.parse(localStorage.getItem(key)??'{}').t??0}catch{return 0}};return stamp(a)-stamp(b)})
  for(const key of keys.slice(0,Math.max(1,Math.ceil(keys.length/2))))localStorage.removeItem(key)
  try{localStorage.setItem(previewKey(id),payload)}catch{/* still full */}
 }
}
export function rememberAttachmentPreview(id:string,file:File,existingUrl?:string){
 if(!id||!isImageFile(file)||file.size>VISION_IMAGE_BYTES)return
 const mime=file.type||IMAGE_MIME_BY_EXTENSION[extension(file.name)]||'image/png'
 const remember=(url:string)=>{previewMemory.set(id,{url,name:file.name,mime});persistPreview(id,url,file.name,mime)}
 if(existingUrl?.startsWith('data:image/'))remember(existingUrl)
 else void fileToBase64(file).then(data=>remember(`data:${mime};base64,${data}`)).catch(()=>{})
}
export function forgetAttachmentPreview(id:string){
 const hit=previewMemory.get(id)
 if(hit?.url.startsWith('blob:'))try{URL.revokeObjectURL(hit.url)}catch{/* ignore */}
 previewMemory.delete(id)
 try{sessionStorage.removeItem(previewKey(id))}catch{/* ignore */}
 try{localStorage.removeItem(previewKey(id))}catch{/* ignore */}
}
export async function ingestAttachments(attachments:AttachmentBridge,projectId:string,sessionId:string,files:readonly File[],onProgress?:AttachmentProgressHandler,signal?:AbortSignal):Promise<AttachmentBatchResult>{
 const{accepted,skipped}=validateAttachmentBatch(files),items:AttachmentIngestResult[]=[],failed:AttachmentBatchResult['failed']=[]
 accepted.forEach((file,index)=>onProgress?.({key:`${file.name}:${file.lastModified}:${index}`,status:'queued',percent:0,name:file.name,size:file.size}))
 for(const[fileIndex,file]of accepted.entries()){
  const key=`${file.name}:${file.lastModified}:${fileIndex}`
  let uploadId='',percent=0,previewUrl:string|undefined
  const emit=(status:AttachmentProgress['status'],extra:Partial<AttachmentProgress>={})=>onProgress?.({key,status,percent,name:file.name,size:file.size,previewUrl,file,...extra})
  const abortUpload=(id:string)=>attachmentOperation(Promise.resolve().then(()=>attachments.abort({uploadId:id,projectId,sessionId})),undefined,2_000).catch(()=>{})
  try{
   if(signal?.aborted)throw attachmentCancelled()
   percent=1;emit('reading')
   const bytes=new Uint8Array(await readFile(file,signal))
   if(bytes.length!==file.size)throw new Error('文件大小发生变化，请重新选择')
   if(isImageFile(file))previewUrl=`data:${file.type||IMAGE_MIME_BY_EXTENSION[extension(file.name)]};base64,${bytesToBase64(bytes)}`
   const sha256=hex(await attachmentOperation(crypto.subtle.digest('SHA-256',bytes),signal,10_000))
   let beginAbandoned=false
   const beginning=attachments.begin({projectId,sessionId,originalName:file.name,mime:file.type||IMAGE_MIME_BY_EXTENSION[extension(file.name)]||'text/plain',size:file.size,sha256})
   void beginning.then(result=>{if(beginAbandoned||signal?.aborted)void abortUpload(result.uploadId)},()=>{})
   let begin
   try{begin=await attachmentOperation(beginning,signal)}catch(error){beginAbandoned=true;throw error}
   uploadId=begin.uploadId
   if(!Number.isSafeInteger(begin.chunkSize)||begin.chunkSize<=0)throw new Error('上传分块大小无效')
   // The bridge envelope has a byte limit; do not trust a larger server hint.
   const chunkSize=Math.min(begin.chunkSize,32*1024)
   let offset=0
   while(offset<bytes.length){
    if(signal?.aborted)throw attachmentCancelled()
    const part=bytes.subarray(offset,Math.min(bytes.length,offset+chunkSize)),expectedOffset=offset+part.length
    const chunk=await attachmentOperation(attachments.chunk({uploadId,offset,contentBase64:bytesToBase64(part)}),signal)
    if(!Number.isSafeInteger(chunk.nextOffset)||chunk.nextOffset!==expectedOffset)throw new Error('上传分块响应偏移无效')
    offset=chunk.nextOffset;percent=Math.min(99,Math.round(offset/Math.max(1,bytes.length)*100));emit('uploading')
   }
   if(signal?.aborted)throw attachmentCancelled()
   percent=99;emit('processing')
   const item=await attachmentOperation(attachments.commit({uploadId,projectId,sessionId}),signal)
   items.push(item);rememberAttachmentPreview(item.attachmentId,file,previewUrl)
   percent=100;emit('complete',{attachmentId:item.attachmentId})
  }catch(e){
   // Best-effort cleanup must never hold the composer hostage. A commit that
   // already persisted remains available in session attachments.
   if(uploadId)void abortUpload(uploadId)
   const error=signal?.aborted?'附件操作已取消':e instanceof Error?e.message:'上传失败'
   failed.push({name:file.name,error,file});emit(signal?.aborted?'cancelled':'failed',{error})
  }
 }
 return{uploaded:items.length,skipped,attachmentIds:items.map(item=>item.attachmentId),items,failed}
}
