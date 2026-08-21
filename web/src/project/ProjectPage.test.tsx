import{cleanup,fireEvent,render,screen,waitFor}from'@testing-library/react'
import userEvent from'@testing-library/user-event'
import{afterEach,expect,it,vi}from'vitest'
import{BridgeClientError,type ProjectBridge}from'../bridge/client'
import type{ProjectDTO}from'../generated/bridge'
import{normalizeProjectName,ProjectPage}from'./ProjectPage'

afterEach(cleanup)
const now='2025-01-01T00:00:00Z'
const created:ProjectDTO={id:'01ARZ3NDEKTSV4RRFFQ69G5FAV',name:'<img src=x onerror=alert(1)>',projectCode:'ITM00001',type:'implementation',status:'created',description:'demo',client:'Acme',createdAt:now,updatedAt:now,version:1,planStart:'2026-01-01',planEnd:'2026-06-30'}
const active:ProjectDTO={...created,id:'01ARZ3NDEKTSV4RRFFQ69G5FAB',name:'Active',status:'active',version:2}
const api=(overrides:Partial<ProjectBridge>={}):ProjectBridge=>({list:vi.fn().mockResolvedValue({items:[]}),create:vi.fn().mockResolvedValue(created),update:vi.fn(),publish:vi.fn().mockImplementation(async payload=>({...created,id:payload.id,status:'chartered' as const,version:2})),close:vi.fn(),reopen:vi.fn(),advanceStatus:vi.fn(),delete:vi.fn().mockResolvedValue({deleted:true,id:created.id}),...overrides})
const fillRequired=async(user:ReturnType<typeof userEvent.setup>,container:HTMLElement)=>{
 await user.type(screen.getByLabelText('C 项目名称',{exact:false}),'  Moon   Tide  ')
 await user.type(screen.getByLabelText('D 项目描述',{exact:false}),'A demo project')
 await user.type(screen.getByLabelText('G 客户',{exact:false}),'Acme')
 const dates=container.querySelectorAll('input[type="date"]')
 fireEvent.change(dates[0],{target:{value:'2026-01-01'}})
 fireEvent.change(dates[1],{target:{value:'2026-06-30'}})
}

it('normalizes Go-style whitespace and counts astral code points',()=>{expect(normalizeProjectName(' \t月\n\u00a0汐 ')).toBe('月 汐');expect(Array.from('😀')).toHaveLength(1)})

it('shows the management table, validates required A–N fields, normalizes create, and renders names as inert text',async()=>{
 const bridge=api(),user=userEvent.setup()
 const{container}=render(<ProjectPage bridge={bridge}/>)
 expect(await screen.findByText('还没有项目')).toBeInTheDocument()
 expect(screen.getByRole('heading',{name:'项目管理'})).toBeInTheDocument()
 await user.click(screen.getByRole('button',{name:/创建项目/}))
 expect(screen.getByRole('button',{name:/创建项目/})).toHaveClass('primary')
 expect(screen.getByText('创建项目 · A–N')).toBeInTheDocument()
 await user.click(screen.getByRole('button',{name:'保存项目'}))
 expect(await screen.findByText('请输入项目名称（C）')).toBeInTheDocument()
 await fillRequired(user,container)
 await user.click(screen.getByRole('button',{name:'保存项目'}))
 await waitFor(()=>expect(bridge.create).toHaveBeenCalledOnce())
 expect(vi.mocked(bridge.create).mock.calls[0][0]).toEqual({name:'Moon Tide',type:'implementation',description:'A demo project',summary:'',objective:'',client:'Acme',contractNo:'',amount:0,budget:0,planStart:'2026-01-01',planEnd:'2026-06-30',remark:''})
 expect(await screen.findByText('项目已创建，编号 ITM00001')).toBeInTheDocument()
 expect(screen.getByText(created.name)).toBeInTheDocument()
 expect(document.querySelector('img')).toBeNull()
})

it('blocks busy re-entry and retains the same attempt for a retryable retry',async()=>{
 let reject!:(e:unknown)=>void
 const first=new Promise<ProjectDTO>((_,r)=>{reject=r}),create=vi.fn().mockReturnValueOnce(first).mockResolvedValue(created),bridge=api({create}),user=userEvent.setup()
 const{container}=render(<ProjectPage bridge={bridge}/>)
 await screen.findByText('还没有项目')
 await user.click(screen.getByRole('button',{name:/创建项目/}))
 await fillRequired(user,container)
 await user.click(screen.getByRole('button',{name:'保存项目'}))
 expect(create).toHaveBeenCalledOnce()
 expect(screen.getByRole('button',{name:'保存中…'})).toBeDisabled()
 reject(new BridgeClientError('uncertain','TIMEOUT',true,'trace'))
 expect(await screen.findByText('uncertain')).toBeInTheDocument()
 await user.click(screen.getByRole('button',{name:'保存项目'}))
 await waitFor(()=>expect(create).toHaveBeenCalledTimes(2))
 expect(create.mock.calls[1][1]?.attempt).toBe(create.mock.calls[0][1]?.attempt)
})

it('keeps stale projects visible when refresh fails',async()=>{
 const list=vi.fn().mockResolvedValueOnce({items:[active]}).mockRejectedValueOnce(new BridgeClientError('offline','OFFLINE',true,'trace')),bridge=api({list}),user=userEvent.setup()
 render(<ProjectPage bridge={bridge}/>)
 expect(await screen.findByText('Active')).toBeInTheDocument()
 await user.click(screen.getByRole('button',{name:'刷新项目'}))
 expect(await screen.findByText('offline')).toBeInTheDocument()
 expect(screen.getByText('Active')).toBeInTheDocument()
})

it('orders by updatedAt DESC, filters by query, and enters the workbench from an active row',async()=>{
 const older={...active,id:'01ARZ3NDEKTSV4RRFFQ69G5FAA',name:'Older',updatedAt:'2025-01-01T00:00:00Z'}
 const newer={...active,id:'01ARZ3NDEKTSV4RRFFQ69G5FAC',name:'Newer',updatedAt:'2025-01-03T00:00:00Z'}
 const beta={...active,id:'01ARZ3NDEKTSV4RRFFQ69G5FAZ',name:'Beta',projectCode:'ITM00009'}
 const onSelect=vi.fn(),user=userEvent.setup(),{container}=render(<ProjectPage bridge={api({list:vi.fn().mockResolvedValue({items:[older,beta,newer]})})} onSelect={onSelect}/>)
 await screen.findByText('Older')
 expect([...container.querySelectorAll('.pm-list .pm-item-title b')].map(node=>node.textContent)).toEqual(['Newer','Beta','Older'])
 await user.type(screen.getByLabelText('搜索项目'),'bet')
 expect(screen.queryByText('Older')).not.toBeInTheDocument()
 await user.click(screen.getByRole('button',{name:'进入工作台'}))
 expect(onSelect).toHaveBeenCalledWith(beta)
})

it('saves an edited created project and publishes it into the workbench',async()=>{
 const saved={...created,name:'在线电商',version:2,client:'范德萨',amount:11,contractNo:'ht-222-06',description:'大范德萨',planStart:'2026-08-18',planEnd:'2026-12-18'}
 const update=vi.fn().mockResolvedValue(saved)
 const bridge=api({list:vi.fn().mockResolvedValue({items:[created]}),update,publish:vi.fn().mockImplementation(async payload=>({...saved,id:payload.id,status:'chartered' as const,version:3}))}),user=userEvent.setup()
 render(<ProjectPage bridge={bridge}/>)
 await user.click(await screen.findByRole('button',{name:'修改'}))
 expect(screen.getByText('修改项目 · ITM00001')).toBeInTheDocument()
 await user.click(screen.getByRole('button',{name:'保存项目'}))
 await waitFor(()=>expect(update).toHaveBeenCalledOnce())
 expect(vi.mocked(update).mock.calls[0][0]).toEqual({id:created.id,version:1,name:'<img src=x onerror=alert(1)>',type:'implementation',description:'demo',summary:'',objective:'',client:'Acme',contractNo:'',amount:0,budget:0,planStart:'2026-01-01',planEnd:'2026-06-30',remark:''})
 expect(await screen.findByText('项目已保存')).toBeInTheDocument()
})

it('gates lifecycle actions: publish confirm flips created to chartered and enables the workbench',async()=>{
 const bridge=api({list:vi.fn().mockResolvedValue({items:[created]})}),user=userEvent.setup()
 render(<ProjectPage bridge={bridge}/>)
 await screen.findByText(created.name)
 expect(screen.queryByRole('button',{name:'进入工作台'})).not.toBeInTheDocument()
 await user.click(screen.getByRole('button',{name:'立项'}))
 expect(screen.getByText(`立项 ${created.projectCode}`)).toBeInTheDocument()
 await user.click(screen.getByRole('button',{name:'确认立项'}))
 await waitFor(()=>expect(bridge.publish).toHaveBeenCalledOnce())
 expect(vi.mocked(bridge.publish).mock.calls[0][0]).toEqual({id:created.id,version:created.version})
 expect(await screen.findByText('已立项 ITM00001，现在可以进入工作台')).toBeInTheDocument()
 expect(screen.getByRole('button',{name:'进入工作台'})).toBeInTheDocument()
})

it('requires the application danger dialog before deleting a created project',async()=>{
 const bridge=api({list:vi.fn().mockResolvedValue({items:[created]})}),user=userEvent.setup()
 render(<ProjectPage bridge={bridge}/>)
 await user.click(await screen.findByRole('button',{name:'删除'}))
 expect(screen.getByText(`删除项目「${created.name}」？`)).toBeInTheDocument()
 expect(bridge.delete).not.toHaveBeenCalled()
 await user.click(screen.getByRole('button',{name:'确认删除'}))
 await waitFor(()=>expect(bridge.delete).toHaveBeenCalledOnce())
 expect(screen.queryByText(created.name)).not.toBeInTheDocument()
})
