import{expect,it,vi}from'vitest'
import type{AttachmentBridge}from'../bridge/client'
import{clipboardImages,ingestAttachments,normalizePastedImages,validateAttachmentBatch}from'./attachments'

const bytes=(values:number[],name:string,type:string)=>{const data=new Uint8Array(values),file=new File([data],name,{type});Object.defineProperty(file,'arrayBuffer',{value:async()=>data.buffer});return file}

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
