import{afterEach,expect,it,vi}from'vitest'
import type{AttachmentBridge}from'../bridge/client'
import{clipboardImages,ingestAttachments,normalizePastedImages,validateAttachmentBatch,prepareAttachmentFiles,attachmentPreview,forgetAttachmentPreview}from'./attachments'

const bytes=(values:number[],name:string,type:string)=>{const data=new Uint8Array(values),file=new File([data],name,{type});Object.defineProperty(file,'arrayBuffer',{value:async()=>data.buffer,configurable:true});return file}

it('normalizes pasted screenshots and accepts only four matching images',()=>{
 const pasted=bytes([0x89,0x50,0x4e,0x47],'image.png','image/png')
 expect(normalizePastedImages([pasted],new Date('2025-01-02T03:04:05Z'))[0].name).toBe('clipboard-20250102-030405Z-1.png')
 const images=Array.from({length:5},(_,i)=>bytes([i],`shot-${i}.png`,'image/png')),{accepted,skipped}=validateAttachmentBatch(images)
 expect(accepted).toHaveLength(4);expect(skipped.join('')).toContain('最多 4 张图片')
})

it('extracts clipboard image items and uploads a selected batch',async()=>{
 const file=bytes([1,2,3],'note.txt','text/plain'),data={items:[{kind:'file',type:'image/png',getAsFile:()=>bytes([1],'image.png','image/png')}],files:[]} as unknown as DataTransfer
 expect(clipboardImages(data)).toHaveLength(1)
 const begin=vi.fn().mockResolvedValue({uploadId:'01ARZ3NDEKTSV4RRFFQ69G5FAB',chunkSize:131072,expiresAt:new Date().toISOString()}),chunk=vi.fn().mockResolvedValue({nextOffset:3}),commit=vi.fn().mockResolvedValue({attachmentId:'01ARZ3NDEKTSV4RRFFQ69G5FAC'}),abort=vi.fn(),attachments={begin,chunk,commit,abort} as unknown as AttachmentBridge
 await ingestAttachments(attachments,'01ARZ3NDEKTSV4RRFFQ69G5FAV','01ARZ3NDEKTSV4RRFFQ69G5FAA',[file])
 expect(begin).toHaveBeenCalledOnce();expect(chunk.mock.calls[0][0]).toMatchObject({offset:0,contentBase64:'AQID'});expect(commit).toHaveBeenCalledOnce()
})

it('uploads a 198 KiB text attachment in transport-safe chunks before committing',async()=>{
 const data=new Uint8Array(198*1024).fill(65),file=new File([data],'large.txt',{type:'text/plain'});Object.defineProperty(file,'arrayBuffer',{value:async()=>data.buffer})
 const chunk=vi.fn().mockImplementation(async payload=>({uploadId:payload.uploadId,nextOffset:payload.offset+atob(payload.contentBase64).length})),commit=vi.fn().mockResolvedValue({attachmentId:'01ARZ3NDEKTSV4RRFFQ69G5FAC'}),attachments={begin:vi.fn().mockResolvedValue({uploadId:'01ARZ3NDEKTSV4RRFFQ69G5FAB',chunkSize:32*1024,expiresAt:new Date().toISOString()}),chunk,commit,abort:vi.fn()} as unknown as AttachmentBridge
 const result=await ingestAttachments(attachments,'01ARZ3NDEKTSV4RRFFQ69G5FAV','01ARZ3NDEKTSV4RRFFQ69G5FAA',[file])
 expect(result.failed).toHaveLength(0);expect(chunk).toHaveBeenCalledTimes(7);expect(chunk.mock.calls.map(call=>call[0].offset)).toEqual([0,32768,65536,98304,131072,163840,196608]);expect(Math.max(...chunk.mock.calls.map(call=>call[0].contentBase64.length))).toBeLessThanOrEqual(43692);expect(commit).toHaveBeenCalledOnce()
})


it.each([0,-1,1.5,Number.MAX_SAFE_INTEGER+1])('aborts an upload with invalid chunk size %s without committing',async chunkSize=>{
 const file=bytes([1,2,3],'note.txt','text/plain'),commit=vi.fn(),abort=vi.fn().mockResolvedValue({aborted:true}),attachments={begin:vi.fn().mockResolvedValue({uploadId:'upload',chunkSize}),chunk:vi.fn(),commit,abort} as unknown as AttachmentBridge
 const result=await ingestAttachments(attachments,'project','session',[file])
 expect(result.failed[0]?.error).toContain('分块大小无效');expect(abort).toHaveBeenCalledOnce();expect(commit).not.toHaveBeenCalled()
})

it('aborts when nextOffset does not exactly match the uploaded part and never commits',async()=>{
 const file=bytes([1,2,3,4],'note.txt','text/plain'),commit=vi.fn(),abort=vi.fn().mockResolvedValue({aborted:true}),attachments={begin:vi.fn().mockResolvedValue({uploadId:'upload',chunkSize:2}),chunk:vi.fn().mockResolvedValue({nextOffset:1}),commit,abort} as unknown as AttachmentBridge
 const result=await ingestAttachments(attachments,'project','session',[file])
 expect(result.failed[0]?.error).toContain('偏移无效');expect(abort).toHaveBeenCalledOnce();expect(commit).not.toHaveBeenCalled()
})

afterEach(()=>{vi.useRealTimers();vi.unstubAllGlobals()})
const uploadBridge=()=>({begin:vi.fn().mockResolvedValue({uploadId:'upload',chunkSize:32768}),chunk:vi.fn().mockImplementation(async p=>({nextOffset:p.offset+atob(p.contentBase64).length})),commit:vi.fn().mockResolvedValue({attachmentId:'uploaded'}),abort:vi.fn().mockResolvedValue({aborted:true})} as unknown as AttachmentBridge)
it('cancels a stuck file read without beginning an upload, then accepts a subsequent file',async()=>{
 const file=bytes([1,2,3],'stuck.txt','text/plain');Object.defineProperty(file,'arrayBuffer',{value:()=>new Promise(()=>{}),configurable:true})
 const controller=new AbortController(),bridge=uploadBridge(),progress=vi.fn()
 const work=ingestAttachments(bridge,'project','session',[file],progress,controller.signal)
 controller.abort()
 expect((await work).failed[0].error).toContain('取消');expect(bridge.begin).not.toHaveBeenCalled()
 expect((await ingestAttachments(bridge,'project','session',[bytes([1],'next.txt','text/plain')])).uploaded).toBe(1)
})
it('settles cancelled in-flight chunks even if cleanup also hangs',async()=>{
 const controller=new AbortController(),bridge=uploadBridge()
 vi.mocked(bridge.chunk).mockImplementation(()=>{controller.abort();return new Promise(()=>{})})
 vi.mocked(bridge.abort).mockImplementation(()=>new Promise(()=>{}))
 const result=await ingestAttachments(bridge,'project','session',[bytes([1],'cancel.txt','text/plain')],undefined,controller.signal)
 expect(result.failed[0].error).toContain('取消');expect(bridge.commit).not.toHaveBeenCalled();expect(bridge.abort).toHaveBeenCalledOnce()
})
it('cleans up a late begin response after cancellation without uploading',async()=>{
 const controller=new AbortController(),bridge=uploadBridge();let resolveBegin!:(value:Awaited<ReturnType<AttachmentBridge['begin']>>)=>void
 vi.mocked(bridge.begin).mockImplementation(()=>{controller.abort();return new Promise(resolve=>{resolveBegin=resolve})})
 const result=await ingestAttachments(bridge,'project','session',[bytes([1],'late.txt','text/plain')],undefined,controller.signal)
 expect(result.failed).toHaveLength(1);resolveBegin({uploadId:'late',chunkSize:32768,expiresAt:new Date().toISOString()});await Promise.resolve();await Promise.resolve()
 expect(bridge.abort).toHaveBeenCalledWith({uploadId:'late',projectId:'project',sessionId:'session'});expect(bridge.chunk).not.toHaveBeenCalled()
})
it('does not leak raw English prepare or upload failures',async()=>{
  vi.stubGlobal('createImageBitmap',vi.fn().mockRejectedValue(new Error('Failed to fetch')))
  const prepared=await prepareAttachmentFiles([new File([new Uint8Array(200*1024)],'large.png',{type:'image/png'})])
  expect(prepared.failed[0]).toBe('large.png（图片处理失败）')
  expect(prepared.failed.join('')).not.toContain('Failed to fetch')
  const begin=vi.fn().mockRejectedValue(new Error('Failed to fetch')),abort=vi.fn()
  const result=await ingestAttachments({begin,chunk:vi.fn(),commit:vi.fn(),abort} as unknown as AttachmentBridge,'project','session',[bytes([1],'note.txt','text/plain')])
  expect(result.failed[0].error).toBe('上传失败')
  expect(result.failed[0].error).not.toContain('Failed to fetch')
})

it('times out image decoding and closes a bitmap arriving after timeout',async()=>{
 vi.useFakeTimers();let resolveBitmap!:(bitmap:ImageBitmap)=>void
 vi.stubGlobal('createImageBitmap',vi.fn(()=>new Promise(resolve=>{resolveBitmap=resolve})))
 const result=prepareAttachmentFiles([new File([new Uint8Array(200*1024)],'large.png',{type:'image/png'})])
 await vi.advanceTimersByTimeAsync(10001)
 expect((await result).failed[0]).toContain('解码超时')
 const close=vi.fn();resolveBitmap({close} as unknown as ImageBitmap);await Promise.resolve()
 expect(close).toHaveBeenCalledOnce()
})
it('uses real data URLs permitted by product CSP instead of blocked blob previews',async()=>{
 const bridge=uploadBridge(),file=bytes([82,73,70,70],'shot.webp','image/webp'),progress=vi.fn()
 await ingestAttachments(bridge,'project','session',[file],progress)
 expect(attachmentPreview('uploaded')?.url).toBe('data:image/webp;base64,UklGRg==')
 expect(progress.mock.calls.at(-1)?.[0].previewUrl).toBe('data:image/webp;base64,UklGRg==')
 forgetAttachmentPreview('uploaded')
})
