import React,{useEffect,useState}from'react'
import{projectBridge}from'../bridge/client'
import type{ProjectDTO}from'../generated/bridge'
import{MemoryPage}from'../memory/MemoryPage'
import{PrivacyConsole}from'./PrivacyConsole'

type IntelTab='memory'|'privacy'

export function PersonalIntelligencePage({onNavigateExpert:_onNavigateExpert}:{onNavigateExpert?:()=>void}):React.JSX.Element{
 const[tab,setTab]=useState<IntelTab>('memory')
 const[projects,setProjects]=useState<ProjectDTO[]>([]),[projectId,setProjectId]=useState('')
 useEffect(()=>{try{projectBridge.list().then(result=>{const visible=result.items.filter(item=>!item.name.startsWith('⁣'));setProjects(visible);setProjectId(current=>current||visible[0]?.id||'')}).catch(()=>{})}catch{/* bridge unavailable outside WebView2 */}},[])
 return <main className="pi-page">
  <header className="pi-head">
   <div><h1>个人智能</h1><p>确认记忆、管理隐私。项目本体由对话自动检索和维护，不再单独占用一个配置页。</p></div>
   <div className="pi-tabs" role="tablist" aria-label="个人智能">
    <button type="button" role="tab" aria-selected={tab==='memory'} className={tab==='memory'?'on':''} onClick={()=>setTab('memory')}>记忆确认</button>
    <button type="button" role="tab" aria-selected={tab==='privacy'} className={tab==='privacy'?'on':''} onClick={()=>setTab('privacy')}>隐私</button>
   </div>
  </header>
  {tab==='memory'&&<section className="pi-panel" aria-label="记忆确认">
   <div className="pi-toolbar"><label>作用项目<select id="pi-memory-project" value={projectId} onChange={e=>setProjectId(e.target.value)}>{projects.length?projects.map(project=><option key={project.id} value={project.id}>{project.name}</option>):<option value="">（暂无项目）</option>}</select></label></div>
   {projectId?<MemoryPage projectId={projectId}/>:<div className="gate-box"><b>需要一个项目</b><span>记忆按项目存放。先在项目管理里创建一个项目，再回来确认候选。</span></div>}
  </section>}
  {tab==='privacy'&&<section className="pi-panel" aria-label="隐私"><PrivacyConsole/></section>}
 </main>
}
