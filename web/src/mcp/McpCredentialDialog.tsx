import React,{useRef,useState}from'react'
import type{McpCredentialSetPayload,McpCredentialSetResult,McpListResult}from'../generated/bridge'
import{Dialog}from'../ui/Dialog'

type Endpoint=McpListResult['endpoints'][number]
export function McpCredentialDialog({endpoint,save,onClose,onSaved}:{endpoint:Endpoint;save:(payload:McpCredentialSetPayload)=>Promise<McpCredentialSetResult>;onClose:()=>void;onSaved:()=>void}):React.JSX.Element{
 const[value,setValue]=useState(''),[env,setEnv]=useState(''),[busy,setBusy]=useState(false),[error,setError]=useState('')
 const attempt=useRef<{requestId:string;env:string;remove:boolean}|null>(null)
 const submit=async(remove:boolean)=>{
  if(endpoint.securityVersion===undefined){setError('请刷新 MCP 清单后重试');return}
  if(endpoint.transport==='stdio'&&!/^[A-Z][A-Z0-9_]{0,63}$/.test(env)){setError('请填写凭据环境变量名称，例如 API_TOKEN');return}
  if(!attempt.current||attempt.current.env!==env||attempt.current.remove!==remove)attempt.current={requestId:crypto.randomUUID(),env,remove}
  const credential=value;setValue('');setBusy(true);setError('')
  try{await save({endpointId:endpoint.endpointId,expectedVersion:endpoint.securityVersion,requestId:attempt.current.requestId,...(env?{env}:{}),...(remove?{remove:true as const}:{credential})});onSaved();onClose()}
  catch(e){setError(`${e instanceof Error?e.message:'凭据保存失败'}。如结果待确认，请重新输入相同凭据重试。`)}finally{setBusy(false)}
 }
 return <Dialog open title="MCP 凭据" description="保存或撤销前，桌面会显示目标确认。凭据不会显示在清单或发送给对话模型。" onClose={()=>{if(!busy)onClose()}}>
  <form onSubmit={e=>{e.preventDefault();void submit(false)}}>
   <p>{endpoint.displayName||endpoint.endpointId} · {endpoint.credentialConfigured?'已配置凭据':'未配置凭据'}</p>
   {endpoint.transport==='stdio'&&<label>环境变量名称<input aria-label="凭据环境变量" value={env} disabled={busy} onChange={e=>setEnv(e.target.value.trim())} placeholder="API_TOKEN"/></label>}
   <label>{endpoint.transport==='https'?'Bearer 凭据':'环境变量凭据'}<input aria-label="MCP 凭据值" type="password" autoComplete="off" value={value} disabled={busy} onChange={e=>setValue(e.target.value)} maxLength={16384}/></label>
   {error&&<p role="alert">{error}</p>}
   <div className="dialog-actions"><button type="button" disabled={busy} onClick={onClose}>取消</button><button type="button" disabled={busy||endpoint.securityVersion===undefined} onClick={()=>void submit(true)}>撤销凭据</button><button className="primary" disabled={busy||!value||endpoint.securityVersion===undefined}>{busy?'等待确认…':'保存凭据'}</button></div>
  </form>
 </Dialog>
}
