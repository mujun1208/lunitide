import React,{useEffect,useState}from'react'
import{planBridge,type PlanBridge}from'../bridge/client'
import type{PlanDTO,PlanRunDTO}from'../generated/bridge'
export function CoordinationPlanPanel({projectId,bridge=planBridge}:{projectId:string;bridge?:PlanBridge}){
 const[plans,setPlans]=useState<PlanDTO[]>([]),[runs,setRuns]=useState<PlanRunDTO[]>([]),[planId,setPlanId]=useState(''),[error,setError]=useState('')
 useEffect(()=>{bridge.list({projectId}).then(r=>{setPlans(r.items);setPlanId(r.items[0]?.id??'')}).catch(e=>setError(String(e)))},[bridge,projectId])
 const refresh=async(id=planId)=>{if(!id){setRuns([]);return}try{setRuns((await bridge.runTree({planId:id})).items);setError('')}catch(e){setError(e instanceof Error?e.message:String(e))}}
 useEffect(()=>{void refresh(planId)},[planId])
 const icon=(status:PlanRunDTO['status'])=>status==='succeeded'?'✓':status==='failed'||status==='cancelled'?'×':'○'
 return <section className="coordination-plan"><header><div><strong>工作计划清单</strong><small>{runs.length?`${runs.filter(r=>r.status==='succeeded').length}/${runs.length} 已完成`:'暂无任务'}</small></div><select aria-label="工作计划" value={planId} onChange={e=>setPlanId(e.target.value)}>{plans.map(p=><option key={p.id} value={p.id}>{p.name}</option>)}</select></header>{error&&<p role="alert">{error}</p>}{runs.length?<ol className="plan-summary-list">{runs.map(r=><li key={r.id} className={`status-${r.status}`} style={{paddingLeft:`${r.depth*16}px`}}><div className="plan-summary-item"><span aria-hidden="true">{icon(r.status)}</span><span><b>{r.todo.title}</b>{r.todo.description&&<em>{r.todo.description}</em>}<small>{r.status}</small></span></div></li>)}</ol>:<p className="plan-empty">当前计划还没有清单项。</p>}</section>
}
