import React,{useCallback,useEffect,useMemo,useRef,useState}from'react'
import{asUserBridgeError}from'../bridge/bridgeUserError'
import{BridgeClientError,createMutationAttempt,deliverableBridge as defaultDeliverableBridge,projectAttachmentBridge as defaultProjectAttachmentBridge,templateBridge as defaultTemplateBridge,type DeliverableBridge,type ProjectAttachmentBridge,type ProjectBridge,type StageBridge,type TemplateBridge}from'../bridge/client'
import type{DeliverableListResult,ProjectAttachmentListResult,ProjectDTO,StageDTO,TemplateListResult}from'../generated/bridge'
import{fileToBase64}from'../session/attachments'
import{Dialog}from'../ui/Dialog'
import{DELIVERABLE_TEMPLATE_LABEL,dbRegistryPhase,deliverablesForPhase,gatePhaseForType,interfaceRegistryPhase,isDeliverableReady}from'./deliverableTypes'
import{ChecklistPanel,buildTestItemsFromDev,DEV_ITEM_STATUSES,TEST_ITEM_STATUSES}from'./ChecklistPanel'
import{ProjectTreeEditor}from'./ProjectTreeEditor'
import{PhaseGenerateBar}from'./PhaseGenerateBar'
import{SchemaEditor}from'./SchemaEditor'
import{WorkBoardPanel}from'./WorkBoardPanel'
import{designPhaseForType,devPhaseForType,isChecklistDocument,parseChecklist,checklistFromBase64,type ChecklistDoc,type ChecklistItem}from'./checklistTypes'
import{fetchIntegrationGateReady}from'./checklistStore'
import{projectFactoryApi}from'./projectFactoryApi'
import{createProjectCrRevision,writeStoredCrRevision}from'./crRevision'
import{ProjectPlanPanel}from'./ProjectPlanPanel'
import{normalizeStatus,statusLabel}from'./projectStatus'
import{listTemplatePages}from'../assets/templatePages'

type DeliverableItem=DeliverableListResult['items'][number]
type TemplateItem=TemplateListResult['items'][number]

const templateLabelFor=(key:string,phase:number,title:string)=>phase===7&&key==='integration_test_list'?'集成测试场景清单':DELIVERABLE_TEMPLATE_LABEL[key]??title

function deliverableUserError(err:unknown,fallback:string):string{
 const detail=err instanceof Error?err.message.trim():''
 return /[\u4e00-\u9fff]/.test(detail)?detail:fallback
}
const problem=(e:unknown)=>e instanceof BridgeClientError?asUserBridgeError(e,'请求失败'):new BridgeClientError(deliverableUserError(e,'请求失败'),'CLIENT_ERROR',false,'renderer')
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
// Doc types that are legitimately evidence-free (no attachment/template/checklist
// required to confirm). Empty today — every current交付物 must carry evidence —
// but kept as the single extension point so future evidence-free docs stay explicit.
const DELIVERABLE_EVIDENCE_FREE=new Set<string>([])

async function resyncBoardIfDirty(projectId:string,key:string,result:{boardDirty?:boolean}){
 const boardKind=key==='interface_list'||key==='api_list'?'interface'
  :key==='dev_checklist'||key==='feature_dev_list'?'dev'
  :key==='test_checklist'?'test'
  :key==='integration_test_list'?'integration'
  :undefined
 if(boardKind&&result.boardDirty){
  await projectFactoryApi.boardSync({projectId,boardKind}).catch(()=>{})
 }
}

export function DeliverablePanel({project,phase,bridge,deliverableBridge=defaultDeliverableBridge,projectAttachments=defaultProjectAttachmentBridge,templates=defaultTemplateBridge,stages,readOnly=false,onProjectUpdated,onStagesUpdated,onDeliverablesChanged,onOpenTask,onSelectBoardItem,currentTaskId,onGoDevItem,expertCount=0,onPrefillPrompt}:{project:ProjectDTO;phase:number;bridge:ProjectBridge;deliverableBridge?:DeliverableBridge;projectAttachments?:ProjectAttachmentBridge;templates?:TemplateBridge;stages?:StageBridge;stageItems?:StageDTO[];readOnly?:boolean;onProjectUpdated?:(project:ProjectDTO)=>void;onStagesUpdated?:(items:StageDTO[])=>void;onDeliverablesChanged?:()=>void;onOpenTask?:(itemId:string,executor?:ChecklistItem['executor'])=>void;onSelectBoardItem?:(itemId:string,brief:string)=>void;currentTaskId?:string;onGoDevItem?:(itemId:string)=>void;expertCount?:number;onPrefillPrompt?:(text:string)=>void}):React.JSX.Element|null{
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
 const devPhase=devPhaseForType(project.type)
 const testPhase=project.type==='operations'?5:6
 const showPlanPanel=phase===devPhase||phase===testPhase
 const [integrationGate,setIntegrationGate]=useState<{ready:boolean;blockers:string[]}>({ready:true,blockers:[]})
 const [emptyDevAck,setEmptyDevAck]=useState(false)
 const [emptyIfaceAck,setEmptyIfaceAck]=useState(false)

 useEffect(()=>{if(phase!==7){setIntegrationGate({ready:true,blockers:[]});return}let cancelled=false;void fetchIntegrationGateReady(project).then(v=>{if(!cancelled)setIntegrationGate(v)}).catch(()=>{if(!cancelled)setIntegrationGate({ready:false,blockers:['集成门禁检查失败']})});return()=>{cancelled=true}},[phase,project,items])

 const load=useCallback(async()=>{setLoadError('');try{const [result,att,tpl]=await Promise.all([deliverableBridge.list({projectId:project.id,phase}),projectAttachments.list({projectId:project.id,phase}),listTemplatePages(templates,{status:'enabled',templateType:'document'})]);setItems(result.items);setAttachments(att.items);setTemplateItems(tpl.items)}catch(e){setLoadError(problem(e).message)}},[deliverableBridge,projectAttachments,templates,project.id,phase])
 useEffect(()=>{void load()},[load])

 const bindUpload=async(file:File,def:{key:string;title:string})=>{if(readOnly||busy||file.size>10*1024*1024){setError(file.size>10*1024*1024?'文件超过 10 MiB 限制':'无法上传');return}setBusy(true);setError('');try{const contentBase64=await fileToBase64(file),mime=file.type||'application/octet-stream',ingested=await projectAttachments.ingest({projectId:project.id,phase,category:'phase_doc',fileName:file.name,mimeType:mime,contentBase64}),payload={projectId:project.id,phase,documentType:def.key,title:def.title,attachmentId:ingested.attachmentId,status:'review' as const},result=await deliverableBridge.upsert(payload,{attempt:createMutationAttempt('deliverable.upsert',payload)});setItems(values=>{const next=values.filter(v=>v.documentType!==def.key);return [...next,{...result,createdAt:new Date().toISOString(),updatedAt:new Date().toISOString()}]});setAttachments(values=>[ingested,...values.filter(v=>v.attachmentId!==ingested.attachmentId)]);await resyncBoardIfDirty(project.id,def.key,result)}catch(e){setError(problem(e).message)}finally{setBusy(false)}}

 const downloadAttachment=async(attachmentId:string,fileName:string)=>{if(busy)return;setBusy(true);setError('');try{const file=await projectAttachments.get({projectId:project.id,attachmentId}),binary=atob(file.contentBase64),bytes=new Uint8Array(binary.length);for(let i=0;i<binary.length;i++)bytes[i]=binary.charCodeAt(i);const blob=new Blob([bytes],{type:file.mimeType||'application/octet-stream'}),url=URL.createObjectURL(blob),link=document.createElement('a');link.href=url;link.download=fileName||file.fileName;link.click();URL.revokeObjectURL(url)}catch(e){setError(problem(e).message)}finally{setBusy(false)}}

 const pickUpload=(def:{key:string;title:string})=>{if(readOnly||busy)return;uploadTarget.current=def;fileRef.current?.click()}

 const confirmDoc=async(def:{key:string;title:string})=>{if(readOnly||busy)return;const existing=byType.get(def.key);
  // Integrity gate (Issue 4b): a deliverable may only be marked 已批准 when it
  // carries real evidence — a bound attachment, a referenced 资产库 template, or
  // (for checklist docs) a saved checklist blob, which也 lands as an attachmentId.
  // This stops the one-click "确认" of an empty交付物 that made the panel look
  // complete without any content behind it.
  if(!DELIVERABLE_EVIDENCE_FREE.has(def.key)&&!existing?.attachmentId){setError(`请先为「${def.title}」生成或上传不少于 32 字节的附件正文后再确认，不能只绑模版。`);return}
  setBusy(true);setError('');try{
  if(def.key==='dev_checklist'||def.key==='test_checklist'){
    if(!existing?.attachmentId){setError(`请先维护「${def.title}」后再确认。`);return}
    const file=await projectAttachments.get({projectId:project.id,attachmentId:existing.attachmentId})
    const parsed=parseChecklist(checklistFromBase64(file.contentBase64))
    if(def.key==='dev_checklist'){
      if(!parsed.items.length){setError('开发清单还是空的，请先维护条目或走三关确认并勾选「无开发任务」。');return}
      if(parsed.items.some(item=>item.status!=='dev_done')){setError('开发未完成，不能确认开发清单。');return}
    }
    if(def.key==='test_checklist'&&parsed.items.some(item=>item.status!=='test_pass')){setError('测试未全部通过，不能确认测试清单。');return}
  }
  const payload={projectId:project.id,phase,documentType:def.key,title:def.title,status:'approved' as const},result=await deliverableBridge.upsert(payload,{attempt:createMutationAttempt('deliverable.upsert',payload)});setItems(values=>{const next=values.filter(v=>v.documentType!==def.key);return [...next,{...result,createdAt:existing?.createdAt??new Date().toISOString(),updatedAt:new Date().toISOString()}]});await resyncBoardIfDirty(project.id,def.key,result);if(def.key==='dev_checklist'){try{const cr=await createProjectCrRevision(project,'开发阶段完成 CR 修订',deliverableBridge);writeStoredCrRevision(project.id,cr.crRevisionId,cr.digest)}catch{/* CR optional */}}onDeliverablesChanged?.()}catch(e){setError(problem(e).message)}finally{setBusy(false)}}

 const bindTemplate=async(def:{key:string;title:string},templateId:string)=>{if(readOnly||busy||!templateId)return;setBusy(true);setError('');try{const existing=byType.get(def.key),payload={projectId:project.id,phase,documentType:def.key,title:def.title,templateId,status:(existing?.status==='approved'||existing?.status==='immutable'?'approved':'review') as 'review'|'approved'},result=await deliverableBridge.upsert(payload,{attempt:createMutationAttempt('deliverable.upsert',payload)});setItems(values=>{const next=values.filter(v=>v.documentType!==def.key);return [...next,{...result,createdAt:existing?.createdAt??new Date().toISOString(),updatedAt:new Date().toISOString()}]});await resyncBoardIfDirty(project.id,def.key,result)}catch(e){setError(problem(e).message)}finally{setBusy(false)}}

 const closeGate=()=>{if(busy)return;setGateStep(0);setConfirmText('');setEmptyDevAck(false);setEmptyIfaceAck(false);setError('')}
 const emptyDevNeedsAck=phase===devPhase&&(!byType.get('dev_checklist')?.attachmentId||byType.get('dev_checklist')?.digest==='items:0')
 const emptyIfaceNeedsAck=phase===interfaceRegistryPhase(project.type)&&(!byType.get('interface_list')?.attachmentId||byType.get('interface_list')?.digest==='items:0')
 const emptyBoardNeedsAck=emptyDevNeedsAck||emptyIfaceNeedsAck
 const emptyBoardAck=emptyDevNeedsAck?emptyDevAck:emptyIfaceAck
 const finalize=async()=>{if(busy||readOnly||confirmText.trim()!==phrase||(emptyBoardNeedsAck&&!emptyBoardAck))return;setBusy(true);setError('');try{
  const advancePayload={id:project.id,version:project.version,phase,emptyBoardAck:emptyBoardNeedsAck?emptyBoardAck:undefined},advanceAttempt=createMutationAttempt('project.advanceStatus',advancePayload);const savedProject=await bridge.advanceStatus(advancePayload,{attempt:advanceAttempt});onProjectUpdated?.(savedProject)
  if(stages){const refreshed=await stages.list({projectId:project.id});onStagesUpdated?.(refreshed.items)}
  closeGate();void load()
 }catch(e){setError(problem(e).message)}finally{setBusy(false)}}

 if(!docs.length)return null
 const phaseTitle=phase===1?'需求架构规范':phase===2?'方案和UI设计':`阶段 ${phase}`
 const checklistImport = (key: string) => {
  if (key === 'dev_checklist') {
    if (project.type === 'operations') {
      return {
        label: '需求任务清单',
        phase: 1,
        documentType: 'req_task_list',
        mapItems: (source: ChecklistDoc, existing: ChecklistDoc) => {
          const known = new Set(existing.items.flatMap(item => [item.id, item.sourceId ?? '']))
          return source.items
            .filter(item => !known.has(item.id) && !known.has(item.sourceId ?? ''))
            .map(item => ({ ...item, status: 'pending' as const, sourceId: item.sourceId ?? item.id }))
        },
      }
    }
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
 {(phase===1||phase===2)&&<PhaseGenerateBar projectId={project.id} phase={phase} expertCount={expertCount} docs={docs.map(d=>({key:d.key,title:d.title}))} items={items} templatesByDoc={templatesByDoc} onGenerated={()=>{void load();onDeliverablesChanged?.()}} onPrefillPrompt={onPrefillPrompt} onBindTemplates={async bindings=>{for(const item of bindings){const existing=byType.get(item.key),payload={projectId:project.id,phase,documentType:item.key,title:item.title,templateId:item.templateId,status:'review' as const},result=await deliverableBridge.upsert(payload,{attempt:createMutationAttempt('deliverable.upsert',payload)});setItems(values=>{const next=values.filter(v=>v.documentType!==item.key);return[...next,{...result,createdAt:existing?.createdAt??new Date().toISOString(),updatedAt:new Date().toISOString()}]});await resyncBoardIfDirty(project.id,item.key,result)}}}/>}
 {phase===1&&<ProjectTreeEditor project={project} readOnly={readOnly} onProjectUpdated={onProjectUpdated}/>}
 {phase===dbRegistryPhase(project.type)&&<SchemaEditor project={project} readOnly={readOnly} onProjectUpdated={onProjectUpdated}/>}
 {checklistDocs.map(def=>{const boardKind=def.key==='interface_list'?'interface':def.key==='test_checklist'?'test':def.key==='integration_test_list'&&phase===7?'integration':def.key==='dev_checklist'?'dev':undefined;return <div key={def.key} className="checklist-block">{boardKind&&boardKind!=='dev'?<WorkBoardPanel project={project} boardKind={boardKind} title={def.title} readOnly={readOnly||isDeliverableReady(byType.get(def.key)?.status)} currentItemId={currentTaskId} onOpenItem={id=>onSelectBoardItem?.(id,'')} onBrief={text=>onPrefillPrompt?.(text)}/>:<ChecklistPanel project={project} phase={phase} documentType={def.key} title={def.title} readOnly={readOnly||isDeliverableReady(byType.get(def.key)?.status)} deliverables={deliverableBridge} attachments={projectAttachments} statusOptions={checklistStatuses(def.key)} importFrom={checklistImport(def.key)} enableTestRollback={def.key==='test_checklist'} autoImport={def.key==='dev_checklist'||def.key==='test_checklist'} onOpenTask={def.key==='dev_checklist'?onOpenTask:undefined} currentTaskId={currentTaskId} onGoDevItem={def.key==='test_checklist'?onGoDevItem:undefined} onSaved={()=>{void load();onDeliverablesChanged?.()}}/>}{!readOnly&&!isDeliverableReady(byType.get(def.key)?.status)&&<button type="button" className="deliverable-upload" disabled={busy} onClick={()=>void confirmDoc(def)}>确认 {def.title}</button>}</div>})}
 {showPlanPanel&&<ProjectPlanPanel project={project} phase={phase} checklistPhase={phase} checklistType={phase===devPhase?'dev_checklist':'test_checklist'} checklistTitle={phase===devPhase?'开发检查清单':'测试检查清单'} readOnly={readOnly}/>}
 {fileDocs.length>0&&<div className="deliverable-grid">{fileDocs.map(item=>{const saved=byType.get(item.key),ready=isDeliverableReady(saved?.status),bound=saved?.attachmentId?attachments.find(a=>a.attachmentId===saved.attachmentId):undefined,options=templatesByDoc.get(item.key)??[],selectedTemplate=saved?.templateId??'';return <div key={item.key} className={`deliverable-card ${ready?'is-confirmed':''}`}><button type="button" disabled={readOnly||busy||ready} onClick={()=>void confirmDoc(item)}><span className="deliverable-ordinal">{String(item.ordinal).padStart(2,'0')}</span><b>{item.title}</b><small>{statusText(saved?.status)}{bound?` · ${bound.fileName}`:''}{!readOnly&&!ready?' · 点击确认':''}</small></button>{options.length>0&&!readOnly&&!ready&&<label className="deliverable-template"><span>资产库模版</span><select value={selectedTemplate} disabled={busy} onChange={e=>void bindTemplate(item,e.target.value)}><option value="">不引用模版</option>{options.map(t=><option key={t.id} value={t.id}>{t.name} ({t.templateCode})</option>)}</select></label>}{bound&&<button type="button" className="deliverable-upload" disabled={busy} onClick={()=>void downloadAttachment(bound.attachmentId,bound.fileName)}>下载</button>}{!readOnly&&!ready&&<button type="button" className="deliverable-upload" disabled={busy} onClick={()=>pickUpload(item)}>{bound?'重新上传':'上传附件'}</button>}</div>})}</div>}
 <Dialog open={gateStep>0} title={`三关确认 · 阶段 ${phase}`} onClose={closeGate} wide><div className="triple-gate">{gateStep===1&&<><p className="gate-note">第一关：核对本阶段 {docs.length} 份交付物均已绑定且内容完整。</p><ul className="triple-gate-list">{docs.map(d=><li key={d.key}>{String(d.ordinal).padStart(2,'0')} {d.title} · {statusText(byType.get(d.key)?.status)}</li>)}</ul><div className="dialog-actions"><button disabled={busy} onClick={closeGate}>取消</button><button className="primary" onClick={()=>setGateStep(2)}>下一关</button></div></>}
 {gateStep===2&&<><p className="gate-note">第二关：确认晋级影响（不可逆）。</p><div className="confirm-triplet"><span>对象：{project.projectCode} · {project.name}</span><span>影响：{advanceHint(project,phase)}</span><span>意图：绑定 artifactManifestDigest 并推进项目状态</span></div><div className="dialog-actions"><button disabled={busy} onClick={()=>setGateStep(1)}>上一步</button><button className="primary" onClick={()=>setGateStep(3)}>下一关</button></div></>}
 {gateStep===3&&<><p className="gate-note">第三关：输入确认语「{phrase}」后提交。</p><input className="triple-gate-input" aria-label="确认语" value={confirmText} onChange={e=>setConfirmText(e.target.value)} placeholder={phrase}/>{emptyDevNeedsAck&&<label className="gate-note"><input type="checkbox" checked={emptyDevAck} onChange={e=>setEmptyDevAck(e.target.checked)}/> 无开发任务</label>}{emptyIfaceNeedsAck&&<label className="gate-note"><input type="checkbox" checked={emptyIfaceAck} onChange={e=>setEmptyIfaceAck(e.target.checked)}/> 无接口任务</label>}{error&&<p className="error" role="alert"><b>{error}</b></p>}<div className="dialog-actions"><button disabled={busy} onClick={()=>setGateStep(2)}>上一步</button><button className="primary" disabled={busy||confirmText.trim()!==phrase||(emptyBoardNeedsAck&&!emptyBoardAck)} onClick={()=>void finalize()}>{busy?'提交中…':'确认晋级'}</button></div></>}
 </div></Dialog></aside>
}
