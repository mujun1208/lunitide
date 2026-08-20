import React,{useCallback,useEffect,useMemo,useState}from'react'
import{BridgeClientError,createMutationAttempt,deliverableBridge as defaultDeliverableBridge,type DeliverableBridge,type ProjectBridge,type StageBridge}from'../bridge/client'
import type{DeliverableListResult,ProjectDTO,StageDTO}from'../generated/bridge'
import{Dialog}from'../ui/Dialog'
import{deliverablesForPhase,gatePhaseForType,isDeliverableReady}from'./deliverableTypes'
import{normalizeStatus,statusLabel}from'./projectStatus'

type DeliverableItem=DeliverableListResult['items'][number]

const problem=(e:unknown)=>e instanceof BridgeClientError?e:new BridgeClientError(e instanceof Error?e.message:'请求失败','CLIENT_ERROR',false,'renderer')
const statusText=(status?:string)=>status==='approved'?'已批准':status==='immutable'?'已锁定':status==='review'?'审核中':status==='draft'?'草稿':'未绑定'

const advanceHint=(project:ProjectDTO,phase:number):string=>{
  const s=normalizeStatus(project.status)
  if(phase===1){
    if(project.type==='operations')return '立项 → 实施中'
    if(project.type==='enhancement')return '立项 → 需求评估'
    return '立项 → 需求架构'
  }
  if(phase===2){
    if(s==='req_architecture'||s==='req_assessment')return `${statusLabel(s,project.type)} → 实施中`
  }
  return '阶段晋级'
}

const confirmPhrase=(phase:number)=>phase===1?'确认需求架构规范':phase===2?'确认方案和UI设计':'确认阶段晋级'

export function DeliverablePanel({project,phase,bridge,deliverableBridge=defaultDeliverableBridge,stages,stageItems,readOnly=false,onProjectUpdated,onStagesUpdated}:{project:ProjectDTO;phase:number;bridge:ProjectBridge;deliverableBridge?:DeliverableBridge;stages?:StageBridge;stageItems?:StageDTO[];readOnly?:boolean;onProjectUpdated?:(project:ProjectDTO)=>void;onStagesUpdated?:(items:StageDTO[])=>void}):React.JSX.Element|null{
 const docs=deliverablesForPhase(phase,project.type)
 const [items,setItems]=useState<DeliverableItem[]>([])
 const [gateStep,setGateStep]=useState<0|1|2|3>(0)
 const [confirmText,setConfirmText]=useState('')
 const [busy,setBusy]=useState(false)
 const [error,setError]=useState('')
 const [loadError,setLoadError]=useState('')
 const gateEnabled=gatePhaseForType(project.type).includes(phase)
 const byType=useMemo(()=>new Map(items.map(item=>[item.documentType,item])),[items])
 const readyCount=useMemo(()=>docs.filter(d=>isDeliverableReady(byType.get(d.key)?.status)).length,[docs,byType])
 const allReady=docs.length>0&&readyCount===docs.length
 const phrase=confirmPhrase(phase)
 const stage=stageItems?.find(s=>s.phase===phase)

 const load=useCallback(async()=>{setLoadError('');try{const result=await deliverableBridge.list({projectId:project.id,phase});setItems(result.items)}catch(e){setLoadError(problem(e).message)}},[deliverableBridge,project.id,phase])
 useEffect(()=>{void load()},[load])

 const confirmDoc=async(def:{key:string;title:string})=>{if(readOnly||busy)return;setBusy(true);setError('');try{const existing=byType.get(def.key),payload={projectId:project.id,phase,documentType:def.key,title:def.title,status:'approved' as const},result=await deliverableBridge.upsert(payload,{attempt:createMutationAttempt('deliverable.upsert',payload)});setItems(values=>{const next=values.filter(v=>v.documentType!==def.key);return [...next,{...result,createdAt:existing?.createdAt??new Date().toISOString(),updatedAt:new Date().toISOString()}]})}catch(e){setError(problem(e).message)}finally{setBusy(false)}}

 const closeGate=()=>{if(busy)return;setGateStep(0);setConfirmText('');setError('')}
 const finalize=async()=>{if(busy||readOnly||confirmText.trim()!==phrase)return;setBusy(true);setError('');try{
  for(const doc of docs){const current=byType.get(doc.key);if(!current?.id)continue;const payload={projectId:project.id,id:current.id,expectedVersion:current.version},attempt=createMutationAttempt('deliverable.confirmGate',payload);const saved=await deliverableBridge.confirmGate(payload,{attempt});setItems(values=>values.map(v=>v.documentType===doc.key?{...v,...saved}:v))}
  const advancePayload={id:project.id,version:project.version,phase},advanceAttempt=createMutationAttempt('project.advanceStatus',advancePayload);const savedProject=await bridge.advanceStatus(advancePayload,{attempt:advanceAttempt});onProjectUpdated?.(savedProject)
  if(stages&&stage){const stagePayload={projectId:project.id,id:stage.id,status:'completed' as const,expectedVersion:stage.version},stageAttempt=createMutationAttempt('stage.update',stagePayload);const savedStage=await stages.update(stagePayload,{attempt:stageAttempt});onStagesUpdated?.((stageItems??[]).map(s=>s.id===savedStage.id?savedStage:s))}
  closeGate();void load()
 }catch(e){setError(problem(e).message)}finally{setBusy(false)}}

 if(!docs.length)return null
 return <aside className="pm-deliverable-panel" aria-label="阶段交付物"><header className="pm-deliverable-head"><div><b>{phase===1?'需求架构规范':phase===2?'方案和UI设计':`阶段 ${phase}`}</b><small>{readyCount} / {docs.length} 已确认</small></div>{gateEnabled&&!readOnly&&<button className="primary" disabled={!allReady||busy} onClick={()=>{setGateStep(1);setConfirmText('');setError('')}}>三关确认晋级</button>}</header>{loadError&&<p className="error" role="alert"><b>{loadError}</b></p>}<div className="deliverable-grid">{docs.map(item=>{const saved=byType.get(item.key),ready=isDeliverableReady(saved?.status);return <button type="button" key={item.key} disabled={readOnly||busy||ready} className={`deliverable-card ${ready?'is-confirmed':''}`} onClick={()=>void confirmDoc(item)}><span className="deliverable-ordinal">{String(item.ordinal).padStart(2,'0')}</span><b>{item.title}</b><small>{statusText(saved?.status)}{!readOnly&&!ready?' · 点击确认':''}</small></button>})}</div>
 <Dialog open={gateStep>0} title={`三关确认 · 阶段 ${phase}`} onClose={closeGate} wide><div className="triple-gate">{gateStep===1&&<><p className="gate-note">第一关：核对本阶段 {docs.length} 份交付物均已绑定且内容完整。</p><ul className="triple-gate-list">{docs.map(d=><li key={d.key}>{String(d.ordinal).padStart(2,'0')} {d.title} · {statusText(byType.get(d.key)?.status)}</li>)}</ul><div className="dialog-actions"><button disabled={busy} onClick={closeGate}>取消</button><button className="primary" onClick={()=>setGateStep(2)}>下一关</button></div></>}
 {gateStep===2&&<><p className="gate-note">第二关：确认晋级影响（不可逆）。</p><div className="confirm-triplet"><span>对象：{project.projectCode} · {project.name}</span><span>影响：{advanceHint(project,phase)}</span><span>意图：绑定 artifactManifestDigest 并推进项目状态</span></div><div className="dialog-actions"><button disabled={busy} onClick={()=>setGateStep(1)}>上一步</button><button className="primary" onClick={()=>setGateStep(3)}>下一关</button></div></>}
 {gateStep===3&&<><p className="gate-note">第三关：输入确认语「{phrase}」后提交。</p><input className="triple-gate-input" aria-label="确认语" value={confirmText} onChange={e=>setConfirmText(e.target.value)} placeholder={phrase}/>{error&&<p className="error" role="alert"><b>{error}</b></p>}<div className="dialog-actions"><button disabled={busy} onClick={()=>setGateStep(2)}>上一步</button><button className="primary" disabled={busy||confirmText.trim()!==phrase} onClick={()=>void finalize()}>{busy?'提交中…':'确认晋级'}</button></div></>}
 </div></Dialog></aside>
}
