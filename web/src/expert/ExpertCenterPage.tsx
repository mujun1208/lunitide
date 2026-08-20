import React,{useCallback,useEffect,useState}from'react'
import{createMutationAttempt,expertBridge,projectBridge,type ExpertBridge,type ProjectBridge}from'../bridge/client'
import type{ExpertCatalogListResult,ExpertCreatePayload,ExpertListResult,ProjectDTO}from'../generated/bridge'
import{IMPLEMENTATION_PHASES}from'../project/projectPhases'
import{Dialog}from'../ui/Dialog'

type CatalogEntry=ExpertCatalogListResult['items'][number]
type ExpertItem=ExpertListResult['experts'][number]
const USAGE:Record<CatalogEntry['usage'],string>={chat:'对话技能',project:'项目专家',both:'对话+项目'}
type PhaseKey=import('../generated/bridge').ExpertMountPayload['phaseKey']
type Division=import('../generated/bridge').ExpertCreatePayload['frontmatter']['division']
type ExpertState=Extract<ExpertItem['state'],'enabled'|'disabled'|'archived'>
interface SixSectionBody{identity:string;mission:string;rules:string;workflow:string;deliverableTemplate:string;successMetrics:string}

const DIVISIONS:Record<Division,string>={engineering:'工程',design:'设计',product:'产品','project-management':'项目管理',testing:'测试',security:'安全',operations:'运维',data:'数据'}
const STATES:Record<ExpertState,string>={enabled:'已启用',disabled:'已停用',archived:'已归档'}
const M8_KEYS:PhaseKey[]=['INITIATION_BOUNDARY','RESEARCH_EVIDENCE','REQUIREMENT_DEFINITION','SOLUTION_EXPERIENCE','ARCHITECTURE_PLAN','DEVELOPMENT_CHANGE','VERIFICATION_ACCEPTANCE','RELEASE_DELIVERY']
const MOUNT_STEPS:Array<{key:PhaseKey;step:number;label:string}>=IMPLEMENTATION_PHASES.map((phase,index)=>({key:M8_KEYS[index],step:phase.phase,label:phase.label}))
const SECTION_FIELDS:Array<{key:keyof SixSectionBody;label:string}>=[
 {key:'identity',label:'① 身份'},{key:'mission',label:'② 使命'},{key:'rules',label:'③ 规则'},
 {key:'workflow',label:'④ 流程'},{key:'deliverableTemplate',label:'⑤ 交付模板'},{key:'successMetrics',label:'⑥ 成功度量'}]
const SEMVER=/^\d+\.\d+\.\d+$/
const displayName=(item:ExpertItem)=>item.name?.trim()||'未命名专家'
const displayDivision=(item:ExpertItem)=>DIVISIONS[item.division]??item.division??'未分类'

export function ExpertCenterPage({bridge=expertBridge,projects=projectBridge,onCreateInChat}:{bridge?:ExpertBridge;projects?:ProjectBridge;onCreateInChat?:()=>void}):React.JSX.Element{
 const[items,setItems]=useState<ExpertItem[]>([]),[divisionFilter,setDivisionFilter]=useState<Division|''>(''),[query,setQuery]=useState('')
 const[view,setView]=useState<'library'|'market'>('library'),[catalog,setCatalog]=useState<CatalogEntry[]>([]),[marketQuery,setMarketQuery]=useState(''),[marketCategory,setMarketCategory]=useState(''),[marketBusy,setMarketBusy]=useState('')
 const[loading,setLoading]=useState(true),[busy,setBusy]=useState(false),[error,setError]=useState(''),[notice,setNotice]=useState('')
 const[editing,setEditing]=useState<'create'|null>(null)
 const[form,setForm]=useState({name:'',division:'engineering' as Division,description:'',semver:'1.0.0',identity:'',mission:'',rules:'',workflow:'',deliverableTemplate:'',successMetrics:''})
 const[projectItems,setProjectItems]=useState<ProjectDTO[]>([])
 const[mounting,setMounting]=useState<ExpertItem|null>(null),[mountProjectId,setMountProjectId]=useState(''),[mountPhases,setMountPhases]=useState<PhaseKey[]>([]),[mountedNow,setMountedNow]=useState<PhaseKey[]>([])

 const load=useCallback(async()=>{setLoading(true);setError('');try{const result=await bridge.list({});setItems(result.experts)}catch(e){setError(e instanceof Error?e.message:'专家清单加载失败')}finally{setLoading(false)}},[bridge])
 const loadCatalog=useCallback(async()=>{if(!bridge.catalogList)return;try{const result=await bridge.catalogList({});setCatalog(result.items)}catch{/* 市场暂不可用时不阻塞专家库 */}},[bridge])
 useEffect(()=>{void load()},[load])
 useEffect(()=>{void loadCatalog()},[loadCatalog])
 useEffect(()=>{try{projects.list().then(result=>{const visible=result.items.filter(item=>!item.name.startsWith('⁣'));setProjectItems(visible);setMountProjectId(current=>current||visible[0]?.id||'')}).catch(()=>{})}catch{/* bridge unavailable outside WebView2 */}},[projects])

 const visible=items.filter(item=>(!divisionFilter||item.division===divisionFilter)&&(!query.trim()||displayName(item).toLowerCase().includes(query.trim().toLowerCase())))
 const beginCreate=()=>{setEditing('create');setError('');setForm({name:'',division:'engineering',description:'',semver:'1.0.0',identity:'',mission:'',rules:'',workflow:'',deliverableTemplate:'',successMetrics:''})}
 const validate=():string|null=>{const{name,description,semver,...section}=form
  if(name.trim().length<1||name.length>128)return'名称需为 1–128 个字符'
  if(description.trim().length<1||description.length>2000)return'描述需为 1–2000 个字符'
  if(!SEMVER.test(semver))return'版本号需为 x.y.z 形式（如 1.0.0）'
  for(const field of SECTION_FIELDS){const body=section[field.key];if(body.trim().length<1)return`六段中「${field.label}」不能为空`;if(body.length>65536)return`六段中「${field.label}」超出 65536 字符上限`}
  return null}
 const submit=async()=>{const problem=validate();if(problem){setError(problem);return}
  setBusy(true);setError('');setNotice('')
  try{
   const base:ExpertCreatePayload={source:'local',frontmatter:{name:form.name.trim(),division:form.division,description:form.description.trim(),semver:form.semver.trim()},sixSection:{identity:form.identity,mission:form.mission,rules:form.rules,workflow:form.workflow,deliverableTemplate:form.deliverableTemplate,successMetrics:form.successMetrics},requestId:crypto.randomUUID()}
   const attempt=createMutationAttempt('expert.create',base);await bridge.create(attempt.payload,{attempt});setNotice(`专家「${form.name.trim()}」已创建`);setEditing(null);await load()
  }catch(e){setError(e instanceof Error?e.message:'专家保存失败')}finally{setBusy(false)}}
 const toggle=async(item:ExpertItem)=>{setBusy(true);setError('');try{const base={expertId:item.expertId,enabled:item.state!=='enabled'},attempt=createMutationAttempt('expert.toggle',base);const result=await bridge.toggle(base,{attempt});setNotice(`专家「${displayName(item)}」已${result.state==='enabled'?'启用':'停用'}`);await load()}catch(e){setError(e instanceof Error?e.message:'专家状态更新失败')}finally{setBusy(false)}}
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
   setMounting(null);await load()
  }catch(e){setError(e instanceof Error?e.message:'挂载操作失败')}finally{setBusy(false)}}
 const installCatalog=async(entry:CatalogEntry)=>{if(!bridge.install||marketBusy)return;setMarketBusy(entry.id);setError('');setNotice('');try{const result=await bridge.install({id:entry.id});setNotice(`已安装「${entry.displayName}」${result.usage==='chat'?'为对话技能':result.usage==='project'?'为项目专家':'（对话技能+项目专家）'}`);await Promise.all([loadCatalog(),load()]);setView('library')}catch(e){setError(e instanceof Error?e.message:'安装失败')}finally{setMarketBusy('')}}
 const marketCategories=[...new Map(catalog.map(entry=>[entry.category,0] as const)).keys()].map(id=>[id,catalog.filter(entry=>entry.category===id).length] as [string,number])
 const visibleCatalog=catalog.filter(entry=>(!marketCategory||entry.category===entry.category)&&(!marketQuery.trim()||`${entry.displayName} ${entry.description} ${entry.category} ${entry.usage}`.toLowerCase().includes(marketQuery.trim().toLowerCase()))).filter(entry=>!marketCategory||entry.category===marketCategory)

 return <main className="expert-center-page">
  <header className="expert-view-head">
   <div><div className="view-title">专家中心</div><div className="view-meta">已安装 {items.length} 名 · 市场 {catalog.length} 个可安装 · 同一专家可挂到多个项目步骤</div></div>
   <div className="view-actions">
    {onCreateInChat&&<button type="button" className="ui-btn primary" onClick={onCreateInChat}>＋ 创建专家</button>}
    <button type="button" className="ui-btn" onClick={beginCreate}>手动填写</button>
   </div>
  </header>
  <section className="skill-center-toolbar">
   <div className="skill-status-tabs" role="tablist" aria-label="专家视图">
    <button type="button" role="tab" aria-selected={view==='library'} onClick={()=>setView('library')}>我的专家</button>
    <button type="button" role="tab" aria-selected={view==='market'} onClick={()=>setView('market')}>专家市场</button>
   </div>
   {view==='library'&&<><select aria-label="条线过滤" value={divisionFilter} onChange={e=>setDivisionFilter(e.target.value as Division|'')}><option value="">全部条线</option>{(Object.keys(DIVISIONS) as Division[]).map(division=><option key={division} value={division}>{DIVISIONS[division]}</option>)}</select>
   <label className="skill-search">搜索专家<input value={query} onChange={e=>setQuery(e.target.value)} placeholder="名称"/></label>
   <button aria-label="刷新专家" onClick={()=>void load()} disabled={loading}>↻</button></>}
   {view==='market'&&<><label className="skill-search">搜索市场<input value={marketQuery} onChange={e=>setMarketQuery(e.target.value)} placeholder="名称、分类或用途"/></label>
   <button aria-label="刷新市场" onClick={()=>void loadCatalog()}>↻</button></>}
  </section>
  {error&&!editing&&<p className="skill-center-error" role="alert">{error}</p>}
  {notice&&<p role="status">{notice}</p>}
  {view==='market'?<div className="skill-market-page" aria-label="专家市场">{!catalog.length?<div className="empty"><b>市场暂不可用</b><span>当前版本未提供专家目录。</span></div>:<>
    <nav className="skill-market-cats" aria-label="市场分类"><button type="button" aria-pressed={!marketCategory} onClick={()=>setMarketCategory('')}>全部<small>{catalog.length}</small></button>{marketCategories.map(([id,count])=><button type="button" key={id} aria-pressed={marketCategory===id} onClick={()=>setMarketCategory(id)}>{id}<small>{count}</small></button>)}</nav>
    <section className="skill-market-shelf" aria-label="可安装专家"><header><b>{marketCategory||'全部角色'}</b><small>{visibleCatalog.length} 个</small></header>{visibleCatalog.length?<div className="skill-market">{visibleCatalog.map(entry=><article className={`skill-market-card ${entry.installed?'is-installed':''}`} key={entry.id}><header><span className="skill-market-glyph" aria-hidden="true">{entry.emoji||entry.displayName.slice(0,1)}</span><div><b>{entry.displayName}</b><small>{USAGE[entry.usage]} · {entry.category}</small></div>{entry.installed?<span className="skill-market-installed">已安装</span>:<button type="button" className="skill-market-add" aria-label={`安装 ${entry.displayName}`} disabled={Boolean(marketBusy)} onClick={()=>void installCatalog(entry)}>{marketBusy===entry.id?'…':'＋'}</button>}</header><p>{entry.description}</p><footer><small>v{entry.version}</small></footer></article>)}</div>:<div className="empty"><b>没有匹配的专家</b><span>换个分类或关键字再试。</span></div>}</section>
   </>}</div>
  :<section className="expert-card-list" aria-label="已安装专家">
    {loading?<p role="status">正在载入专家…</p>:visible.length?visible.map(item=><article className="expert-card" key={item.expertId}>
     <div className="expert-card-main">
      <b>{displayName(item)}</b>
      <small>{displayDivision(item)} · v{item.semver||'—'} · {STATES[item.state]??item.state} · 已挂载 {item.mountedPhaseCount} 处</small>
     </div>
     <div className="expert-card-actions">
      {item.state==='enabled'?<button type="button" className="ui-btn primary" disabled={busy} onClick={()=>void openMount(item)}>挂载</button>:null}
      {item.state==='enabled'?<button type="button" className="ui-btn" disabled={busy} onClick={()=>void toggle(item)}>停用</button>:item.state==='disabled'?<button type="button" className="ui-btn" disabled={busy} onClick={()=>void toggle(item)}>启用</button>:null}
     </div>
    </article>):<div className="empty"><b>还没有安装专家</b><span>{query?'没有匹配的专家。':'去「专家市场」安装，或点击「创建专家」用对话生成。'}</span></div>}
   </section>}
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
  <Dialog open={!!editing} wide title="创建专家向导" description="填写基础信息和六段岗位说明书。任一段为空都会拒绝保存。" onClose={()=>{if(!busy)setEditing(null)}}>
   <form className="editor-dialog" onSubmit={e=>{e.preventDefault();void submit()}}>
    <div className="form-grid expert-form-grid">
     <label>名称<input value={form.name} maxLength={128} onChange={e=>setForm({...form,name:e.target.value})}/></label>
     <label>条线<select value={form.division} onChange={e=>setForm({...form,division:e.target.value as Division})}>{(Object.keys(DIVISIONS) as Division[]).map(division=><option key={division} value={division}>{DIVISIONS[division]}</option>)}</select></label>
     <label>版本号<input value={form.semver} onChange={e=>setForm({...form,semver:e.target.value})}/></label>
     <label className="wide">描述<textarea rows={2} value={form.description} maxLength={2000} onChange={e=>setForm({...form,description:e.target.value})}/></label>
     {SECTION_FIELDS.map(field=><label key={field.key}>{field.label}<textarea rows={4} value={form[field.key]} maxLength={65536} onChange={e=>setForm({...form,[field.key]:e.target.value})}/></label>)}
    </div>
    {error&&<p className="skill-center-error" role="alert">{error}</p>}
    <div className="dialog-actions"><button type="button" disabled={busy} onClick={()=>setEditing(null)}>取消</button><button className="primary" disabled={busy}>{busy?'保存中…':'创建专家'}</button></div>
   </form>
  </Dialog>
 </main>
}
