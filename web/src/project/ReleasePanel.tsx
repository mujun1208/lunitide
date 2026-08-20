import React,{useState}from'react'
import{BridgeClientError,createMutationAttempt,releaseBridge as defaultReleaseBridge,type ReleaseBridge}from'../bridge/client'

const problem=(e:unknown)=>e instanceof BridgeClientError?e:new BridgeClientError(e instanceof Error?e.message:'请求失败','CLIENT_ERROR',false,'renderer')

export function ReleasePanel({bridge=defaultReleaseBridge,readOnly=false}:{bridge?:ReleaseBridge;readOnly?:boolean}):React.JSX.Element{
 const [revisionId,setRevisionId]=useState('')
 const [digest,setDigest]=useState('')
 const [packageId,setPackageId]=useState('')
 const [busy,setBusy]=useState(false)
 const [error,setError]=useState('')

 const build=async()=>{if(readOnly||busy||!revisionId.trim()||digest.trim().length!==64)return;setBusy(true);setError('');setPackageId('');try{const payload={crRevisionId:revisionId.trim(),expectedDigest:digest.trim()},attempt=createMutationAttempt('release.buildPackage',payload),result=await bridge.buildPackage(payload,{attempt});setPackageId(result.packageId)}catch(e){setError(problem(e).message)}finally{setBusy(false)}}

 return <aside className="pm-release-panel" aria-label="发布打包"><header className="pm-deliverable-head"><div><b>发布打包</b><small>基于 CR 修订构建发布包</small></div></header><div className="release-panel-body"><label>CR Revision ID<input value={revisionId} onChange={e=>setRevisionId(e.target.value)} placeholder="01ARZ3NDEKTSV4RRFFQ69G5FAV" disabled={readOnly||busy}/></label><label>Expected Digest (sha256)<input value={digest} onChange={e=>setDigest(e.target.value)} placeholder="64 位十六进制摘要" disabled={readOnly||busy}/></label>{error&&<p className="error" role="alert"><b>{error}</b></p>}{packageId&&<p className="release-result" role="status">packageId: <code>{packageId}</code></p>}<button className="primary" disabled={readOnly||busy||!revisionId.trim()||digest.trim().length!==64} onClick={()=>void build()}>{busy?'构建中…':'构建发布包'}</button></div></aside>
}
