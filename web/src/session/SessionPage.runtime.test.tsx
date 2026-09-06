import { act, cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, expect, it, vi } from 'vitest'
import { BridgeClientError, type AttachmentBridge, type ChatBridge, type ChatStream, type ContextBridge, type MessageBridge, type ProviderBridge, type SessionBridge, type SkillBridge, type StreamEvent } from '../bridge/client'
import type { MessageDTO, ProjectDTO, ProviderDTO, SessionDTO } from '../generated/bridge'
import { ATTACHMENT_FILE_MAX, SessionPage, persistedExecutionMode, generalDefaultExecutionMode, TURN_RESUME_PROMPT, turnFailureNotice } from './SessionPage'
import { rememberAttachmentPreview } from './attachments'
import { resetLiveChatForTests } from './liveChat'
import { RootErrorBoundary } from '../RootErrorBoundary'

afterEach(()=>{cleanup();resetLiveChatForTests();localStorage.removeItem('lunitide:microphone-device-id');localStorage.removeItem('lunitide:active-turn:01ARZ3NDEKTSV4RRFFQ69G5FAA');localStorage.removeItem('lunitide:persist-failed:01ARZ3NDEKTSV4RRFFQ69G5FAA');localStorage.removeItem('lunitide:session-experts:01ARZ3NDEKTSV4RRFFQ69G5FAA')})
const P='01ARZ3NDEKTSV4RRFFQ69G5FAV',S='01ARZ3NDEKTSV4RRFFQ69G5FAA',NOW='2025-01-01T00:00:00Z'
const project:ProjectDTO={id:P,name:'Runtime',projectCode:'ITM00001',type:'implementation',status:'active',createdAt:NOW,updatedAt:NOW,version:1}
const session:SessionDTO={id:S,projectId:P,title:'Session',pinned:false,status:'active',createdAt:NOW,updatedAt:NOW,version:1}
const sessionBridge:SessionBridge={list:vi.fn().mockResolvedValue({items:[session]}),create:vi.fn(),update:vi.fn(),delete:vi.fn()}
const page=(items:MessageDTO[]=[])=>({items,hasMore:false,nextCursor:null,snapshotSequence:items.length})
const provider:ProviderDTO={id:'01ARZ3NDEKTSV4RRFFQ69G5FAB',name:'Ready',protocol:'openai_compatible',baseUrl:'https://example.test',models:[{modelId:'model',displayName:'Model',isDefault:true}],status:'enabled',credentialState:'configured',credentialBackupCount:0,createdAt:NOW,updatedAt:NOW,version:1}
const providers={list:vi.fn().mockResolvedValue({items:[provider]})} as unknown as ProviderBridge
const micBase={echoCancellation:true,noiseSuppression:true,autoGainControl:true}
const micDefault={audio:micBase}
// `ideal`, not `exact`: unplugging a USB mic must not hard-fail getUserMedia.
// A missing device silently falls back to the default, and speech.ts writes the
// device actually acquired back to storage, so a stale id self-heals.
const micDevice=(id:string)=>({audio:{...micBase,deviceId:{ideal:id}}})

it('maps stream failure codes to a safe cause without leaking UPSTREAM_FAILED',()=>{
 expect(turnFailureNotice({code:'UPSTREAM_FAILED'})).toMatch(/^无法执行。/)
 expect(turnFailureNotice({code:'UPSTREAM_FAILED'})).not.toContain('desktop=true')
 expect(turnFailureNotice({code:'UPSTREAM_FAILED'})).not.toContain('写到桌面请用')
 expect(turnFailureNotice({code:'UPSTREAM_FAILED'})).not.toContain('*.gen')
 expect(turnFailureNotice({code:'UPSTREAM_FAILED'})).not.toContain('不要用 command.run')
 expect(turnFailureNotice({code:'UPSTREAM_FAILED'})).not.toContain('UPSTREAM_FAILED')
 expect(turnFailureNotice({code:'UPSTREAM_FAILED'})).not.toContain('模型请求失败')
 expect(turnFailureNotice({code:'UPSTREAM_TIMEOUT'})).toContain('请求超时')
 expect(turnFailureNotice({code:'ASSISTANT_RESPONSE_TOO_LARGE'})).toContain('过大')
 for (const code of ['UPSTREAM_FAILED','UPSTREAM_TIMEOUT','ASSISTANT_RESPONSE_TOO_LARGE','REQUEST_TOO_LARGE'] as const) {
  expect(turnFailureNotice({code})).not.toMatch(/写到桌面请用对应 \*\.gen/)
 }
})

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
 await waitFor(()=>expect(screen.getAllByRole('status').some(node=>/已跳过 1 个/.test(node.textContent??''))).toBe(true))
 expect(screen.getAllByRole('status').some(node => /0 个文件/.test(node.textContent ?? ''))).toBe(true)
 expect(begin).toHaveBeenCalledOnce();expect(commit).toHaveBeenCalledOnce();expect(abort).not.toHaveBeenCalled()
 expect(input.value).toBe('')
},25_000)

it('falls back for invalid persisted execution modes',()=>{
 expect(persistedExecutionMode('plan')).toBe('approval')
 expect(persistedExecutionMode('corrupted')).toBe('approval')
 expect(persistedExecutionMode(null)).toBe('full-access')
})
it('new sessions honor the general default mode setting',()=>{
 try{
  localStorage.setItem('lunitide:general',JSON.stringify({defaultMode:'approval'}))
  expect(generalDefaultExecutionMode()).toBe('approval')
  expect(persistedExecutionMode(null)).toBe('approval')
  // An explicit per-session mode still wins over the default.
  expect(persistedExecutionMode('full-access')).toBe('full-access')
  localStorage.setItem('lunitide:general',JSON.stringify({defaultMode:'legacy-junk'}))
  expect(generalDefaultExecutionMode()).toBe('full-access')
 } finally { localStorage.removeItem('lunitide:general') }
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
 expect(start).toHaveBeenCalledOnce()
 expect(append).toHaveBeenCalledOnce()
 fireEvent.change(input,{target:{value:''}})
 cancel.mockClear()
 await user.click(screen.getByRole('button', { name: '停止' }))
 expect(cancel).toHaveBeenCalledTimes(1)
 await waitFor(()=>expect(screen.getAllByRole('status').some(node => /已取消/.test(node.textContent ?? ''))).toBe(true))
 expect(screen.getByText(/终止打断了/)).toBeInTheDocument()
 await act(async()=>onEvent({v:'1.0',kind:'event',id:'01ARZ3NDEKTSV4RRFFQ69G5FAE',streamId:stream.streamId,sequence:1,type:'delta',delta:{text:'## 实时标题\n\n- 第一项\n- 第二项'}}))
 expect(screen.queryByRole('heading',{name:'实时标题'})).toBeNull()
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

it('hides embedding and gui models from the session selector',async()=>{
 const mixed:ProviderDTO={...provider,models:[
  {modelId:'chat-l',displayName:'Chat LLM',isDefault:true,kind:'llm'},
  {modelId:'emb',displayName:'Embed',isDefault:false,kind:'embedding'},
  {modelId:'gui',displayName:'UI-TARS',isDefault:false,kind:'gui'},
 ]}
 const user=await open({personal:true,initialSession:session,providers:{list:vi.fn().mockResolvedValue({items:[mixed]})} as unknown as ProviderBridge,chat:{start:vi.fn(),approve:vi.fn(),dispose:vi.fn()} as ChatBridge})
 await user.click(await screen.findByRole('button',{name:'已配置模型'}))
 expect(screen.getByRole('menuitem',{name:/Chat LLM/})).toBeInTheDocument()
 expect(screen.queryByRole('menuitem',{name:/Embed/})).toBeNull()
 expect(screen.queryByRole('menuitem',{name:/UI-TARS/})).toBeNull()
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
 expect(screen.getByRole('button',{name:/选技能/})).toBeInTheDocument();expect(screen.getByRole('button',{name:/选专家/})).toBeInTheDocument()
 await user.click(screen.getByRole('button',{name:/@ 上下文/}));expect(screen.getByLabelText('向月汐提问，或描述你想完成的任务…')).toHaveValue('@')
})

it('shows a retryable alert when 选技能 list fails instead of an empty catalog',async()=>{
 const skills={list:vi.fn().mockRejectedValue(new BridgeClientError('down','ENGINE_UNAVAILABLE',true,'engine'))} as unknown as SkillBridge
 const user=await open({personal:true,providers,initialSession:session,skills})
 await user.click(screen.getByRole('button',{name:'添加上下文'}))
 await user.click(screen.getByRole('button',{name:/选技能/}))
 expect(await screen.findByRole('alert')).toHaveTextContent('技能列表暂时读不到')
 expect(screen.getByRole('button',{name:'再试一次'})).toBeInTheDocument()
 expect(screen.getByRole('listbox',{name:'技能候选'})).toBeInTheDocument()
})

it('shows published-skill empty copy without a failure alert',async()=>{
 const skills={list:vi.fn().mockResolvedValue({items:[]})} as unknown as SkillBridge
 const user=await open({personal:true,providers,initialSession:session,skills})
 await user.click(screen.getByRole('button',{name:'添加上下文'}))
 await user.click(screen.getByRole('button',{name:/选技能/}))
 expect(await screen.findByText('还没有已发布技能。')).toBeInTheDocument()
 expect(screen.queryByRole('button',{name:'再试一次'})).toBeNull()
})

it('shows a retryable alert when @ 上下文 list fails',async()=>{
 const attachments={list:vi.fn().mockRejectedValue(new BridgeClientError('down','ENGINE_UNAVAILABLE',true,'engine')),get:vi.fn(),ingest:vi.fn(),delete:vi.fn()} as unknown as AttachmentBridge
 const user=await open({personal:true,providers,initialSession:session,attachments})
 await user.click(screen.getByRole('button',{name:'添加上下文'}))
 await user.click(screen.getByRole('button',{name:/@ 上下文/}))
 expect(await screen.findByRole('alert')).toHaveTextContent('可引用的上下文暂时读不到')
 expect(screen.getByRole('button',{name:'再试一次'})).toBeInTheDocument()
 expect(screen.getByRole('listbox',{name:'上下文候选'})).toBeInTheDocument()
})

it('shows a retryable alert when 选专家 list fails',async()=>{
 const experts={list:vi.fn().mockRejectedValue(new BridgeClientError('down','ENGINE_UNAVAILABLE',true,'engine')),sessionMountGet:vi.fn().mockResolvedValue({expertIds:[]}),sessionMountSet:vi.fn()} as unknown as import('../bridge/client').ExpertBridge
 const user=await open({personal:true,providers,initialSession:session,experts})
 await user.click(screen.getByRole('button',{name:'添加上下文'}))
 await user.click(screen.getByRole('button',{name:/选专家/}))
 expect(await screen.findByRole('alert')).toHaveTextContent('专家列表暂时读不到')
 expect(screen.getByRole('button',{name:'再试一次'})).toBeInTheDocument()
 expect(screen.getByRole('listbox',{name:'专家候选'})).toBeInTheDocument()
})

it('keeps the plus menu usable after a send follow-up failure',async()=>{
 const pick=vi.fn().mockResolvedValue({canceled:false,items:[]})
 const experts={list:vi.fn().mockResolvedValue({experts:[]}),sessionMountGet:vi.fn().mockResolvedValue({expertIds:[]}),sessionMountSet:vi.fn()} as unknown as import('../bridge/client').ExpertBridge
 const user=await open({personal:true,providers,initialSession:session,desktopFiles:{pick,readChunk:vi.fn()},experts})
 await user.click(screen.getByRole('button',{name:'添加上下文'}))
 await user.click(screen.getByRole('button',{name:/附件 \/ 文件/}))
 expect(await screen.findByText(/系统没打开文件框/)).toBeInTheDocument()
 await user.click(screen.getByRole('button',{name:'添加上下文'}))
 expect(screen.getByRole('button',{name:/选专家/})).toBeInTheDocument()
 await user.click(screen.getByRole('button',{name:/选专家/}))
 await waitFor(()=>expect(experts.list).toHaveBeenCalled())
 expect(screen.getByRole('listbox',{name:'专家候选'})).toBeInTheDocument()
})

it('opens the host file dialog for 附件 / 文件 and stays silent on cancel',async()=>{
 const pick=vi.fn().mockResolvedValue({canceled:true,items:[]})
 const user=await open({personal:true,providers,initialSession:session,desktopFiles:{pick,readChunk:vi.fn()}})
 await user.click(screen.getByRole('button',{name:'添加上下文'}))
 await user.click(screen.getByRole('button',{name:/附件 \/ 文件/}))
 await waitFor(()=>expect(pick).toHaveBeenCalledWith({folder:false,multiple:true}))
 expect(screen.queryByText(/系统没打开文件框/)).toBeNull()
})

it('names skipped exe from a host folder pick and does not call it a dialog failure',async()=>{
 const pick=vi.fn().mockResolvedValue({canceled:false,items:[{path:'C:/notes.txt',fileName:'notes.txt',mime:'text/plain',size:2}],skipped:['setup.exe']})
 const begin=vi.fn().mockResolvedValue({uploadId:'01ARZ3NDEKTSV4RRFFQ69G5FAC',chunkSize:128*1024,expiresAt:NOW})
 const chunk=vi.fn().mockResolvedValue({nextOffset:2})
 const commit=vi.fn().mockResolvedValue({attachmentId:'01ARZ3NDEKTSV4RRFFQ69G5FAD',projectId:P,sessionId:S,originalName:'notes.txt',mime:'text/plain',size:2,sha256:'hash',parseStatus:'succeeded',parseErrorCode:'',parsedTextBytes:2,createdAt:NOW})
 const attachments={list:vi.fn().mockResolvedValue({items:[]}),ingest:vi.fn(),begin,chunk,commit,abort:vi.fn(),get:vi.fn(),delete:vi.fn()} as unknown as AttachmentBridge
 const user=await open({personal:true,providers,initialSession:session,attachments,desktopFiles:{pick,readChunk:vi.fn().mockResolvedValue({contentBase64:btoa('hi'),nextOffset:2,eof:true})}})
 await user.click(screen.getByRole('button',{name:'添加上下文'}))
 await user.click(screen.getByRole('button',{name:/上传文件夹/}))
 await waitFor(()=>expect(pick).toHaveBeenCalledWith({folder:true,multiple:false}))
 expect(await screen.findByText(/setup\.exe/)).toBeInTheDocument()
 expect(screen.queryByText(/系统没打开文件框/)).toBeNull()
 await waitFor(()=>expect(commit).toHaveBeenCalled())
})

it('says the folder is empty instead of blaming the file dialog',async()=>{
 const pick=vi.fn().mockResolvedValue({canceled:false,items:[],skipped:['setup.exe']})
 const user=await open({personal:true,providers,initialSession:session,desktopFiles:{pick,readChunk:vi.fn()}})
 await user.click(screen.getByRole('button',{name:'添加上下文'}))
 await user.click(screen.getByRole('button',{name:/上传文件夹/}))
 expect(await screen.findByText(/没有可导入的文件/)).toBeInTheDocument()
 expect(screen.getByText(/setup\.exe/)).toBeInTheDocument()
 expect(screen.queryByText(/系统没打开文件框/)).toBeNull()
})

it('keeps multiple mounted experts on the conversation after send',async()=>{
 const expertA={expertId:'01ARZ3NDEKTSV4RRFFQ69G5FAC',name:'安全工程师',division:'security' as const,source:'local' as const,semver:'1.0.0',state:'enabled' as const,versionCount:1,mountedPhaseCount:0}
 const expertB={...expertA,expertId:'01ARZ3NDEKTSV4RRFFQ69G5FAD',name:'测试专家',division:'testing' as const}
 const sessionMountSet=vi.fn().mockImplementation(async(payload:{expertIds:string[]})=>({expertIds:payload.expertIds}))
 const experts={list:vi.fn().mockResolvedValue({experts:[expertA,expertB]}),sessionMountGet:vi.fn().mockResolvedValue({expertIds:[]}),sessionMountSet,detail:vi.fn(),create:vi.fn(),update:vi.fn(),toggle:vi.fn(),archive:vi.fn(),mount:vi.fn(),mountingGet:vi.fn(),scenarioCreate:vi.fn(),scenarioList:vi.fn(),scenarioDelete:vi.fn()} as unknown as import('../bridge/client').ExpertBridge
 const append=vi.fn().mockResolvedValue({}),start=vi.fn().mockResolvedValue({cancel:vi.fn(),dispose:vi.fn()})
 const user=await open({personal:true,providers,initialSession:session,experts,messages:{list:vi.fn().mockResolvedValue(page()),append} as MessageBridge,chat:{start,approve:vi.fn(),dispose:vi.fn()}})
 await user.click(screen.getByRole('button',{name:'添加上下文'}))
 await user.click(screen.getByRole('button',{name:/选专家/}))
 expect(await screen.findByRole('listbox',{name:'专家候选'})).toBeInTheDocument()
 await user.click(await screen.findByRole('option',{name:/安全工程师/}))
 await user.click(screen.getByRole('option',{name:/测试专家/}))
 expect(screen.getByLabelText('已挂载专家')).toHaveTextContent('安全工程师')
 expect(screen.getByLabelText('已挂载专家')).toHaveTextContent('测试专家')
 expect(screen.queryByLabelText('本会话协作专家')).toBeNull()
 await waitFor(()=>expect(sessionMountSet).toHaveBeenCalled())
 fireEvent.change(screen.getByLabelText('向月汐提问，或描述你想完成的任务…'),{target:{value:'请协作审查'}})
 await user.click(screen.getByRole('button',{name:'↑ 发送并对话'}))
 await waitFor(()=>expect(start).toHaveBeenCalled())
 expect(screen.getByLabelText('已挂载专家')).toHaveTextContent('安全工程师')
 expect(screen.getByLabelText('已挂载专家')).toHaveTextContent('测试专家')
 const sent=String(vi.mocked(append).mock.calls[0][0].text)
 expect(sent).toContain('[引用专家 安全工程师|01ARZ3NDEKTSV4RRFFQ69G5FAC]')
 expect(sent).toContain('[引用专家 测试专家|01ARZ3NDEKTSV4RRFFQ69G5FAD]')
 expect(sent).toContain('请协作审查')
 expect(sent).not.toContain('PPT专家')
 expect(sent).not.toContain('小说编写专家')
})

it('does not show 继续上次 just because the last durable message is the user',async()=>{
 const userMessage:MessageDTO={id:'01ARZ3NDEKTSV4RRFFQ69G5FAC',sessionId:S,role:'user',status:'completed',sequence:1,text:'帮我写个文件',createdAt:NOW}
 const start=vi.fn()
 render(<SessionPage project={project} bridge={sessionBridge} onBack={vi.fn()} personal initialSession={session} providers={providers} messages={{list:vi.fn().mockResolvedValue(page([userMessage])),append:vi.fn()} as MessageBridge} chat={{start,approve:vi.fn(),dispose:vi.fn()}}/>)
 expect(await screen.findByText('帮我写个文件')).toBeInTheDocument()
 expect(screen.queryByRole('button',{name:'继续上次'})).toBeNull()
 await act(async()=>{await new Promise(resolve=>setTimeout(resolve,80))})
 expect(start).not.toHaveBeenCalled()
})

it('shows 继续上次 after remount when the server turn is interrupted',async()=>{
 const userMessage:MessageDTO={id:'01ARZ3NDEKTSV4RRFFQ69G5FAC',sessionId:S,role:'user',status:'completed',sequence:1,text:'帮我写个文件',createdAt:NOW}
 const start=vi.fn()
 const inspectTurn=vi.fn().mockResolvedValue({status:'interrupted',persistFailed:false,persistDraft:''})
 render(<SessionPage project={project} bridge={sessionBridge} onBack={vi.fn()} personal initialSession={session} providers={providers} messages={{list:vi.fn().mockResolvedValue(page([userMessage])),append:vi.fn()} as MessageBridge} chat={{start,approve:vi.fn(),inspectTurn,dispose:vi.fn()}}/>)
 expect(await screen.findByRole('button',{name:'继续上次'})).toBeInTheDocument()
 expect(screen.queryByRole('button',{name:'只重试写入'})).toBeNull()
 await act(async()=>{await new Promise(resolve=>setTimeout(resolve,80))})
 expect(start).not.toHaveBeenCalled()
 expect(inspectTurn).toHaveBeenCalledWith({sessionId:S})
})

it('restores a running server draft into the bubble without persist-failed',async()=>{
 const inspectTurn=vi.fn().mockResolvedValue({status:'running',persistFailed:false,persistDraft:'流到一半还没写完'})
 const start=vi.fn()
 render(<SessionPage project={project} bridge={sessionBridge} onBack={vi.fn()} personal initialSession={session} providers={providers} messages={{list:vi.fn().mockResolvedValue(page()),append:vi.fn()} as MessageBridge} chat={{start,approve:vi.fn(),inspectTurn,dispose:vi.fn()}}/>)
 expect(await screen.findByRole('button',{name:'继续上次'})).toBeInTheDocument()
 expect(screen.queryByRole('button',{name:'只重试写入'})).toBeNull()
 expect(await screen.findByText('流到一半还没写完')).toBeInTheDocument()
 expect(await screen.findByText(/上次没写完/)).toBeInTheDocument()
 expect(screen.queryByText(/^已完成$/)).toBeNull()
 await act(async()=>{await new Promise(resolve=>setTimeout(resolve,80))})
 expect(start).not.toHaveBeenCalled()
})

it('keeps mounted experts on 继续上次',async()=>{
 const ppt={expertId:'01ARZ3NDEKTSV4RRFFQ69G5FAC',name:'PPT专家',division:'design' as const,source:'local' as const,semver:'1.0.0',state:'enabled' as const,versionCount:1,mountedPhaseCount:0}
 const inspectTurn=vi.fn().mockResolvedValue({status:'running',persistFailed:false,persistDraft:'流到一半还没写完'})
 const append=vi.fn().mockResolvedValue({})
 const start=vi.fn().mockResolvedValue({cancel:vi.fn(),dispose:vi.fn()})
 const experts={list:vi.fn().mockResolvedValue({experts:[ppt]}),sessionMountGet:vi.fn().mockResolvedValue({expertIds:[ppt.expertId]}),sessionMountSet:vi.fn(),detail:vi.fn(),create:vi.fn(),update:vi.fn(),toggle:vi.fn(),archive:vi.fn(),mount:vi.fn(),mountingGet:vi.fn(),scenarioCreate:vi.fn(),scenarioList:vi.fn(),scenarioDelete:vi.fn()} as unknown as import('../bridge/client').ExpertBridge
 render(<SessionPage project={project} bridge={sessionBridge} onBack={vi.fn()} personal initialSession={session} providers={providers} experts={experts} messages={{list:vi.fn().mockResolvedValue(page()),append} as MessageBridge} chat={{start,approve:vi.fn(),inspectTurn,dispose:vi.fn()}}/>)
 expect(await screen.findByRole('button',{name:'继续上次'})).toBeInTheDocument()
 await userEvent.setup().click(screen.getByRole('button',{name:'继续上次'}))
 await waitFor(()=>expect(append).toHaveBeenCalled())
 expect(JSON.stringify(append.mock.calls)).toContain(`[引用专家 PPT专家|${ppt.expertId}]`)
 await waitFor(()=>expect(start).toHaveBeenCalled())
 expect(start.mock.calls[0][0].sessionId).toBe(S)
 expect(start.mock.calls[0][0].messages).toBeUndefined()
})

it('opens memory from the inject summary',async()=>{
 let onEvent!:(event:StreamEvent)=>void
 const onOpenMemory=vi.fn()
 const question:MessageDTO={id:'01ARZ3NDEKTSV4RRFFQ69G5FAC',sessionId:S,role:'user',text:'记住晚上用深色',status:'completed',sequence:1,createdAt:NOW}
 const start=vi.fn().mockImplementation(async(_payload,onStreamEvent)=>{onEvent=onStreamEvent;return{streamId:'01ARZ3NDEKTSV4RRFFQ69G5FAD',cancel:vi.fn(),dispose:vi.fn()}})
 const user=userEvent.setup()
 render(<SessionPage project={project} bridge={sessionBridge} personal providers={providers} initialSession={session} chat={{start,dispose:vi.fn()}} messages={{list:vi.fn().mockResolvedValue(page([question])),append:vi.fn().mockResolvedValue({})} as MessageBridge} onBack={vi.fn()} onOpenMemory={onOpenMemory}/>)
 await screen.findByText('记住晚上用深色')
 await user.type(screen.getByLabelText('向月汐提问，或描述你想完成的任务…'),'继续')
 await user.click(screen.getByRole('button',{name:'↑ 发送并对话'}))
 await waitFor(()=>expect(start).toHaveBeenCalledOnce())
 act(()=>{onEvent({v:'1.0',kind:'event',id:'01ARZ3NDEKTSV4RRFFQ69G5FAE',streamId:'01ARZ3NDEKTSV4RRFFQ69G5FAD',sequence:1,type:'delta',delta:{text:'好'}});onEvent({v:'1.0',kind:'event',id:'01ARZ3NDEKTSV4RRFFQ69G5FAF',streamId:'01ARZ3NDEKTSV4RRFFQ69G5FAD',sequence:2,type:'completed',completed:{memorySummary:'本轮用了偏好 1'}})})
 await user.click(await screen.findByRole('button',{name:'本轮用了偏好 1'}))
 expect(onOpenMemory).toHaveBeenCalledOnce()
})

it('shows 只重试写入 after remount from the server persist draft',async()=>{
 const inspectTurn=vi.fn().mockResolvedValue({status:'completed',persistFailed:true,persistDraft:'已经生成但没落库'})
 const start=vi.fn().mockResolvedValue({cancel:vi.fn(),dispose:vi.fn()})
 render(<SessionPage project={project} bridge={sessionBridge} onBack={vi.fn()} personal initialSession={session} providers={providers} messages={{list:vi.fn().mockResolvedValue(page()),append:vi.fn()} as MessageBridge} chat={{start,approve:vi.fn(),inspectTurn,dispose:vi.fn()}}/>)
 expect(await screen.findByRole('button',{name:'只重试写入'})).toBeInTheDocument()
 expect(screen.queryByRole('button',{name:'继续上次'})).toBeNull()
 expect(await screen.findByText('已经生成但没落库')).toBeInTheDocument()
 await waitFor(()=>expect(screen.getByRole('button',{name:'已配置模型'})).toBeInTheDocument())
 await userEvent.setup().click(screen.getByRole('button',{name:'只重试写入'}))
 await waitFor(()=>expect(start).toHaveBeenCalled())
 expect(start.mock.calls[0][0].messages?.[0]?.content).toBe('\u2063persist-retry')
})

it('restores persist-failed draft without auto-sending resume',async()=>{
 localStorage.setItem(`lunitide:persist-failed:${S}`,JSON.stringify({draft:'已经生成但没落库'}))
 const userMessage:MessageDTO={id:'01ARZ3NDEKTSV4RRFFQ69G5FAC',sessionId:S,role:'user',status:'completed',sequence:1,text:'帮我写个文件',createdAt:NOW}
 const start=vi.fn()
 render(<SessionPage project={project} bridge={sessionBridge} onBack={vi.fn()} personal initialSession={session} providers={providers} messages={{list:vi.fn().mockResolvedValue(page([userMessage])),append:vi.fn()} as MessageBridge} chat={{start,approve:vi.fn(),dispose:vi.fn()}}/>)
 expect(await screen.findByText('帮我写个文件')).toBeInTheDocument()
 expect(await screen.findByRole('button',{name:'只重试写入'})).toBeInTheDocument()
 expect(screen.queryByRole('button',{name:'继续上次'})).toBeNull()
 await act(async()=>{await new Promise(resolve=>setTimeout(resolve,80))})
 expect(start).not.toHaveBeenCalled()
})

it('does not auto-send TURN_RESUME_PROMPT on retryable failed or unfinished turn',async()=>{
 localStorage.setItem(`lunitide:active-turn:${S}`,JSON.stringify({status:'interrupted',resumeCount:0}))
 const userMessage:MessageDTO={id:'01ARZ3NDEKTSV4RRFFQ69G5FAC',sessionId:S,role:'user',status:'completed',sequence:1,text:'帮我写个文件',createdAt:NOW}
 const start=vi.fn().mockRejectedValue(new BridgeClientError('模型请求失败','UPSTREAM_FAILED',true,'engine'))
 const append=vi.fn().mockResolvedValue({})
 const user=userEvent.setup()
 render(<SessionPage project={project} bridge={sessionBridge} onBack={vi.fn()} personal initialSession={session} providers={providers} messages={{list:vi.fn().mockResolvedValue(page([userMessage])),append} as MessageBridge} chat={{start,approve:vi.fn(),dispose:vi.fn()}}/>)
 expect(await screen.findByText('帮我写个文件')).toBeInTheDocument()
 await act(async()=>{await new Promise(resolve=>setTimeout(resolve,80))})
 expect(start).not.toHaveBeenCalled()
 expect(JSON.stringify(start.mock.calls)).not.toContain(TURN_RESUME_PROMPT)
 fireEvent.change(screen.getByLabelText('向月汐提问，或描述你想完成的任务…'),{target:{value:'再试一次'}})
 await user.click(screen.getByRole('button',{name:'↑ 发送并对话'}))
 await waitFor(()=>expect(start).toHaveBeenCalledOnce())
 expect(JSON.stringify(start.mock.calls)).not.toContain(TURN_RESUME_PROMPT)
 expect(vi.mocked(append).mock.calls[0][0].text).toBe('再试一次')
 expect(await screen.findByText(/无法执行/)).toBeInTheDocument()
 expect(screen.queryByText(/写到桌面请用/)).toBeNull()
 expect(screen.queryByText(/desktop=true/)).toBeNull()
 expect(screen.queryByText('模型请求失败')).toBeNull()
 expect(screen.queryByText(/代码 UPSTREAM_FAILED/)).toBeNull()
 expect(screen.queryByRole('button',{name:'从最新页重试'})).toBeNull()
 await act(async()=>{await new Promise(resolve=>setTimeout(resolve,80))})
 expect(start).toHaveBeenCalledOnce()
 fireEvent.change(screen.getByLabelText('向月汐提问，或描述你想完成的任务…'),{target:{value:'继续'}})
 await user.click(screen.getByRole('button',{name:'↑ 发送并对话'}))
 await waitFor(()=>expect(start).toHaveBeenCalledTimes(2))
 expect(vi.mocked(append).mock.calls[1][0].text).toBe('继续')
 expect(JSON.stringify(start.mock.calls)).not.toContain(TURN_RESUME_PROMPT)
})

it('surfaces stream failure only as 无法执行 without the UPSTREAM_FAILED card',async()=>{
 let onEvent!:(event:StreamEvent)=>void
 const stream:ChatStream={streamId:'01ARZ3NDEKTSV4RRFFQ69G5FAD',cancel:vi.fn().mockResolvedValue(true),dispose:vi.fn()}
 const start=vi.fn().mockImplementation(async(_payload,onStreamEvent)=>{onEvent=onStreamEvent;return stream})
 const user=await open({personal:true,providers,initialSession:session,messages:{list:vi.fn().mockResolvedValue(page()),append:vi.fn().mockResolvedValue({})} as MessageBridge,chat:{start,approve:vi.fn(),dispose:vi.fn()}})
 fireEvent.change(screen.getByLabelText('向月汐提问，或描述你想完成的任务…'),{target:{value:'打开网页'}})
 await user.click(screen.getByRole('button',{name:'↑ 发送并对话'}))
 await waitFor(()=>expect(start).toHaveBeenCalledOnce())
 await act(async()=>onEvent({v:'1.0',kind:'event',id:'01ARZ3NDEKTSV4RRFFQ69G5FAE',streamId:stream.streamId,sequence:1,type:'failed',error:{code:'UPSTREAM_FAILED',message:'模型请求失败',retryable:true}}))
 expect(await screen.findByText(/无法执行/)).toBeInTheDocument()
 expect(screen.queryByText(/写到桌面请用/)).toBeNull()
 expect(screen.queryByText(/desktop=true/)).toBeNull()
 expect(screen.queryByText('模型请求失败')).toBeNull()
 expect(screen.queryByText(/代码 UPSTREAM_FAILED/)).toBeNull()
 expect(screen.queryByRole('button',{name:'从最新页重试'})).toBeNull()
 await act(async()=>{await new Promise(resolve=>setTimeout(resolve,80))})
 expect(start).toHaveBeenCalledOnce()
 expect(JSON.stringify(start.mock.calls)).not.toContain(TURN_RESUME_PROMPT)
})

it('reloads persisted assistant history after a failed stream',async()=>{
 let onEvent!:(event:StreamEvent)=>void
 const userMessage:MessageDTO={id:'01ARZ3NDEKTSV4RRFFQ69G5FAC',sessionId:S,role:'user',status:'completed',sequence:1,text:'写十二星座小说',createdAt:NOW}
 const assistantMessage:MessageDTO={id:'01ARZ3NDEKTSV4RRFFQ69G5FAD',sessionId:S,role:'assistant',status:'completed',sequence:2,text:'已写好白羊座。\n无法执行。模型结果不完整，请重试。',createdAt:NOW}
 const list=vi.fn()
  .mockResolvedValueOnce(page([userMessage]))
  .mockResolvedValueOnce(page([userMessage]))
  .mockResolvedValue(page([userMessage,assistantMessage]))
 const stream:ChatStream={streamId:'01ARZ3NDEKTSV4RRFFQ69G5FAD',cancel:vi.fn().mockResolvedValue(true),dispose:vi.fn()}
 const start=vi.fn().mockImplementation(async(_payload,onStreamEvent)=>{onEvent=onStreamEvent;return stream})
 render(<SessionPage project={project} bridge={sessionBridge} onBack={vi.fn()} personal initialSession={session} providers={providers} messages={{list,append:vi.fn().mockResolvedValue(userMessage)} as MessageBridge} chat={{start,approve:vi.fn(),dispose:vi.fn()}}/>)
 expect(await screen.findByText('写十二星座小说')).toBeInTheDocument()
 fireEvent.change(screen.getByLabelText('向月汐提问，或描述你想完成的任务…'),{target:{value:'写十二星座小说'}})
 await userEvent.setup().click(screen.getByRole('button',{name:'↑ 发送并对话'}))
 await waitFor(()=>expect(start).toHaveBeenCalledOnce())
 await act(async()=>{
  onEvent({v:'1.0',kind:'event',id:'01ARZ3NDEKTSV4RRFFQ69G5FAE',streamId:stream.streamId,sequence:1,type:'delta',delta:{text:'已写好白羊座。'}})
  onEvent({v:'1.0',kind:'event',id:'01ARZ3NDEKTSV4RRFFQ69G5FAF',streamId:stream.streamId,sequence:2,type:'failed',error:{code:'UPSTREAM_FAILED',message:'模型请求失败',retryable:true}})
 })
 await waitFor(()=>expect(list.mock.calls.length).toBeGreaterThan(2))
 expect(screen.getAllByText(/已写好白羊座/).length).toBeGreaterThan(0)
 expect(screen.getAllByText(/无法执行/).length).toBeGreaterThan(0)
 expect(screen.queryByText('AGENT · 失败')).toBeNull()
})

it('folds persisted thinking-only history into one closed row',async()=>{
 const assistantMessage:MessageDTO={id:'01ARZ3NDEKTSV4RRFFQ69G5FAD',sessionId:S,role:'assistant',status:'completed',sequence:2,text:'【思考过程】\n先规划结构再写大纲。\n\n无法执行。模型结果不完整，请重试。',createdAt:NOW}
 render(<SessionPage project={project} bridge={sessionBridge} onBack={vi.fn()} personal initialSession={session} messages={{list:vi.fn().mockResolvedValue(page([assistantMessage])),append:vi.fn()} as MessageBridge}/>)
 expect(await screen.findByText(/无法执行/)).toBeInTheDocument()
 const details=screen.getByText('任务过程').closest('details')!
 expect(details).not.toHaveAttribute('open')
 expect(screen.queryByText('【思考过程】')).toBeNull()
 expect(document.querySelector('.thinking-reasoning')).toBeNull()
 await userEvent.click(screen.getByText('任务过程'))
 expect(details).toHaveAttribute('open')
 expect(screen.getByText(/先规划结构再写大纲/)).toBeInTheDocument()
})

it('closes composer popovers when clicking outside',async()=>{
 const user=await open({personal:true,providers,initialSession:session,chat:{start:vi.fn(),dispose:vi.fn()}})
 await user.click(screen.getByRole('button',{name:'添加上下文'}))
 expect(screen.getByRole('button',{name:/选技能/})).toBeInTheDocument()
 fireEvent.click(document.body)
 expect(screen.queryByRole('button',{name:/选技能/})).toBeNull()
 await user.click(screen.getByRole('button',{name:'执行模式'}))
 expect(screen.getByRole('button',{name:/免审批/})).toBeInTheDocument()
 fireEvent.click(document.body)
 expect(screen.queryByRole('button',{name:/免审批/})).toBeNull()
 await user.click(await screen.findByRole('button',{name:'结构化输出'}))
 expect(screen.getByRole('menuitem',{name:/提取事件/})).toBeInTheDocument()
 fireEvent.click(document.body)
 expect(screen.queryByRole('menuitem',{name:/提取事件/})).toBeNull()
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

it('shows the uploaded image in the user bubble instead of a raw attachment token',async()=>{
 const id='01ARZ3NDEKTSV4RRFFQ69G5FAD'
 const bytes=new Uint8Array([1,2,3]),file=new File([bytes],'shot.png',{type:'image/png'})
 rememberAttachmentPreview(id,file)
 const userMessage:MessageDTO={id:'01ARZ3NDEKTSV4RRFFQ69G5FAC',sessionId:S,role:'user',text:`[attachment:${id}|shot.png] 看看这个图片`,status:'completed',sequence:1,createdAt:NOW}
 render(<SessionPage project={project} bridge={sessionBridge} messages={{list:vi.fn().mockResolvedValue(page([userMessage])),append:vi.fn()} as MessageBridge} onBack={vi.fn()} personal initialSession={session}/>)
 expect(await screen.findByText('看看这个图片')).toBeInTheDocument()
 expect(screen.getByRole('img',{name:'shot.png'})).toBeInTheDocument()
 expect(screen.queryByText(/\[attachment:/)).toBeNull()
})

it('persists pending image attachments into the sent message so the bubble can render them',async()=>{
 const id='01ARZ3NDEKTSV4RRFFQ69G5FAD'
 const bytes=new Uint8Array([1,2,3]),file=new File([bytes],'shot.png',{type:'image/png'});Object.defineProperty(file,'arrayBuffer',{value:async()=>bytes.buffer})
 const attachments={list:vi.fn().mockResolvedValue({items:[]}),ingest:vi.fn(),begin:vi.fn().mockResolvedValue({uploadId:'01ARZ3NDEKTSV4RRFFQ69G5FAC',chunkSize:128*1024,expiresAt:NOW}),chunk:vi.fn().mockResolvedValue({nextOffset:3}),commit:vi.fn().mockResolvedValue({attachmentId:id}),abort:vi.fn(),get:vi.fn(),delete:vi.fn()} as unknown as AttachmentBridge
 const start=vi.fn().mockResolvedValue({cancel:vi.fn(),dispose:vi.fn()}),append=vi.fn().mockResolvedValue({})
 const user=await open({personal:true,providers,initialSession:session,attachments,chat:{start,dispose:vi.fn()},messages:{list:vi.fn().mockResolvedValue(page()),append} as MessageBridge})
 const picker=document.querySelector('.message-actions input[type="file"]:not([webkitdirectory])') as HTMLInputElement
 await fireEvent.change(picker,{target:{files:[file]}})
 await waitFor(()=>expect(attachments.commit).toHaveBeenCalledOnce())
 await user.type(screen.getByLabelText('向月汐提问，或描述你想完成的任务…'),'看看这个图片')
 await user.click(screen.getByRole('button',{name:'↑ 发送并对话'}))
 await waitFor(()=>expect(append).toHaveBeenCalledOnce())
 expect(append.mock.calls[0][0].text).toContain(`[attachment:${id}|shot.png]`)
 expect(start.mock.calls[0][0]).toMatchObject({contextRefs:[{type:'attachment',id}]})
})

it('waits for initial attachments, then includes their ids in the first chat context',async()=>{
 let finish!:(value:{nextOffset:number})=>void
 const data=new Uint8Array([1,2,3]),file=new File([data],'initial.txt',{type:'text/plain'});Object.defineProperty(file,'arrayBuffer',{value:async()=>data.buffer})
 const chunk=vi.fn().mockImplementation(()=>new Promise(resolve=>{finish=resolve})),commit=vi.fn().mockResolvedValue({attachmentId:'attachment-initial'}),attachments={list:vi.fn().mockResolvedValue({items:[]}),begin:vi.fn().mockResolvedValue({uploadId:'upload',chunkSize:3}),chunk,commit,abort:vi.fn(),get:vi.fn(),delete:vi.fn()} as unknown as AttachmentBridge
 const start=vi.fn().mockResolvedValue({cancel:vi.fn(),dispose:vi.fn()}),append=vi.fn().mockResolvedValue({})
 await open({personal:true,initialSession:session,initialPrompt:'首条问题',initialUploadFiles:[file],attachments,providers,chat:{start,dispose:vi.fn()},messages:{list:vi.fn().mockResolvedValue(page()),append} as MessageBridge})
 await waitFor(()=>expect(chunk).toHaveBeenCalledOnce());expect(start).not.toHaveBeenCalled();finish({nextOffset:3})
 await waitFor(()=>expect(start).toHaveBeenCalledOnce());expect(start.mock.calls[0][0]).toMatchObject({contextRefs:[{type:'attachment',id:'attachment-initial'}]});expect(append.mock.calls[0][0].text).toContain('[attachment:attachment-initial|initial.txt]')
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
 await waitFor(()=>expect(start).toHaveBeenCalledOnce());expect(start.mock.calls[0][0]).toMatchObject({contextRefs:[{type:'attachment',id:'attachment-retried'}]});expect(append).toHaveBeenCalledOnce();expect(append.mock.calls[0][0].text).toContain('[attachment:attachment-retried|retry.txt]')
})

it('shows the auto-equip chip for an intent-matched turn and deep-links to the MCP page',async()=>{
 let onEvent!:(event:StreamEvent)=>void
 const start=vi.fn().mockImplementation(async(_payload,onStreamEvent)=>{onEvent=onStreamEvent;return{streamId:'01ARZ3NDEKTSV4RRFFQ69G5FAD',cancel:vi.fn(),dispose:vi.fn()}})
 const onOpenMcp=vi.fn(),user=userEvent.setup()
 render(<SessionPage project={project} bridge={sessionBridge} onBack={vi.fn()} personal initialSession={session} providers={providers} messages={{list:vi.fn().mockResolvedValue(page()),append:vi.fn().mockResolvedValue({})} as MessageBridge} chat={{start,dispose:vi.fn()}} onOpenMcp={onOpenMcp}/>)
 await screen.findByText('还没有消息')
 await user.type(screen.getByLabelText('向月汐提问，或描述你想完成的任务…'),'帮我做个PPT')
 await user.click(screen.getByRole('button',{name:'↑ 发送并对话'}));await waitFor(()=>expect(start).toHaveBeenCalledOnce())
 await act(async()=>onEvent({v:'1.0',kind:'event',id:'01ARZ3NDEKTSV4RRFFQ69G5FAE',streamId:'01ARZ3NDEKTSV4RRFFQ69G5FAD',sequence:1,type:'equip',equip:{experts:['PPT专家'],skills:['slide-builder'],missingMcp:['playwright']}}))
 const chip=await screen.findByLabelText('本轮自动装备')
 expect(chip).toHaveTextContent('PPT专家');expect(chip).toHaveTextContent('slide-builder');expect(chip).toHaveTextContent('playwright')
 await user.click(screen.getByRole('button',{name:'去连接'}));expect(onOpenMcp).toHaveBeenCalledOnce()
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
 expect(details).toHaveAttribute('open')
 expect(screen.queryByText('内部推理')).toBeNull()
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

it('does not auto-open the terminal workspace for command tool activity',async()=>{
 let onEvent!:(event:StreamEvent)=>void
 const stream:ChatStream={streamId:'01ARZ3NDEKTSV4RRFFQ69G5FAD',cancel:vi.fn().mockResolvedValue(true),dispose:vi.fn()}
 const start=vi.fn().mockImplementation(async(_payload,onStreamEvent)=>{onEvent=onStreamEvent;return stream})
 const attachments={list:vi.fn().mockResolvedValue({items:[]}),get:vi.fn(),ingest:vi.fn(),delete:vi.fn()} as unknown as AttachmentBridge
 const user=await open({personal:true,initialSession:session,attachments,providers,chat:{start,approve:vi.fn(),dispose:vi.fn()}})
 await user.type(screen.getByLabelText('向月汐提问，或描述你想完成的任务…'),'运行测试')
 await user.click(screen.getByRole('button',{name:'↑ 发送并对话'}))
 await waitFor(()=>expect(start).toHaveBeenCalledOnce())
 await act(async()=>onEvent({v:'1.0',kind:'event',id:'01ARZ3NDEKTSV4RRFFQ69G5FAE',streamId:stream.streamId,sequence:1,type:'tool_started',tool:{callId:'call-1',name:'command.run',argsDigest:'digest',summary:'go test ./...'}}))
 expect(screen.queryByLabelText('统一工作区')).toBeNull()
})

it('keeps tool activity inside the task process and follows the growing stream',async()=>{
 let onEvent!:(event:StreamEvent)=>void
 const start=vi.fn().mockImplementation(async(_payload,onStreamEvent)=>{onEvent=onStreamEvent;return{streamId:'01ARZ3NDEKTSV4RRFFQ69G5FAD',cancel:vi.fn(),dispose:vi.fn()}})
 const user=await open({messages:{list:vi.fn().mockResolvedValue(page()),append:vi.fn().mockResolvedValue({})} as MessageBridge,chat:{start,dispose:vi.fn()},providers})
 const box=document.querySelector('.conversation-scroll') as HTMLDivElement,scrollTo=vi.fn()
 Object.defineProperties(box,{scrollHeight:{value:900},clientHeight:{value:300},scrollTop:{value:400,writable:true}});Object.defineProperty(box,'scrollTo',{value:scrollTo})
 await user.type(screen.getByLabelText('向月汐提问，或描述你想完成的任务…'),'读取文件');await user.click(screen.getByRole('button',{name:'↑ 发送并对话'}));await waitFor(()=>expect(start).toHaveBeenCalledOnce())
 scrollTo.mockClear();await act(async()=>onEvent({v:'1.0',kind:'event',id:'01ARZ3NDEKTSV4RRFFQ69G5FAE',streamId:'01ARZ3NDEKTSV4RRFFQ69G5FAD',sequence:1,type:'tool_started',tool:{callId:'read-1',name:'fs.read',argsDigest:'a'.repeat(64),summary:'README.md'}}))
 const details=screen.getByText('任务过程').closest('details')!;expect(details).toHaveTextContent('读取');expect(details).toHaveTextContent('README.md');await waitFor(()=>expect(scrollTo).toHaveBeenCalledWith({top:600,behavior:'auto'}))
 scrollTo.mockClear()
 await act(async()=>onEvent({v:'1.0',kind:'event',id:'01ARZ3NDEKTSV4RRFFQ69G5FAF',streamId:'01ARZ3NDEKTSV4RRFFQ69G5FAD',sequence:2,type:'thinking',thinking:{text:'逐步推理'.repeat(20)}}))
 expect(scrollTo).not.toHaveBeenCalled()
 expect(box.classList.contains('is-streaming')).toBe(true)
})

it('pauses auto-follow on wheel-up so mermaid layout and later tokens do not steal scroll',async()=>{
 let onEvent!:(event:StreamEvent)=>void
 const start=vi.fn().mockImplementation(async(_payload,onStreamEvent)=>{onEvent=onStreamEvent;return{streamId:'01ARZ3NDEKTSV4RRFFQ69G5FAD',cancel:vi.fn(),dispose:vi.fn()}})
 const user=await open({messages:{list:vi.fn().mockResolvedValue(page()),append:vi.fn().mockResolvedValue({})} as MessageBridge,chat:{start,dispose:vi.fn()},providers})
 const box=document.querySelector('.conversation-scroll') as HTMLDivElement,scrollTo=vi.fn()
 Object.defineProperties(box,{scrollHeight:{value:900},clientHeight:{value:300},scrollTop:{value:400,writable:true}});Object.defineProperty(box,'scrollTo',{value:scrollTo})
 await user.type(screen.getByLabelText('向月汐提问，或描述你想完成的任务…'),'画图');await user.click(screen.getByRole('button',{name:'↑ 发送并对话'}));await waitFor(()=>expect(start).toHaveBeenCalledOnce())
 await act(async()=>onEvent({v:'1.0',kind:'event',id:'01ARZ3NDEKTSV4RRFFQ69G5FAE',streamId:'01ARZ3NDEKTSV4RRFFQ69G5FAD',sequence:1,type:'delta',delta:{text:'卡片'}}))
 await waitFor(()=>expect(scrollTo).toHaveBeenCalledWith({top:600,behavior:'auto'}))
 scrollTo.mockClear();box.scrollTop=200;fireEvent.wheel(box,{deltaY:-48});fireEvent.scroll(box)
 expect(screen.getByRole('button',{name:'回到最新消息'})).toBeInTheDocument()
 await act(async()=>onEvent({v:'1.0',kind:'event',id:'01ARZ3NDEKTSV4RRFFQ69G5FAF',streamId:'01ARZ3NDEKTSV4RRFFQ69G5FAD',sequence:2,type:'delta',delta:{text:'```mermaid\nflowchart TD\nA-->B\n```'}}))
 expect(scrollTo).not.toHaveBeenCalled()
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

it('does not auto-open the browser workspace for background web.search',async()=>{
 let onEvent!:(event:StreamEvent)=>void
 const stream:ChatStream={streamId:'01ARZ3NDEKTSV4RRFFQ69G5FAD',cancel:vi.fn().mockResolvedValue(true),dispose:vi.fn()}
 const start=vi.fn().mockImplementation(async(_payload,onStreamEvent)=>{onEvent=onStreamEvent;return stream})
 const user=await open({personal:true,initialSession:session,providers,chat:{start,dispose:vi.fn()},attachments:{list:vi.fn().mockResolvedValue({items:[]}),get:vi.fn(),ingest:vi.fn(),delete:vi.fn()} as unknown as AttachmentBridge})
 await user.type(screen.getByLabelText('向月汐提问，或描述你想完成的任务…'),'现在市面上除了飞算AI，还有没有类似他的产品')
 await user.click(screen.getByRole('button',{name:'↑ 发送并对话'}))
 await waitFor(()=>expect(start).toHaveBeenCalledOnce())
 await act(async()=>onEvent({v:'1.0',kind:'event',id:'01ARZ3NDEKTSV4RRFFQ69G5FAE',streamId:stream.streamId,sequence:1,type:'tool_started',tool:{callId:'search-1',name:'web.search',argsDigest:'a'.repeat(64),summary:'搜索：飞算AI'}}))
 await act(async()=>onEvent({v:'1.0',kind:'event',id:'01ARZ3NDEKTSV4RRFFQ69G5FAF',streamId:stream.streamId,sequence:2,type:'tool_completed',tool:{callId:'search-1',name:'web.search',argsDigest:'a'.repeat(64),summary:'query: 飞算AI\nresults_url: https://cn.bing.com/search?q=%E9%A3%9E%E7%AE%97AI',artifact:{kind:'html',path:'search.html',content:'<h1>搜索结果 · 飞算AI</h1>'}}}))
 expect(screen.queryByLabelText('统一工作区')).toBeNull()
 expect(screen.queryByText('search.html')).toBeNull()
})

it('does not auto-open the workspace for pptx.gen deliverables',async()=>{
 let onEvent!:(event:StreamEvent)=>void
 const stream:ChatStream={streamId:'01ARZ3NDEKTSV4RRFFQ69G5FAD',cancel:vi.fn().mockResolvedValue(true),dispose:vi.fn()}
 const start=vi.fn().mockImplementation(async(_payload,onStreamEvent)=>{onEvent=onStreamEvent;return stream})
 const user=await open({personal:true,initialSession:session,providers,chat:{start,dispose:vi.fn()},attachments:{list:vi.fn().mockResolvedValue({items:[]}),get:vi.fn(),ingest:vi.fn(),delete:vi.fn()} as unknown as AttachmentBridge})
 await user.type(screen.getByLabelText('向月汐提问，或描述你想完成的任务…'),'帮我做一份个人介绍PPT')
 await user.click(screen.getByRole('button',{name:'↑ 发送并对话'}))
 await waitFor(()=>expect(start).toHaveBeenCalledOnce())
 await act(async()=>onEvent({v:'1.0',kind:'event',id:'01ARZ3NDEKTSV4RRFFQ69G5FAE',streamId:stream.streamId,sequence:1,type:'tool_completed',tool:{callId:'ppt-1',name:'pptx.gen',argsDigest:'a'.repeat(64),summary:'wrote deck.pptx',artifact:{kind:'pptx',path:'deck.pptx',content:''}}}))
 expect(screen.queryByLabelText('统一工作区')).toBeNull()
 expect(screen.getByText('deck.pptx')).toBeInTheDocument()
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
 const button=screen.getByRole('button',{name:'语音输入'});await user.click(button);expect(getUserMedia).toHaveBeenCalledWith(micDefault);expect(instance.start).toHaveBeenCalledOnce();expect(screen.getByRole('button',{name:'停止语音输入'})).toHaveAttribute('aria-pressed','true');expect(screen.getByRole('status',{name:'正在接收麦克风声音'})).toBeInTheDocument()
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
 expect(getUserMedia).toHaveBeenCalledWith(micDevice('usb-mic'))
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
 expect(getUserMedia).toHaveBeenNthCalledWith(1,micDevice('missing-mic'))
 expect(getUserMedia).toHaveBeenNthCalledWith(2,{audio:true})
 expect(localStorage.getItem('lunitide:microphone-device-id')).toBeNull()
 expect(screen.getByRole('button',{name:'停止语音输入'})).toBeInTheDocument()
 delete (window as any).SpeechRecognition;delete (window as any).AudioContext
})

it('opens project workbench chat with the home composer instead of the session list',async()=>{
 const attachments={list:vi.fn().mockResolvedValue({items:[]}),get:vi.fn(),ingest:vi.fn(),delete:vi.fn()} as unknown as AttachmentBridge
 render(<SessionPage project={project} bridge={sessionBridge} messages={{list:vi.fn().mockResolvedValue(page()),append:vi.fn()} as MessageBridge} onBack={vi.fn()} initialSession={session} homeChat attachments={attachments}/>)
 expect(await screen.findByText('还没有消息')).toBeInTheDocument()
 expect(screen.queryByText('新建会话')).toBeNull()
 expect(document.querySelector('.personal-chat-page')).not.toBeNull()
 expect(screen.getByLabelText('向月汐提问，或描述你想完成的任务…')).toBeInTheDocument()
 expect(screen.getByRole('button',{name:'执行模式'})).toBeInTheDocument()
})

it('does not auto-open the terminal workspace for project home-chat command activity',async()=>{
 let onEvent!:(event:StreamEvent)=>void
 const stream:ChatStream={streamId:'01ARZ3NDEKTSV4RRFFQ69G5FAD',cancel:vi.fn().mockResolvedValue(true),dispose:vi.fn()}
 const start=vi.fn().mockImplementation(async(_payload,onStreamEvent)=>{onEvent=onStreamEvent;return stream})
 const attachments={list:vi.fn().mockResolvedValue({items:[]}),get:vi.fn(),ingest:vi.fn(),delete:vi.fn()} as unknown as AttachmentBridge
 const user=userEvent.setup()
 render(<SessionPage project={project} bridge={sessionBridge} messages={{list:vi.fn().mockResolvedValue(page()),append:vi.fn()} as MessageBridge} onBack={vi.fn()} initialSession={session} homeChat attachments={attachments} providers={providers} chat={{start,approve:vi.fn(),dispose:vi.fn()}}/>)
 await screen.findByText('还没有消息')
 await user.type(screen.getByLabelText('向月汐提问，或描述你想完成的任务…'),'运行测试')
 await user.click(screen.getByRole('button',{name:'↑ 发送并对话'}))
 await waitFor(()=>expect(start).toHaveBeenCalledOnce())
 await act(async()=>onEvent({v:'1.0',kind:'event',id:'01ARZ3NDEKTSV4RRFFQ69G5FAE',streamId:stream.streamId,sequence:1,type:'tool_started',tool:{callId:'call-1',name:'command.run',argsDigest:'digest',summary:'$ go test ./...'}}))
 expect(screen.queryByLabelText('统一工作区')).toBeNull()
})

it('sends from project workbench home-chat without throwing and includes the active phase',async()=>{
 let onEvent!:(event:StreamEvent)=>void
 const stream:ChatStream={streamId:'01ARZ3NDEKTSV4RRFFQ69G5FAD',cancel:vi.fn().mockResolvedValue(true),dispose:vi.fn()}
 const start=vi.fn().mockImplementation(async(_payload,onStreamEvent)=>{onEvent=onStreamEvent;return stream})
 const append=vi.fn().mockResolvedValue({})
 const attachments={list:vi.fn().mockResolvedValue({items:[]}),get:vi.fn(),ingest:vi.fn(),delete:vi.fn()} as unknown as AttachmentBridge
 const user=userEvent.setup()
 render(<RootErrorBoundary><SessionPage project={project} bridge={sessionBridge} messages={{list:vi.fn().mockResolvedValue(page()),append} as MessageBridge} onBack={vi.fn()} initialSession={session} homeChat attachments={attachments} providers={providers} chat={{start,approve:vi.fn(),dispose:vi.fn()}} projectPhase={1} projectPhaseLabel="需求架构规范" projectSidePanel={<div>交付物</div>} projectApprovalPanel={<div>审批</div>} projectSideLabel="交付物"/></RootErrorBoundary>)
 await screen.findByText('还没有消息')
 await user.type(screen.getByLabelText('向月汐提问，或描述你想完成的任务…'),'画一下架构图')
 await expect(user.click(screen.getByRole('button',{name:'↑ 发送并对话'}))).resolves.toBeUndefined()
 await waitFor(()=>expect(start).toHaveBeenCalledOnce())
 expect(start.mock.calls[0][0]).toMatchObject({sessionId:S,projectId:P,projectPhase:1,projectPhaseLabel:'需求架构规范'})
 expect(screen.queryByText('界面遇到了一个错误')).toBeNull()
 await act(async()=>onEvent({v:'1.0',kind:'event',id:'01ARZ3NDEKTSV4RRFFQ69G5FAE',streamId:stream.streamId,sequence:1,type:'delta',delta:{text:'```mermaid\nflowchart TD\nsubgraph ui["界面"]\nA["工作台"]\nend\nA-->B\n```'}}))
 expect(screen.getByRole('status')).toBeInTheDocument()
 expect(screen.getByLabelText('向月汐提问，或描述你想完成的任务…')).toBeInTheDocument()
})

it('prefixes only selected PM chips on rethink and never the conversation catalog',async()=>{
 const ai={expertId:'01ARZ3NDEKTSV4RRFFQ69G5FAC',name:'AI 工程师',division:'engineering' as const,source:'local' as const,semver:'1.0.0',state:'enabled' as const,versionCount:1,mountedPhaseCount:0}
 const ppt={...ai,expertId:'01ARZ3NDEKTSV4RRFFQ69G5FAD',name:'PPT专家',division:'product' as const}
 const experts={list:vi.fn().mockResolvedValue({experts:[ai,ppt]}),sessionMountGet:vi.fn().mockResolvedValue({expertIds:[ai.expertId]}),sessionMountSet:vi.fn().mockResolvedValue({expertIds:[ai.expertId]}),detail:vi.fn(),create:vi.fn(),update:vi.fn(),toggle:vi.fn(),archive:vi.fn(),mount:vi.fn(),mountingGet:vi.fn(),scenarioCreate:vi.fn(),scenarioList:vi.fn(),scenarioDelete:vi.fn()} as unknown as import('../bridge/client').ExpertBridge
 const append=vi.fn().mockResolvedValue({}),start=vi.fn().mockResolvedValue({cancel:vi.fn(),dispose:vi.fn()})
 render(<SessionPage project={project} bridge={sessionBridge} messages={{list:vi.fn().mockResolvedValue(page()),append} as MessageBridge} onBack={vi.fn()} initialSession={session} homeChat experts={experts} providers={providers} chat={{start,approve:vi.fn(),dispose:vi.fn()}} projectPhase={1} projectPhaseLabel="需求架构规范"/>)
 await screen.findByText('还没有消息')
 await waitFor(()=>expect(screen.getByLabelText('已挂载专家')).toHaveTextContent('AI 工程师'))
 fireEvent.change(screen.getByLabelText('向月汐提问，或描述你想完成的任务…'),{target:{value:'重新思考，给出一个新的方案。'}})
 fireEvent.click(screen.getByRole('button',{name:'↑ 发送并对话'}))
 await waitFor(()=>expect(append).toHaveBeenCalled())
 const sent=String(vi.mocked(append).mock.calls[0][0].text)
 expect(sent).toContain(`[引用专家 AI 工程师|${ai.expertId}]`)
 expect(sent).toContain('重新思考，给出一个新的方案。')
 expect(sent).not.toContain('PPT专家')
 expect(sent).not.toContain('小说编写专家')
 expect(sent).not.toContain('报告编写专家')
})

it('shows a compact chip when context usage is high and commits the preview',async()=>{
 const compactPreview=vi.fn().mockResolvedValue({checkpointId:'01ARZ3NDEKTSV4RRFFQ69G5FAE',version:1,sourceStartSeq:1,sourceEndSeq:20,sourceDigest:'a'.repeat(64),summaryPreview:'earlier turns',humanSummary:'把更早的对话收成摘要',status:'succeeded'})
 const compactCommit=vi.fn().mockResolvedValue({checkpointId:'01ARZ3NDEKTSV4RRFFQ69G5FAE',version:1,status:'succeeded',activated:true})
 const status=vi.fn().mockResolvedValue({canonicalLogicalTokens:82000,canonicalTokenizerId:'lunitide-canonical-v1',canonicalTokenizerRevision:'v1.0.0',modelContextWindow:100000,activeCheckpointVersion:0,budgetUsage:0.82,isCompacting:false})
 const context={status,compactPreview,compactCommit,compactCancel:vi.fn(),handoffCreate:vi.fn(),handoffImport:vi.fn(),handoffInspect:vi.fn(),handoffList:vi.fn(),handoffListImports:vi.fn(),handoffRevoke:vi.fn()} as unknown as ContextBridge
 const user=await open({context})
 await user.click(await screen.findByRole('button',{name:'压缩 82%'}))
 await waitFor(()=>expect(compactPreview).toHaveBeenCalledWith({sessionId:S}))
 await user.click(screen.getByRole('button',{name:'应用压缩'}))
 await waitFor(()=>expect(compactCommit).toHaveBeenCalledWith({checkpointId:'01ARZ3NDEKTSV4RRFFQ69G5FAE',baseVersion:1}))
})

it('keeps the user.ask wizard and follow-up draft when chat.start fails after approve', async () => {
  let onEvent!: (event: StreamEvent) => void
  const start = vi.fn()
    .mockImplementationOnce(async (_payload, onStreamEvent) => {
      onEvent = onStreamEvent
      return { streamId: '01ARZ3NDEKTSV4RRFFQ69G5FAD', cancel: vi.fn(), dispose: vi.fn() }
    })
    .mockRejectedValue(new BridgeClientError('核心引擎暂时不可用', 'ENGINE_UNAVAILABLE', true, 'engine'))
  const approve = vi.fn().mockResolvedValue({ status: 'approved', summary: '已批准' })
  const user = userEvent.setup()
  render(
    <SessionPage
      project={project}
      bridge={sessionBridge}
      personal
      initialSession={session}
      providers={providers}
      chat={{ start, approve, dispose: vi.fn() }}
      messages={{ list: vi.fn().mockResolvedValue(page()), append: vi.fn().mockResolvedValue({}) } as MessageBridge}
      onBack={vi.fn()}
    />,
  )
  const input = await screen.findByLabelText('向月汐提问，或描述你想完成的任务…')
  await user.type(input, '帮我定部署方案')
  await user.click(screen.getByRole('button', { name: '↑ 发送并对话' }))
  await waitFor(() => expect(start).toHaveBeenCalledOnce())
  const ask = JSON.stringify({
    title: '需求边界',
    questions: [{
      id: 'deploy',
      prompt: '部署方式',
      options: [
        { id: 'k8s', label: '容器化' },
        { id: 'vm', label: '虚拟机' },
      ],
    }],
  })
  await act(async () => {
    onEvent({
      v: '1.0',
      kind: 'event',
      id: '01ARZ3NDEKTSV4RRFFQ69G5FAE',
      streamId: '01ARZ3NDEKTSV4RRFFQ69G5FAD',
      sequence: 1,
      type: 'approval_required',
      tool: { callId: 'ask-1', name: 'user.ask', argsDigest: 'a'.repeat(64), summary: ask },
    })
    onEvent({
      v: '1.0',
      kind: 'event',
      id: '01ARZ3NDEKTSV4RRFFQ69G5FAF',
      streamId: '01ARZ3NDEKTSV4RRFFQ69G5FAD',
      sequence: 2,
      type: 'completed',
    })
  })
  const wizard = await screen.findByRole('form', { name: '需求边界' })
  await user.click(screen.getByRole('radio', { name: /容器化/ }))
  await user.click(screen.getByRole('button', { name: '提交决策' }))
  await waitFor(() => expect(approve).toHaveBeenCalledOnce())
  await waitFor(() => expect(start).toHaveBeenCalledTimes(2))
  expect(wizard).toBeInTheDocument()
  expect(screen.getByRole('form', { name: '需求边界' })).toBeInTheDocument()
  expect((screen.getByLabelText('向月汐提问，或描述你想完成的任务…') as HTMLTextAreaElement).value).toContain('部署方式：容器化')
  expect(await screen.findByText(/这次没发出去/)).toBeInTheDocument()
  expect(screen.queryByText('ENGINE_UNAVAILABLE')).toBeNull()
})

it('does not delete 月伴对话 after removing its last round', async () => {
  const companion = { ...session, title: '月伴对话' }
  const question: MessageDTO = { id: '01ARZ3NDEKTSV4RRFFQ69G5FAC', sessionId: S, role: 'user', text: '最后一个问题', status: 'completed', sequence: 1, createdAt: NOW }
  const answer: MessageDTO = { ...question, id: '01ARZ3NDEKTSV4RRFFQ69G5FAD', role: 'assistant', text: '最后一个回答', sequence: 2 }
  const rewind = vi.fn().mockResolvedValue({ sessionId: S, messageId: question.id, deletedCount: 2, lastSequence: 0, historyRevision: 2 })
  const remove = vi.fn().mockResolvedValue({ deleted: true, id: S })
  const onDeleted = vi.fn()
  const user = userEvent.setup()
  render(
    <SessionPage
      project={project}
      bridge={{ ...sessionBridge, list: vi.fn().mockResolvedValue({ items: [companion] }), delete: remove }}
      initialSession={companion}
      personal
      messages={{ list: vi.fn().mockResolvedValue(page([question, answer])), append: vi.fn(), rewind } as MessageBridge}
      onBack={vi.fn()}
      onDeleted={onDeleted}
    />,
  )
  await screen.findByText('最后一个回答')
  await user.click(screen.getByRole('button', { name: '删除' }))
  await user.click(screen.getByRole('button', { name: '确认删除' }))
  await waitFor(() => expect(rewind).toHaveBeenCalled())
  expect(remove).not.toHaveBeenCalled()
  expect(onDeleted).not.toHaveBeenCalled()
})

it('asks before saving a finished personal chat as a skill', async () => {
  const onSaveAsSkill = vi.fn()
  const confirm = vi.spyOn(window, 'confirm').mockReturnValue(true)
  const userMsg: MessageDTO = {id:'01ARZ3NDEKTSV4RRFFQ69G5FA1',sessionId:S,role:'user',status:'completed',sequence:1,text:'整理成周报技能',createdAt:NOW}
  const agentMsg: MessageDTO = {...userMsg,id:'01ARZ3NDEKTSV4RRFFQ69G5FA2',role:'assistant',sequence:2,text:'可以按这个结构沉淀。'}
  render(<SessionPage project={project} bridge={sessionBridge} messages={{list:vi.fn().mockResolvedValue(page([userMsg,agentMsg])),append:vi.fn()} as MessageBridge} onBack={vi.fn()} personal initialSession={session} onSaveAsSkill={onSaveAsSkill}/>)
  fireEvent.click(await screen.findByRole('button',{name:'存为技能'}))
  expect(confirm).toHaveBeenCalled()
  expect(onSaveAsSkill).toHaveBeenCalledWith(expect.stringContaining('整理成周报技能'))
  confirm.mockRestore()
})
