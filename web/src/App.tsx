import React,{useEffect,useState}from'react'
import{createChatBridge,messageBridge,projectBridge,providerBridge,sessionBridge,stageBridge,type ChatBridge,type MessageBridge,type ProjectBridge,type ProviderBridge,type SessionBridge,type StageBridge}from'./bridge/client'
import type{ProjectDTO,StageDTO}from'./generated/bridge'
import{ProjectPage}from'./project/ProjectPage'
import{ProviderApp}from'./provider/ProviderApp'
import{SessionPage}from'./session/SessionPage'

const STAGE_LABELS=['需求与调研','架构','方案','UI','数据库与接口','开发','系统测试','集成验收','CR发布部署']

export function App({projects=projectBridge,providers=providerBridge,sessions=sessionBridge,messages=messageBridge,stages=stageBridge,chat}:{projects?:ProjectBridge;providers?:ProviderBridge;sessions?:SessionBridge;messages?:MessageBridge;stages?:StageBridge;chat?:ChatBridge}):React.JSX.Element{
const[page,setPage]=useState<'projects'|'providers'>('projects'),[selected,setSelected]=useState<ProjectDTO>()
const chatBridge=chat??createChatBridge(window.chrome?.webview??{postMessage:()=>{},addEventListener:()=>{},removeEventListener:()=>{}})

const sidebar=<aside className="sidebar" aria-label="侧边导航">
<div className="s-logo"><img src="/favicon.svg" alt="Lunitide" className="g" /><b>Lunitide</b><span>月汐</span></div>
<div className="s-title">工作区</div>
<button className={`s-item ${page==='projects'&&!selected?'on':''}`} onClick={()=>{setPage('projects');setSelected(undefined)}}><span className="ic" aria-hidden="true">◈</span>项目总览</button>
<button className={`s-item ${page==='providers'&&!selected?'on':''}`} onClick={()=>{setPage('providers');setSelected(undefined)}}><span className="ic" aria-hidden="true">◈</span>供应商</button>
<div className="s-title" style={{marginTop:'6px'}}>智能体</div>
{STAGE_LABELS.map((l,i)=><div key={i} className="s-item"><span className="ic" aria-hidden="true">✦</span>{l}</div>)}
{selected&&<div className="s-proj"><b>当前项目</b>{selected.name}</div>}
</aside>

if(selected)return<div className="app-shell">{sidebar}<SessionPage key={selected.id} project={selected} bridge={sessions} messages={messages} onBack={()=>setSelected(undefined)} stages={stages} chat={chatBridge} providers={providers}/><ActivityPanel project={selected}/></div>
return<div className="app-shell no-activity">{sidebar}<main className="main">{page==='projects'?<ProjectPage bridge={projects} onSelect={setSelected}/>:<ProviderApp bridge={providers}/>}</main></div>
}

function ActivityPanel({project}:{project:ProjectDTO}):React.JSX.Element{
return<aside className="activity" aria-label="实时活动">
<div className="a-title">实时活动</div>
<div className="act-item"><span className="ic ok"></span><div><b>项目</b><span className="t">{project.name}</span></div></div>
<div className="act-item"><span className="ic ok"></span><div><b>就绪</b><span className="t">本地引擎已连接</span></div></div>
<div className="collect"><span>会话记忆</span><div className="bar"><i style={{width:'0%'}}></i></div><span>—</span></div>
</aside>
}
