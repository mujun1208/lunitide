import React,{useEffect,useState} from 'react'
import {subagentBridge,type SubagentBridge} from '../bridge/client'
import {TOOL_LABELS} from './toolLabels'
import './subagentActivity.css'

export type SubagentActivity={callId:string;name:string;status:string;summary?:string}
type Progress={id:string;profile?:string;purpose?:string;status:string;stage?:string;tool?:string;detail?:string;summary?:string}
const STATUS:Record<string,string>={running:'运行中',completed:'已完成',failed:'失败',cancelled:'已取消'}
const STAGES:Record<string,string>={starting:'开始任务',thinking:'正在分析',searching:'正在查询',tool:'执行工具',completed:'已完成',failed:'失败',cancelled:'已取消'}

export function parseSubagentProgress(summary:string|undefined):Progress|undefined{
 if(!summary)return
 try{
  const value=JSON.parse(summary) as Record<string,unknown>
  const id=typeof value.id==='string'?value.id:value.subagentId
 if(typeof id!=='string'||!/^[0-9A-HJKMNP-TV-Z]{26}$/.test(id)||typeof value.status!=='string'||!Object.hasOwn(STATUS,value.status))return
  const text=(key:string)=>typeof value[key]==='string'?value[key] as string:undefined
  return{id,status:value.status,profile:text('profile'),purpose:text('purpose'),stage:text('stage'),tool:text('tool'),detail:text('detail'),summary:text('summary')}
 }catch{return}
}

export function SubagentActivityRow({activity,bridge=subagentBridge}:{activity:SubagentActivity;bridge?:Pick<SubagentBridge,'join'>}){
 const current=parseSubagentProgress(activity.summary)
 const [remembered,setRemembered]=useState<Progress|undefined>(current),[open,setOpen]=useState(false),[report,setReport]=useState(''),[error,setError]=useState(''),[retry,setRetry]=useState(0)
 useEffect(()=>{const next=parseSubagentProgress(activity.summary);if(next)setRemembered(previous=>({...previous,...next,profile:next.profile??previous?.profile,purpose:next.purpose??previous?.purpose}))},[activity.summary])
 const progress=current?{...remembered,...current,profile:current.profile??remembered?.profile,purpose:current.purpose??remembered?.purpose}:remembered
 const terminal=progress?.status==='completed'||progress?.status==='failed'||progress?.status==='cancelled'
 useEffect(()=>{if(!open||!progress?.id||progress.status!=='completed')return;let alive=true;setError('');void bridge.join({subagentId:progress.id,waitMs:1000,maxSummaryBytes:8192}).then(value=>{if(alive)setReport((value as {summary?:string}).summary??value.observations?.map(item=>item.summary).join('\n')??'暂无摘要')}).catch(e=>{if(alive)setError(e instanceof Error?e.message:'读取结果失败')});return()=>{alive=false}},[open,progress?.id,progress?.status,bridge,retry])
 const status=progress?.status??(activity.status==='tool_completed'?'completed':'running')
 const title=progress?.purpose||progress?.profile||(activity.name==='subagent.join'?'收集子任务结果':'子任务')
 const detail=progress?.tool?(TOOL_LABELS[progress.tool]??'执行工具'):STAGES[progress?.stage??'']||STATUS[status]
 return <details className={`subagent-activity-row is-${status}`} open={open} onToggle={e=>setOpen(e.currentTarget.open)}><summary><span className="subagent-activity-indicator" aria-hidden="true">{status==='completed'?'✓':status==='failed'?'!':status==='cancelled'?'■':'◌'}</span><b title={title}>{title}</b><span role="status">{terminal?STATUS[status]:detail}</span></summary><div className="subagent-activity-detail">{progress?.profile&&<small>{progress.profile}</small>}{!terminal&&<p>{progress?.detail||'正在等待子任务进度'}</p>}{terminal&&<pre>{report||progress?.summary||progress?.detail||(status==='completed'&&!error?'正在读取结果…':'任务已结束')}</pre>}{error&&<p role="alert">{error} <button type="button" onClick={()=>setRetry(value=>value+1)}>重试</button></p>}</div></details>
}
