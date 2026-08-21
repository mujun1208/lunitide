import{cleanup,render,screen}from'@testing-library/react'
import userEvent from'@testing-library/user-event'
import{afterEach,expect,it,vi}from'vitest'
import{CapabilityCenter}from'./CapabilityCenter'

afterEach(cleanup)
vi.mock('./PluginPage',()=>({PluginPage:()=><h1>插件</h1>}))
vi.mock('../mcp/McpPage',()=>({McpPage:()=><h1>MCP</h1>}))

it('keeps plugin and MCP as one surface with tabs',async()=>{
 const user=userEvent.setup()
 render(<CapabilityCenter/>)
 expect(screen.getByRole('heading',{name:'插件'})).toBeInTheDocument()
 expect(screen.queryByRole('heading',{name:'MCP'})).toBeNull()
 await user.click(screen.getByRole('tab',{name:'MCP'}))
 expect(screen.getByRole('heading',{name:'MCP'})).toBeInTheDocument()
 expect(screen.queryByRole('heading',{name:'插件'})).toBeNull()
})
