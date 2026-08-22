import React,{useCallback,useEffect,useMemo,useRef,useState}from'react'
import{BridgeClientError,createMutationAttempt,deliverableBridge as defaultDeliverableBridge,projectAttachmentBridge as defaultProjectAttachmentBridge,templateBridge as defaultTemplateBridge,type DeliverableBridge,type ProjectAttachmentBridge,type ProjectBridge,type StageBridge,type TemplateBridge}from'../bridge/client'
import type{DeliverableListResult,ProjectAttachmentListResult,ProjectDTO,StageDTO,TemplateListResult}from'../generated/bridge'
import{fileToBase64}from'../session/attachments'
import{Dialog}from'../ui/Dialog'
import{DELIVERABLE_TEMPLATE_LABEL,deliverablesForPhase,gatePhaseForType,isDeliverableReady}from'./deliverableTypes'
import{ChecklistPanel,buildTestItemsFromDev,DEV_ITEM_STATUSES,TEST_ITEM_STATUSES}from'./ChecklistPanel'
import{designPhaseForType,devPhaseForType,isChecklistDocument,type ChecklistDoc}from'./checklistTypes'
import{fetchIntegrationGateReady}from'./checklistStore'
import{createProjectCrRevision,writeStoredCrRevision}from'./crRevision'
import{ProjectPlanPanel}from'./ProjectPlanPanel'
import{normalizeStatus,statusLabel}from'./projectStatus'

type DeliverableItem=DeliverableListResult['items'][number]
type TemplateItem=TemplateListResult['items'][number]

const templateLabelFor=(key:string,phase:number,title:string)=>phase===7&&key==='integration_test_list'?'集成测试场景清单':DELIVERABLE_TEMPLATE_LABEL[key]??title

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

export function DeliverablePanel({project,phase,bridge,deliverableBridge=defaultDeliverableBridge,projectAttachments=defaultProjectAttachmentBridge,templates=defaultTemplateBridge,stages,stageItems,readOnly=false,onProjectUpdated,onStagesUpdated,onDeliverablesChanged}:{project:ProjectDTO;phase:number;bridge:ProjectBridge;deliverableBridge?:DeliverableBridge;projectAttachments?:ProjectAttachmentBridge;templates?:TemplateBridge;stages?:StageBridge;stageItems?:StageDTO[];readOnly?:boolean;onProjectUpdated?:(project:ProjectDTO)=>void;onStagesUpdated?:(items:StageDTO[])=>void;onDeliverablesChanged?:()=>void}):React.JSX.Element|null{
 const docs=deliverablesForPhase(phase,project.type)
 const checklistDocs=useMemo(()=>docs.filter(d=>isChecklistDocument(d.key)),[docs])
 const fileDocs=useMemo(()=>docs.filter(d=>!isChecklistDocument(d.key)),[docs])
 const [items,setItems]=useState<DeliverableItem[]>([])
 const [attachments,setAttachments]=useState<ProjectAttachmentListResult['items']>([])
 const [templateItems,setTemplateItems]=useState<TemplateItem[]>([])
 const [gateStep,setGateStep]=useState<0|1|2|3>(0)
 const [confirmText,setConfirmText]=useState('')
 const [busy,setBusy]=useState(false)
 const [error,setError]=useState('')
 const [loadError,setLoadError]=useState('')
 const fileRef=useRef<HTMLInputElement>(null)
 const uploadTarget=useRef<{key:string;title:string}|null>(null)
 const gateEnabled=gatePhaseForType(project.type).includes(phase)
 const byType=useMemo(()=>new Map(items.map(item=>[item.documentType,item])),[items])
 const templatesByDoc=useMemo(()=>{const map=new Map<string,TemplateItem[]>();for(const doc of docs){const label=templateLabelFor(doc.key,phase,doc.title);const matched=templateItems.filter(t=>t.status==='enabled'&&t.templateType==='document'&&(t.documentType===label||t.documentType===doc.key));map.set(doc.key,matched)}return map},[docs,phase,templateItems])
 const readyCount=useMemo(()=>docs.filter(d=>isDeliverableReady(byType.get(d.key)?.status)).length,[docs,byType])
 const allReady=docs.length>0&&readyCount===docs.length
 const phrase=confirmPhrase(phase)
 const stage=stageItems?.find(s=>s.phase===phase)
 const devPhase=devPhaseForType(project.type)
 const testPhase=project.type==='operations'?5:6
 const showPlanPanel=phase===devPhase||phase===testPhase
 const [integrationGate,setIntegrationGate]=useState<{ready:boolean;blockers:string[]}>({ready:true,blockers:[]})

 useEffect(()=>{if(phase!==7){setIntegrationGate({ready:true,blockers:[]});return}let cancelled=false;void fetchIntegrationGateReady(project).then(v=>{if(!cancelled)setIntegrationGate(v)}).catch(()=>{if(!cancelled)setIntegrationGate({ready:false,blockers:['集成门禁检查失败']})});return()=>{cancelled=true}},[phase,project,items])

 const load=useCallback(async()=>{setLoadError('');try{const [result,att,tpl]=await Promise.all([deliverableBridge.list({projectId:project.id,phase}),projectAttachments.list({projectId:project.id,phase}),templates.list({status:'enabled',templateType:'document'})]);setItems(result.items);setAttachments(att.items);setTemplateItems(tpl.items)}catch(e){setLoadError(problem(e).message)}},[deliverableBridge,projectAttachments,templates,project.id,phase])
 useEffect(()=>{void load()},[load])

 const bindUpload=async(file:File,def:{key:string;title:string})=>{if(readOnly||busy||file.size>10*1024*1024){setError(file.size>10*1024*1024?'文件超过 10 MiB 限制':'无法上传');return}setBusy(true);setError('');try{const contentBase64=await fileToBase64(file),mime=file.type||'application/octet-stream',ingested=await projectAttachments.ingest({projectId:project.id,phase,category:'phase_doc',fileName:file.name,mimeType:mime,contentBase64}),payload={projectId:project.id,phase,documentType:def.key,title:def.title,attachmentId:ingested.attachmentId,status:'review' as const},result=await deliverableBridge.upsert(payload,{attempt:createMutationAttempt('deliverable.upsert',payload)});setItems(values=>{const next=values.filter(v=>v.documentType!==def.key);return [...next,{...result,createdAt:new Date().toISOString(),updatedAt:new Date().toISOString()}]});setAttachments(values=>[ingested,...values.filter(v=>v.attachmentId!==ingested.attachmentId)])}catch(e){setError(problem(e).message)}finally{setBusy(false)}}

 const downloadAttachment=async(attachmentId:string,fileName:string)=>{if(busy)return;setBusy(true);setError('');try{const file=await projectAttachments.get({projectId:project.id,attachmentId}),binary=atob(file.contentBase64),bytes=new Uint8Array(binary.length);for(let i=0;i<binary.length;i++)bytes[i]=binary.charCodeAt(i);const blob=new Blob([bytes],{type:file.mimeType||'application/octet-stream'}),url=URL.createObjectURL(blob),link=document.createElement('a');link.href=url;link.download=fileName||file.fileName;link.click();URL.revokeObjectURL(url)}catch(e){setError(problem(e).message)}finally{setBusy(false)}}

 const pickUpload=(def:{key:string;title:string})=>{if(readOnly||busy)return;uploadTarget.current=def;fileRef.current?.click()}

 const confirmDoc=async(def:{key:string;title:string})=>{if(readOnly||busy)return;setBusy(true);setError('');try{const existing=byType.get(def.key),payload={projectId:project.id,phase,documentType:def.key,title:def.title,status:'approved' as const},result=await deliverableBridge.upsert(payload,{attempt:createMutationAttempt('deliverable.upsert',payload)});setItems(values=>{const next=values.filter(v=>v.documentType!==def.key);return [...next,{...result,createdAt:existing?.createdAt??new Date().toISOString(),updatedAt:new Date().toISOString()}]});if(def.key==='dev_checklist'){try{const cr=await createProjectCrRevision(project,'开发阶段完成 CR 修订',deliverableBridge);writeStoredCrRevision(project.id,cr.crRevisionId,cr.digest)}catch{/* CR optional */}}onDeliverablesChanged?.()}catch(e){setError(problem(e).message)}finally{setBusy(false)}}

 const bindTemplate=async(def:{key:string;title:string},templateId:string)=>{if(readOnly||busy||!templateId)return;setBusy(true);setError('');try{const existing=byType.get(def.key),payload={projectId:project.id,phase,documentType:def.key,title:def.title,templateId,status:(existing?.status==='approved'||existing?.status==='immutable'?'approved':'review') as 'review'|'approved'},result=await deliverableBridge.upsert(payload,{attempt:createMutationAttempt('deliverable.upsert',payload)});setItems(values=>{const next=values.filter(v=>v.documentType!==def.key);return [...next,{...result,createdAt:existing?.createdAt??new Date().toISOString(),updatedAt:new Date().toISOString()}]})}catch(e){setError(problem(e).message)}finally{setBusy(false)}}

 const lockDeliverable=async(current:DeliverableItem)=>{let working:DeliverableItem=current;while(working.status!=='immutable'&&working.gateConfirmations<3){const payload={projectId:project.id,id:working.id,expectedVersion:working.version},attempt=createMutationAttempt('deliverable.confirmGate',payload);const saved=await deliverableBridge.confirmGate(payload,{attempt});working={...working,...saved}}
 return working}

 const closeGate=()=>{if(busy)return;setGateStep(0);setConfirmText('');setError('')}
 const finalize=async()=>{if(busy||readOnly||confirmText.trim()!==phrase)return;setBusy(true);setError('');try{
  const locked=new Map<string,DeliverableItem>()
  for(const doc of docs){const current=byType.get(doc.key);if(!current?.id)continue;locked.set(doc.key,await lockDeliverable(current))}
  setItems(values=>values.map(v=>locked.get(v.documentType)??v))
  const advancePayload={id:project.id,version:project.version,phase},advanceAttempt=createMutationAttempt('project.advanceStatus',advancePayload);const savedProject=await bridge.advanceStatus(advancePayload,{attempt:advanceAttempt});onProjectUpdated?.(savedProject)
  if(stages&&stage){const stagePayload={projectId:project.id,id:stage.id,status:'completed' as const,expectedVersion:stage.version},stageAttempt=createMutationAttempt('stage.update',stagePayload);const savedStage=await stages.update(stagePayload,{attempt:stageAttempt});onStagesUpdated?.((stageItems??[]).map(s=>s.id===savedStage.id?savedStage:s))}
  closeGate();void load()
 }catch(e){setError(problem(e).message)}finally{setBusy(false)}}

 if(!docs.length)return null
 const phaseTitle=phase===1?'需求架构规范':phase===2?'方案和UI设计':`阶段 ${phase}`
 const checklistImport = (key: string) => {
  if (key === 'dev_checklist') {
    return {
      label: '功能开发清单',
      phase: designPhaseForType(project.type),
      documentType: 'feature_dev_list',
      mapItems: (source: ChecklistDoc, _existing: ChecklistDoc) => source.items.map(item => ({ ...item, status: 'pending' as const })),
    }
  }
  if (key === 'test_checklist') {
    return {
      label: '开发检查清单',
      phase: devPhaseForType(project.type),
      documentType: 'dev_checklist',
      mapItems: (source: ChecklistDoc, existing: ChecklistDoc) => buildTestItemsFromDev(source, existing),
    }
  }
  return undefined
 }
 const checklistStatuses=(key:string)=>key==='test_checklist'||key==='integration_test_list'?TEST_ITEM_STATUSES:DEV_ITEM_STATUSES
 const gateBlocked=phase===7&&!integrationGate.ready
 return <aside className="pm-deliverable-panel" aria-label="阶段交付物"><header className="pm-deliverable-head"><div><b>{phaseTitle}</b><small>{readyCount} / {docs.length} 已确认 · {attachments.length} 个附件</small></div>{gateEnabled&&!readOnly&&<button className="primary" disabled={!allReady||busy||gateBlocked} onClick={()=>{setGateStep(1);setConfirmText('');setError('')}}>三关确认晋级</button>}</header>
 {gateBlocked&&<div className="integration-gate" role="alert"><b>集成门禁未通过</b><ul>{integrationGate.blockers.map(b=><li key={b}>{b}</li>)}</ul></div>}{loadError&&<p className="error" role="alert"><b>{loadError}</b></p>}{error&&<p className="error" role="alert"><b>{error}</b></p>}<input ref={fileRef} hidden type="file" accept=".pdf,.doc,.docx,.md,.txt,.json,.yaml,.yml,.xlsx,.xls,.png,.jpg,.jpeg,.webp" onChange={e=>{const file=e.target.files?.[0],target=uploadTarget.current;e.target.value='';if(file&&target)void bindUpload(file,target)}}/>
 {checklistDocs.map(def=><div key={def.key} className="checklist-block"><ChecklistPanel project={project} phase={phase} documentType={def.key} title={def.title} readOnly={readOnly||isDeliverableReady(byType.get(def.key)?.status)} deliverables={deliverableBridge} attachments={projectAttachments} statusOptions={checklistStatuses(def.key)} importFrom={checklistImport(def.key)} enableTestRollback={def.key==='test_checklist'} onSaved={()=>{void load();onDeliverablesChanged?.()}}/>{!readOnly&&!isDeliverableReady(byType.get(def.key)?.status)&&<button type="button" className="deliverable-upload" disabled={busy} onClick={()=>void confirmDoc(def)}>确认 {def.title}</button>}</div>)}
 {showPlanPanel&&<ProjectPlanPanel project={project} phase={phase} checklistPhase={phase} checklistType={phase===devPhase?'dev_checklist':'test_checklist'} checklistTitle={phase===devPhase?'开发检查清单':'测试检查清单'} readOnly={readOnly}/>}
 {fileDocs.length>0&&<div className="deliverable-grid">{fileDocs.map(item=>{const saved=byType.get(item.key),ready=isDeliverableReady(saved?.status),bound=saved?.attachmentId?attachments.find(a=>a.attachmentId===saved.attachmentId):undefined,options=templatesByDoc.get(item.key)??[],selectedTemplate=saved?.templateId??'';return <div key={item.key} className={`deliverable-card ${ready?'is-confirmed':''}`}><button type="button" disabled={readOnly||busy||ready} onClick={()=>void confirmDoc(item)}><span className="deliverable-ordinal">{String(item.ordinal).padStart(2,'0')}</span><b>{item.title}</b><small>{statusText(saved?.status)}{bound?` · ${bound.fileName}`:''}{!readOnly&&!ready?' · 点击确认':''}</small></button>{options.length>0&&!readOnly&&!ready&&<label className="deliverable-template"><span>资产库模版</span><select value={selectedTemplate} disabled={busy} onChange={e=>void bindTemplate(item,e.target.value)}><option value="">不引用模版</option>{options.map(t=><option key={t.id} value={t.id}>{t.name} ({t.templateCode})</option>)}</select></label>}{bound&&<button type="button" className="deliverable-upload" disabled={busy} onClick={()=>void downloadAttachment(bound.attachmentId,bound.fileName)}>下载</button>}{!readOnly&&!ready&&<button type="button" className="deliverable-upload" disabled={busy} onClick={()=>pickUpload(item)}>{bound?'重新上传':'上传附件'}</button>}</div>})}</div>}
 <Dialog open={gateStep>0} title={`三关确认 · 阶段 ${phase}`} onClose={closeGate} wide><div className="triple-gate">{gateStep===1&&<><p className="gate-note">第一关：核对本阶段 {docs.length} 份交付物均已绑定且内容完整。</p><ul className="triple-gate-list">{docs.map(d=><li key={d.key}>{String(d.ordinal).padStart(2,'0')} {d.title} · {statusText(byType.get(d.key)?.status)}</li>)}</ul><div className="dialog-actions"><button disabled={busy} onClick={closeGate}>取消</button><button className="primary" onClick={()=>setGateStep(2)}>下一关</button></div></>}
 {gateStep===2&&<><p className="gate-note">第二关：确认晋级影响（不可逆）。</p><div className="confirm-triplet"><span>对象：{project.projectCode} · {project.name}</span><span>影响：{advanceHint(project,phase)}</span><span>意图：绑定 artifactManifestDigest 并推进项目状态</span></div><div className="dialog-actions"><button disabled={busy} onClick={()=>setGateStep(1)}>上一步</button><button className="primary" onClick={()=>setGateStep(3)}>下一关</button></div></>}
 {gateStep===3&&<><p className="gate-note">第三关：输入确认语「{phrase}」后提交。</p><input className="triple-gate-input" aria-label="确认语" value={confirmText} onChange={e=>setConfirmText(e.target.value)} placeholder={phrase}/>{error&&<p className="error" role="alert"><b>{error}</b></p>}<div className="dialog-actions"><button disabled={busy} onClick={()=>setGateStep(2)}>上一步</button><button className="primary" disabled={busy||confirmText.trim()!==phrase} onClick={()=>void finalize()}>{busy?'提交中…':'确认晋级'}</button></div></>}
 </div></Dialog></aside>
}
