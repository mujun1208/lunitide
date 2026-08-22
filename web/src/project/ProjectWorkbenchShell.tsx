import React,{useEffect,useRef,useState}from'react'
import type{ProjectBridge,SessionBridge,MessageBridge,StageBridge,ChatBridge,ProviderBridge,PlanBridge,ReviewBridge}from'../bridge/client'
import{createMutationAttempt,planBridge,reviewBridge}from'../bridge/client'
import type{ProjectDTO,SessionDTO}from'../generated/bridge'
import{SessionPage}from'../session/SessionPage'
import{ensureProjectStages,inferActivePhase,phaseStepClass,phasesForProjectType,PROJECT_TYPE_SHORT,readPreferredPhase,writePreferredPhase}from'./projectPhases'
import{isReadOnly,statusLabel}from'./projectStatus'
import{DeliverablePanel}from'./DeliverablePanel'
import{RegistryPanel}from'./RegistryPanel'
import{ReleasePanel}from'./ReleasePanel'
import{dbRegistryPhase,deliverablesForPhase,interfaceRegistryPhase,isRegistryPhase,releasePhaseForType}from'./deliverableTypes'
import{devPhaseForType}from'./checklistTypes'
import{fetchDevGateReady}from'./projectDevGate'
import{applySessionPhaseExperts}from'./phaseExperts'
import{workspaceTabForPhase}from'./devWorkflowChips'
import{ProjectApprovalPanel}from'./ProjectApprovalPanel'
import type{WorkbenchNav,WorkbenchStats}from'./projectWorkbenchNav'
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
  plans?:PlanBridge
  reviews?:ReviewBridge
  onBack:()=>void
  onProjectUpdated?:(project:ProjectDTO)=>void
  initialSession?:SessionDTO
  initialPrompt?:string
  initialProviderId?:string
  initialModelId?:string
}

export function ProjectWorkbenchShell({project,projects,sessions,messages,stages,chat,providers,plans=planBridge,reviews=reviewBridge,onBack,onProjectUpdated,initialSession,initialPrompt,initialProviderId,initialModelId}:ShellProps):React.JSX.Element{
 const[projectState,setProjectState]=useState(project)
 const[stageItems,setStageItems]=useState<StageDTO[]>([])
 const[activePhase,setActivePhase]=useState(()=>readPreferredPhase(project.id)??1)
 const[drawer,setDrawer]=useState(false)
 const[sidebarCollapsed,setSidebarCollapsed]=useState(false)
 const[session,setSession]=useState<SessionDTO|undefined>(initialSession)
 const readOnly=isReadOnly(projectState.status)
 const hasRegistryPanel=(phase:number)=>isRegistryPhase(phase,projectState.type)
 const releasePhase=releasePhaseForType(projectState.type)
 const hasSidePanel=(phase:number)=>phase===releasePhase||hasRegistryPanel(phase)||deliverablesForPhase(phase,projectState.type).length>0
 const[devGateReady,setDevGateReady]=useState(true)
 const[workbenchStats,setWorkbenchStats]=useState<WorkbenchStats>({running:false,changes:0})
 const[pendingApprovals,setPendingApprovals]=useState(0)
 const workbenchNavRef=useRef<WorkbenchNav|undefined>(undefined)
 const mounted=useRef(true)
 const[sidebarWidth,startSidebarResize]=usePanelResize({storageKey:'lunitide:sidebar-width',initial:288,min:210,max:()=>Math.min(420,window.innerWidth-520)})
 const[sidePanelOpen,setSidePanelOpen]=useState(()=>hasSidePanel(readPreferredPhase(project.id)??1))
 useEffect(()=>{mounted.current=true;setProjectState(project);setActivePhase(readPreferredPhase(project.id)??1);setSidePanelOpen(hasSidePanel(readPreferredPhase(project.id)??1));let cancelled=false;void ensureProjectStages(stages,project.id,project.type).then(items=>{if(cancelled||!mounted.current)return;setStageItems(items);const map=new Map(items.map(s=>[s.phase,s]));setActivePhase(v=>inferActivePhase(phasesForProjectType(project.type),map,v??readPreferredPhase(project.id)))}).catch(()=>{});return()=>{mounted.current=false;cancelled=true}},[project,stages])
 useEffect(()=>{let cancelled=false;void fetchDevGateReady(projectState.id,projectState.type).then(ready=>{if(!cancelled)setDevGateReady(ready)}).catch(()=>{if(!cancelled)setDevGateReady(false)});return()=>{cancelled=true}},[projectState.id,projectState.type,activePhase])
 useEffect(()=>{let cancelled=false;const poll=async()=>{try{const listed=await plans.list({projectId:projectState.id});const active=listed.items.find(p=>p.status==='active')??listed.items[0];if(!active){if(!cancelled)setPendingApprovals(0);return}const pending=await reviews.list({planId:active.id});if(!cancelled)setPendingApprovals(pending.items.filter(r=>r.status==='pending').length)}catch{}};void poll();const timer=window.setInterval(poll,8000);return()=>{cancelled=true;window.clearInterval(timer)}},[plans,reviews,projectState.id])
 useEffect(()=>{if(initialSession){setSession(initialSession);return}let cancelled=false;void(async()=>{try{const listed=await sessions.list({projectId:project.id});if(cancelled)return;const latest=[...listed.items].sort((a,b)=>(b.updatedAt||b.createdAt).localeCompare(a.updatedAt||a.createdAt)||b.id.localeCompare(a.id))[0];if(latest){setSession(latest);return}const payload={projectId:project.id,title:project.name};const created=await sessions.create(payload,{attempt:createMutationAttempt('session.create',payload)});if(!cancelled)setSession(created)}catch{/* SessionPage can still show the list fallback */}})();return()=>{cancelled=true}},[project.id,project.name,initialSession,sessions])
 const phaseDefs=phasesForProjectType(projectState.type),stageMap=new Map(stageItems.map(s=>[s.phase,s]))
 const selectPhase=(phase:number)=>{setActivePhase(phase);writePreferredPhase(projectState.id,phase);setSidePanelOpen(hasSidePanel(phase))}
 const patchProject=(next:ProjectDTO)=>{setProjectState(next);onProjectUpdated?.(next)}
 const showSidePanel=sidePanelOpen&&hasSidePanel(activePhase)
 const showRelease=activePhase===releasePhase
 const showRegistry=hasRegistryPanel(activePhase)
 const sideLabel=showRelease?'发布':showRegistry?(activePhase===interfaceRegistryPhase(projectState.type)?'接口':'数据库'):'交付物'
 const activePhaseDef=phaseDefs.find(def=>def.phase===activePhase)
 useEffect(()=>{if(!session?.id||!activePhaseDef?.label)return;void applySessionPhaseExperts(session.id,projectState.id,activePhaseDef.label).catch(()=>{})},[session?.id,projectState.id,activePhaseDef?.label])
 const goDevPhase=()=>selectPhase(devPhaseForType(projectState.type))
 const workspaceTab=workspaceTabForPhase(activePhase,activePhaseDef?.label)
 const projectSidePanel=showSidePanel?(showRelease?<ReleasePanel project={projectState} readOnly={readOnly}/>:showRegistry?<RegistryPanel project={projectState} phase={activePhase} readOnly={readOnly} devGateReady={devGateReady} onGoDevPhase={goDevPhase}/>:<DeliverablePanel project={projectState} phase={activePhase} bridge={projects} stages={stages} stageItems={stageItems} readOnly={readOnly} onProjectUpdated={patchProject} onStagesUpdated={setStageItems} onDeliverablesChanged={()=>void fetchDevGateReady(projectState.id,projectState.type).then(setDevGateReady).catch(()=>setDevGateReady(false))}/>):undefined
 const projectApprovalPanel=<ProjectApprovalPanel projectId={projectState.id}/>
 const openWorkbenchWorkspace=(kind:'plan'|'changes')=>{workbenchNavRef.current?.openWorkspace(kind);setSidePanelOpen(true)}
 const openWorkbenchApproval=()=>{workbenchNavRef.current?.openApproval();setSidePanelOpen(true)}
 return <div className={`launch-shell pm-workbench ${sidebarCollapsed?'sidebar-collapsed':''} ${readOnly?'is-readonly':''}`} style={{'--sidebar-expanded-width':`${sidebarWidth}px`} as React.CSSProperties}><button className="drawer-toggle" aria-label={sidebarCollapsed?'展开阶段菜单':'收起阶段菜单'} aria-controls="pm-phase-sidebar" aria-expanded={drawer||!sidebarCollapsed} onClick={()=>window.innerWidth<=680?setDrawer(v=>!v):setSidebarCollapsed(v=>!v)}><span aria-hidden="true">◧</span></button>{drawer&&<button className="drawer-scrim" aria-label="关闭阶段菜单" onClick={()=>setDrawer(false)}/>}<aside id="pm-phase-sidebar" className={`launch-sidebar pm-phase-sidebar ${drawer?'drawer-open':''}`} aria-label="项目阶段导航" aria-hidden={sidebarCollapsed&&!drawer?true:undefined}><button className="launch-brand" onClick={onBack} aria-label="返回项目管理"><span className="real-moon small" aria-hidden="true"/><span><b>LUNITIDE</b><em>月汐</em></span></button><div className="pm-workbench-project"><b>{projectState.name}</b><span>{PROJECT_TYPE_SHORT[projectState.type]} · {statusLabel(projectState.status,projectState.type)}</span><small>{projectState.projectCode}</small></div><div className="pm-workbench-status" role="toolbar" aria-label="工作台状态"><button type="button" className={`pm-status-chip ${workbenchStats.running?'on':''}`} onClick={()=>{openWorkbenchWorkspace('plan');setDrawer(false)}}><span>运行</span>{workbenchStats.running&&<i className="pm-status-dot" aria-hidden="true"/>}</button><button type="button" className={`pm-status-chip ${workbenchStats.changes>0?'has-badge':''}`} onClick={()=>{openWorkbenchWorkspace('changes');setDrawer(false)}}><span>变更</span>{workbenchStats.changes>0&&<b>{workbenchStats.changes}</b>}</button><button type="button" className={`pm-status-chip ${pendingApprovals>0?'has-badge alert':''}`} onClick={()=>{openWorkbenchApproval();setDrawer(false)}}><span>审批</span>{pendingApprovals>0&&<b>{pendingApprovals}</b>}</button></div><div className="pm-phase-nav-title">项目阶段</div><nav className="pm-phase-list" aria-label="八阶段菜单">{phaseDefs.map(def=>{const s=stageMap.get(def.phase),cls=phaseStepClass(s?.status),on=def.phase===activePhase;return <button type="button" key={def.phase} className={`pm-phase-item ${cls} ${on?'on':''}`} aria-current={on?'step':undefined} onClick={()=>{selectPhase(def.phase);setDrawer(false)}}><span className="pm-phase-num">{s?.status==='completed'||s?.status==='approved'?'✓':def.phase}</span><span>{def.label}</span></button>})}</nav><p className="pm-phase-note"><b>类型投影</b>{projectState.type==='operations'?' · 运维六阶段':' · 实施/增强八阶段'}</p><div className="launch-bottom"><button onClick={onBack}>← 返回项目管理</button></div></aside><div className="panel-resizer sidebar-resizer" role="separator" aria-label="调整阶段栏宽度" aria-orientation="vertical" onPointerDown={startSidebarResize}/><main className="launch-content pm-workbench-center"><SessionPage key={`${projectState.id}:${session?.id??''}:${activePhase}`} project={projectState} bridge={sessions} messages={messages} stages={stages} onBack={onBack} chat={chat} providers={providers} initialSession={session} initialPrompt={initialPrompt} initialProviderId={initialProviderId} initialModelId={initialModelId} readOnly={readOnly} homeChat projectPhase={activePhase} projectPhaseLabel={activePhaseDef?.label} projectSidePanel={projectSidePanel} projectSideLabel={sideLabel} projectApprovalPanel={projectApprovalPanel} pendingApprovalCount={pendingApprovals} onRegisterWorkbenchNav={nav=>{workbenchNavRef.current=nav}} onWorkbenchStatsChange={setWorkbenchStats} initialWorkspaceTab={workspaceTab}/></main></div>
}
