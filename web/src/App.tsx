import React,{useState}from'react'
import{messageBridge,projectBridge,providerBridge,sessionBridge,type MessageBridge,type ProjectBridge,type ProviderBridge,type SessionBridge}from'./bridge/client'
import type{ProjectDTO}from'./generated/bridge'
import{ProjectPage}from'./project/ProjectPage'
import{ProviderApp}from'./provider/ProviderApp'
import{SessionPage}from'./session/SessionPage'
export function App({projects=projectBridge,providers=providerBridge,sessions=sessionBridge,messages=messageBridge}:{projects?:ProjectBridge;providers?:ProviderBridge;sessions?:SessionBridge;messages?:MessageBridge}):React.JSX.Element{const[page,setPage]=useState<'projects'|'providers'>('projects'),[selected,setSelected]=useState<ProjectDTO>();return <><nav className="top-nav" aria-label="主导航"><strong>Lunitide <span>月汐</span></strong><div><button aria-current={page==='projects'?'page':undefined} onClick={()=>{setPage('projects');setSelected(undefined)}}>项目</button><button aria-current={page==='providers'?'page':undefined} onClick={()=>{setPage('providers');setSelected(undefined)}}>供应商</button></div></nav>{selected?<SessionPage project={selected} bridge={sessions} messages={messages} onBack={()=>setSelected(undefined)}/>:page==='projects'?<ProjectPage bridge={projects} onSelect={setSelected}/>:<ProviderApp bridge={providers}/>}</>}
