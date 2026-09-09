import {expect,it,vi} from 'vitest'
import {createChatBridge,type WebViewTransport} from './client'

const id='01ARZ3NDEKTSV4RRFFQ69G5FAV'
async function usageHarness(){
 let listener:(e:MessageEvent)=>void=()=>{}
 const sent:any[]=[]
 const transport:WebViewTransport={addEventListener:(_name,fn)=>{listener=fn},removeEventListener:vi.fn(),postMessage:message=>{sent.push(message)}}
 const seen:any[]=[]
 const promise=createChatBridge(transport).start({providerId:id,modelId:'model',messages:[{role:'user',content:'test'}]},event=>seen.push(event))
 listener(new MessageEvent('message',{data:{v:'1.0',kind:'response',id,requestId:sent[0].id,ok:true,payload:{streamId:id}}}))
 await promise
 return {seen,emit:(sequence:number,type:string,body:unknown)=>listener(new MessageEvent('message',{data:{v:'1.0',kind:'event',id,streamId:id,sequence,type,...(body as object)}}))}
}

it('accepts legacy usage and optional reported zero, known cache and partial cache without dropping completion',async()=>{
 for(const extras of [{},{cachedInputTokens:40,cacheWriteInputTokens:5,cacheUsageReported:true},{cachedInputTokens:0,cacheWriteInputTokens:0,cacheUsageReported:true},{cachedInputTokens:20,cacheUsageReported:false}]){
  const h=await usageHarness(),usage={inputTokens:50,outputTokens:4,totalTokens:54,...extras}
  h.emit(1,'usage',{usage});h.emit(2,'completed',{})
  expect(h.seen.map(event=>event.type)).toEqual(['usage','completed'])
  expect(h.seen[0].usage).toEqual(usage)
 }
})

it('rejects fabricated cache counters and invalid cache metadata types',async()=>{
 for(const extras of [{cachedInputTokens:-1},{cacheWriteInputTokens:1.5},{cachedInputTokens:'4'},{cacheUsageReported:'yes'},{cachedInputTokens:51},{cachedInputTokens:40,cacheWriteInputTokens:11}]){
  const h=await usageHarness()
  h.emit(1,'usage',{usage:{inputTokens:50,outputTokens:4,totalTokens:54,...extras}})
  expect(h.seen).toHaveLength(1)
  expect(h.seen[0]).toMatchObject({type:'failed',error:{code:'INVALID_BRIDGE_EVENT'}})
 }
})
