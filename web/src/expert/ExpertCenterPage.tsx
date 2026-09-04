import React,{useCallback,useEffect,useMemo,useState}from'react'
import{createMutationAttempt,expertBridge,getMcpBridge,projectBridge,skillBridge as defaultSkillBridge,type ExpertBridge,type McpBridge,type ProjectBridge,type SkillBridge}from'../bridge/client'
import type{ExpertCatalogListResult,ExpertCreatePayload,ExpertDetailResult,ExpertListResult,ExpertScenarioListResult,ProjectDTO,SkillDTO}from'../generated/bridge'
import{brainBindKey,CONVERSATION_EXPERTS,conversationExpertByNameOrID,conversationExpertEmoji,conversationExpertKind,conversationExpertRole,expertCatalogKey,expertKindOf,isOpsColleague,mcpBindKey,mcpFallbackForExpert,missingPreferredSkills,preferredMcpForExperts,preferredSkillsForExperts,requiredToolsForExperts,shouldOpenExpertAsColleague,splitBoundKeys,type ExpertBrain}from'./conversationExperts'
import{useZh}from'../i18n/language'
import{ExpertDetailTabs}from'./ExpertDetailTabs'
import{ExpertGrowthPanel}from'./ExpertGrowthPanel'
import{ExpertKnowledgePanel}from'./ExpertKnowledgePanel'
import{IMPLEMENTATION_PHASES}from'../project/projectPhases'
import{ConfirmDialog,Dialog}from'../ui/Dialog'
import{usePanelResize}from'../ui/usePanelResize'

type CatalogEntry=ExpertCatalogListResult['items'][number]
type ExpertItem=ExpertListResult['experts'][number]
type ScenarioItem=ExpertScenarioListResult['items'][number]
type PhaseKey=import('../generated/bridge').ExpertMountPayload['phaseKey']
type Division=import('../generated/bridge').ExpertCreatePayload['frontmatter']['division']
type ExpertState=Extract<ExpertItem['state'],'enabled'|'disabled'|'archived'>
interface SixSectionBody{identity:string;mission:string;rules:string;workflow:string;deliverableTemplate:string;successMetrics:string}
interface ExpertMeta{expertId:string;name:string;division:string;source:string;state:string;semver:string;currentVersionId:string}

const DIVISIONS:Record<Division,string>={engineering:'工程',design:'设计',product:'产品','project-management':'项目管理',testing:'测试',security:'安全',operations:'运维',data:'数据'}
const STATES:Record<ExpertState,string>={enabled:'已启用',disabled:'已停用',archived:'已归档'}
const STATUS_CLASS:Record<ExpertState,string>={enabled:'status-published',disabled:'status-disabled',archived:'status-deprecated'}
const SOURCE:Record<ExpertItem['source'],string>={pack:'技能包',local:'本地',builtin:'内置'}
const FILTERS:Array<{id:ExpertState|'';label:string}>=[{id:'',label:'全部'},{id:'enabled',label:'已启用'},{id:'disabled',label:'已停用'},{id:'archived',label:'已归档'}]
const M8_KEYS:PhaseKey[]=['INITIATION_BOUNDARY','RESEARCH_EVIDENCE','REQUIREMENT_DEFINITION','SOLUTION_EXPERIENCE','ARCHITECTURE_PLAN','DEVELOPMENT_CHANGE','VERIFICATION_ACCEPTANCE','RELEASE_DELIVERY']
const MOUNT_STEPS:Array<{key:PhaseKey;step:number;label:string}>=IMPLEMENTATION_PHASES.map((phase,index)=>({key:M8_KEYS[index],step:phase.phase,label:phase.label}))
const SECTION_FIELDS:Array<{key:keyof SixSectionBody;label:string}>=[
 {key:'identity',label:'① 身份'},{key:'mission',label:'② 使命'},{key:'rules',label:'③ 规则'},
 {key:'workflow',label:'④ 流程'},{key:'deliverableTemplate',label:'⑤ 交付模板'},{key:'successMetrics',label:'⑥ 成功度量'}]
const SEMVER=/^\d+\.\d+\.\d+$/
const EMPTY_FORM={name:'',division:'engineering' as Division,description:'',semver:'1.0.0',identity:'',mission:'',rules:'',workflow:'',deliverableTemplate:'',successMetrics:'',changeNote:''}
const EMPTY_SCENARIO={title:'',summary:'',phaseKey:'ARCHITECTURE_PLAN' as PhaseKey,body:'{"steps":["定位","处置","验收"]}'}
const displayName=(item:ExpertItem)=>item.name?.trim()||'未命名专家'
const isMroColleague=(item:ExpertItem)=>isOpsColleague(expertCatalogKey(item))||isOpsColleague(item.name??'')
const displayDivision=(item:ExpertItem)=>DIVISIONS[item.division]??item.division??'未分类'
const kindLabel=(kind:'agent'|'prompt_skill')=>kind==='agent'?'同事专家':'技能包'
const boundSkillKeys=(value:object)=>{const raw=asRecord(value).boundSkills;return Array.isArray(raw)?raw.filter((item):item is string=>typeof item==='string'&&item.trim().length>0):[]}
const catalogUsage=(entry:CatalogEntry)=>{
 const kind=((entry as {kind?:string}).kind??conversationExpertKind(entry.id))
 if(kind==='agent'||conversationExpertByNameOrID(entry.id)||conversationExpertByNameOrID(entry.displayName))return'同事专家'
 return'人设卡'
}
const phaseLabel=(key:PhaseKey)=>{const step=MOUNT_STEPS.find(item=>item.key===key);return step?`${step.step} · ${step.label}`:key==='OPERATIONS_RETROSPECTIVE'?'运维复盘':key}
const asRecord=(value:object)=>value as Record<string,unknown>
const expertMeta=(value:object):ExpertMeta=>{const row=asRecord(value);return{expertId:String(row.expertId??''),name:String(row.name??''),division:String(row.division??''),source:String(row.source??''),state:String(row.state??''),semver:String(row.semver??''),currentVersionId:String(row.currentVersionId??'')}}
const sixOf=(value:object):SixSectionBody=>{const row=asRecord(value);return{identity:String(row.identity??''),mission:String(row.mission??''),rules:String(row.rules??''),workflow:String(row.workflow??''),deliverableTemplate:String(row.deliverableTemplate??''),successMetrics:String(row.successMetrics??'')}}
const sha256Hex=async(value:string)=>{const digest=await globalThis.crypto.subtle.digest('SHA-256',new TextEncoder().encode(value));return Array.from(new Uint8Array(digest)).map(byte=>byte.toString(16).padStart(2,'0')).join('')}

export function ExpertCenterPage({bridge=expertBridge,projects=projectBridge,skills=defaultSkillBridge,mcp,onCreateInChat,onOpenExpert,onUseInSession,onOpenWorkbench,initialSelectedId}:{bridge?:ExpertBridge;projects?:ProjectBridge;skills?:SkillBridge;mcp?:McpBridge;onCreateInChat?:()=>void;onOpenExpert?:(item:ExpertItem)=>void;onUseInSession?:(item:ExpertItem)=>void;onOpenWorkbench?:(item:ExpertItem)=>void;initialSelectedId?:string}):React.JSX.Element{
 const[items,setItems]=useState<ExpertItem[]>([]),[selectedId,setSelectedId]=useState(''),[stateFilter,setStateFilter]=useState<ExpertState|''>(''),[kindFilter,setKindFilter]=useState<''|'agent'|'prompt_skill'>(''),[divisionFilter,setDivisionFilter]=useState<Division|''>(''),[query,setQuery]=useState('')
 const[publishedSkills,setPublishedSkills]=useState<SkillDTO[]>([]),[skillDraft,setSkillDraft]=useState<string[]>([]),[mcpDraft,setMcpDraft]=useState<string[]>([]),[brainDraft,setBrainDraft]=useState<ExpertBrain>('lunitide'),[mcpPresets,setMcpPresets]=useState<Array<{id:string;name:string;description:string}>>([])
 const[view,setView]=useState<'library'|'market'>('library'),[catalog,setCatalog]=useState<CatalogEntry[]>([]),[marketQuery,setMarketQuery]=useState(''),[marketCategory,setMarketCategory]=useState(''),[marketBusy,setMarketBusy]=useState(''),[marketError,setMarketError]=useState(''),[marketLoading,setMarketLoading]=useState(false)
 const[loading,setLoading]=useState(true),[busy,setBusy]=useState(false),[error,setError]=useState(''),[notice,setNotice]=useState('')
 const[addOpen,setAddOpen]=useState(false),[editing,setEditing]=useState<'create'|'edit'|null>(null),[archiving,setArchiving]=useState<ExpertItem|null>(null)
 const[form,setForm]=useState(EMPTY_FORM)
 const[detail,setDetail]=useState<ExpertDetailResult|null>(null),[detailEpoch,setDetailEpoch]=useState(0),[scenarios,setScenarios]=useState<ScenarioItem[]>([]),[scenarioForm,setScenarioForm]=useState(EMPTY_SCENARIO)
 const[projectItems,setProjectItems]=useState<ProjectDTO[]>([])
 const[mounting,setMounting]=useState<ExpertItem|null>(null),[mountProjectId,setMountProjectId]=useState(''),[mountPhases,setMountPhases]=useState<PhaseKey[]>([]),[mountedNow,setMountedNow]=useState<PhaseKey[]>([])
 const[detailWidth,startDetailResize]=usePanelResize({storageKey:'lunitide:expert-detail-width',initial:380,min:280,max:()=>Math.min(560,Math.max(320,window.innerWidth-360)),reverse:true})
 const zh=useZh()

 const load=useCallback(async()=>{setLoading(true);setError('');try{const result=await bridge.list({});setItems(result.experts);setSelectedId(current=>{if(initialSelectedId&&result.experts.some(item=>item.expertId===initialSelectedId))return initialSelectedId;return result.experts.some(item=>item.expertId===current)?current:(result.experts[0]?.expertId??'')})}catch(e){setError(e instanceof Error?e.message:'专家清单加载失败')}finally{setLoading(false)}},[bridge,initialSelectedId])
 const loadCatalog=useCallback(async()=>{if(!bridge.catalogList){setMarketError('当前版本未提供专家目录。');return}setMarketLoading(true);setMarketError('');try{const result=await bridge.catalogList({});setCatalog(result.items??[])}catch(e){setCatalog([]);setMarketError(e instanceof Error?e.message:'专家市场加载失败')}finally{setMarketLoading(false)}},[bridge])
 const refresh=async()=>{await load();setDetailEpoch(value=>value+1)}
 useEffect(()=>{void load()},[load])
 useEffect(()=>{void loadCatalog()},[loadCatalog])
 useEffect(()=>{try{projects.list().then(result=>{const visible=result.items.filter(item=>!item.name.startsWith('⁣'));setProjectItems(visible);setMountProjectId(current=>current||visible[0]?.id||'')}).catch(()=>{})}catch{/* bridge unavailable outside WebView2 */}},[projects])

 const showMarket=marketLoading||!!marketError||catalog.length>0
 useEffect(()=>{if(!showMarket&&view==='market')setView('library')},[showMarket,view])

 const counts=useMemo(()=>Object.fromEntries(FILTERS.map(tab=>[tab.id,tab.id?items.filter(item=>item.state===tab.id).length:items.length])),[items])as Record<ExpertState|'',number>
 const visible=useMemo(()=>items.filter(item=>(!stateFilter||item.state===stateFilter)&&(!kindFilter||expertKindOf(item)===kindFilter)&&(!divisionFilter||item.division===divisionFilter)&&(!query.trim()||displayName(item).toLowerCase().includes(query.trim().toLowerCase()))),[divisionFilter,items,kindFilter,query,stateFilter])
 const selected=items.find(item=>item.expertId===selectedId)??visible[0]
 useEffect(()=>{const expertId=selected?.expertId;if(!expertId){setDetail(null);setScenarios([]);setScenarioForm(EMPTY_SCENARIO);setSkillDraft([]);setMcpDraft([]);setBrainDraft('lunitide');return}let alive=true;setDetail(null);setScenarios([]);setScenarioForm(EMPTY_SCENARIO);Promise.all([bridge.detail({expertId}),bridge.scenarioList({expertId,state:'active'})]).then(([next,cards])=>{if(!alive)return;setDetail(next);setScenarios(cards.items);const name=String(asRecord(next.expert).name??'');const stored=splitBoundKeys(boundSkillKeys(next.expert));setSkillDraft(stored.skills.length?stored.skills:preferredSkillsForExperts([{name}]));setMcpDraft(stored.mcp.length?stored.mcp:preferredMcpForExperts([{name}]));setBrainDraft(stored.brain)}).catch(e=>{if(!alive)return;setDetail(null);setScenarios([]);setError(e instanceof Error?e.message:'专家详情加载失败')});return()=>{alive=false}},[bridge,detailEpoch,selected?.expertId])
 useEffect(()=>{if(!bridge.skillsSet)return;let alive=true;void skills.list({status:'published'}).then(result=>{if(alive)setPublishedSkills(result.items.filter(item=>item.status==='published'))}).catch(()=>{if(alive)setPublishedSkills([])});return()=>{alive=false}},[bridge.skillsSet,skills])
 useEffect(()=>{let alive=true;const client=mcp??(()=>{try{return getMcpBridge()}catch{return undefined}})();if(!client?.presets)return;void client.presets().then(result=>{if(alive)setMcpPresets((result.items??[]).map(item=>({id:item.id,name:item.name,description:item.description})))}).catch(()=>{if(alive)setMcpPresets([])});return()=>{alive=false}},[mcp])

 const beginCreate=()=>{setAddOpen(false);setEditing('create');setError('');setForm(EMPTY_FORM)}
 const beginEdit=()=>{if(!selected||!detail)return;const body=sixOf(detail.sixSection);setEditing('edit');setError('');setForm({...EMPTY_FORM,...body,name:displayName(selected),division:selected.division,semver:selected.semver,changeNote:''})}
 const validateSix=():string|null=>{for(const field of SECTION_FIELDS){const body=form[field.key];if(body.trim().length<1)return`六段中「${field.label}」不能为空`;if(body.length>65536)return`六段中「${field.label}」超出 65536 字符上限`}return null}
 const validateCreate=():string|null=>{if(form.name.trim().length<1||form.name.length>128)return'名称需为 1–128 个字符';if(form.description.trim().length<1||form.description.length>2000)return'描述需为 1–2000 个字符';if(!SEMVER.test(form.semver))return'版本号需为 x.y.z 形式（如 1.0.0）';return validateSix()}
 const sixPayload=():SixSectionBody=>({identity:form.identity,mission:form.mission,rules:form.rules,workflow:form.workflow,deliverableTemplate:form.deliverableTemplate,successMetrics:form.successMetrics})
 const submit=async()=>{if(editing==='create'){const problem=validateCreate();if(problem){setError(problem);return}setBusy(true);setError('');setNotice('');try{const base:ExpertCreatePayload={source:'local',frontmatter:{name:form.name.trim(),division:form.division,description:form.description.trim(),semver:form.semver.trim()},sixSection:sixPayload(),requestId:crypto.randomUUID()},attempt=createMutationAttempt('expert.create',base);await bridge.create(attempt.payload,{attempt});setNotice(`专家「${form.name.trim()}」已创建`);setEditing(null);await refresh()}catch(e){setError(e instanceof Error?e.message:'专家保存失败')}finally{setBusy(false)};return}
  if(editing!=='edit'||!selected||!detail)return;const problem=validateSix();if(problem){setError(problem);return}if(form.changeNote.trim().length<1||form.changeNote.length>2000){setError('变更说明需为 1–2000 个字符');return}
  const expectedVersionId=expertMeta(detail.expert).currentVersionId;if(!expectedVersionId){setError('当前版本不可用，无法保存');return}
  setBusy(true);setError('');setNotice('');try{const base={expertId:selected.expertId,expectedVersionId,sixSection:sixPayload(),changeNote:form.changeNote.trim()},attempt=createMutationAttempt('expert.update',base);const result=await bridge.update(base,{attempt});setNotice(`专家「${displayName(selected)}」已更新为 ${result.semver}`);setEditing(null);await refresh()}catch(e){setError(e instanceof Error?e.message:'专家保存失败')}finally{setBusy(false)}}
 const toggle=async(item:ExpertItem)=>{setBusy(true);setError('');try{const base={expertId:item.expertId,enabled:item.state!=='enabled'},attempt=createMutationAttempt('expert.toggle',base);const result=await bridge.toggle(base,{attempt});setNotice(`专家「${displayName(item)}」已${result.state==='enabled'?'启用':'停用'}`);await refresh()}catch(e){setError(e instanceof Error?e.message:'专家状态更新失败')}finally{setBusy(false)}}
 const archive=async()=>{if(!archiving)return;setBusy(true);setError('');setNotice('');try{const confirmToken=await sha256Hex(`expert.archive|${archiving.expertId}`),base={expertId:archiving.expertId,confirmToken},attempt=createMutationAttempt('expert.archive',base);await bridge.archive(base,{attempt});setNotice(`专家「${displayName(archiving)}」已归档`);setArchiving(null);await refresh()}catch(e){setError(e instanceof Error?e.message:'专家归档失败')}finally{setBusy(false)}}
 const openMount=async(item:ExpertItem)=>{setMounting(item);setError('');const projectId=mountProjectId||projectItems[0]?.id||'';setMountProjectId(projectId);if(!projectId){setMountedNow([]);setMountPhases([]);return}try{const result=await bridge.mountingGet({projectId});const current=result.matrix.flatMap(row=>row.mountings.filter(m=>m.expertId===item.expertId&&m.state==='mounted').map(()=>row.phaseKey as PhaseKey));setMountedNow(current);setMountPhases(current)}catch{setMountedNow([]);setMountPhases([])}}
 useEffect(()=>{if(!mounting||!mountProjectId)return;let alive=true;bridge.mountingGet({projectId:mountProjectId}).then(result=>{if(!alive)return;const current=result.matrix.flatMap(row=>row.mountings.filter(m=>m.expertId===mounting.expertId&&m.state==='mounted').map(()=>row.phaseKey as PhaseKey));setMountedNow(current);setMountPhases(current)}).catch(()=>{if(alive){setMountedNow([]);setMountPhases([])}});return()=>{alive=false}},[bridge,mounting,mountProjectId])
 const togglePhase=(key:PhaseKey)=>setMountPhases(current=>current.includes(key)?current.filter(item=>item!==key):[...current,key])
 const confirmMount=async()=>{if(!mounting||!mountProjectId)return;setBusy(true);setError('');setNotice('')
  try{
   const toMount=mountPhases.filter(key=>!mountedNow.includes(key)),toUnmount=mountedNow.filter(key=>!mountPhases.includes(key))
   if(!toMount.length&&!toUnmount.length){setNotice('挂载没有变化');setMounting(null);return}
   for(const phaseKey of toMount){const base={projectId:mountProjectId,phaseKey,expertId:mounting.expertId,action:'mount' as const},attempt=createMutationAttempt('expert.mount',base);await bridge.mount(base,{attempt})}
   for(const phaseKey of toUnmount){const base={projectId:mountProjectId,phaseKey,expertId:mounting.expertId,action:'unmount' as const},attempt=createMutationAttempt('expert.mount',base);await bridge.mount(base,{attempt})}
   const projectName=projectItems.find(project=>project.id===mountProjectId)?.name??'项目'
   const labels=mountPhases.map(key=>MOUNT_STEPS.find(step=>step.key===key)).filter(Boolean).map(step=>`${step!.step} ${step!.label}`)
   setNotice(labels.length?`已将「${displayName(mounting)}」挂载到 ${projectName}：${labels.join('、')}`:`已从 ${projectName} 卸下「${displayName(mounting)}」`)
   setMounting(null);await refresh()
  }catch(e){setError(e instanceof Error?e.message:'挂载操作失败')}finally{setBusy(false)}}
 const submitScenario=async()=>{if(!selected)return;const title=scenarioForm.title.trim(),summary=scenarioForm.summary.trim();if(title.length<1||title.length>128){setError('场景标题需为 1–128 个字符');return}if(summary.length<1||summary.length>2048){setError('场景摘要需为 1–2048 个字符');return}let scenario:object;try{scenario=JSON.parse(scenarioForm.body)as object}catch{setError('场景 JSON 无法解析');return}if(!scenario||typeof scenario!=='object'||Array.isArray(scenario)||!Object.keys(scenario).length){setError('场景 JSON 需为至少一个字段的对象');return}
  setBusy(true);setError('');setNotice('');try{const base={expertId:selected.expertId,title,summary,phaseKey:scenarioForm.phaseKey,scenario},attempt=createMutationAttempt('expert.scenario.create',base);await bridge.scenarioCreate(base,{attempt});setNotice(`已添加场景卡「${title}」`);setScenarioForm(EMPTY_SCENARIO);setDetailEpoch(value=>value+1)}catch(e){setError(e instanceof Error?e.message:'场景卡创建失败')}finally{setBusy(false)}}
 const removeScenario=async(card:ScenarioItem)=>{if(!window.confirm(`归档场景卡「${card.title}」？`))return;setBusy(true);setError('');try{const base={scenarioCardId:card.scenarioCardId},attempt=createMutationAttempt('expert.scenario.delete',base);await bridge.scenarioDelete(base,{attempt});setNotice(`已归档场景卡「${card.title}」`);setDetailEpoch(value=>value+1)}catch(e){setError(e instanceof Error?e.message:'场景卡归档失败')}finally{setBusy(false)}}
 const skillFloor=selected&&expertKindOf(selected)==='agent'?preferredSkillsForExperts([{name:displayName(selected),id:expertCatalogKey(selected)}]):[]
 const mcpFloor=selected&&expertKindOf(selected)==='agent'?preferredMcpForExperts([{name:displayName(selected),id:expertCatalogKey(selected)}]):[]
 const requiredTools=selected?requiredToolsForExperts([{name:displayName(selected),id:expertCatalogKey(selected)}]):[]
 const mcpFallback=selected?mcpFallbackForExpert(displayName(selected)):''
 const missingSkills=missingPreferredSkills(skillFloor,publishedSkills)
 const installMissing=async(templateId:string)=>{setBusy(true);setError('');setNotice('');try{await skills.install?.({templateId});const listed=await skills.list({status:'published'});setPublishedSkills(listed.items.filter(item=>item.status==='published'));setNotice(`已安装「${templateId}」，保存运行时后即可使用`)}catch(e){setError(e instanceof Error?e.message:`安装 ${templateId} 失败，请去技能中心`)}finally{setBusy(false)}}
 const saveSkills=async()=>{if(!selected||!bridge.skillsSet)return;setBusy(true);setError('');setNotice('');try{const merged=[...new Set([...skillFloor,...skillDraft,...[...new Set([...mcpFloor,...mcpDraft])].map(mcpBindKey),...(brainDraft!=='lunitide'?[brainBindKey(brainDraft)]:[])])];const base={expertId:selected.expertId,skillKeys:merged},attempt=createMutationAttempt('expert.skills.set',base);const result=await bridge.skillsSet(base,{attempt});const split=splitBoundKeys(result.skillKeys??merged);setSkillDraft(split.skills);setMcpDraft(split.mcp);setBrainDraft(split.brain);setNotice(`已更新「${displayName(selected)}」的运行时绑定`);setDetailEpoch(value=>value+1)}catch(e){setError(e instanceof Error?e.message:'运行时绑定失败')}finally{setBusy(false)}}
 const toggleSkillKey=(key:string)=>{if(skillFloor.includes(key))return;setSkillDraft(current=>current.includes(key)?current.filter(item=>item!==key):[...current,key])}
 const toggleMcpKey=(id:string)=>{if(mcpFloor.includes(id))return;setMcpDraft(current=>current.includes(id)?current.filter(item=>item!==id):[...current,id])}
 const installCatalog=async(entry:CatalogEntry)=>{if(!bridge.install||marketBusy)return;setMarketBusy(entry.id);setError('');setNotice('');try{const result=await bridge.install({id:entry.id});setNotice(`已安装「${entry.displayName}」${result.usage==='chat'?'为对话技能':result.usage==='project'?'为项目专家':'（对话技能+项目专家）'}`);await Promise.all([loadCatalog(),refresh()]);setView('library')}catch(e){setError(e instanceof Error?e.message:'安装失败')}finally{setMarketBusy('')}}
 const marketCategories=[...new Map(catalog.map(entry=>[entry.category,0] as const)).keys()].map(id=>[id,catalog.filter(entry=>entry.category===id).length] as [string,number])
 const visibleCatalog=catalog.filter(entry=>(!marketQuery.trim()||`${entry.displayName} ${entry.description} ${entry.category} ${entry.usage}`.toLowerCase().includes(marketQuery.trim().toLowerCase()))).filter(entry=>!marketCategory||entry.category===marketCategory)
 const featuredCatalog=visibleCatalog.filter(entry=>CONVERSATION_EXPERTS.some(item=>item.id===entry.id||item.name===entry.displayName||item.name===entry.name))
 const listedCatalog=visibleCatalog.filter(entry=>!featuredCatalog.some(item=>item.id===entry.id))
 const detailReady=!!selected&&!!detail&&expertMeta(detail.expert).expertId===selected.expertId
 const six=detailReady&&detail?sixOf(detail.sixSection):null
 const mountedRows=(detailReady?detail?.mountings??[]:[]).filter(row=>row.state==='mounted')
 const projectNameOf=(id:string)=>projectItems.find(project=>project.id===id)?.name??id.slice(0,8)

 return <main className="skill-center expert-center-page">
  <header className="skill-center-header"><div><h1 className="view-title">专家中心</h1><p>{items.length} 名已安装{showMarket?` · 市场 ${catalog.length} 个，内嵌改编，不是上游全量`:''}</p><small>技能包把技能挂在专家身上，不钉对话输入框。同事专家是同一月汐引擎上的人设和工具，不是独立进程；点「打开专家」进同事聊天。{showMarket?'缺的用 expert.create。':''}</small></div><button className="primary skill-chat-create" aria-label="添加专家" onClick={()=>setAddOpen(true)}>+ 添加专家</button></header>
  <Dialog open={addOpen} title="添加专家" description="选择创建专家的方式" onClose={()=>setAddOpen(false)}><div className="skill-add-options"><button type="button" className="skill-add-option" onClick={()=>{setAddOpen(false);onCreateInChat?.()}}><span className="skill-add-option-icon">💬</span><span className="skill-add-option-title">通过对话创建</span><small>在对话中描述岗位，AI 引导你完成六段说明书，确认后生成专家</small></button><button type="button" className="skill-add-option" onClick={beginCreate}><span className="skill-add-option-icon">✎</span><span className="skill-add-option-title">手动填写</span><small>填写名称、条线和六段岗位说明书后直接创建</small></button></div></Dialog>
  <section className="skill-center-toolbar">
   <div className="skill-status-tabs" role="tablist" aria-label="专家视图">
    <button type="button" role="tab" aria-selected={view==='library'} onClick={()=>setView('library')}>专家库</button>
    {showMarket&&<button type="button" role="tab" aria-selected={view==='market'} onClick={()=>setView('market')}>专家市场</button>}
   </div>
   {view==='library'&&<>
    <div className="skill-status-tabs" role="tablist" aria-label="专家状态">{FILTERS.map(tab=><button type="button" role="tab" aria-selected={stateFilter===tab.id} key={tab.id||'all'} onClick={()=>setStateFilter(tab.id)}>{tab.label}<small>{counts[tab.id]}</small></button>)}</div>
    <div className="skill-status-tabs" role="tablist" aria-label="专家种类"><button type="button" role="tab" aria-selected={!kindFilter} onClick={()=>setKindFilter('')}>全部种类</button><button type="button" role="tab" aria-selected={kindFilter==='agent'} onClick={()=>setKindFilter('agent')}>同事专家<small>{items.filter(item=>expertKindOf(item)==='agent').length}</small></button><button type="button" role="tab" aria-selected={kindFilter==='prompt_skill'} onClick={()=>setKindFilter('prompt_skill')}>技能包<small>{items.filter(item=>expertKindOf(item)==='prompt_skill').length}</small></button></div>
    <nav className="skill-market-cats expert-div-chips" aria-label="条线过滤">
     <button type="button" aria-pressed={!divisionFilter} onClick={()=>setDivisionFilter('')}>全部</button>
     {(Object.keys(DIVISIONS) as Division[]).map(division=><button type="button" key={division} aria-pressed={divisionFilter===division} onClick={()=>setDivisionFilter(division)}>{DIVISIONS[division]}</button>)}
    </nav>
    <label className="skill-search">搜索专家<input value={query} onChange={e=>setQuery(e.target.value)} placeholder="名称"/></label>
    <button aria-label="刷新专家" onClick={()=>void load()} disabled={loading}>↻</button></>}
   {view==='market'&&<><label className="skill-search">搜索市场<input value={marketQuery} onChange={e=>setMarketQuery(e.target.value)} placeholder="名称、分类或用途"/></label>
   <button aria-label="刷新市场" onClick={()=>void loadCatalog()}>↻</button></>}
  </section>
  {error&&!editing&&!mounting&&<p className="skill-center-error" role="alert">{error}</p>}
  {notice&&<p className="skill-center-notice" role="status">{notice}</p>}
  {view==='market'?<div className="skill-market-page" aria-label="专家市场">{marketLoading?<p role="status">正在载入专家市场…</p>:marketError?<div className="empty"><b>市场加载失败</b><span>{marketError}</span></div>:!catalog.length?<div className="empty"><b>目录为空</b><span>引擎没有返回可安装的专家目录。</span></div>:<>
    <nav className="skill-market-cats" aria-label="市场分类"><button type="button" aria-pressed={!marketCategory} onClick={()=>setMarketCategory('')}>全部<small>{catalog.length}</small></button>{marketCategories.map(([id,count])=><button type="button" key={id} aria-pressed={marketCategory===id} onClick={()=>setMarketCategory(id)}>{id}<small>{count}</small></button>)}</nav>
    {featuredCatalog.length>0&&<section className="skill-market-shelf" aria-label="对话专家"><header><b>对话专家</b><small>{featuredCatalog.length} 个岗位，安装后是同事专家（同一引擎，不是独立进程）</small></header><div className="skill-market">{featuredCatalog.map(entry=><article className={`skill-market-card ${entry.installed?'is-installed':''}`} key={entry.id}><header><span className="skill-market-glyph" aria-hidden="true">{entry.emoji||entry.displayName.slice(0,1)}</span><div><b>{entry.displayName}</b><small>{catalogUsage(entry)} · {entry.category}</small></div>{entry.installed?<span className="skill-market-installed">已安装</span>:<button type="button" className="skill-market-add" aria-label={`安装 ${entry.displayName}`} disabled={Boolean(marketBusy)} onClick={()=>void installCatalog(entry)}>{marketBusy===entry.id?'…':'＋'}</button>}</header><p>{entry.description}</p><footer><small>v{entry.version}</small></footer></article>)}</div></section>}
    <section className="skill-market-shelf" aria-label="可安装专家"><header><b>{marketCategory||'人设卡与其他角色'}</b><small>{listedCatalog.length} 个</small></header>{listedCatalog.length?<div className="skill-market">{listedCatalog.map(entry=><article className={`skill-market-card ${entry.installed?'is-installed':''}`} key={entry.id}><header><span className="skill-market-glyph" aria-hidden="true">{entry.emoji||entry.displayName.slice(0,1)}</span><div><b>{entry.displayName}</b><small>{catalogUsage(entry)} · {entry.category}</small></div>{entry.installed?<span className="skill-market-installed">已安装</span>:<button type="button" className="skill-market-add" aria-label={`安装 ${entry.displayName}`} disabled={Boolean(marketBusy)} onClick={()=>void installCatalog(entry)}>{marketBusy===entry.id?'…':'＋'}</button>}</header><p>{entry.description}</p><footer><small>v{entry.version}</small></footer></article>)}</div>:<div className="empty"><b>没有匹配的专家</b><span>换个分类或关键字再试。</span></div>}</section>
   </>}</div>
  :<div className="skill-center-layout" style={{'--detail-width':`${detailWidth}px`} as React.CSSProperties}>
   <section className="skill-table expert-list" aria-label="已安装专家"><div className="skill-table-inner"><div className="skill-table-head"><span>专家</span><span>版本</span><span>状态</span><span>条线</span></div>{loading?<p role="status">正在载入专家…</p>:visible.length?visible.map(item=><button type="button" className={`skill-row ${selected?.expertId===item.expertId?'active':''}`} key={item.expertId} onClick={()=>{setSelectedId(item.expertId);setEditing(null)}}><span><b>{displayName(item)}</b><small>{kindLabel(expertKindOf(item))} · 已挂载 {item.mountedPhaseCount} 处 · {SOURCE[item.source]??item.source}</small></span><code>v{item.semver||'—'}</code><i className={`skill-status ${STATUS_CLASS[item.state]}`}>{STATES[item.state]??item.state}</i><code>{displayDivision(item)}</code></button>):<div className="empty"><b>暂无专家</b><span>{query||stateFilter||kindFilter||divisionFilter?'没有匹配的专家。':showMarket?'去「专家市场」安装，或点击「添加专家」用对话生成。':'点击「添加专家」用对话生成。'}</span></div>}</div></section>
   <div className="panel-resizer split-resizer" role="separator" aria-label="调整详情栏宽度" aria-orientation="vertical" onPointerDown={startDetailResize}/>
   <aside className="skill-detail expert-detail" aria-label="专家详情">{selected?<>
    <div className="skill-detail-title"><div><h2>{displayName(selected)}</h2><code>{SOURCE[selected.source]??selected.source} · v{selected.semver||'—'} · {selected.versionCount} 个版本</code></div><span className={`skill-status ${STATUS_CLASS[selected.state]}`}>{STATES[selected.state]??selected.state}</span></div>
    <section className="expert-persona-card" aria-label="专家名片"><span className="skill-market-glyph" aria-hidden="true">{conversationExpertEmoji(displayName(selected))}</span><div><b>{displayName(selected)}</b><small>{kindLabel(expertKindOf(selected))} · {conversationExpertRole(selected.division)} · {displayDivision(selected)}</small></div></section>
    <p>{selected.state==='archived'?'该专家已归档，岗位说明书只读。':`已挂载 ${selected.mountedPhaseCount} 处 · ${displayDivision(selected)}`}</p>
    <div className="skill-detail-actions">
     {selected.state==='enabled'&&onOpenExpert?<button type="button" disabled={busy} onClick={()=>onOpenExpert(selected)}>{shouldOpenExpertAsColleague(displayName(selected))?'打开同事':'打开专家'}</button>:null}
     {selected.state==='enabled'&&onUseInSession?<button type="button" disabled={busy} onClick={()=>onUseInSession(selected)}>在当前会话使用</button>:null}
     {selected.state==='enabled'&&onOpenWorkbench&&isMroColleague(selected)?<button type="button" disabled={busy} onClick={()=>onOpenWorkbench(selected)}>{zh?'打开工作台':'Open workbench'}</button>:null}
     {selected.state==='enabled'?<button type="button" disabled={busy} onClick={()=>void openMount(selected)}>挂载</button>:null}
     {selected.state!=='archived'?<button type="button" disabled={busy||!detailReady} onClick={beginEdit}>编辑专家</button>:null}
     {selected.state==='enabled'?<button type="button" disabled={busy} onClick={()=>void toggle(selected)}>停用</button>:selected.state==='disabled'?<button type="button" disabled={busy} onClick={()=>void toggle(selected)}>启用</button>:null}
     {selected.state!=='archived'?<button type="button" className="danger" disabled={busy} onClick={()=>setArchiving(selected)}>归档</button>:null}
    </div>
    <ExpertDetailTabs
     overview={<>
    <h3>运行时</h3>
    <p>技能、工具门闸和 MCP 挂在这位专家身上。对话输入框不会自动出现技能芯片。</p>
    {requiredTools.length?<div className="expert-mount-chips skill-permissions" aria-label="必备工具">{requiredTools.map(tool=><span key={tool}>{tool}</span>)}</div>:null}
    {mcpFallback?<p>{mcpFallback}</p>:null}
    {selected.state!=='archived'&&expertKindOf(selected)==='agent'?<fieldset className="expert-mount-steps" aria-label="大脑"><legend>大脑</legend><label className="expert-mount-step"><input type="radio" name="expert-brain" checked={brainDraft==='lunitide'} onChange={()=>setBrainDraft('lunitide')}/><span><b>月汐引擎</b></span></label><label className="expert-mount-step"><input type="radio" name="expert-brain" checked={brainDraft==='codex'} onChange={()=>setBrainDraft('codex')}/><span><b>本机 Codex</b><small> 一次性执行，可接上上一轮；失败会写「已回落月汐」，并标明本机 Codex 未在 PATH；密钥留在已登录的 CLI</small></span></label><label className="expert-mount-step"><input type="radio" name="expert-brain" checked={brainDraft==='claude'} onChange={()=>setBrainDraft('claude')}/><span><b>本机 Claude Code</b><small> 一次性执行，可接上上一轮；失败会写「已回落月汐」；密钥留在已登录的 CLI</small></span></label></fieldset>:null}
    {missingSkills.length?<div className="expert-missing-skills" role="status"><p>岗位需要但技能库还没有：</p>{missingSkills.map(key=><button type="button" key={key} disabled={busy} onClick={()=>void installMissing(key)}>去技能中心安装 {key}</button>)}</div>:null}
    <h3>挂载技能</h3>
    {skillDraft.length?<div className="expert-mount-chips skill-permissions" aria-label="已挂载技能">{skillDraft.map(key=><span key={key}>{publishedSkills.find(item=>item.name===key||item.name===`tpl-${key}`||item.entryPoint?.endsWith(`://${key}`))?.displayName??key}</span>)}</div>:<p>尚未挂技能，仅提示词。</p>}
    <h3>可用 MCP</h3>
    {mcpDraft.length?<div className="expert-mount-chips skill-permissions" aria-label="已授权 MCP">{mcpDraft.map(id=><span key={id}>{mcpPresets.find(item=>item.id===id)?.name??id}</span>)}</div>:<p>尚未授权 MCP。未连接时走上面的回退。</p>}
    {selected.state!=='archived'&&bridge.skillsSet?<form className="scenario-create" onSubmit={e=>{e.preventDefault();void saveSkills()}}>
     {publishedSkills.length?<fieldset className="expert-mount-steps"><legend>已发布技能{skillFloor.length?'（岗位底线不能卸完）':''}</legend>{publishedSkills.map(item=>{const key=item.entryPoint?.replace(/^builtin:\/\//,'')||item.name.replace(/^tpl-/,'');const locked=skillFloor.includes(key);return <label key={item.id} className="expert-mount-step"><input type="checkbox" checked={skillDraft.includes(key)||skillDraft.includes(item.name)||locked} disabled={locked} onChange={()=>toggleSkillKey(key)}/><span><b>{item.displayName||item.name}</b>{locked?' · 底线':''}</span></label>})}</fieldset>:null}
     {mcpPresets.length?<fieldset className="expert-mount-steps"><legend>MCP 预置{mcpFloor.length?'（岗位底线不能卸完）':''}</legend>{mcpPresets.map(item=>{const locked=mcpFloor.includes(item.id);return <label key={item.id} className="expert-mount-step"><input type="checkbox" checked={mcpDraft.includes(item.id)||locked} disabled={locked} onChange={()=>toggleMcpKey(item.id)}/><span><b>{item.name}</b>{locked?' · 底线':''}<small> {item.description}</small></span></label>})}</fieldset>:null}
     <div className="dialog-actions"><button className="primary" disabled={busy}>保存运行时绑定</button></div>
    </form>:null}
    <h3>六段说明书</h3>
    {six?SECTION_FIELDS.map(field=><div className="sd-sec" key={field.key}><h4>{field.label}</h4><div className="sd-dep">{six[field.key]||'（空）'}</div></div>):<p role="status">正在载入详情…</p>}
    <h3>版本链</h3>
    {detailReady&&detail?.versions.length?<ul className="expert-version-list">{detail.versions.map(version=><li key={version.versionId}><b>v{version.semver}</b><small>{version.changeNote}</small><code>{version.createdAt.slice(0,10)}</code></li>)}</ul>:<p>暂无版本记录</p>}
    <h3>项目挂载</h3>
    {mountedRows.length?<div className="expert-mount-chips skill-permissions">{mountedRows.map(row=><span key={`${row.projectId}:${row.phaseKey}`}>{projectNameOf(row.projectId)} · {phaseLabel(row.phaseKey)}</span>)}</div>:<p>尚未挂载到项目步骤。启用后可点「挂载」。</p>}
    <h3>场景卡</h3>
    {scenarios.length?<div className="expert-scenario-list">{scenarios.map(card=><article className="expert-scenario-card" key={card.scenarioCardId}><header><b>{card.title}</b>{card.state==='active'?<button type="button" disabled={busy} aria-label={`归档场景 ${card.title}`} onClick={()=>void removeScenario(card)}>归档</button>:null}</header><small>{phaseLabel(card.phaseKey)}</small><p>{card.summary}</p></article>)}</div>:<p>还没有场景卡。</p>}
    {selected.state!=='archived'&&<form className="scenario-create" onSubmit={e=>{e.preventDefault();void submitScenario()}}>
     <label>场景标题<input value={scenarioForm.title} maxLength={128} onChange={e=>setScenarioForm({...scenarioForm,title:e.target.value})}/></label>
     <label>适用步骤<select value={scenarioForm.phaseKey} onChange={e=>setScenarioForm({...scenarioForm,phaseKey:e.target.value as PhaseKey})}>{MOUNT_STEPS.map(step=><option key={step.key} value={step.key}>{step.step} · {step.label}</option>)}</select></label>
     <label className="wide">摘要<textarea rows={2} value={scenarioForm.summary} maxLength={2048} onChange={e=>setScenarioForm({...scenarioForm,summary:e.target.value})}/></label>
     <label className="wide">场景 JSON<textarea rows={4} value={scenarioForm.body} onChange={e=>setScenarioForm({...scenarioForm,body:e.target.value})}/></label>
     <div className="dialog-actions"><button className="primary" disabled={busy}>添加场景卡</button></div>
    </form>}
     </>}
     knowledge={selected?<ExpertKnowledgePanel expertId={selected.expertId} knowledgeGet={bridge.knowledgeGet} upsertDocument={bridge.kbUpsertDocument}/>:null}
     growth={selected?<ExpertGrowthPanel expertId={selected.expertId} growthGet={bridge.growthGet}/>:null}
    />
   </>:<div className="empty"><b>选择专家查看详情</b></div>}</aside>
  </div>}
  <Dialog open={!!mounting} title={`挂载「${mounting?displayName(mounting):''}」`} description="选择项目和要挂上的步骤。同一专家可以挂到多个步骤、多个项目；对话里也可以再选一次这位专家。" onClose={()=>{if(!busy)setMounting(null)}}>
   <form className="editor-dialog expert-mount-form" onSubmit={e=>{e.preventDefault();void confirmMount()}}>
    <label>项目<select aria-label="挂载到项目" value={mountProjectId} onChange={e=>setMountProjectId(e.target.value)}>{projectItems.length?projectItems.map(project=><option key={project.id} value={project.id}>{project.name}</option>):<option value="">（暂无项目）</option>}</select></label>
    <fieldset className="expert-mount-steps"><legend>项目 8 个步骤</legend>
     {MOUNT_STEPS.map(step=><label key={step.key} className="expert-mount-step"><input type="checkbox" checked={mountPhases.includes(step.key)} onChange={()=>togglePhase(step.key)}/><span><b>{step.step} · {step.label}</b></span></label>)}
    </fieldset>
    {error&&<p className="skill-center-error" role="alert">{error}</p>}
    <div className="dialog-actions"><button type="button" disabled={busy} onClick={()=>setMounting(null)}>取消</button><button className="primary" disabled={busy||!mountProjectId}>{busy?'保存中…':'确认挂载'}</button></div>
   </form>
  </Dialog>
  <Dialog open={!!editing} wide title={editing==='edit'?`编辑 ${selected?displayName(selected):'专家'}`:'创建专家向导'} description={editing==='edit'?'修改六段岗位说明书并填写变更说明，保存后追加一个新版本。':'填写基础信息和六段岗位说明书。任一段为空都会拒绝保存。'} onClose={()=>{if(!busy)setEditing(null)}}>
   <form className="editor-dialog" onSubmit={e=>{e.preventDefault();void submit()}}>
    <div className="form-grid expert-form-grid">
     {editing==='create'&&<><label>名称<input value={form.name} maxLength={128} onChange={e=>setForm({...form,name:e.target.value})}/></label>
     <label>条线<select value={form.division} onChange={e=>setForm({...form,division:e.target.value as Division})}>{(Object.keys(DIVISIONS) as Division[]).map(division=><option key={division} value={division}>{DIVISIONS[division]}</option>)}</select></label>
     <label>版本号<input value={form.semver} onChange={e=>setForm({...form,semver:e.target.value})}/></label>
     <label className="wide">描述<textarea rows={2} value={form.description} maxLength={2000} onChange={e=>setForm({...form,description:e.target.value})}/></label></>}
     {SECTION_FIELDS.map(field=><label key={field.key}>{field.label}<textarea rows={4} value={form[field.key]} maxLength={65536} onChange={e=>setForm({...form,[field.key]:e.target.value})}/></label>)}
     {editing==='edit'&&<label className="wide">变更说明<input value={form.changeNote} maxLength={2000} onChange={e=>setForm({...form,changeNote:e.target.value})}/></label>}
    </div>
    {error&&<p className="skill-center-error" role="alert">{error}</p>}
    <div className="dialog-actions"><button type="button" disabled={busy} onClick={()=>setEditing(null)}>取消</button><button className="primary" disabled={busy}>{busy?'保存中…':editing==='edit'?'保存修改':'创建专家'}</button></div>
   </form>
  </Dialog>
  <ConfirmDialog open={!!archiving} title={`归档专家「${archiving?displayName(archiving):''}」？`} description="归档后不可再启用或挂载，已有版本会一并归档。" confirmLabel="确认归档" busy={busy} onCancel={()=>setArchiving(null)} onConfirm={()=>void archive()}/>
 </main>
}
