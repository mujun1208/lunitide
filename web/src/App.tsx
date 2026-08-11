import React,{useEffect,useMemo,useState}from'react'
import{createChatBridge,messageBridge,projectBridge,providerBridge,sessionBridge,stageBridge,type ChatBridge,type MessageBridge,type ProjectBridge,type ProviderBridge,type SessionBridge,type StageBridge,type StreamEvent}from'./bridge/client'
import type{ProjectDTO,StageDTO}from'./generated/bridge'
import{ProjectPage}from'./project/ProjectPage'
import{ProviderApp}from'./provider/ProviderApp'
import{SessionPage}from'./session/SessionPage'
import{PlanPage}from'./plan/PlanPage'
import{ReviewPage}from'./review/ReviewPage'
import{MemoryPage}from'./memory/MemoryPage'
import{OntologyPage}from'./ontology/OntologyPage'
import{SkillPage}from'./skill/SkillPage'
import{SettingsPage,initAppearance}from'./settings/SettingsPage'

const STAGE_LABELS=['需求与调研','架构','方案','UI','数据库与接口','开发','系统测试','集成验收','CR发布部署']

/** 黑色星空 + 月亮氛围层（参考设计文档 System_Design_Doc/index.html） */
function Atmosphere():React.JSX.Element{
  const stars=useMemo(()=>Array.from({length:80},()=>({
    left:Math.random()*100,top:Math.random()*100,
    size:Math.random()*2+0.5,delay:Math.random()*4
  })),[])
  return<>
    <div className="sky" aria-hidden="true"/>
    <div className="moon-glow" aria-hidden="true"/>
    <div className="moon" aria-hidden="true"/>
    <div className="stars" aria-hidden="true">{stars.map((s,i)=><i key={i} style={{left:`${s.left}%`,top:`${s.top}%`,width:`${s.size}px`,height:`${s.size}px`,animationDelay:`${s.delay}s`}}/>)}</div>
  </>
}

export function App({projects=projectBridge,providers=providerBridge,sessions=sessionBridge,messages=messageBridge,stages=stageBridge,chat}:{projects?:ProjectBridge;providers?:ProviderBridge;sessions?:SessionBridge;messages?:MessageBridge;stages?:StageBridge;chat?:ChatBridge}):React.JSX.Element{
const[page,setPage]=useState<'projects'|'providers'|'plans'|'reviews'|'memory'|'ontology'|'skills'|'settings'>('projects'),[selected,setSelected]=useState<ProjectDTO>()
const chatBridge=chat??createChatBridge(window.chrome?.webview??{postMessage:()=>{},addEventListener:()=>{},removeEventListener:()=>{}})
useEffect(()=>{initAppearance();try{const lastId=localStorage.getItem('lunitide:lastProject');if(lastId){projects.list().then(r=>{const found=r.items.find(p=>p.id===lastId);if(found)setSelected(found)}).catch(()=>{})}}catch{}},[])
useEffect(()=>{try{if(selected)localStorage.setItem('lunitide:lastProject',selected.id);else localStorage.removeItem('lunitide:lastProject')}catch{}},[selected])

const sidebar=<aside className="sidebar" aria-label="侧边导航">
<div className="s-logo"><img src="/favicon.svg" alt="Lunitide" className="g" /><b>Lunitide</b><span>月汐</span></div>
<div className="s-title">工作区</div>
<button className={`s-item ${page==='projects'&&!selected?'on':''}`} onClick={()=>{setPage('projects');setSelected(undefined)}}><span className="ic" aria-hidden="true">◈</span>项目总览</button>
<button className={`s-item ${page==='providers'&&!selected?'on':''}`} onClick={()=>{setPage('providers');setSelected(undefined)}}><span className="ic" aria-hidden="true">◈</span>供应商</button>
<div className="s-title" style={{marginTop:'6px'}}>高级功能</div>
<button className={`s-item ${page==='plans'&&!selected?'on':''}`} onClick={()=>{setPage('plans');setSelected(undefined)}}><span className="ic" aria-hidden="true">◈</span>计划管理</button>
<button className={`s-item ${page==='reviews'&&!selected?'on':''}`} onClick={()=>{setPage('reviews');setSelected(undefined)}}><span className="ic" aria-hidden="true">◈</span>治理审批</button>
<button className={`s-item ${page==='memory'&&!selected?'on':''}`} onClick={()=>{setPage('memory');setSelected(undefined)}}><span className="ic" aria-hidden="true">◈</span>记忆面板</button>
<button className={`s-item ${page==='ontology'&&!selected?'on':''}`} onClick={()=>{setPage('ontology');setSelected(undefined)}}><span className="ic" aria-hidden="true">◈</span>本体浏览</button>
<button className={`s-item ${page==='skills'&&!selected?'on':''}`} onClick={()=>{setPage('skills');setSelected(undefined)}}><span className="ic" aria-hidden="true">◈</span>技能市场</button>
<div className="s-title" style={{marginTop:'6px'}}>系统</div>
<button className={`s-item ${page==='settings'&&!selected?'on':''}`} onClick={()=>{setPage('settings');setSelected(undefined)}}><span className="ic" aria-hidden="true">⚙</span>设置中心</button>
{selected&&<div className="s-proj"><b>当前项目</b>{selected.name}</div>}
</aside>

const activeProjectId=selected?.id??''
if(selected)return<div className="app-shell"><Atmosphere/>{sidebar}<SessionPage key={selected.id} project={selected} bridge={sessions} messages={messages} onBack={()=>setSelected(undefined)} stages={stages} chat={chatBridge} providers={providers}/><ActivityPanel project={selected}/></div>
return<div className="app-shell no-activity"><Atmosphere/>{sidebar}<main className="main">{page==='plans'?<PlanPage projectId={activeProjectId}/>:page==='reviews'?<ReviewPage projectId={activeProjectId}/>:page==='memory'?<MemoryPage projectId={activeProjectId}/>:page==='ontology'?<OntologyPage projectId={activeProjectId}/>:page==='skills'?<SkillPage/>:page==='settings'?<SettingsPage onNavigateProviders={()=>setPage('providers')}/>:page==='projects'?<ProjectPage bridge={projects} onSelect={setSelected}/>:<ProviderApp bridge={providers}/>}</main></div>
}

function ActivityPanel({project}:{project:ProjectDTO}):React.JSX.Element{
return<aside className="activity" aria-label="实时活动">
<div className="a-title">实时活动</div>
<div className="act-item"><span className="ic ok"></span><div><b>项目</b><span className="t">{project.name}</span></div></div>
<div className="act-item"><span className="ic ok"></span><div><b>就绪</b><span className="t">本地引擎已连接</span></div></div>
<div className="collect"><span>会话记忆</span><div className="bar"><i style={{width:'0%'}}></i></div><span>—</span></div>
</aside>
}
