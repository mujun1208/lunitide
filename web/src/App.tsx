import React,{useState}from'react'
import{projectBridge,providerBridge,type ProjectBridge,type ProviderBridge}from'./bridge/client'
import{ProjectPage}from'./project/ProjectPage'
import{ProviderApp}from'./provider/ProviderApp'

export function App({projects=projectBridge,providers=providerBridge}:{projects?:ProjectBridge;providers?:ProviderBridge}):React.JSX.Element{
 const[page,setPage]=useState<'projects'|'providers'>('projects')
 return <><nav className="top-nav" aria-label="主导航"><strong>Lunitide <span>月汐</span></strong><div><button aria-current={page==='projects'?'page':undefined} onClick={()=>setPage('projects')}>项目</button><button aria-current={page==='providers'?'page':undefined} onClick={()=>setPage('providers')}>供应商</button></div></nav>{page==='projects'?<ProjectPage bridge={projects}/>:<ProviderApp bridge={providers}/>}</>
}
