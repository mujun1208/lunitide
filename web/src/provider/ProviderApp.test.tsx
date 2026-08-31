import { cleanup, render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, expect, it, vi } from 'vitest'
import { BridgeClientError, type ProviderBridge } from '../bridge/client'
import type { ProviderDTO } from '../generated/bridge'
import { ProviderApp } from './ProviderApp'
afterEach(cleanup)
const provider:ProviderDTO={id:'01ARZ3NDEKTSV4RRFFQ69G5FAV',name:'Demo',protocol:'openai_compatible',baseUrl:'https://example.com',models:[{modelId:'m',displayName:'M',isDefault:true}],status:'enabled',credentialState:'configured',createdAt:new Date().toISOString(),updatedAt:new Date().toISOString(),version:1}
const credential={credentialSubmissionId:'01ARZ3NDEKTSV4RRFFQ69G5FAA',providerId:provider.id,expiresAt:new Date().toISOString(),expiresInSeconds:60}
function api(overrides:Partial<ProviderBridge>={}):ProviderBridge{return{list:vi.fn().mockResolvedValue({items:[]}),get:vi.fn(),create:vi.fn().mockResolvedValue(provider),update:vi.fn(),delete:vi.fn(),revealCredential:vi.fn().mockResolvedValue({credential:'saved-key'}),submitCredential:vi.fn().mockResolvedValue(credential),syncModels:vi.fn(),test:vi.fn(),...overrides}}
async function fillCreate(user:ReturnType<typeof userEvent.setup>,urlValue='https://example.com/v1'){await user.click(screen.getByRole('button',{name:/新建供应商/}));await user.type(screen.getByLabelText('供应商名称'),'Demo');const url=screen.getByLabelText('基础 URL');await user.clear(url);await user.type(url,urlValue);await user.type(screen.getByLabelText('模型 1 ID'),'m');await user.type(screen.getByLabelText('模型 1 显示名称'),'M')}
it('submits exact public request before bound create',async()=>{const bridge=api(),user=userEvent.setup();render(<ProviderApp bridge={bridge}/>);expect(await screen.findByText('还没有供应商')).toBeInTheDocument();await fillCreate(user);await user.type(screen.getByLabelText(/API 凭据/),'test-only');await user.click(screen.getByRole('button',{name:'安全保存'}));await waitFor(()=>expect(bridge.create).toHaveBeenCalledOnce());const submitted=vi.mocked(bridge.submitCredential).mock.calls[0][0],created=vi.mocked(bridge.create).mock.calls[0][0];expect(submitted.request).not.toHaveProperty('credentialSubmissionId');expect(created.credentialSubmissionId).toBe(credential.credentialSubmissionId)})
it('writes chat prefer after a successful LLM save',async()=>{const onPreferLLM=vi.fn(),bridge=api(),user=userEvent.setup();render(<ProviderApp bridge={bridge} onPreferLLM={onPreferLLM}/>);await screen.findByText('还没有供应商');await fillCreate(user);await user.type(screen.getByLabelText(/API 凭据/),'test-only');await user.click(screen.getByRole('button',{name:'安全保存'}));await waitFor(()=>expect(onPreferLLM).toHaveBeenCalledWith(provider.id,'m'))})
it.each(['https://例子.com','https://exa_mple.com'] as const)('prevents invalid HTTPS/ASCII host %s in the UI',async bad=>{const bridge=api(),user=userEvent.setup();render(<ProviderApp bridge={bridge}/>);await screen.findByText('还没有供应商');await fillCreate(user,bad);await user.click(screen.getByRole('button',{name:'安全保存'}));expect(await screen.findByRole('status')).toHaveTextContent(/HTTPS|ASCII|主机名/);expect(bridge.create).not.toHaveBeenCalled();expect(bridge.submitCredential).not.toHaveBeenCalled()})
it('accepts plaintext http origin for local providers in the UI',async()=>{const bridge=api(),user=userEvent.setup();render(<ProviderApp bridge={bridge}/>);await screen.findByText('还没有供应商');await fillCreate(user,'http://127.0.0.1:11434/v1');await user.click(screen.getByRole('button',{name:'安全保存'}));await waitFor(()=>expect(bridge.create).toHaveBeenCalledOnce());expect(vi.mocked(bridge.create).mock.calls[0][0].baseUrl).toBe('http://127.0.0.1:11434/v1');expect(bridge.submitCredential).not.toHaveBeenCalled()})
it('retains exact retryable public mutation attempt and blocks busy re-entry',async()=>{let rejectFirst!:(e:unknown)=>void;const first=new Promise<ProviderDTO>((_,reject)=>{rejectFirst=reject});const create=vi.fn().mockReturnValueOnce(first).mockResolvedValue(provider),bridge=api({create}),user=userEvent.setup();render(<ProviderApp bridge={bridge}/>);await screen.findByText('还没有供应商');await fillCreate(user);await user.click(screen.getByRole('button',{name:'安全保存'}));await user.click(screen.getByRole('button',{name:'处理中…'}));expect(create).toHaveBeenCalledOnce();rejectFirst(new BridgeClientError('uncertain','TIMEOUT',true,'trace'));await waitFor(()=>expect(screen.getAllByText('uncertain')).not.toHaveLength(0));await user.click(screen.getByRole('button',{name:'安全保存'}));await waitFor(()=>expect(create).toHaveBeenCalledTimes(2));expect(create.mock.calls[1][0]).toEqual(create.mock.calls[0][0]);expect(create.mock.calls[1][1]?.attempt).toBe(create.mock.calls[0][1]?.attempt)})
it('does not auto-resend cleared credential plaintext after an uncertain credential outcome',async()=>{const submitCredential=vi.fn().mockRejectedValue(new BridgeClientError('uncertain credential','TIMEOUT',true,'trace')),bridge=api({submitCredential}),user=userEvent.setup();render(<ProviderApp bridge={bridge}/>);await screen.findByText('还没有供应商');await fillCreate(user);const secret=screen.getByLabelText(/API 凭据/);await user.type(secret,'one-use-secret');await user.click(screen.getByRole('button',{name:'安全保存'}));expect((await screen.findAllByText('uncertain credential')).length).toBeGreaterThan(0);expect(secret).toHaveValue('');await user.click(screen.getByRole('button',{name:'安全保存'}));expect(await screen.findByText(/不会自动继续保存/)).toBeInTheDocument();expect(submitCredential).toHaveBeenCalledOnce();expect(bridge.create).not.toHaveBeenCalled()})
it('shows refresh errors even while stale providers remain visible',async()=>{const list=vi.fn().mockResolvedValueOnce({items:[provider]}).mockRejectedValueOnce(new BridgeClientError('refresh failed','OFFLINE',true,'trace')),bridge=api({list}),user=userEvent.setup();render(<ProviderApp bridge={bridge}/>);expect(await screen.findByText('Demo')).toBeInTheDocument();await user.click(screen.getByRole('button',{name:'刷新供应商'}));expect(await screen.findByText('refresh failed')).toBeInTheDocument();expect(screen.getByText('Demo')).toBeInTheDocument()})
it('reveals a configured credential visibly and does not resubmit it unchanged',async()=>{const configured={...provider,credentialState:'configured' as const},update=vi.fn().mockResolvedValue(configured),bridge=api({list:vi.fn().mockResolvedValue({items:[configured]}),get:vi.fn().mockResolvedValue(configured),update}),user=userEvent.setup();render(<ProviderApp bridge={bridge}/>);await user.click(await screen.findByRole('button',{name:/Demo/}));await user.click(await screen.findByRole('button',{name:'编辑'}));expect(bridge.revealCredential).not.toHaveBeenCalled();await user.click(screen.getByRole('button',{name:'显示已保存 API Key'}));const secret=await screen.findByLabelText(/^API Key/);await waitFor(()=>expect(secret).toHaveValue('saved-key'));expect(secret).toHaveAttribute('type','password');await user.click(screen.getByRole('button',{name:'显示 API Key'}));expect(secret).toHaveAttribute('type','text');await user.click(screen.getByRole('button',{name:'隐藏 API Key'}));expect(secret).toHaveAttribute('type','password');await user.click(screen.getByRole('button',{name:'安全保存'}));await waitFor(()=>expect(update).toHaveBeenCalledOnce());expect(bridge.submitCredential).not.toHaveBeenCalled()})
it('replaces a revealed credential only after the user edits it',async()=>{const update=vi.fn().mockResolvedValue(provider),bridge=api({list:vi.fn().mockResolvedValue({items:[provider]}),get:vi.fn().mockResolvedValue(provider),update}),user=userEvent.setup();render(<ProviderApp bridge={bridge}/>);await user.click(await screen.findByRole('button',{name:/Demo/}));await user.click(screen.getByRole('button',{name:'编辑'}));await user.click(screen.getByRole('button',{name:'显示已保存 API Key'}));const secret=await screen.findByLabelText(/^API Key/);await waitFor(()=>expect(secret).toHaveValue('saved-key'));await user.clear(secret);await user.type(secret,'replacement');await user.click(screen.getByRole('button',{name:'安全保存'}));await waitFor(()=>expect(bridge.submitCredential).toHaveBeenCalledOnce());expect(vi.mocked(bridge.submitCredential).mock.calls[0][0].credential).toBe('replacement')})
it('clears credential state on cancel and ignores a late reveal',async()=>{let resolve!:(value:{credential:string})=>void;const revealCredential=vi.fn().mockReturnValueOnce(new Promise(r=>{resolve=r})).mockReturnValueOnce(new Promise(()=>{})),bridge=api({list:vi.fn().mockResolvedValue({items:[provider]}),get:vi.fn().mockResolvedValue(provider),revealCredential}),user=userEvent.setup();render(<ProviderApp bridge={bridge}/>);await user.click(await screen.findByRole('button',{name:/Demo/}));await user.click(screen.getByRole('button',{name:'编辑'}));await user.click(screen.getByRole('button',{name:'显示已保存 API Key'}));await user.click(screen.getByRole('button',{name:'取消'}));resolve({credential:'late-secret'});await Promise.resolve();await user.click(screen.getByRole('button',{name:'编辑'}));expect(await screen.findByLabelText(/^API Key/)).toHaveValue('')})
it('handles CAS update conflict by fetching latest and never auto-retrying',async()=>{const latest={...provider,name:'Server Demo',version:2},update=vi.fn().mockRejectedValue(new BridgeClientError('conflict','PROVIDER_VERSION_CONFLICT',false,'trace')),get=vi.fn().mockResolvedValueOnce(provider).mockResolvedValueOnce(latest),bridge=api({list:vi.fn().mockResolvedValue({items:[provider]}),get,update}),user=userEvent.setup();render(<ProviderApp bridge={bridge}/>);await user.click(await screen.findByRole('button',{name:/Demo/}));await screen.findByRole('heading',{name:'Demo'});await user.click(screen.getByRole('button',{name:'编辑'}));const name=screen.getByLabelText('供应商名称');await user.clear(name);await user.type(name,'My edit');await user.click(screen.getByRole('button',{name:'安全保存'}));expect(await screen.findByText('保存发生并发冲突')).toBeInTheDocument();expect(screen.getByText(/服务器现在是 v2/)).toBeInTheDocument();expect(update).toHaveBeenCalledOnce();expect(vi.mocked(update).mock.calls[0][0].expectedVersion).toBe(1);await user.click(screen.getByRole('button',{name:'载入服务器版本'}));expect(screen.getByLabelText('供应商名称')).toHaveValue('Server Demo');expect(update).toHaveBeenCalledOnce()})


it('does not let a late reveal overwrite a replacement edited in the same generation',async()=>{
 let resolveReveal!:(value:{credential:string})=>void
 const revealCredential=vi.fn().mockReturnValue(new Promise(r=>{resolveReveal=r})),update=vi.fn().mockResolvedValue(provider)
 const bridge=api({list:vi.fn().mockResolvedValue({items:[provider]}),get:vi.fn().mockResolvedValue(provider),revealCredential,update}),user=userEvent.setup()
 render(<ProviderApp bridge={bridge}/>);await user.click(await screen.findByRole('button',{name:/Demo/}));await user.click(screen.getByRole('button',{name:'编辑'}));await user.click(screen.getByRole('button',{name:'显示已保存 API Key'}))
 const secret=await screen.findByLabelText(/^API Key/);await user.type(secret,'replacement');resolveReveal({credential:'saved-key'});await waitFor(()=>expect(secret).toHaveValue('replacement'))
 await user.click(screen.getByRole('button',{name:'安全保存'}));await waitFor(()=>expect(update).toHaveBeenCalledOnce())
 expect(vi.mocked(bridge.submitCredential).mock.calls[0][0].credential).toBe('replacement')
})

it('clears stale selected details when refresh removes the provider',async()=>{const list=vi.fn().mockResolvedValueOnce({items:[provider]}).mockResolvedValueOnce({items:[]}),bridge=api({list,get:vi.fn().mockResolvedValue(provider)}),user=userEvent.setup();render(<ProviderApp bridge={bridge}/>);await user.click(await screen.findByRole('button',{name:/Demo/}));expect(await screen.findByRole('heading',{name:'Demo'})).toBeInTheDocument();await user.click(screen.getByRole('button',{name:'刷新供应商'}));expect(await screen.findByText('选择供应商查看配置')).toBeInTheDocument();expect(screen.queryByRole('heading',{name:'Demo'})).not.toBeInTheDocument()})

it('blocks credential-less create after submitted credential and non-retryable create failure',async()=>{
 const create=vi.fn().mockRejectedValueOnce(new BridgeClientError('invalid config','INVALID_REQUEST',false,'trace')).mockResolvedValue(provider),bridge=api({create}),user=userEvent.setup()
 render(<ProviderApp bridge={bridge}/>);await screen.findByText('还没有供应商');await fillCreate(user);await user.type(screen.getByLabelText(/API 凭据/),'one-use');await user.click(screen.getByRole('button',{name:'安全保存'}))
 expect(await screen.findByText('凭据已提交但配置未保存，请重新输入 API Key')).toBeInTheDocument();await user.click(screen.getByRole('button',{name:'安全保存'}))
 expect(screen.getByText('凭据已提交但配置未保存，请重新输入 API Key')).toBeInTheDocument();expect(bridge.submitCredential).toHaveBeenCalledOnce();expect(create).toHaveBeenCalledOnce()
 await user.type(screen.getByLabelText(/API 凭据/),'replacement');await user.click(screen.getByRole('button',{name:'安全保存'}));await waitFor(()=>expect(create).toHaveBeenCalledTimes(2));expect(bridge.submitCredential).toHaveBeenCalledTimes(2)
})

it('blocks credential-less update after submitted credential and CAS conflict until re-entry',async()=>{
 const latest={...provider,version:2},update=vi.fn().mockRejectedValue(new BridgeClientError('conflict','PROVIDER_VERSION_CONFLICT',false,'trace'))
 const get=vi.fn().mockResolvedValueOnce(provider).mockResolvedValueOnce(latest),bridge=api({list:vi.fn().mockResolvedValue({items:[provider]}),get,update}),user=userEvent.setup()
 render(<ProviderApp bridge={bridge}/>);await user.click(await screen.findByRole('button',{name:/Demo/}));await user.click(screen.getByRole('button',{name:'编辑'}));await user.click(screen.getByRole('button',{name:'显示已保存 API Key'}));const secret=await screen.findByLabelText(/^API Key/);await waitFor(()=>expect(secret).toHaveValue('saved-key'));await user.clear(secret);await user.type(secret,'replacement');await user.click(screen.getByRole('button',{name:'安全保存'}))
 expect(await screen.findByText('凭据已提交但配置未保存，请重新输入 API Key')).toBeInTheDocument();expect(await screen.findByText('保存发生并发冲突')).toBeInTheDocument();await user.click(screen.getByRole('button',{name:'安全保存'}));expect(update).toHaveBeenCalledOnce();expect(bridge.submitCredential).toHaveBeenCalledOnce()
})

it('rebinds a local OpenAI-compatible origin change with a placeholder credential',async()=>{
 const local={...provider,name:'LM Studio',baseUrl:'http://127.0.0.1:1234'}
 const saved={...local,baseUrl:'http://192.168.31.100:1234',version:2}
 const update=vi.fn().mockResolvedValue(saved)
 const bridge=api({list:vi.fn().mockResolvedValue({items:[local]}),get:vi.fn().mockResolvedValue(local),update})
 const user=userEvent.setup()
 render(<ProviderApp bridge={bridge}/>)
 await user.click(await screen.findByRole('button',{name:/LM Studio/}))
 await user.click(screen.getByRole('button',{name:'编辑'}))
 const url=screen.getByLabelText('基础 URL')
 await user.clear(url)
 await user.type(url,'http://192.168.31.100:1234')
 await user.click(screen.getByRole('button',{name:'安全保存'}))
 await waitFor(()=>expect(bridge.submitCredential).toHaveBeenCalledOnce())
 expect(vi.mocked(bridge.submitCredential).mock.calls[0][0].credential).toBe('lm-studio')
 await waitFor(()=>expect(update).toHaveBeenCalledOnce())
 expect(vi.mocked(update).mock.calls[0][0].credentialSubmissionId).toBe(credential.credentialSubmissionId)
})

it('asks for API Key re-entry when a remote origin changes',async()=>{
 const update=vi.fn().mockResolvedValue(provider)
 const bridge=api({list:vi.fn().mockResolvedValue({items:[provider]}),get:vi.fn().mockResolvedValue(provider),update})
 const user=userEvent.setup()
 render(<ProviderApp bridge={bridge}/>)
 await user.click(await screen.findByRole('button',{name:/Demo/}))
 await user.click(screen.getByRole('button',{name:'编辑'}))
 const url=screen.getByLabelText('基础 URL')
 await user.clear(url)
 await user.type(url,'https://example.org/v1')
 await user.click(screen.getByRole('button',{name:'安全保存'}))
 expect(await screen.findByRole('status')).toHaveTextContent(/重新填写 API Key|lm-studio/)
 expect(update).not.toHaveBeenCalled()
 expect(bridge.submitCredential).not.toHaveBeenCalled()
})

it('creates a vision catalog model from the vision tab',async()=>{
 const create=vi.fn().mockImplementation(async payload=>({...provider,name:payload.name,models:payload.models,version:1}))
 const bridge=api({create}),user=userEvent.setup()
 render(<ProviderApp bridge={bridge}/>)
 await screen.findByText('还没有供应商')
 await user.click(screen.getByRole('tab',{name:'视觉模型'}))
 await fillCreate(user)
 await user.click(screen.getByRole('button',{name:'安全保存'}))
 await waitFor(()=>expect(create).toHaveBeenCalledOnce())
 const models=vi.mocked(create).mock.calls[0][0].models
 expect(models[0]).toMatchObject({modelId:'m',kind:'vision',kindDefault:true,isDefault:true})
})

it('creates a volc speech provider from the voice tab',async()=>{
 const create=vi.fn().mockImplementation(async payload=>({...provider,name:payload.name,protocol:payload.protocol,baseUrl:payload.baseUrl,models:payload.models,version:1}))
 const bridge=api({create}),user=userEvent.setup()
 render(<ProviderApp bridge={bridge}/>)
 await screen.findByText('还没有供应商')
 await user.click(screen.getByRole('tab',{name:'语音模型'}))
 await user.click(screen.getByRole('button',{name:/新建供应商/}))
 expect(screen.getByLabelText('协议')).toHaveValue('volc_speech')
 expect(screen.getByLabelText('基础 URL')).toHaveValue('https://openspeech.bytedance.com')
 expect(screen.getByLabelText('模型 1 ID')).toHaveValue('volc.seedasr.sauc.duration')
 expect(screen.getByLabelText('模型 1 类型')).toHaveValue('voice')
 expect(screen.queryByLabelText('模型 1 上下文窗口')).not.toBeInTheDocument()
 await user.type(screen.getByLabelText('供应商名称'),'Volc')
 await user.type(screen.getByLabelText(/API 凭据/),'test-only')
 await user.click(screen.getByRole('button',{name:'安全保存'}))
 await waitFor(()=>expect(create).toHaveBeenCalledOnce())
 const saved=vi.mocked(create).mock.calls[0][0]
 expect(saved.protocol).toBe('volc_speech')
 expect(saved.baseUrl).toBe('https://openspeech.bytedance.com')
 expect(saved.models[0]).toMatchObject({modelId:'volc.seedasr.sauc.duration',kind:'voice',kindDefault:true,isDefault:true})
})

