import{cleanup,render,screen}from'@testing-library/react'
import userEvent from'@testing-library/user-event'
import{afterEach,expect,it,vi}from'vitest'
import type{ChatBridge,MessageBridge,ProjectBridge,ProviderBridge,SessionBridge,StageBridge}from'./bridge/client'
import{App}from'./App'
afterEach(cleanup)
const stages:StageBridge={list:vi.fn().mockResolvedValue({items:[]}),create:vi.fn()}
const messages:MessageBridge={list:vi.fn().mockResolvedValue({items:[],hasMore:false,nextCursor:null,snapshotSequence:0}),append:vi.fn()}
const chat:ChatBridge={start:vi.fn(),dispose:vi.fn()}
const providers:ProviderBridge={list:vi.fn().mockResolvedValue({items:[]}),get:vi.fn(),create:vi.fn(),update:vi.fn(),delete:vi.fn(),submitCredential:vi.fn(),syncModels:vi.fn(),test:vi.fn()}as ProviderBridge
it('navigates between top-level projects and providers',async()=>{const projects:ProjectBridge={list:vi.fn().mockResolvedValue({items:[]}),create:vi.fn(),delete:vi.fn()},user=userEvent.setup();render(<App projects={projects} providers={providers} stages={stages} messages={messages} chat={chat}/>);expect(await screen.findByRole('heading',{name:'项目列表'})).toBeInTheDocument();await user.click(screen.getByRole('button',{name:'供应商'}));expect(await screen.findByText('还没有供应商')).toBeInTheDocument()})
it('opens a selected project session page with projectId and returns',async()=>{const project={id:'01ARZ3NDEKTSV4RRFFQ69G5FAV',name:'Moon',status:'active' as const,createdAt:'2026-01-01T00:00:00Z',updatedAt:'2026-01-01T00:00:00Z',version:1},projects:ProjectBridge={list:vi.fn().mockResolvedValue({items:[project]}),create:vi.fn(),delete:vi.fn()},sessions:SessionBridge={list:vi.fn().mockResolvedValue({items:[]}),create:vi.fn(),delete:vi.fn()},user=userEvent.setup();render(<App projects={projects} sessions={sessions} stages={stages} messages={messages} chat={chat} providers={providers}/>);await user.click(await screen.findByRole('heading',{name:'Moon'}));expect(await screen.findByRole('heading',{name:'Moon · 会话'})).toBeInTheDocument();expect(sessions.list).toHaveBeenCalledWith({projectId:project.id});await user.click(screen.getByRole('button',{name:'← 返回项目'}));expect(await screen.findByRole('heading',{name:'项目列表'})).toBeInTheDocument()})
