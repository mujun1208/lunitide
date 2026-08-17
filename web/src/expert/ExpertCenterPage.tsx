import React,{useCallback,useEffect,useState}from'react'
import{createMutationAttempt,expertBridge,projectBridge,type ExpertBridge,type ProjectBridge}from'../bridge/client'
import type{ExpertCreatePayload,ExpertDetailResult,ExpertListResult,ExpertMountingGetResult,ExpertScenarioListResult,ProjectDTO}from'../generated/bridge'

type ExpertItem=ExpertListResult['experts'][number]
type ScenarioItem=ExpertScenarioListResult['items'][number]
type PhaseKey=import('../generated/bridge').ExpertMountPayload['phaseKey']
type Division=import('../generated/bridge').ExpertCreatePayload['frontmatter']['division']
type ExpertState=Extract<ExpertItem['state'],'enabled'|'disabled'|'archived'>
interface SixSectionBody{identity:string;mission:string;rules:string;workflow:string;deliverableTemplate:string;successMetrics:string}

const DIVISIONS:Record<Division,string>={engineering:'工程',design:'设计',product:'产品','project-management':'项目管理',testing:'测试',security:'安全',operations:'运维',data:'数据'}
const STATES:Record<ExpertState,string>={enabled:'已启用',disabled:'已停用',archived:'已归档'}
const SOURCES:Record<ExpertItem['source'],string>={pack:'专家包',local:'本地',builtin:'内置'}
const PHASES:Array<{key:PhaseKey;label:string}>=[
 {key:'INITIATION_BOUNDARY',label:'1 立项边界'},{key:'RESEARCH_EVIDENCE',label:'2 调研证据'},{key:'REQUIREMENT_DEFINITION',label:'3 需求定义'},
 {key:'SOLUTION_EXPERIENCE',label:'4 方案体验'},{key:'ARCHITECTURE_PLAN',label:'5 架构计划'},{key:'DEVELOPMENT_CHANGE',label:'6 开发变更'},
 {key:'VERIFICATION_ACCEPTANCE',label:'7 验证验收'},{key:'RELEASE_DELIVERY',label:'8 发布交付'},{key:'OPERATIONS_RETROSPECTIVE',label:'9 运维复盘'}]
const SECTION_FIELDS:Array<{key:keyof SixSectionBody;label:string}>=[
 {key:'identity',label:'身份 identity'},{key:'mission',label:'使命 mission'},{key:'rules',label:'规则 rules'},
 {key:'workflow',label:'工作流 workflow'},{key:'deliverableTemplate',label:'交付模板 deliverableTemplate'},{key:'successMetrics',label:'成功度量 successMetrics'}]
const SEMVER=/^\d+\.\d+\.\d+$/
const archiveToken=async(expertId:string):Promise<string>=>{
 const bytes=new TextEncoder().encode(`expert.archive|${expertId}`)
 const digest=await globalThis.crypto.subtle.digest('SHA-256',bytes)
 return Array.from(new Uint8Array(digest)).map(byte=>byte.toString(16).padStart(2,'0')).join('')
}

export function ExpertCenterPage({bridge=expertBridge,projects=projectBridge}:{bridge?:ExpertBridge;projects?:ProjectBridge}):React.JSX.Element{
 const[items,setItems]=useState<ExpertItem[]>([]),[selectedId,setSelectedId]=useState(''),[divisionFilter,setDivisionFilter]=useState<Division|''>(''),[stateFilter,setStateFilter]=useState<ExpertState|''>(''),[query,setQuery]=useState('')
 const[loading,setLoading]=useState(true),[busy,setBusy]=useState(false),[error,setError]=useState(''),[notice,setNotice]=useState('')
 const[detail,setDetail]=useState<ExpertDetailResult>()
 const[editing,setEditing]=useState<'create'|'update'|null>(null)
 const[form,setForm]=useState({name:'',division:'engineering' as Division,description:'',semver:'1.0.0',identity:'',mission:'',rules:'',workflow:'',deliverableTemplate:'',successMetrics:''})
 const[changeNote,setChangeNote]=useState('')
 const[archiveConfirm,setArchiveConfirm]=useState<{token:string}|null>(null)
 const[projectItems,setProjectItems]=useState<ProjectDTO[]>([]),[mountProjectId,setMountProjectId]=useState('')
 const[matrix,setMatrix]=useState<ExpertMountingGetResult>()
 const[scenarios,setScenarios]=useState<ScenarioItem[]>([]),[scenarioState,setScenarioState]=useState<'active'|'archived'>('active')
 const[scenarioForm,setScenarioForm]=useState({title:'',summary:'',phaseKey:'DEVELOPMENT_CHANGE' as PhaseKey,scenarioJson:'{\n  "steps": []\n}'})

 const load=useCallback(async()=>{setLoading(true);setError('');try{const result=await bridge.list({});setItems(result.experts);setSelectedId(current=>result.experts.some(item=>item.expertId===current)?current:(result.experts[0]?.expertId??''))}catch(e){setError(e instanceof Error?e.message:'专家清单加载失败')}finally{setLoading(false)}},[bridge])
 useEffect(()=>{void load()},[load])
 useEffect(()=>{try{projects.list().then(result=>{setProjectItems(result.items.filter(item=>!item.name.startsWith('⁣')));setMountProjectId(current=>current||result.items.find(item=>!item.name.startsWith('⁣'))?.id||'')}).catch(()=>{})}catch{/* bridge unavailable outside WebView2: mounting matrix stays hidden */}},[projects])
 const loadDetail=useCallback(async(expertId:string)=>{if(!expertId){setDetail(undefined);return}try{setDetail(await bridge.detail({expertId}))}catch(e){setError(e instanceof Error?e.message:'专家详情加载失败')}},[bridge])
 useEffect(()=>{void loadDetail(selectedId)},[selectedId,loadDetail])
 useEffect(()=>{if(!mountProjectId){setMatrix(undefined);return}let alive=true;bridge.mountingGet({projectId:mountProjectId}).then(result=>{if(alive)setMatrix(result)}).catch(()=>{if(alive)setMatrix(undefined)});return()=>{alive=false}},[bridge,mountProjectId])

 const visible=items.filter(item=>(!divisionFilter||item.division===divisionFilter)&&(!stateFilter||item.state===stateFilter)&&(!query.trim()||item.name.toLowerCase().includes(query.trim().toLowerCase())))
 const selected=items.find(item=>item.expertId===selectedId)??visible[0]
 const sectionOf=(value:unknown):SixSectionBody=>{const raw=(value??{})as Partial<Record<keyof SixSectionBody,unknown>>;return{identity:String(raw.identity??''),mission:String(raw.mission??''),rules:String(raw.rules??''),workflow:String(raw.workflow??''),deliverableTemplate:String(raw.deliverableTemplate??''),successMetrics:String(raw.successMetrics??'')}}
 const beginCreate=()=>{setEditing('create');setArchiveConfirm(null);setForm({name:'',division:'engineering',description:'',semver:'1.0.0',identity:'',mission:'',rules:'',workflow:'',deliverableTemplate:'',successMetrics:''});setChangeNote('')}
 const beginUpdate=()=>{if(!detail)return;setEditing('update');setChangeNote('');const section=sectionOf(detail.sixSection);setForm({name:String((detail.expert as{ name?: unknown }).name??selected?.name??''),division:(String((detail.expert as{ division?: unknown }).division)||'engineering') as Division,description:String((detail.expert as{ description?: unknown }).description??''),semver:String((detail.expert as{ semver?: unknown }).semver||selected?.semver||'1.0.0'),...section})}
 const validate=():string|null=>{const{name,description,semver,...section}=form
  if(name.trim().length<1||name.length>128)return'名称需为 1–128 个字符'
  if(description.trim().length<1||description.length>2000)return'描述需为 1–2000 个字符'
  if(!SEMVER.test(semver))return'版本号需为 x.y.z 形式（如 1.0.0）'
  for(const field of SECTION_FIELDS){const body=section[field.key];if(body.trim().length<1)return`六段中「${field.label}」不能为空`;if(body.length>65536)return`六段中「${field.label}」超出 65536 字符上限`}
  return null}
 const submit=async()=>{if(!selected&&editing!=='create')return;const problem=validate();if(problem){setError(problem);return}
  setBusy(true);setError('');setNotice('')
  try{
   if(editing==='create'){const base:ExpertCreatePayload={source:'local',frontmatter:{name:form.name.trim(),division:form.division,description:form.description.trim(),semver:form.semver.trim()},sixSection:{identity:form.identity,mission:form.mission,rules:form.rules,workflow:form.workflow,deliverableTemplate:form.deliverableTemplate,successMetrics:form.successMetrics},requestId:crypto.randomUUID()};const attempt=createMutationAttempt('expert.create',base);await bridge.create(attempt.payload,{attempt});setNotice(`专家「${form.name.trim()}」已创建`)}
   else if(editing==='update'&&detail){const versions=detail.versions;const base={expertId:selected!.expertId,expectedVersionId:versions[0]?.versionId??'',sixSection:{identity:form.identity,mission:form.mission,rules:form.rules,workflow:form.workflow,deliverableTemplate:form.deliverableTemplate,successMetrics:form.successMetrics},changeNote:changeNote.trim()||'更新六段'},attempt=createMutationAttempt('expert.update',base);await bridge.update(base,{attempt});setNotice(`专家「${selected!.name}」已发布新版本 ${form.semver.trim()}`)}
   setEditing(null);await load();if(selected)await loadDetail(selected.expertId)}
  catch(e){setError(e instanceof Error?e.message:'专家保存失败')}finally{setBusy(false)}}
 const toggle=async()=>{if(!selected)return;setBusy(true);setError('');try{const base={expertId:selected.expertId,enabled:selected.state!=='enabled'},attempt=createMutationAttempt('expert.toggle',base);const result=await bridge.toggle(base,{attempt});setNotice(`专家「${selected.name}」已${result.state==='enabled'?'启用':'停用'}${result.affectedMountings?`，影响 ${result.affectedMountings} 处挂载`:''}`);await load()}catch(e){setError(e instanceof Error?e.message:'专家状态更新失败')}finally{setBusy(false)}}
 const requestArchive=async()=>{if(!selected)return;setBusy(true);setError('');try{setArchiveConfirm({token:await archiveToken(selected.expertId)})}catch{setError('当前环境无法计算归档确认令牌')}finally{setBusy(false)}}
 const archive=async()=>{if(!selected||!archiveConfirm)return;setBusy(true);setError('');try{const base={expertId:selected.expertId,confirmToken:archiveConfirm.token},attempt=createMutationAttempt('expert.archive',base);await bridge.archive(base,{attempt});setNotice(`专家「${selected.name}」已归档（终态，不可恢复）`);setArchiveConfirm(null);await load()}catch(e){setError(e instanceof Error?e.message:'专家归档失败')}finally{setBusy(false)}}
 const mount=async(phaseKey:PhaseKey,action:'mount'|'unmount')=>{if(!selected||!mountProjectId)return;setBusy(true);setError('');try{const base={projectId:mountProjectId,phaseKey,expertId:selected.expertId,action},attempt=createMutationAttempt('expert.mount',base);const result=await bridge.mount(base,{attempt});setNotice(`已${result.state==='mounted'?'挂载':'卸载'}「${selected.name}」→ ${PHASES.find(phase=>phase.key===phaseKey)?.label}`);await load();setMatrix(await bridge.mountingGet({projectId:mountProjectId}))}catch(e){setError(e instanceof Error?e.message:'挂载操作失败')}finally{setBusy(false)}}
 const mountedIds=(phaseKey:PhaseKey)=>matrix?.matrix.find(row=>row.phaseKey===phaseKey)?.mountings.filter(m=>m.state==='mounted').map(m=>m.expertId)??[]

 const loadScenarios=useCallback(async(expertId:string,state:'active'|'archived')=>{if(!expertId){setScenarios([]);return}try{const result=await bridge.scenarioList({expertId,state});setScenarios(result.items)}catch{setScenarios([])}},[bridge])
 useEffect(()=>{void loadScenarios(selected?.expertId??'',scenarioState)},[selected?.expertId,scenarioState,loadScenarios])
 const createScenario=async()=>{if(!selected)return
  let scenario:object;try{const parsed:unknown=JSON.parse(scenarioForm.scenarioJson);if(!parsed||typeof parsed!=='object'||Array.isArray(parsed))throw new Error('not object');scenario=parsed as object}catch{setError('场景 JSON 需为合法对象');return}
  if(scenarioForm.title.trim().length<1||scenarioForm.title.length>128){setError('场景卡标题需为 1–128 个字符');return}
  if(scenarioForm.summary.trim().length<1||scenarioForm.summary.length>2048){setError('场景卡摘要需为 1–2048 个字符');return}
  setBusy(true);setError('')
  try{const base={expertId:selected.expertId,title:scenarioForm.title.trim(),summary:scenarioForm.summary.trim(),phaseKey:scenarioForm.phaseKey,scenario},attempt=createMutationAttempt('expert.scenario.create',base);const result=await bridge.scenarioCreate(base,{attempt});setNotice(`场景卡「${result.title}」已创建（摘要 ${result.digest.slice(0,12)}…）`);setScenarioForm({...scenarioForm,title:'',summary:''});setScenarioState('active');await loadScenarios(selected.expertId,'active')}
  catch(e){setError(e instanceof Error?e.message:'场景卡创建失败')}finally{setBusy(false)}}
 const deleteScenario=async(scenarioCardId:string)=>{if(!selected)return;setBusy(true);setError('');try{const base={scenarioCardId},attempt=createMutationAttempt('expert.scenario.delete',base);await bridge.scenarioDelete(base,{attempt});setNotice('场景卡已归档');await loadScenarios(selected.expertId,scenarioState)}catch(e){setError(e instanceof Error?e.message:'场景卡归档失败')}finally{setBusy(false)}}

 return <main className="skill-center"><header className="skill-center-header"><div><h1>专家中心</h1><p>{items.length} 位专家 · {items.filter(item=>item.state==='enabled').length} 位启用 · 挂载 {items.reduce((sum,item)=>sum+item.mountedPhaseCount,0)} 处</p><small>六段式专家画像与九阶段挂载矩阵（M8 FR-19）。</small></div><button className="primary skill-chat-create" aria-label="新建本地专家" onClick={beginCreate}>＋ 新建专家</button></header>
  <section className="skill-center-toolbar"><div className="skill-status-tabs" role="tablist" aria-label="专家条线"><button type="button" role="tab" aria-selected={divisionFilter===''} onClick={()=>setDivisionFilter('')}>全部</button>{(Object.keys(DIVISIONS) as Division[]).map(division=><button type="button" role="tab" aria-selected={divisionFilter===division} key={division} onClick={()=>setDivisionFilter(division)}>{DIVISIONS[division]}</button>)}</div><label className="skill-search">搜索专家<input value={query} onChange={e=>setQuery(e.target.value)} placeholder="名称"/></label><select aria-label="状态过滤" value={stateFilter} onChange={e=>setStateFilter(e.target.value as ExpertState|'')}><option value="">全部状态</option>{(Object.keys(STATES) as ExpertState[]).map(state=><option key={state} value={state}>{STATES[state]}</option>)}</select><button aria-label="刷新专家" onClick={()=>void load()} disabled={loading}>↻</button></section>
  {error&&<p className="skill-center-error" role="alert">{error}</p>}
  {notice&&<p role="status">{notice}</p>}
  <div className="skill-center-layout"><section className="skill-table" aria-label="专家列表"><div className="skill-table-head"><span>专家</span><span>条线</span><span>版本</span><span>状态</span></div>
   {loading?<p role="status">正在载入专家…</p>:visible.length?visible.map(item=><button type="button" className={`skill-row ${selected?.expertId===item.expertId?'active':''}`} key={item.expertId} onClick={()=>{setSelectedId(item.expertId);setEditing(null);setArchiveConfirm(null)}}><span><b>{item.name}</b><small>{SOURCES[item.source]} · {item.semver}</small></span><code>{DIVISIONS[item.division]}</code><code>v{item.versionCount}</code><i className={`skill-status status-${item.state==='enabled'?'published':item.state==='disabled'?'disabled':'deprecated'}`}>{STATES[item.state]}</i></button>):<div className="empty"><b>暂无专家</b><span>{query?'没有匹配的专家。':'点击「新建专家」创建本地六段式专家。'}</span></div>}</section>
   <aside className="skill-detail" aria-label="专家详情">
    {editing?(
     <form onSubmit={e=>{e.preventDefault();void submit()}}><div className="skill-detail-title"><h2>{editing==='create'?'新建专家':`编辑 ${selected?.name??''}`}</h2><button type="button" onClick={()=>setEditing(null)}>取消</button></div>
      <label>名称<input value={form.name} maxLength={128} onChange={e=>setForm({...form,name:e.target.value})} disabled={editing==='update'}/></label>
      <label>条线<select value={form.division} onChange={e=>setForm({...form,division:e.target.value as Division})} disabled={editing==='update'}>{(Object.keys(DIVISIONS) as Division[]).map(division=><option key={division} value={division}>{DIVISIONS[division]}</option>)}</select></label>
      <label>描述<textarea value={form.description} maxLength={2000} onChange={e=>setForm({...form,description:e.target.value})} disabled={editing==='update'}/></label>
      <label>版本号（仅创建时）<input value={form.semver} onChange={e=>setForm({...form,semver:e.target.value})} disabled={editing==='update'}/></label>
      {SECTION_FIELDS.map(field=><label key={field.key}>{field.label}<textarea className="skill-manifest-editor" value={form[field.key]} maxLength={65536} onChange={e=>setForm({...form,[field.key]:e.target.value})}/></label>)}
      {editing==='update'&&<label>变更说明<input value={changeNote} maxLength={500} onChange={e=>setChangeNote(e.target.value)} placeholder="本次六段修订的要点"/></label>}
      <button className="primary" disabled={busy}>{busy?'保存中…':editing==='create'?'创建专家':'发布新版本'}</button></form>
    ):selected?(
     <><div className="skill-detail-title"><div><h2>{selected.name}</h2><code>{SOURCES[selected.source]} · {selected.semver} · {selected.versionCount} 个版本</code></div><span className={`skill-status status-${selected.state==='enabled'?'published':selected.state==='disabled'?'disabled':'deprecated'}`}>{STATES[selected.state]}</span></div>
      <p>{String((detail?.expert as{description?:unknown})?.description??'')}</p>
      <h3>六段画像（摘要 {detail?.versions[0]?.sixSectionDigest.slice(0,12)??''}…）</h3>
      {detail?SECTION_FIELDS.map(field=><details key={field.key}><summary>{field.label}</summary><pre>{sectionOf(detail.sixSection)[field.key]||'（空）'}</pre></details>):<p role="status">正在载入详情…</p>}
      <h3>版本历史</h3>
      {detail?.versions.length?detail.versions.slice(0,5).map(version=><p key={version.versionId}><code>v{version.semver}</code> {version.changeNote||'无变更说明'} <small>{version.createdAt}</small></p>):<p>暂无版本记录</p>}
      <h3>九阶段挂载</h3>
      <label>项目<select value={mountProjectId} onChange={e=>setMountProjectId(e.target.value)}>{projectItems.length?projectItems.map(project=><option key={project.id} value={project.id}>{project.name}</option>):<option value="">（暂无项目）</option>}</select></label>
      {matrix?PHASES.map(phase=>{const ids=mountedIds(phase.key),mounted=ids.includes(selected.expertId);return<div className="skill-path" key={phase.key}><b>{phase.label}</b><span>{ids.length?`已挂 ${ids.length} 位`:'默认'}</span>{selected.state==='enabled'&&(mounted?<button type="button" disabled={busy||!mountProjectId} onClick={()=>void mount(phase.key,'unmount')}>卸载</button>:<button type="button" disabled={busy||!mountProjectId} onClick={()=>void mount(phase.key,'mount')}>挂载此专家</button>)}</div>}):<p role="status">正在载入挂载矩阵…</p>}
      <h3>场景卡</h3>
      <div className="skill-status-tabs" role="tablist" aria-label="场景卡状态"><button type="button" role="tab" aria-selected={scenarioState==='active'} onClick={()=>setScenarioState('active')}>活跃</button><button type="button" role="tab" aria-selected={scenarioState==='archived'} onClick={()=>setScenarioState('archived')}>已归档</button></div>
      {scenarios.length?scenarios.map(card=><details key={card.scenarioCardId}><summary><b>{card.title}</b> <small>{PHASES.find(phase=>phase.key===card.phaseKey)?.label}</small></summary><p>{card.summary}</p><p><small>{card.createdAt} · {card.updatedAt}</small></p>{card.state==='active'&&<div className="skill-detail-actions"><button disabled={busy} onClick={()=>void deleteScenario(card.scenarioCardId)}>归档此场景卡</button></div>}</details>):<p>{scenarioState==='active'?'暂无活跃场景卡':'暂无已归档场景卡'}</p>}
      {selected.source==='local'&&selected.state!=='archived'&&<form onSubmit={e=>{e.preventDefault();void createScenario()}} aria-label="新建场景卡"><label>标题<input value={scenarioForm.title} maxLength={128} onChange={e=>setScenarioForm({...scenarioForm,title:e.target.value})} placeholder="如：数据库慢查询处置"/></label><label>摘要<textarea value={scenarioForm.summary} maxLength={2048} onChange={e=>setScenarioForm({...scenarioForm,summary:e.target.value})} placeholder="该场景要解决的问题与产出"/></label><label>适用阶段<select value={scenarioForm.phaseKey} onChange={e=>setScenarioForm({...scenarioForm,phaseKey:e.target.value as PhaseKey})}>{PHASES.map(phase=><option key={phase.key} value={phase.key}>{phase.label}</option>)}</select></label><label>场景 JSON<textarea className="skill-manifest-editor" value={scenarioForm.scenarioJson} maxLength={65536} onChange={e=>setScenarioForm({...scenarioForm,scenarioJson:e.target.value})}/></label><button className="primary" disabled={busy}>{busy?'保存中…':'创建场景卡'}</button></form>}
      <div className="skill-detail-actions">
       <button onClick={beginUpdate} disabled={busy||selected.source!=='local'||selected.state==='archived'}>编辑新版本</button>
       {selected.state==='enabled'?<button onClick={()=>void toggle()} disabled={busy}>停用</button>:selected.state==='disabled'?<button onClick={()=>void toggle()} disabled={busy}>启用</button>:null}
       {selected.state!=='archived'&&<button className="danger" onClick={()=>void requestArchive()} disabled={busy}>归档…</button>}
      </div>
      {archiveConfirm&&<div className="skill-center-error" role="alert"><p>归档为终态且不可恢复。确认令牌：<code>{archiveConfirm.token.slice(0,16)}…</code></p><div className="skill-detail-actions"><button className="danger" onClick={()=>void archive()} disabled={busy}>确认归档</button><button onClick={()=>setArchiveConfirm(null)}>取消</button></div></div>}
     </>
    ):<div className="empty"><b>选择专家查看详情</b></div>}
   </aside></div></main>
}
