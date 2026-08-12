import { expect, it, vi } from 'vitest'
import { createPlanBridge, type WebViewTransport } from './client'

const U='01ARZ3NDEKTSV4RRFFQ69G5FAV'
it('sends the exact coordination bridge methods and payloads',async()=>{
 let listener:(e:MessageEvent)=>void=()=>{};const sent:any[]=[]
 const run={id:U,planId:U,nodeId:U,role:'planner',todo:{id:U,title:'todo',description:''},status:'queued',depth:0,createdAt:'2026-01-01T00:00:00Z',updatedAt:'2026-01-01T00:00:00Z',version:1}
 const transport:WebViewTransport={addEventListener:(_t,l)=>{listener=l},removeEventListener:vi.fn(),postMessage:m=>{sent.push(m);const payload=(m as any).method==='plan.run.tree'?{items:[run]}:{run,executionStarted:false};queueMicrotask(()=>listener(new MessageEvent('message',{data:{v:'1.0',kind:'response',id:U,requestId:(m as any).id,ok:true,payload}})))}}
 const b=createPlanBridge(transport),create={planId:U,nodeId:U,role:'planner',title:'todo',description:''},spawn={parentRunId:U,nodeId:U,role:'worker',title:'child',description:''}
 await b.createTodo(create);await b.startRun({runId:U});await b.runTree({planId:U});await b.spawnRun(spawn);await b.joinRun({runId:U,mode:'all'});await b.cancelRun({runId:U})
 expect(sent.map(x=>[x.method,x.payload])).toEqual([['plan.todo.create',create],['plan.run.start',{runId:U}],['plan.run.tree',{planId:U}],['plan.run.spawn',spawn],['plan.run.join',{runId:U,mode:'all'}],['plan.run.cancel',{runId:U}]])
})
