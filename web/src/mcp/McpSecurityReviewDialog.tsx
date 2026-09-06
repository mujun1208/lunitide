import React,{useEffect,useState}from'react'
import type{McpSecurityReviewPayload,McpSecurityReviewResult,McpListResult}from'../generated/bridge'
import{Dialog}from'../ui/Dialog'

export function McpSecurityReviewDialog({endpoint,review,onClose,onSaved}:{endpoint:McpListResult['endpoints'][number];review:(p:McpSecurityReviewPayload)=>Promise<McpSecurityReviewResult>;onClose:()=>void;onSaved:()=>void}):React.JSX.Element{
 const[observed,setObserved]=useState<McpSecurityReviewResult|null>(null),[confirmed,setConfirmed]=useState(false),[error,setError]=useState(''),[busy,setBusy]=useState(true)
 useEffect(()=>{let active=true;if(endpoint.securityVersion===undefined){setError('请刷新清单后重试');setBusy(false);return}
  void review({endpointId:endpoint.endpointId,expectedVersion:endpoint.securityVersion,action:'inspect'}).then(result=>{if(active)setObserved(result)}).catch(e=>{if(active)setError(e instanceof Error?e.message:'无法读取服务器定义')}).finally(()=>{if(active)setBusy(false)})
  return()=>{active=false}
 },[endpoint,review])
 const accept=async()=>{if(!observed||!confirmed||endpoint.securityVersion===undefined)return;setBusy(true);setError('');try{await review({endpointId:endpoint.endpointId,expectedVersion:endpoint.securityVersion,action:'accept',observedDigest:observed.observedDigest,confirmed:true});onSaved();onClose()}catch(e){setError(e instanceof Error?e.message:'服务器复核失败')}finally{setBusy(false)}}
 return <Dialog open title="复核 MCP 变更" description="确认后保存当前服务器身份和工具定义。以后再次变化仍会暂停调用。" onClose={()=>{if(!busy)onClose()}}>
  {observed&&<><p>{endpoint.displayName||endpoint.endpointId}</p><p>工具（{observed.tools.length}）：{observed.tools.join('、')}</p><p>已锁定启动参数：{observed.lockedArgs.join(' ')||'远程 HTTPS'}</p><details><summary>校验摘要</summary><p>{observed.identityDigest}</p><p>{observed.observedDigest}</p></details><label><input type="checkbox" checked={confirmed} onChange={e=>setConfirmed(e.target.checked)} disabled={busy}/>我信任该服务器及以上工具</label></>}
  {error&&<p role="alert">{error}</p>}<div className="dialog-actions"><button disabled={busy} onClick={onClose}>取消</button><button className="primary" disabled={busy||!observed||!confirmed} onClick={()=>void accept()}>{busy?'复核中…':'确认变更'}</button></div>
 </Dialog>
}
