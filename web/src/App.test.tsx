import{cleanup,render,screen}from'@testing-library/react'
import userEvent from'@testing-library/user-event'
import{afterEach,expect,it,vi}from'vitest'
import type{ProjectBridge,ProviderBridge}from'./bridge/client'
import{App}from'./App'
afterEach(cleanup)
it('navigates between top-level projects and providers',async()=>{const projects:ProjectBridge={list:vi.fn().mockResolvedValue({items:[]}),create:vi.fn()},providers={list:vi.fn().mockResolvedValue({items:[]}),get:vi.fn(),create:vi.fn(),update:vi.fn(),delete:vi.fn(),submitCredential:vi.fn(),syncModels:vi.fn(),test:vi.fn()}as ProviderBridge,user=userEvent.setup();render(<App projects={projects} providers={providers}/>);expect(await screen.findByRole('heading',{name:'项目列表'})).toBeInTheDocument();await user.click(screen.getByRole('button',{name:'供应商'}));expect(await screen.findByText('还没有供应商')).toBeInTheDocument()})
