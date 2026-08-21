import React,{useEffect,useRef,useState}from'react'
import type{ProjectBridge,SessionBridge,MessageBridge,StageBridge,ChatBridge,ProviderBridge}from'../bridge/client'
import{createMutationAttempt}from'../bridge/client'
import type{ProjectDTO,SessionDTO}from'../generated/bridge'
import{SessionPage}from'../session/SessionPage'
import{ensureProjectStages,inferActivePhase,phaseStepClass,phasesForProjectType,PROJECT_TYPE_SHORT,readPreferredPhase,writePreferredPhase}from'./projectPhases'
import{isReadOnly,statusLabel}from'./projectStatus'
import{DeliverablePanel}from'./DeliverablePanel'
import{RegistryPanel}from'./RegistryPanel'
import{ReleasePanel}from'./ReleasePanel'
import{deliverablesForPhase}from'./deliverableTypes'
import{usePanelResize}from'../ui/usePanelResize'
import type{StageDTO}from'../generated/bridge'

type ShellProps={
  project:ProjectDTO
  projects:ProjectBridge
  sessions:SessionBridge
  messages:MessageBridge
  stages:StageBridge
  chat:ChatBridge
  providers:ProviderBridge
  onBack:()=>void
  onProjectUpdated?:(project:ProjectDTO)=>void
  initialSession?:SessionDTO
  initialPrompt?:string
  initialProviderId?:string
  initialModelId?:string
}

export function ProjectWorkbenchShell({project,projects,sessions,messages,stages,chat,providers,onBack,onProjectUpdated,initialSession,initialPrompt,initialProviderId,initialModelId}:ShellProps):React.JSX.Element{
 const[projectState,setProjectState]=useState(project)
 const[stageItems,setStageItems]=useState<StageDTO[]>([])
 const[activePhase,setActivePhase]=useState(()=>readPreferredPhase(project.id)??1)
 const[drawer,setDrawer]=useState(false)
 const[sidebarCollapsed,setSidebarCollapsed]=useState(false)
 const[session,setSession]=useState<SessionDTO|undefined>(initialSession)
 const readOnly=isReadOnly(projectState.status)
 const hasRegistryPanel=(phase:number)=>phase===3||phase===4
 const hasSidePanel=(phase:number)=>phase===8||hasRegistryPanel(phase)||deliverablesForPhase(phase,projectState.type).length>0
 const[sidePanelOpen,setSidePanelOpen]=useState(()=>hasSidePanel(readPreferredPhase(project.id)??1))
 const mounted=useRef(true)
 const[sidebarWidth,startSidebarResize]=usePanelResize({storageKey:'lunitide:sidebar-width',initial:288,min:210,max:()=>Math.min(420,window.innerWidth-520)})
 useEffect(()=>{mounted.current=true;setProjectState(project);setActivePhase(readPreferredPhase(project.id)??1);setSidePanelOpen(hasSidePanel(readPreferredPhase(project.id)??1));let cancelled=false;void ensureProjectStages(stages,project.id,project.type).then(items=>{if(cancelled||!mounted.current)return;setStageItems(items);const map=new Map(items.map(s=>[s.phase,s]));setActivePhase(v=>inferActivePhase(phasesForProjectType(project.type),map,v??readPreferredPhase(project.id)))}).catch(()=>{});return()=>{mounted.current=false;cancelled=true}},[project,stages])
 useEffect(()=>{if(initialSession){setSession(initialSession);return}let cancelled=false;void(async()=>{try{const listed=await sessions.list({projectId:project.id});if(cancelled)return;const latest=[...listed.items].sort((a,b)=>(b.updatedAt||b.createdAt).localeCompare(a.updatedAt||a.createdAt)||b.id.localeCompare(a.id))[0];if(latest){setSession(latest);return}const payload={projectId:project.id,title:project.name};const created=await sessions.create(payload,{attempt:createMutationAttempt('session.create',payload)});if(!cancelled)setSession(created)}catch{/* SessionPage can still show the list fallback */}})();return()=>{cancelled=true}},[project.id,project.name,initialSession,sessions])
 const phaseDefs=phasesForProjectType(projectState.type),stageMap=new Map(stageItems.map(s=>[s.phase,s]))
 const selectPhase=(phase:number)=>{setActivePhase(phase);writePreferredPhase(projectState.id,phase);setSidePanelOpen(hasSidePanel(phase))}
 const patchProject=(next:ProjectDTO)=>{setProjectState(next);onProjectUpdated?.(next)}
 const showSidePanel=sidePanelOpen&&hasSidePanel(activePhase)
 const showRelease=activePhase===8
 const showRegistry=hasRegistryPanel(activePhase)
 const sideLabel=showRelease?'发布':showRegistry?'注册表':'交付物'
 const projectSidePanel=showSidePanel?(showRelease?<ReleasePanel readOnly={readOnly}/>:showRegistry?<RegistryPanel project={projectState} phase={activePhase} readOnly={readOnly}/>:<DeliverablePanel project={projectState} phase={activePhase} bridge={projects} stages={stages} stageItems={stageItems} readOnly={readOnly} onProjectUpdated={patchProject} onStagesUpdated={setStageItems}/>):undefined
 return <div className={`launch-shell pm-workbench ${sidebarCollapsed?'sidebar-collapsed':''} ${readOnly?'is-readonly':''}`} style={{'--sidebar-expanded-width':`${sidebarWidth}px`} as React.CSSProperties}><button className="drawer-toggle" aria-label={sidebarCollapsed?'展开阶段菜单':'收起阶段菜单'} aria-controls="pm-phase-sidebar" aria-expanded={drawer||!sidebarCollapsed} onClick={()=>window.innerWidth<=680?setDrawer(v=>!v):setSidebarCollapsed(v=>!v)}><span aria-hidden="true">◧</span></button>{drawer&&<button className="drawer-scrim" aria-label="关闭阶段菜单" onClick={()=>setDrawer(false)}/>}<aside id="pm-phase-sidebar" className={`launch-sidebar pm-phase-sidebar ${drawer?'drawer-open':''}`} aria-label="项目阶段导航" aria-hidden={sidebarCollapsed&&!drawer?true:undefined}><button className="launch-brand" onClick={onBack} aria-label="返回项目管理"><span className="real-moon small" aria-hidden="true"/><span><b>LUNITIDE</b><em>月汐</em></span></button><div className="pm-workbench-project"><b>{projectState.name}</b><span>{PROJECT_TYPE_SHORT[projectState.type]} · {statusLabel(projectState.status,projectState.type)}</span><small>{projectState.projectCode}</small></div><div className="pm-phase-nav-title">项目阶段</div><nav className="pm-phase-list" aria-label="八阶段菜单">{phaseDefs.map(def=>{const s=stageMap.get(def.phase),cls=phaseStepClass(s?.status),on=def.phase===activePhase;return <button type="button" key={def.phase} className={`pm-phase-item ${cls} ${on?'on':''}`} aria-current={on?'step':undefined} onClick={()=>{selectPhase(def.phase);setDrawer(false)}}><span className="pm-phase-num">{s?.status==='completed'||s?.status==='approved'?'✓':def.phase}</span><span>{def.label}</span></button>})}</nav><p className="pm-phase-note"><b>类型投影</b>{projectState.type==='operations'?' · 运维六阶段':' · 实施/增强八阶段'}</p><div className="launch-bottom"><button onClick={onBack}>← 返回项目管理</button></div></aside><div className="panel-resizer sidebar-resizer" role="separator" aria-label="调整阶段栏宽度" aria-orientation="vertical" onPointerDown={startSidebarResize}/><main className="launch-content pm-workbench-center"><SessionPage key={`${projectState.id}:${session?.id??''}:${activePhase}`} project={projectState} bridge={sessions} messages={messages} stages={stages} onBack={onBack} chat={chat} providers={providers} initialSession={session} initialPrompt={initialPrompt} initialProviderId={initialProviderId} initialModelId={initialModelId} readOnly={readOnly} homeChat projectSidePanel={projectSidePanel} projectSideLabel={sideLabel}/></main></div>
}
