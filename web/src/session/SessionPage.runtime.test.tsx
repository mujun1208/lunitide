import { act, cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, expect, it, vi } from 'vitest'
import { BridgeClientError, type AttachmentBridge, type ChatBridge, type ChatStream, type MessageBridge, type ProviderBridge, type SessionBridge, type StreamEvent } from '../bridge/client'
import type { MessageDTO, ProjectDTO, ProviderDTO, SessionDTO } from '../generated/bridge'
import { ATTACHMENT_FILE_MAX, SessionPage, persistedExecutionMode } from './SessionPage'
import { resetLiveChatForTests } from './liveChat'

afterEach(()=>{cleanup();resetLiveChatForTests();localStorage.removeItem('lunitide:microphone-device-id')})
const P='01ARZ3NDEKTSV4RRFFQ69G5FAV',S='01ARZ3NDEKTSV4RRFFQ69G5FAA',NOW='2025-01-01T00:00:00Z'
const project:ProjectDTO={id:P,name:'Runtime',projectCode:'ITM00001',type:'implementation',status:'active',createdAt:NOW,updatedAt:NOW,version:1}
const session:SessionDTO={id:S,projectId:P,title:'Session',pinned:false,status:'active',createdAt:NOW,updatedAt:NOW,version:1}
const sessionBridge:SessionBridge={list:vi.fn().mockResolvedValue({items:[session]}),create:vi.fn(),update:vi.fn(),delete:vi.fn()}
const page=(items:MessageDTO[]=[])=>({items,hasMore:false,nextCursor:null,snapshotSequence:items.length})
const provider:ProviderDTO={id:'01ARZ3NDEKTSV4RRFFQ69G5FAB',name:'Ready',protocol:'openai_compatible',baseUrl:'https://example.test',models:[{modelId:'model',displayName:'Model',isDefault:true}],status:'enabled',credentialState:'configured',createdAt:NOW,updatedAt:NOW,version:1}
const providers={list:vi.fn().mockResolvedValue({items:[provider]})} as unknown as ProviderBridge

async function open(props:Partial<React.ComponentProps<typeof SessionPage>>={}){
 const user=userEvent.setup()
 render(<SessionPage project={project} bridge={sessionBridge} messages={{list:vi.fn().mockResolvedValue(page()),append:vi.fn()} as MessageBridge} onBack={vi.fn()} {...props}/>)
 await user.click(await screen.findByText('Session'))
 await screen.findByText('还没有消息')
 return user
}

it('encodes the maximum safe attachment payload and rejects larger files',async()=>{
 const begin=vi.fn().mockResolvedValue({uploadId:'01ARZ3NDEKTSV4RRFFQ69G5FAC',chunkSize:128*1024,expiresAt:NOW}),chunk=vi.fn().mockImplementation(async payload=>({nextOffset:payload.offset+atob(payload.contentBase64).length})),commit=vi.fn().mockResolvedValue({attachmentId:'01ARZ3NDEKTSV4RRFFQ69G5FAD',projectId:P,sessionId:S,originalName:'safe.txt',mime:'text/plain',size:ATTACHMENT_FILE_MAX,sha256:'hash',parseStatus:'succeeded',parseErrorCode:'',parsedTextBytes:ATTACHMENT_FILE_MAX,createdAt:NOW}),abort=vi.fn().mockResolvedValue({aborted:true})
 const attachments={list:vi.fn().mockResolvedValue({items:[]}),ingest:vi.fn(),begin,chunk,commit,abort,get:vi.fn(),delete:vi.fn()} as unknown as AttachmentBridge
 const user=await open({attachments})
 await user.click(screen.getByRole('button',{name:'附件'}))
 const input=document.querySelector('input[type="file"]') as HTMLInputElement
 const bytes=new Uint8Array(ATTACHMENT_FILE_MAX);bytes.fill(0x61)
 const file=new File([bytes],'safe.txt',{type:'text/plain'})
 Object.defineProperty(file,'arrayBuffer',{value:async()=>bytes.buffer})
 await fireEvent.change(input,{target:{files:[file]}})
 await waitFor(()=>expect(commit).toHaveBeenCalledOnce(),{timeout:20_000})
 expect(begin).toHaveBeenCalledWith(expect.objectContaining({projectId:P,sessionId:S,originalName:'safe.txt',size:ATTACHMENT_FILE_MAX,sha256:expect.stringMatching(/^[0-9a-f]{64}$/)}))
 expect(chunk).toHaveBeenCalledTimes(80)
 expect(chunk.mock.calls[0][0]).toMatchObject({uploadId:'01ARZ3NDEKTSV4RRFFQ69G5FAC',offset:0})
 expect(chunk.mock.calls[79][0]).toMatchObject({offset:ATTACHMENT_FILE_MAX-128*1024})
 const oversized=new File([new Uint8Array(ATTACHMENT_FILE_MAX+1)],'too-large.txt',{type:'text/plain'})
 await fireEvent.change(input,{target:{files:[oversized]}})
 expect(await screen.findByRole('status')).toHaveTextContent('已跳过 1 个')
 expect(screen.getByRole('status')).toHaveTextContent('0 个文件')
 expect(begin).toHaveBeenCalledOnce();expect(commit).toHaveBeenCalledOnce();expect(abort).not.toHaveBeenCalled()
 expect(input.value).toBe('')
},25_000)

it('falls back for invalid persisted execution modes',()=>{
 expect(persistedExecutionMode('plan')).toBe('approval')
 expect(persistedExecutionMode('corrupted')).toBe('approval')
 expect(persistedExecutionMode(null)).toBe('full-access')
})

it('blocks Enter re-entry after chat.start resolves, retains one stream, and cancels it',async()=>{
 let onEvent!:(event:StreamEvent)=>void
 const cancel=vi.fn().mockResolvedValue(true),dispose=vi.fn()
 const stream:ChatStream={streamId:'01ARZ3NDEKTSV4RRFFQ69G5FAD',cancel,dispose}
 const start=vi.fn().mockImplementation(async(_payload,onStreamEvent)=>{onEvent=onStreamEvent;return stream})
 const chat:ChatBridge={start,approve:vi.fn(),dispose:vi.fn()}
 const append=vi.fn().mockResolvedValue({})
 const messages:MessageBridge={list:vi.fn().mockResolvedValue(page()),append}
 const user=await open({messages,chat,providers})
 const input=screen.getByLabelText('向月汐提问，或描述你想完成的任务…')
 await user.type(input,'first')
 await user.click(screen.getByRole('button',{name:'↑ 发送并对话'}))
 await waitFor(()=>expect(start).toHaveBeenCalledOnce())
 await user.type(input,'second')
 await user.keyboard('{Enter}')
 expect(start).toHaveBeenCalledOnce()
 expect(append).toHaveBeenCalledOnce()
 await user.click(screen.getByRole('button',{name:'停止'}))
 expect(cancel).toHaveBeenCalledOnce()
 await act(async()=>onEvent({v:'1.0',kind:'event',id:'01ARZ3NDEKTSV4RRFFQ69G5FAE',streamId:stream.streamId,sequence:1,type:'delta',delta:{text:'## 实时标题\n\n- 第一项\n- 第二项'}}))
 const response=screen.getByRole('heading',{name:'实时标题'})
 expect(response.closest('.bubble')).toHaveTextContent('实时标题 第一项 第二项')
 expect(response.closest('.bubble')).toHaveClass('message-body')
 await act(async()=>onEvent({v:'1.0',kind:'event',id:'01ARZ3NDEKTSV4RRFFQ69G5FAF',streamId:stream.streamId,sequence:2,type:'cancelled'}))
 expect(screen.getByRole('status')).toHaveTextContent('已取消')
 expect(start).toHaveBeenCalledOnce()
 expect(dispose).not.toHaveBeenCalled()
})


it('shows only attachments bound to the current session and cannot delete hidden attachments',async()=>{
 const other='01ARZ3NDEKTSV4RRFFQ69G5FAZ',remove=vi.fn().mockResolvedValue({deleted:true})
 const make=(attachmentId:string,sessionId?:string,originalName='file.txt')=>({attachmentId,projectId:P,sessionId,originalName,mime:'text/plain',size:1,sha256:'hash',parseStatus:'succeeded' as const,parseErrorCode:'',parsedTextBytes:1,createdAt:NOW})
 const attachments={list:vi.fn().mockResolvedValue({items:[make('01ARZ3NDEKTSV4RRFFQ69G5FAC',S,'mine.txt'),make('01ARZ3NDEKTSV4RRFFQ69G5FAD',other,'other.txt'),make('01ARZ3NDEKTSV4RRFFQ69G5FAE',undefined,'shared.txt')]}),ingest:vi.fn(),get:vi.fn(),delete:remove} as unknown as AttachmentBridge
 const user=await open({attachments});await user.click(screen.getByRole('button',{name:'附件'}))
 expect(await screen.findByText('mine.txt')).toBeInTheDocument();expect(screen.queryByText('other.txt')).toBeNull();expect(screen.queryByText('shared.txt')).toBeNull();expect(screen.getAllByRole('button',{name:'删除'})).toHaveLength(1);expect(remove).not.toHaveBeenCalled()
})

it('switches models inside a reopened historical personal session',async()=>{
 const second:ProviderDTO={...provider,id:'01ARZ3NDEKTSV4RRFFQ69G5FBA',name:'Second',models:[{modelId:'model-b',displayName:'Model B',isDefault:true}]}
 const start=vi.fn().mockResolvedValue({cancel:vi.fn(),dispose:vi.fn()})
 const user=await open({personal:true,initialSession:session,providers:{list:vi.fn().mockResolvedValue({items:[provider,second]})} as unknown as ProviderBridge,chat:{start,approve:vi.fn(),dispose:vi.fn()} as ChatBridge,messages:{list:vi.fn().mockResolvedValue(page()),append:vi.fn().mockResolvedValue({})} as MessageBridge})
 const button=await screen.findByRole('button',{name:'已配置模型'})
 expect(button).toHaveTextContent('Model')
 await user.click(button)
 await user.click(await screen.findByRole('menuitem',{name:/Model B/}))
 await waitFor(()=>expect(screen.getByRole('button',{name:'已配置模型'})).toHaveTextContent('Model B'))
 const input=screen.getByLabelText('向月汐提问，或描述你想完成的任务…')
 await user.type(input,'你好')
 await user.click(screen.getByRole('button',{name:'↑ 发送并对话'}))
 await waitFor(()=>expect(start).toHaveBeenCalledOnce())
 expect(start.mock.calls[0][0]).toMatchObject({providerId:second.id,modelId:'model-b'})
})

it('does not expose chat for disabled, unconfigured, or model-less providers and rejects invalid initial selection',async()=>{
 const bad=[{...provider,status:'disabled' as const},{...provider,id:'01ARZ3NDEKTSV4RRFFQ69G5FAC',credentialState:'missing' as const},{...provider,id:'01ARZ3NDEKTSV4RRFFQ69G5FAD',models:[]}]
 const providerBridge={list:vi.fn().mockResolvedValue({items:bad})} as unknown as ProviderBridge,start=vi.fn(),chat={start,approve:vi.fn(),dispose:vi.fn()} as ChatBridge
 await open({providers:providerBridge,chat,initialProviderId:provider.id,initialModelId:'model'})
 expect(await screen.findByText(/配置并启用至少一个模型/)).toBeInTheDocument();expect(screen.queryByRole('button',{name:'↑ 发送并对话'})).toBeNull();expect(start).not.toHaveBeenCalled()
})

it('preserves near-limit user text and sends auto-edit mode through chat.start',async()=>{
 const append=vi.fn().mockResolvedValue({}),start=vi.fn().mockResolvedValue({cancel:vi.fn(),dispose:vi.fn()}),messages={list:vi.fn().mockResolvedValue(page()),append} as MessageBridge
 const user=await open({messages,chat:{start,approve:vi.fn(),dispose:vi.fn()},providers,personal:true,initialSession:session})
 expect(await screen.findByRole('button',{name:'已配置模型'})).toHaveTextContent('Model')
 const providerCalls=vi.mocked(providers.list).mock.calls.length;await user.click(screen.getByRole('button',{name:'已配置模型'}));await waitFor(()=>expect(vi.mocked(providers.list).mock.calls.length).toBeGreaterThan(providerCalls));await user.click(screen.getByRole('button',{name:'已配置模型'}))
 expect(screen.queryByLabelText('供应商')).toBeNull();expect(screen.queryByLabelText('模型')).toBeNull()
 await user.click(screen.getByRole('button',{name:'执行模式'}));await user.click(screen.getByRole('button',{name:/自动审批/}))
 expect(localStorage.getItem(`lunitide:execution-mode:${S}`)).toBe('auto-edit')
 const raw='界'.repeat(2048);fireEvent.change(screen.getByLabelText('向月汐提问，或描述你想完成的任务…'),{target:{value:raw}})
 await user.click(screen.getByRole('button',{name:'↑ 发送并对话'}))
 await waitFor(()=>expect(start).toHaveBeenCalled());expect(vi.mocked(append).mock.calls[0][0].text).toBe(raw);expect(start.mock.calls[0][0]).toMatchObject({sessionId:S,executionMode:'auto-edit'})
})

it('offers personal composer context actions',async()=>{
 const user=await open({personal:true,providers,initialSession:session})
 await user.click(screen.getByRole('button',{name:'添加上下文'}));expect(screen.getByRole('button',{name:/附件 \/ 文件/})).toBeInTheDocument();expect(screen.getByRole('button',{name:/上传文件夹/})).toBeInTheDocument()
 await user.click(screen.getByRole('button',{name:/@ 上下文/}));expect(screen.getByLabelText('向月汐提问，或描述你想完成的任务…')).toHaveValue('@')
})

it('uploads multiple dropped files and a pasted screenshot from the composer',async()=>{
 let upload=0;const begin=vi.fn().mockImplementation(async()=>({uploadId:`01ARZ3NDEKTSV4RRFFQ69G5F${String.fromCharCode(65+(upload++))}`,chunkSize:128*1024,expiresAt:NOW})),chunk=vi.fn().mockImplementation(async payload=>({nextOffset:payload.offset+atob(payload.contentBase64).length})),commit=vi.fn().mockImplementation(async()=>({attachmentId:`attachment-${upload}`})),attachments={list:vi.fn().mockResolvedValue({items:[]}),ingest:vi.fn(),begin,chunk,commit,abort:vi.fn().mockResolvedValue({aborted:true}),get:vi.fn(),delete:vi.fn()} as unknown as AttachmentBridge
 await open({personal:true,providers,initialSession:session,attachments})
 const make=(name:string,type:string)=>{const bytes=new Uint8Array([1,2,3]),file=new File([bytes],name,{type});Object.defineProperty(file,'arrayBuffer',{value:async()=>bytes.buffer});return file},composer=document.querySelector('.message-input') as HTMLElement,dataTransfer={types:['Files'],files:[make('a.txt','text/plain'),make('b.md','text/markdown')],dropEffect:'none'}
 fireEvent.dragEnter(composer,{dataTransfer});expect(composer).toHaveClass('is-dragging');fireEvent.drop(composer,{dataTransfer});await waitFor(()=>expect(commit).toHaveBeenCalledTimes(2));expect(composer).not.toHaveClass('is-dragging');expect(screen.getByText(/a\.txt/).closest('.attachment-card')).toHaveTextContent('等待随下一条消息发送');expect(screen.getByRole('button',{name:'移除附件 a.txt'})).toBeInTheDocument()
 const image=make('image.png','image/png'),clipboardData={items:[{kind:'file',type:'image/png',getAsFile:()=>image}],files:[image]}
 fireEvent.paste(screen.getByLabelText('向月汐提问，或描述你想完成的任务…'),{clipboardData});await waitFor(()=>expect(commit).toHaveBeenCalledTimes(3));expect(begin.mock.calls[2][0].originalName).toMatch(/^clipboard-.*\.png$/)
 const picker=document.querySelector('.message-actions input[type="file"]:not([webkitdirectory])') as HTMLInputElement;expect(picker.multiple).toBe(true);expect(picker.accept).toContain('image/png')
})

it('waits for initial attachments, then includes their ids in the first chat context',async()=>{
 let finish!:(value:{nextOffset:number})=>void
 const data=new Uint8Array([1,2,3]),file=new File([data],'initial.txt',{type:'text/plain'});Object.defineProperty(file,'arrayBuffer',{value:async()=>data.buffer})
 const chunk=vi.fn().mockImplementation(()=>new Promise(resolve=>{finish=resolve})),commit=vi.fn().mockResolvedValue({attachmentId:'attachment-initial'}),attachments={list:vi.fn().mockResolvedValue({items:[]}),begin:vi.fn().mockResolvedValue({uploadId:'upload',chunkSize:3}),chunk,commit,abort:vi.fn(),get:vi.fn(),delete:vi.fn()} as unknown as AttachmentBridge
 const start=vi.fn().mockResolvedValue({cancel:vi.fn(),dispose:vi.fn()}),append=vi.fn().mockResolvedValue({})
 await open({personal:true,initialSession:session,initialPrompt:'首条问题',initialUploadFiles:[file],attachments,providers,chat:{start,dispose:vi.fn()},messages:{list:vi.fn().mockResolvedValue(page()),append} as MessageBridge})
 await waitFor(()=>expect(chunk).toHaveBeenCalledOnce());expect(start).not.toHaveBeenCalled();finish({nextOffset:3})
 await waitFor(()=>expect(start).toHaveBeenCalledOnce());expect(start.mock.calls[0][0]).toMatchObject({contextRefs:[{type:'attachment',id:'attachment-initial'}]})
})

it('keeps the initial prompt and does not auto-send when an initial attachment fails',async()=>{
 const data=new Uint8Array([1]),file=new File([data],'broken.txt',{type:'text/plain'});Object.defineProperty(file,'arrayBuffer',{value:async()=>data.buffer})
 const attachments={list:vi.fn().mockResolvedValue({items:[]}),begin:vi.fn().mockResolvedValue({uploadId:'upload',chunkSize:1}),chunk:vi.fn().mockRejectedValue(new Error('network failed')),commit:vi.fn(),abort:vi.fn().mockResolvedValue({aborted:true}),get:vi.fn(),delete:vi.fn()} as unknown as AttachmentBridge,start=vi.fn()
 await open({personal:true,initialSession:session,initialPrompt:'保留的问题',initialUploadFiles:[file],attachments,providers,chat:{start,dispose:vi.fn()}})
 expect(await screen.findByText(/初始附件上传失败或被跳过/)).toBeInTheDocument();expect(screen.getByLabelText('向月汐提问，或描述你想完成的任务…')).toHaveValue('保留的问题');expect(start).not.toHaveBeenCalled()
})

it('retries a failed initial attachment and continues with its context id',async()=>{
 const data=new Uint8Array([1]),file=new File([data],'retry.txt',{type:'text/plain'});Object.defineProperty(file,'arrayBuffer',{value:async()=>data.buffer})
 const chunk=vi.fn().mockRejectedValueOnce(new Error('temporary failure')).mockResolvedValue({nextOffset:1}),attachments={list:vi.fn().mockResolvedValue({items:[]}),begin:vi.fn().mockResolvedValue({uploadId:'upload',chunkSize:1}),chunk,commit:vi.fn().mockResolvedValue({attachmentId:'attachment-retried'}),abort:vi.fn().mockResolvedValue({aborted:true}),get:vi.fn(),delete:vi.fn()} as unknown as AttachmentBridge,start=vi.fn().mockResolvedValue({cancel:vi.fn(),dispose:vi.fn()}),append=vi.fn().mockResolvedValue({}),user=await open({personal:true,initialSession:session,initialPrompt:'解读它',initialUploadFiles:[file],attachments,providers,chat:{start,dispose:vi.fn()},messages:{list:vi.fn().mockResolvedValue(page()),append} as MessageBridge})
 await user.click(await screen.findByRole('button',{name:'重试附件上传并继续提问'}))
 await waitFor(()=>expect(start).toHaveBeenCalledOnce());expect(start.mock.calls[0][0]).toMatchObject({contextRefs:[{type:'attachment',id:'attachment-retried'}]});expect(append).toHaveBeenCalledOnce()
})

it('collapses streaming thinking by default, expands on demand and shows a live status line',async()=>{
 let onEvent!:(event:StreamEvent)=>void
 const start=vi.fn().mockImplementation(async(_payload,onStreamEvent)=>{onEvent=onStreamEvent;return{streamId:'01ARZ3NDEKTSV4RRFFQ69G5FAD',cancel:vi.fn(),dispose:vi.fn()}})
 const user=await open({messages:{list:vi.fn().mockResolvedValue(page()),append:vi.fn().mockResolvedValue({})} as MessageBridge,chat:{start,dispose:vi.fn()},providers})
 await user.type(screen.getByLabelText('向月汐提问，或描述你想完成的任务…'),'分析')
 await user.click(screen.getByRole('button',{name:'↑ 发送并对话'}));await waitFor(()=>expect(start).toHaveBeenCalledOnce())
 await act(async()=>onEvent({v:'1.0',kind:'event',id:'01ARZ3NDEKTSV4RRFFQ69G5FAE',streamId:'01ARZ3NDEKTSV4RRFFQ69G5FAD',sequence:1,type:'thinking',thinking:{text:'**内部推理**'}}))
 const details=screen.getByText('任务过程').closest('details')!
 expect(details).not.toHaveAttribute('open')
 expect(screen.getByText('正在思考…')).toBeInTheDocument()
 await user.click(screen.getByText('任务过程'))
 expect(details).toHaveAttribute('open');expect(screen.getByText('内部推理').tagName).toBe('STRONG')
 await act(async()=>onEvent({v:'1.0',kind:'event',id:'01ARZ3NDEKTSV4RRFFQ69G5FAF',streamId:'01ARZ3NDEKTSV4RRFFQ69G5FAD',sequence:2,type:'delta',delta:{text:'最终答案'}}))
 expect(screen.getByText('最终答案')).toBeInTheDocument()
})

it('can navigate from the bottom to an earlier round without pending auto-follow undoing it',async()=>{
 const first:MessageDTO={id:'01ARZ3NDEKTSV4RRFFQ69G5FAC',sessionId:S,role:'user',text:'第一轮',status:'completed',sequence:1,createdAt:NOW},second:MessageDTO={...first,id:'01ARZ3NDEKTSV4RRFFQ69G5FAD',text:'第二轮',sequence:2}
 const messages={list:vi.fn().mockResolvedValue(page([first,second])),append:vi.fn()} as MessageBridge
 render(<SessionPage project={project} bridge={sessionBridge} messages={messages} onBack={vi.fn()} personal initialSession={session}/>)
 await screen.findByText('第一轮')
 const box=document.querySelector('.conversation-scroll') as HTMLDivElement,rounds=[...document.querySelectorAll<HTMLElement>('[data-round-id]')],scrollTo=vi.fn()
 Object.defineProperties(box,{scrollTop:{value:900,writable:true},scrollHeight:{value:1200},clientHeight:{value:300}});Object.defineProperty(box,'scrollTo',{value:scrollTo});Object.defineProperty(box,'getBoundingClientRect',{value:()=>({top:100} as DOMRect)});Object.defineProperty(rounds[0],'getBoundingClientRect',{value:()=>({top:-700} as DOMRect)})
 await userEvent.click(screen.getByRole('button',{name:'定位到第 1 轮'}))
 expect(scrollTo).toHaveBeenLastCalledWith({top:88,behavior:'smooth'});expect(screen.getByRole('button',{name:'定位到第 1 轮'})).toHaveAttribute('aria-current','step');expect(screen.getByRole('button',{name:'回到最新消息'})).toBeInTheDocument()
})

it('opens and closes the personal workspace without unmounting conversation or composer',async()=>{
 const attachments={list:vi.fn().mockResolvedValue({items:[]}),get:vi.fn(),ingest:vi.fn(),delete:vi.fn()} as unknown as AttachmentBridge
 const user=await open({personal:true,initialSession:session,attachments})
 const composer=screen.getByLabelText('向月汐提问，或描述你想完成的任务…')
 await user.type(composer,'draft')
 await user.click(screen.getByRole('button',{name:'展开右侧工作区'}));expect(screen.getByLabelText('统一工作区')).toBeInTheDocument();expect(composer).toHaveValue('draft')
 await user.click(screen.getByRole('button',{name:'收起右侧工作区'}));expect(screen.queryByLabelText('统一工作区')).toBeNull();expect(composer).toHaveValue('draft')
})

it('automatically opens the terminal workspace for command tool activity',async()=>{
 let onEvent!:(event:StreamEvent)=>void
 const stream:ChatStream={streamId:'01ARZ3NDEKTSV4RRFFQ69G5FAD',cancel:vi.fn().mockResolvedValue(true),dispose:vi.fn()}
 const start=vi.fn().mockImplementation(async(_payload,onStreamEvent)=>{onEvent=onStreamEvent;return stream})
 const attachments={list:vi.fn().mockResolvedValue({items:[]}),get:vi.fn(),ingest:vi.fn(),delete:vi.fn()} as unknown as AttachmentBridge
 const user=await open({personal:true,initialSession:session,attachments,providers,chat:{start,approve:vi.fn(),dispose:vi.fn()}})
 await user.type(screen.getByLabelText('向月汐提问，或描述你想完成的任务…'),'运行测试')
 await user.click(screen.getByRole('button',{name:'↑ 发送并对话'}))
 await waitFor(()=>expect(start).toHaveBeenCalledOnce())
 await act(async()=>onEvent({v:'1.0',kind:'event',id:'01ARZ3NDEKTSV4RRFFQ69G5FAE',streamId:stream.streamId,sequence:1,type:'tool_started',tool:{callId:'call-1',name:'command.run',argsDigest:'digest',summary:'go test ./...'}}))
 expect(screen.getByLabelText('统一工作区')).toBeInTheDocument()
 expect(screen.getByRole('tab',{name:'终端'})).toHaveAttribute('aria-selected','true')
 expect(screen.getByLabelText('命令调用情况')).toHaveTextContent('go test ./...')
})

it('keeps tool activity inside the task process and follows the growing stream',async()=>{
 let onEvent!:(event:StreamEvent)=>void
 const start=vi.fn().mockImplementation(async(_payload,onStreamEvent)=>{onEvent=onStreamEvent;return{streamId:'01ARZ3NDEKTSV4RRFFQ69G5FAD',cancel:vi.fn(),dispose:vi.fn()}})
 const user=await open({messages:{list:vi.fn().mockResolvedValue(page()),append:vi.fn().mockResolvedValue({})} as MessageBridge,chat:{start,dispose:vi.fn()},providers})
 const box=document.querySelector('.conversation-scroll') as HTMLDivElement,scrollTo=vi.fn()
 Object.defineProperties(box,{scrollHeight:{value:900},clientHeight:{value:300},scrollTop:{value:600,writable:true}});Object.defineProperty(box,'scrollTo',{value:scrollTo})
 await user.type(screen.getByLabelText('向月汐提问，或描述你想完成的任务…'),'读取文件');await user.click(screen.getByRole('button',{name:'↑ 发送并对话'}));await waitFor(()=>expect(start).toHaveBeenCalledOnce())
 scrollTo.mockClear();await act(async()=>onEvent({v:'1.0',kind:'event',id:'01ARZ3NDEKTSV4RRFFQ69G5FAE',streamId:'01ARZ3NDEKTSV4RRFFQ69G5FAD',sequence:1,type:'tool_started',tool:{callId:'read-1',name:'fs.read',argsDigest:'a'.repeat(64),summary:'README.md'}}))
 const details=screen.getByText('任务过程').closest('details')!;expect(details).toHaveTextContent('读取');expect(details).toHaveTextContent('README.md');await waitFor(()=>expect(scrollTo).toHaveBeenCalledWith({top:900,behavior:'auto'}))
})

it('opens image selection from share instead of showing a permanent toolbar',async()=>{
 const first:MessageDTO={id:'01ARZ3NDEKTSV4RRFFQ69G5FAC',sessionId:S,role:'user',text:'第一条',status:'completed',sequence:1,createdAt:NOW},second:MessageDTO={...first,id:'01ARZ3NDEKTSV4RRFFQ69G5FAD',role:'assistant',text:'第二条',sequence:2}
 const user=userEvent.setup(),messageApi={list:vi.fn().mockResolvedValue(page([first,second])),append:vi.fn()} as MessageBridge
 render(<SessionPage project={project} bridge={sessionBridge} messages={messageApi} onBack={vi.fn()}/>);await user.click(await screen.findByText('Session'));await screen.findByText('第一条')
 expect(screen.queryByRole('button',{name:'生成图片'})).toBeNull();expect(screen.getByLabelText('月汐回复')).toBeInTheDocument()
 await user.click(screen.getByLabelText('助手消息操作').querySelectorAll('button')[1]);expect(screen.getByText('已选择 1 条')).toBeInTheDocument();expect(screen.getByRole('checkbox',{name:'选择助手消息 2'})).toBeChecked()
 await user.click(screen.getByRole('button',{name:'全选当前已加载'}));expect(screen.getByText('已选择 2 条')).toBeInTheDocument();await user.click(screen.getByRole('button',{name:'生成图片'}));const dialog=screen.getByRole('dialog',{name:'生成对话图片'});expect(dialog).toHaveTextContent('第一条');expect(dialog).toHaveTextContent('第二条')
})

it('shows copy success feedback',async()=>{
 const message:MessageDTO={id:'01ARZ3NDEKTSV4RRFFQ69G5FAC',sessionId:S,role:'assistant',text:'可复制内容',status:'completed',sequence:1,createdAt:NOW},writeText=vi.fn().mockResolvedValue(undefined)
 const user=userEvent.setup();Object.defineProperty(navigator.clipboard,'writeText',{configurable:true,value:writeText});render(<SessionPage project={project} bridge={sessionBridge} initialSession={session} personal messages={{list:vi.fn().mockResolvedValue(page([message])),append:vi.fn()} as MessageBridge} onBack={vi.fn()}/>)
 await screen.findByText('可复制内容');await user.click(screen.getByRole('button',{name:'复制'}));expect(writeText).toHaveBeenCalledWith('可复制内容');expect(await screen.findByRole('status')).toHaveTextContent('复制成功')
})

it('rewinds an assistant answer and automatically asks the original question again',async()=>{
 const question:MessageDTO={id:'01ARZ3NDEKTSV4RRFFQ69G5FAC',sessionId:S,role:'user',text:'原来的问题',status:'completed',sequence:1,createdAt:NOW},answer:MessageDTO={...question,id:'01ARZ3NDEKTSV4RRFFQ69G5FAD',role:'assistant',text:'旧回答',sequence:2},rewind=vi.fn().mockResolvedValue({sessionId:S,messageId:question.id,deletedCount:2,lastSequence:0,historyRevision:2}),append=vi.fn().mockResolvedValue({}),start=vi.fn().mockResolvedValue({streamId:'01ARZ3NDEKTSV4RRFFQ69G5FAE',cancel:vi.fn(),dispose:vi.fn()}),list=vi.fn().mockResolvedValue(page([question,answer]))
 const user=userEvent.setup();render(<SessionPage project={project} bridge={sessionBridge} initialSession={session} personal providers={providers} chat={{start,dispose:vi.fn()}} messages={{list,append,rewind} as MessageBridge} onBack={vi.fn()}/>)
 await screen.findByText('旧回答');await user.click(screen.getByLabelText('助手消息操作').querySelector('button:last-child')!);expect(screen.getByRole('dialog')).toHaveTextContent('清除旧回答并重新生成')
 await user.click(screen.getByRole('button',{name:'重新生成'}));await waitFor(()=>expect(rewind).toHaveBeenCalledWith(expect.objectContaining({sessionId:S,messageId:question.id}),expect.anything()));await waitFor(()=>expect(append).toHaveBeenCalledWith(expect.objectContaining({sessionId:S,text:'原来的问题'}),expect.anything()));expect(start).toHaveBeenCalledWith(expect.objectContaining({sessionId:S}),expect.any(Function))
})

it('deletes the empty session and returns home after removing its last round',async()=>{
 const question:MessageDTO={id:'01ARZ3NDEKTSV4RRFFQ69G5FAC',sessionId:S,role:'user',text:'最后一个问题',status:'completed',sequence:1,createdAt:NOW},answer:MessageDTO={...question,id:'01ARZ3NDEKTSV4RRFFQ69G5FAD',role:'assistant',text:'最后一个回答',sequence:2},rewind=vi.fn().mockResolvedValue({sessionId:S,messageId:question.id,deletedCount:2,lastSequence:0,historyRevision:2}),remove=vi.fn().mockResolvedValue({deleted:true,id:S}),onDeleted=vi.fn()
 const user=userEvent.setup();render(<SessionPage project={project} bridge={{...sessionBridge,delete:remove}} initialSession={session} personal messages={{list:vi.fn().mockResolvedValue(page([question,answer])),append:vi.fn(),rewind}as MessageBridge} onBack={vi.fn()} onDeleted={onDeleted}/>)
 await screen.findByText('最后一个回答');await user.click(screen.getByRole('button',{name:'删除'}));await user.click(screen.getByRole('button',{name:'确认删除'}));await waitFor(()=>expect(remove).toHaveBeenCalledWith({id:S},expect.objectContaining({attempt:expect.any(Object)})));expect(onDeleted).toHaveBeenCalledWith(S)
})

it('refreshes newly configured models when the provider revision changes',async()=>{
 const added:ProviderDTO={...provider,models:[...provider.models,{modelId:'new-model',displayName:'New Model',isDefault:false}],version:2}
 const list=vi.fn().mockResolvedValueOnce({items:[provider]}).mockResolvedValue({items:[added]})
 const props={project,bridge:sessionBridge,initialSession:session,personal:true,providers:{list} as unknown as ProviderBridge,chat:{start:vi.fn(),dispose:vi.fn()} as ChatBridge,messages:{list:vi.fn().mockResolvedValue(page()),append:vi.fn()} as MessageBridge,onBack:vi.fn()}
 const user=userEvent.setup(),view=render(<SessionPage {...props} providersRevision={0}/>);await screen.findByText('还没有消息')
 view.rerender(<SessionPage {...props} providersRevision={1}/>);await waitFor(()=>expect(list).toHaveBeenCalledTimes(2))
 await user.click(screen.getByRole('button',{name:'已配置模型'}));expect(screen.getByRole('menuitem',{name:/New Model/})).toBeInTheDocument()
})

it('waits for the latest provider list before opening a historical chat model menu',async()=>{
 const added:ProviderDTO={...provider,models:[...provider.models,{modelId:'latest-model',displayName:'Latest Model',isDefault:false}],version:2}
 let resolveLatest!:(value:{items:ProviderDTO[]})=>void
 const list=vi.fn().mockResolvedValueOnce({items:[provider]}).mockImplementationOnce(()=>new Promise(resolve=>{resolveLatest=resolve}))
 const user=await open({personal:true,initialSession:session,providers:{list} as unknown as ProviderBridge,chat:{start:vi.fn(),dispose:vi.fn()}})
 const button=await screen.findByRole('button',{name:'已配置模型'});await user.click(button);expect(screen.queryByRole('menu')).toBeNull();resolveLatest({items:[added]});expect(await screen.findByRole('menuitem',{name:/Latest Model/})).toBeInTheDocument()
})

it('does not open a stale historical model menu when refreshing providers fails',async()=>{
 const list=vi.fn().mockResolvedValueOnce({items:[provider]}).mockRejectedValueOnce(new BridgeClientError('模型列表格式无效','INVALID_BRIDGE_RESULT',false,'test'))
 const user=await open({personal:true,initialSession:session,providers:{list} as unknown as ProviderBridge,chat:{start:vi.fn(),dispose:vi.fn()}})
 await user.click(await screen.findByRole('button',{name:'已配置模型'}));expect(screen.queryByRole('menu')).toBeNull();expect(await screen.findByText('模型列表格式无效')).toBeInTheDocument()
})

it('keeps an icon retry action on the completed live response',async()=>{
 let onEvent!:(event:StreamEvent)=>void
 const question:MessageDTO={id:'01ARZ3NDEKTSV4RRFFQ69G5FAC',sessionId:S,role:'user',text:'再回答一次',status:'completed',sequence:1,createdAt:NOW}
 const start=vi.fn().mockImplementation(async(_payload,onStreamEvent)=>{onEvent=onStreamEvent;return{streamId:'01ARZ3NDEKTSV4RRFFQ69G5FAD',cancel:vi.fn(),dispose:vi.fn()}})
 const user=userEvent.setup();render(<SessionPage project={project} bridge={sessionBridge} personal providers={providers} initialSession={session} chat={{start,dispose:vi.fn()}} messages={{list:vi.fn().mockResolvedValue(page([question])),append:vi.fn().mockResolvedValue({}),rewind:vi.fn()} as MessageBridge} onBack={vi.fn()}/>);await screen.findByText('再回答一次')
 await user.type(screen.getByLabelText('向月汐提问，或描述你想完成的任务…'),'继续');await user.click(screen.getByRole('button',{name:'↑ 发送并对话'}));await waitFor(()=>expect(start).toHaveBeenCalledOnce())
 act(()=>{onEvent({v:'1.0',kind:'event',id:'01ARZ3NDEKTSV4RRFFQ69G5FAE',streamId:'01ARZ3NDEKTSV4RRFFQ69G5FAD',sequence:1,type:'delta',delta:{text:'新回答'}});onEvent({v:'1.0',kind:'event',id:'01ARZ3NDEKTSV4RRFFQ69G5FAF',streamId:'01ARZ3NDEKTSV4RRFFQ69G5FAD',sequence:2,type:'completed'})})
 const actions=screen.getByLabelText('当前助手回复操作');expect(actions.querySelector('button[aria-label="重试"] svg')).not.toBeNull();expect(actions).not.toHaveTextContent('重试')
})

it('automatically opens a sandboxed browser preview only after a completed HTML artifact',async()=>{
 let onEvent!:(event:StreamEvent)=>void
 const stream:ChatStream={streamId:'01ARZ3NDEKTSV4RRFFQ69G5FAD',cancel:vi.fn().mockResolvedValue(true),dispose:vi.fn()}
 const start=vi.fn().mockImplementation(async(_payload,onStreamEvent)=>{onEvent=onStreamEvent;return stream})
 const user=await open({personal:true,initialSession:session,providers,chat:{start,dispose:vi.fn()},attachments:{list:vi.fn().mockResolvedValue({items:[]}),get:vi.fn(),ingest:vi.fn(),delete:vi.fn()} as unknown as AttachmentBridge})
 await user.type(screen.getByLabelText('向月汐提问，或描述你想完成的任务…'),'生成网页')
 await user.click(screen.getByRole('button',{name:'↑ 发送并对话'}))
 await waitFor(()=>expect(start).toHaveBeenCalledOnce())
 await act(async()=>onEvent({v:'1.0',kind:'event',id:'01ARZ3NDEKTSV4RRFFQ69G5FAE',streamId:stream.streamId,sequence:1,type:'tool_completed',tool:{callId:'call-html',name:'workspace.write',argsDigest:'a'.repeat(64),summary:'wrote index.html',artifact:{kind:'html',path:'index.html',content:'<h1>网页</h1>'}}}))
 expect(screen.getByRole('tab',{name:'浏览器'})).toHaveAttribute('aria-selected','true')
 expect(screen.getByTitle('HTML 预览 index.html')).toHaveAttribute('sandbox','')
})


it('closes the attachment menu as soon as the file picker is opened',async()=>{
 const user=await open({personal:true,providers,initialSession:session,attachments:{list:vi.fn().mockResolvedValue({items:[]}),get:vi.fn(),ingest:vi.fn(),delete:vi.fn()} as unknown as AttachmentBridge})
 await user.click(screen.getByRole('button',{name:'添加上下文'}))
 const picker=document.querySelector('.message-actions input[type="file"]:not([webkitdirectory])') as HTMLInputElement
 const click=vi.spyOn(picker,'click').mockImplementation(()=>{})
 await user.click(screen.getByRole('button',{name:/附件 \/ 文件/}))
 expect(click).toHaveBeenCalledOnce();expect(screen.queryByRole('button',{name:/上传文件夹/})).toBeNull();expect(screen.getByRole('button',{name:'添加上下文'})).toHaveAttribute('aria-expanded','false')
})

it('fills recognized speech into the composer and exposes recording state',async()=>{
 let instance:any
 class Recognition{lang='';continuous=false;interimResults=false;onresult:any=null;onerror:any=null;onend:any=null;start=vi.fn();stop=vi.fn(()=>this.onend?.());constructor(){instance=this}}
 ;(window as any).SpeechRecognition=Recognition
 const stop=vi.fn(),getUserMedia=vi.fn().mockResolvedValue({getTracks:()=>[{stop}]}),getByteFrequencyData=vi.fn((samples:Uint8Array)=>samples.fill(128)),close=vi.fn().mockResolvedValue(undefined)
 Object.defineProperty(navigator,'mediaDevices',{configurable:true,value:{getUserMedia}})
 ;(window as any).AudioContext=class{createAnalyser(){return{fftSize:0,smoothingTimeConstant:0,frequencyBinCount:16,getByteFrequencyData}}createMediaStreamSource(){return{connect:vi.fn()}}close=close}
 const user=await open({personal:true,providers,initialSession:session})
 const button=screen.getByRole('button',{name:'语音输入'});await user.click(button);expect(getUserMedia).toHaveBeenCalledWith({audio:true});expect(instance.start).toHaveBeenCalledOnce();expect(screen.getByRole('button',{name:'停止语音输入'})).toHaveAttribute('aria-pressed','true');expect(screen.getByRole('status',{name:'正在接收麦克风声音'})).toBeInTheDocument()
 act(()=>instance.onresult({results:[{0:{transcript:'语音内容'},isFinal:true}]}));expect(screen.getByLabelText('向月汐提问，或描述你想完成的任务…')).toHaveValue('语音内容')
 await user.click(screen.getByRole('button',{name:'停止语音输入'}));expect(instance.stop).toHaveBeenCalledOnce();expect(stop).toHaveBeenCalled();expect(close).toHaveBeenCalled();delete (window as any).SpeechRecognition;delete (window as any).AudioContext
})

it('uses the selected microphone and keeps voice beside send',async()=>{
 localStorage.setItem('lunitide:microphone-device-id','usb-mic')
 class Recognition{lang='';continuous=false;interimResults=false;onresult:any=null;onerror:any=null;onend:any=null;start=vi.fn();stop=vi.fn()}
 ;(window as any).SpeechRecognition=Recognition
 const getUserMedia=vi.fn().mockResolvedValue({getTracks:()=>[{stop:vi.fn()}]})
 Object.defineProperty(navigator,'mediaDevices',{configurable:true,value:{getUserMedia}})
 ;(window as any).AudioContext=class{createAnalyser(){return{fftSize:0,smoothingTimeConstant:0,frequencyBinCount:16,getByteFrequencyData:vi.fn()}}createMediaStreamSource(){return{connect:vi.fn()}}close(){return Promise.resolve()}}
 const user=await open({personal:true,providers,initialSession:session,chat:{start:vi.fn(),approve:vi.fn(),dispose:vi.fn()}})
 const mic=screen.getByRole('button',{name:'语音输入'}),send=screen.getByRole('button',{name:'↑ 发送并对话'})
 expect(mic.closest('.composer-primary-actions')).toBe(send.closest('.composer-primary-actions'))
 await user.click(mic)
 expect(getUserMedia).toHaveBeenCalledWith({audio:{deviceId:{exact:'usb-mic'}}})
 delete (window as any).SpeechRecognition;delete (window as any).AudioContext
})

it('offers Windows settings and a direct retry when microphone permission is denied',async()=>{
 class Recognition{lang='';continuous=false;interimResults=false;onresult:any=null;onerror:any=null;onend:any=null;start=vi.fn();stop=vi.fn()}
 ;(window as any).SpeechRecognition=Recognition
 Object.defineProperty(navigator,'mediaDevices',{configurable:true,value:{getUserMedia:vi.fn().mockRejectedValue(new DOMException('denied','NotAllowedError'))}})
 const user=await open({personal:true,providers,initialSession:session})
 await user.click(screen.getByRole('button',{name:'语音输入'}))
 expect(await screen.findByRole('alert')).toHaveTextContent('MICROPHONE_PERMISSION_DENIED')
 expect(screen.getByRole('button',{name:'打开 Windows 麦克风设置'})).toBeInTheDocument()
 expect(screen.getByRole('button',{name:'重新尝试麦克风'})).toBeInTheDocument()
 delete (window as any).SpeechRecognition
})

it('falls back to the default microphone when the saved device disappears',async()=>{
 localStorage.setItem('lunitide:microphone-device-id','missing-mic')
 class Recognition{lang='';continuous=false;interimResults=false;onresult:any=null;onerror:any=null;onend:any=null;start=vi.fn();stop=vi.fn()}
 ;(window as any).SpeechRecognition=Recognition
 const stream={getTracks:()=>[{stop:vi.fn()}]},getUserMedia=vi.fn().mockRejectedValueOnce(new DOMException('missing','NotFoundError')).mockResolvedValueOnce(stream)
 Object.defineProperty(navigator,'mediaDevices',{configurable:true,value:{getUserMedia}})
 ;(window as any).AudioContext=class{createAnalyser(){return{fftSize:0,smoothingTimeConstant:0,frequencyBinCount:16,getByteFrequencyData:vi.fn()}}createMediaStreamSource(){return{connect:vi.fn()}}close(){return Promise.resolve()}}
 const user=await open({personal:true,providers,initialSession:session})
 await user.click(screen.getByRole('button',{name:'语音输入'}))
 expect(getUserMedia).toHaveBeenNthCalledWith(1,{audio:{deviceId:{exact:'missing-mic'}}})
 expect(getUserMedia).toHaveBeenNthCalledWith(2,{audio:true})
 expect(localStorage.getItem('lunitide:microphone-device-id')).toBeNull()
 expect(screen.getByRole('button',{name:'停止语音输入'})).toBeInTheDocument()
 delete (window as any).SpeechRecognition;delete (window as any).AudioContext
})
