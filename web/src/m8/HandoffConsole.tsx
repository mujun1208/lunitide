import React,{useEffect,useState}from'react'
import{contextBridge,projectBridge,sessionBridge,type ContextBridge,type ProjectBridge,type SessionBridge}from'../bridge/client'
import type{ContextHandoffInspectResult,ContextHandoffListResult,ProjectDTO,SessionDTO}from'../generated/bridge'

type Capsule=ContextHandoffListResult['items'][number]
type Pending={checkpointId:string;summary:string}

export function HandoffConsole({
 context=contextBridge,
 projects=projectBridge,
 sessions=sessionBridge,
}:{context?:ContextBridge;projects?:ProjectBridge;sessions?:SessionBridge}):React.JSX.Element{
 const[projectItems,setProjectItems]=useState<ProjectDTO[]>([]),[projectId,setProjectId]=useState('')
 const[sessionItems,setSessionItems]=useState<SessionDTO[]>([]),[sessionId,setSessionId]=useState('')
 const[capsules,setCapsules]=useState<Capsule[]>([]),[imports,setImports]=useState<Capsule[]>([])
 const[inspect,setInspect]=useState<ContextHandoffInspectResult>(),[pending,setPending]=useState<Pending>()
 const[importId,setImportId]=useState(''),[inspectId,setInspectId]=useState('')
 const[busy,setBusy]=useState(false),[error,setError]=useState(''),[notice,setNotice]=useState('')

 useEffect(()=>{projects.list().then(r=>{const visible=r.items.filter(item=>!item.name.startsWith('⁣'));setProjectItems(visible);setProjectId(current=>current||visible[0]?.id||'')}).catch(()=>{})},[projects])
 useEffect(()=>{if(!projectId){setSessionItems([]);setSessionId('');return}sessions.list({projectId}).then(r=>{setSessionItems(r.items);setSessionId(current=>r.items.some(item=>item.id===current)?current:r.items[0]?.id||'')}).catch(()=>{setSessionItems([]);setSessionId('')})},[projectId,sessions])
 useEffect(()=>{setPending(undefined);setInspect(undefined);setNotice('');if(!sessionId){setCapsules([]);setImports([]);return}void reload(sessionId)},[sessionId,context])

 const reload=async(id:string)=>{
  setError('')
  try{
   const[listed,imported]=await Promise.all([context.handoffList({sourceSessionId:id,limit:50}),context.handoffListImports({targetSessionId:id})])
   setCapsules(listed.items);setImports(imported.items)
  }catch(e){setError(e instanceof Error?e.message:'交接列表加载失败')}
 }
 const previewExport=async()=>{
  if(!sessionId||busy)return
  setBusy(true);setError('');setNotice('');setPending(undefined)
  try{
   const ready=await context.compactPreview({sessionId})
   setPending({checkpointId:ready.checkpointId,summary:ready.humanSummary||ready.summaryPreview||''})
  }catch(e){setError(e instanceof Error?e.message:'压缩预览失败')}finally{setBusy(false)}
 }
 const confirmExport=async()=>{
  if(!sessionId||!pending||busy)return
  setBusy(true);setError('');setNotice('')
  try{
   const created=await context.handoffCreate({sourceSessionId:sessionId,checkpointId:pending.checkpointId})
   setPending(undefined)
   setNotice(`已导出交接胶囊 ${created.capsuleId}`)
   await reload(sessionId)
  }catch(e){setError(e instanceof Error?e.message:'导出交接失败')}finally{setBusy(false)}
 }
 const doImport=async()=>{
  if(!sessionId||!importId.trim()||busy)return
  setBusy(true);setError('');setNotice('')
  try{
   const r=await context.handoffImport({capsuleId:importId.trim(),targetSessionId:sessionId})
   setNotice(r.alreadyImported?'该胶囊已导入（幂等）':`已导入胶囊 · 摘要有效：${r.digestValid?'是':'否'}`)
   setImportId('')
   await reload(sessionId)
  }catch(e){setError(e instanceof Error?e.message:'导入失败')}finally{setBusy(false)}
 }
 const doInspect=async()=>{
  if(!inspectId.trim()||busy)return
  setBusy(true);setError('')
  try{setInspect(await context.handoffInspect({capsuleId:inspectId.trim()}))}
  catch(e){setError(e instanceof Error?e.message:'查看失败')}finally{setBusy(false)}
 }
 const doRevoke=async(id:string)=>{
  if(busy)return
  setBusy(true);setError('');setNotice('')
  try{await context.handoffRevoke({capsuleId:id});setNotice('已撤销胶囊');if(sessionId)await reload(sessionId)}
  catch(e){setError(e instanceof Error?e.message:'撤销失败')}finally{setBusy(false)}
 }

 return <div className="org-section" style={{marginTop:6}}>
  <p className="view-meta">导出走压缩预览 → 二次确认创建胶囊；不会把压缩检查点激活到源会话。设备级接收人与有效期合同仍无独立 RPC。</p>
  <div className="org-form" style={{marginBottom:10,flexWrap:'wrap'}}>
   <label className="view-meta" htmlFor="handoff-project">项目</label>
   <select id="handoff-project" value={projectId} onChange={e=>setProjectId(e.target.value)}>{projectItems.length?projectItems.map(p=><option key={p.id} value={p.id}>{p.name}</option>):<option value="">（暂无项目）</option>}</select>
   <label className="view-meta" htmlFor="handoff-session">会话</label>
   <select id="handoff-session" value={sessionId} onChange={e=>setSessionId(e.target.value)} disabled={!sessionItems.length}>{sessionItems.length?sessionItems.map(s=><option key={s.id} value={s.id}>{s.title}</option>):<option value="">（暂无会话）</option>}</select>
   <button className="primary" disabled={busy||!sessionId} onClick={()=>void previewExport()}>{busy?'处理中…':'生成压缩预览'}</button>
  </div>
  {notice&&<p className="org-notice" role="status">{notice}</p>}
  {error&&<p className="skill-center-error" role="alert">{error}</p>}
  {pending&&<article className="screen-route"><b>压缩预览</b><p style={{whiteSpace:'pre-wrap'}}>{pending.summary||'（无摘要文本）'}</p><div className="org-form"><button className="primary" disabled={busy} onClick={()=>void confirmExport()}>确认导出胶囊</button><button disabled={busy} onClick={()=>setPending(undefined)}>取消</button></div></article>}
  {!sessionId?<div className="gate-box"><b>需要会话</b><span>选择一个项目下的会话后，可以导出、导入或撤销交接胶囊。</span></div>:<>
   <div className="org-section"><h4>本会话导出的胶囊（{capsules.length}）</h4><div className="org-card">{capsules.length?capsules.map(item=><div className="org-row static" key={item.capsuleId}><span><b>{item.capsuleId}</b><small>{item.status} · {item.createdAt.slice(0,19).replace('T',' ')}</small></span><code><button type="button" disabled={busy} onClick={()=>void doRevoke(item.capsuleId)}>撤销</button></code></div>):<div className="empty"><b>还没有导出胶囊</b><span>先生成压缩预览，确认后再导出可导入到其它会话的交接包。</span></div>}</div></div>
   <div className="org-section"><h4>导入到本会话（{imports.length}）</h4>
    <form className="org-form" onSubmit={e=>{e.preventDefault();void doImport()}}><input value={importId} onChange={e=>setImportId(e.target.value)} placeholder="胶囊 ID" aria-label="导入胶囊 ID"/><button disabled={busy||!importId.trim()}>导入</button></form>
    <div className="org-card" style={{marginTop:8}}>{imports.length?imports.map(item=><div className="org-row static" key={item.capsuleId}><span><b>{item.capsuleId}</b><small>{item.status}</small></span><code/></div>):<div className="empty"><b>尚未导入</b><span>把其它会话导出的胶囊 ID 粘贴到上方。</span></div>}</div>
   </div>
   <div className="org-section"><h4>查看胶囊</h4>
    <form className="org-form" onSubmit={e=>{e.preventDefault();void doInspect()}}><input value={inspectId} onChange={e=>setInspectId(e.target.value)} placeholder="胶囊 ID" aria-label="查看胶囊 ID"/><button disabled={busy||!inspectId.trim()}>查看</button></form>
    {inspect&&<article className="screen-route" style={{marginTop:8}}><b>{inspect.status}</b><p>来源会话 {inspect.sourceSessionId}{inspect.sourceDeleted?' · 源已删除':''}{inspect.humanSummary?` · ${inspect.humanSummary}`:''}</p></article>}
   </div>
  </>}
 </div>
}
