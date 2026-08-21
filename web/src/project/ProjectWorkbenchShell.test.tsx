import{cleanup,render,screen,waitFor}from'@testing-library/react'
import{afterEach,expect,it,vi}from'vitest'
import type{ChatBridge,MessageBridge,ProjectBridge,ProviderBridge,SessionBridge,StageBridge}from'../bridge/client'
import type{ProjectDTO,SessionDTO}from'../generated/bridge'
import{ProjectWorkbenchShell}from'./ProjectWorkbenchShell'

vi.mock('../session/SessionPage',()=>({SessionPage:(props:{homeChat?:boolean;initialSession?:{id:string;title:string};projectSidePanel?:unknown})=> <div data-testid="workbench-chat">{props.homeChat?'home':'legacy'}:{props.initialSession?.id??'none'}:{props.initialSession?.title??''}:{props.projectSidePanel?'nested':'none'}</div>}))
vi.mock('./DeliverablePanel',()=>({DeliverablePanel:()=><div data-testid="deliverable-panel">交付物</div>}))
vi.mock('./RegistryPanel',()=>({RegistryPanel:()=><div data-testid="registry-panel">注册表</div>}))
vi.mock('./ReleasePanel',()=>({ReleasePanel:()=><div data-testid="release-panel">发布</div>}))

afterEach(cleanup)
const NOW='2025-01-01T00:00:00Z'
const project:ProjectDTO={id:'01ARZ3NDEKTSV4RRFFQ69G5FAV',name:'在线电商',projectCode:'ITM00003',type:'implementation',status:'active',createdAt:NOW,updatedAt:NOW,version:1}
const session:SessionDTO={id:'01ARZ3NDEKTSV4RRFFQ69G5FAA',projectId:project.id,title:'你好',pinned:false,status:'active',createdAt:NOW,updatedAt:NOW,version:1}

it('reuses the latest project session and renders home-chat in the middle column',async()=>{
 const sessions={list:vi.fn().mockResolvedValue({items:[session]}),create:vi.fn(),update:vi.fn(),delete:vi.fn()} as unknown as SessionBridge
 const stages={list:vi.fn().mockResolvedValue({items:[]}),create:vi.fn(),update:vi.fn()} as unknown as StageBridge
 render(<ProjectWorkbenchShell project={project} projects={{} as ProjectBridge} sessions={sessions} messages={{} as MessageBridge} stages={stages} chat={{} as ChatBridge} providers={{} as ProviderBridge} onBack={vi.fn()}/>)
 await waitFor(()=>expect(screen.getByTestId('workbench-chat')).toHaveTextContent(`home:${session.id}:你好:nested`))
 expect(sessions.create).not.toHaveBeenCalled()
})

it('creates a project session when none exist so the middle column is immediately a chat',async()=>{
 const created={...session,id:'01ARZ3NDEKTSV4RRFFQ69G5FAB',title:project.name}
 const sessions={list:vi.fn().mockResolvedValue({items:[]}),create:vi.fn().mockResolvedValue(created),update:vi.fn(),delete:vi.fn()} as unknown as SessionBridge
 const stages={list:vi.fn().mockResolvedValue({items:[]}),create:vi.fn(),update:vi.fn()} as unknown as StageBridge
 render(<ProjectWorkbenchShell project={project} projects={{} as ProjectBridge} sessions={sessions} messages={{} as MessageBridge} stages={stages} chat={{} as ChatBridge} providers={{} as ProviderBridge} onBack={vi.fn()}/>)
 await waitFor(()=>expect(sessions.create).toHaveBeenCalledOnce())
 expect(await screen.findByTestId('workbench-chat')).toHaveTextContent(`home:${created.id}:${project.name}:nested`)
})
