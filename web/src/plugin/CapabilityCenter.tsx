import React,{useState}from'react'
import{McpPage}from'../mcp/McpPage'
import{PluginPage}from'./PluginPage'

type Surface='plugins'|'mcp'

export function CapabilityCenter({initial='plugins',onCreateInChat}:{initial?:Surface;onCreateInChat?:()=>void}):React.JSX.Element{
 const[surface,setSurface]=useState<Surface>(initial)
 return <div className="capability-center">
  <nav className="capability-tabs skill-status-tabs" role="tablist" aria-label="扩展能力">
   <button type="button" role="tab" aria-selected={surface==='plugins'} onClick={()=>setSurface('plugins')}>插件</button>
   <button type="button" role="tab" aria-selected={surface==='mcp'} onClick={()=>setSurface('mcp')}>MCP</button>
  </nav>
  {surface==='plugins'?<PluginPage onCreateInChat={onCreateInChat}/>:<McpPage/>}
 </div>
}
