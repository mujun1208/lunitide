import React,{useState}from'react'
import{diagnosticsBridge,memoryOpsBridge,type DiagnosticsBridge,type MemoryOpsBridge}from'../bridge/client'

export function PrivacyConsole({
 diagnostics=diagnosticsBridge,
 memory=memoryOpsBridge,
}:{diagnostics?:DiagnosticsBridge;memory?:MemoryOpsBridge}):React.JSX.Element{
 const[busy,setBusy]=useState(false),[error,setError]=useState(''),[notice,setNotice]=useState('')
 const[includeLogs,setIncludeLogs]=useState(false),[redactPaths,setRedactPaths]=useState(true)
 const[exportPath,setExportPath]=useState(''),[confirmPurge,setConfirmPurge]=useState(false)

 const exportPack=async()=>{
  setBusy(true);setError('');setNotice('')
  try{
   const r=await diagnostics.exportDiagnostics({includeLogs,redactPaths})
   setExportPath(r.path)
   setNotice(`诊断包已导出：${r.path}`)
  }catch(e){setError(e instanceof Error?e.message:'诊断包导出失败')}finally{setBusy(false)}
 }
 const exportMemory=async()=>{
  setBusy(true);setError('');setNotice('')
  try{
   const r=await memory.export({})
   const blob=new Blob([JSON.stringify(r,null,2)],{type:'application/json'})
   const url=URL.createObjectURL(blob)
   const a=document.createElement('a')
   a.href=url;a.download=`lunitide-memory-export-${Date.now()}.json`;a.click()
   URL.revokeObjectURL(url)
   setNotice(`已导出记忆快照：事实 ${r.facts.length} · 候选 ${r.candidates.length} · 痕迹 ${r.traces.length}`)
  }catch(e){setError(e instanceof Error?e.message:'记忆导出失败')}finally{setBusy(false)}
 }
 const purge=async()=>{
  setBusy(true);setError('');setNotice('')
  try{
   const r=await memory.purge({})
   setConfirmPurge(false)
   setNotice(`已清除本机记忆：事实 ${r.factsTombstoned} · 候选 ${r.candidates} · 记忆 ${r.memories}`)
  }catch(e){setError(e instanceof Error?e.message:'记忆清除失败')}finally{setBusy(false)}
 }

 return <div className="org-section" style={{marginTop:6}}>
  <p className="view-meta">本机导出与清除已接通。设备信任、吊销回执和跨设备删除证明仍无公开 RPC，不在本页伪装。</p>
  {notice&&<p className="org-notice" role="status">{notice}</p>}
  {error&&<p className="skill-center-error" role="alert">{error}</p>}
  <div className="org-section"><h4>诊断包</h4>
   <div className="org-card org-bound">
    <div><b>导出脱敏诊断包</b><small>供排障使用；可选择是否带日志。</small></div>
    <div className="org-bound-actions">
     <label className="view-meta"><input type="checkbox" checked={includeLogs} onChange={e=>setIncludeLogs(e.target.checked)}/> 包含日志</label>
     <label className="view-meta"><input type="checkbox" checked={redactPaths} onChange={e=>setRedactPaths(e.target.checked)}/> 脱敏路径</label>
     <button className="primary" disabled={busy} onClick={()=>void exportPack()}>导出诊断包</button>
    </div>
   </div>
   {exportPath&&<p className="view-meta">路径：<code>{exportPath}</code></p>}
  </div>
  <div className="org-section"><h4>记忆</h4>
   <div className="org-card org-bound">
    <div><b>导出本机记忆快照</b><small>下载 JSON：事实、候选、痕迹与设置。</small></div>
    <div className="org-bound-actions"><button disabled={busy} onClick={()=>void exportMemory()}>导出记忆 JSON</button></div>
   </div>
   <div className="org-card org-bound" style={{marginTop:8}}>
    <div><b>清除本机记忆</b><small>tombstone 事实与候选，不可恢复。</small></div>
    <div className="org-bound-actions">{confirmPurge?<><button className="danger" disabled={busy} onClick={()=>void purge()}>确认清除</button><button disabled={busy} onClick={()=>setConfirmPurge(false)}>取消</button></>:<button disabled={busy} onClick={()=>setConfirmPurge(true)}>清除记忆…</button>}</div>
   </div>
  </div>
 </div>
}
