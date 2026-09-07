import {expect,it,vi} from 'vitest'
import {capBridgeDeadlineMs,createMcpBridge,type WebViewTransport} from './client'
it('keeps a cold MCP connection pending beyond the former 15s timeout and delivers its result',async()=>{
 vi.useFakeTimers()
 try{
  let listener:(e:MessageEvent)=>void=()=>{}
  const sent:Array<{id:string;method:string;deadlineMs:number}>=[]
  const transport:WebViewTransport={addEventListener:(_t,l)=>{listener=l as (e:MessageEvent)=>void},removeEventListener:vi.fn(),postMessage:m=>{sent.push(m as {id:string;method:string;deadlineMs:number})}}
  const bridge=createMcpBridge(transport)
  const pending=bridge.health({endpointId:'mcp-fixture'})
  expect(sent[0].deadlineMs).toBe(80000)
  await vi.advanceTimersByTimeAsync(60000)
  const payload={state:'ready',driftDetected:false,checkedAt:'2026-09-07T00:00:00Z'}
  listener(new MessageEvent('message',{data:{v:'1.0',kind:'response',id:'01ARZ3NDEKTSV4RRFFQ69G5FAV',requestId:sent[0].id,ok:true,payload}}))
  await expect(pending).resolves.toEqual(payload)
  expect(sent).toHaveLength(1)
 }finally{vi.useRealTimers()}
})
it('extends only setup RPCs, retaining the call and list limits',()=>{
 for(const method of ['mcp.add','mcp.toggle','mcp.health'])expect(capBridgeDeadlineMs(method,80000)).toBe(80000)
 for(const method of ['mcp.invoke','mcp6.invoke','mcp.list'])expect(capBridgeDeadlineMs(method,80000)).toBe(30000)
})
