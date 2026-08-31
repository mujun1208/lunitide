import React,{useEffect,useState}from'react'
import{BridgeClientError,createMutationAttempt,skillImportBridge as defaultSkillImportBridge,type SkillImportBridge}from'../bridge/client'
import type{SkillImportDiscoverResult}from'../generated/bridge'
import{Dialog}from'../ui/Dialog'

const MOCK_ARCHIVE='aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa'
const problem=(e:unknown)=>e instanceof BridgeClientError?e:new BridgeClientError(e instanceof Error?e.message:'请求失败','CLIENT_ERROR',false,'renderer')

type Props={open:boolean;onClose:()=>void;onApproved?:()=>void;bridge?:SkillImportBridge;initialUrl?:string}

export function SkillImportWizard({open,onClose,onApproved,bridge=defaultSkillImportBridge,initialUrl=''}:Props):React.JSX.Element{
 const [step,setStep]=useState<1|2|3|4|5>(1)
 const [sourceUrl,setSourceUrl]=useState(initialUrl)
 const [commit,setCommit]=useState('')
 const [candidate,setCandidate]=useState<SkillImportDiscoverResult|null>(null)
 const [busy,setBusy]=useState(false)
 const [error,setError]=useState('')

 const reset=()=>{setStep(1);setSourceUrl(initialUrl);setCommit('');setCandidate(null);setBusy(false);setError('')}
 useEffect(()=>{if(open)setSourceUrl(initialUrl)},[open,initialUrl])
 const close=()=>{if(busy)return;reset();onClose()}

 const discover=async()=>{if(!sourceUrl.trim()||!commit.trim())return;setBusy(true);setError('');try{const payload={assetType:'skill' as const,sourceUrl:sourceUrl.trim(),immutableCommit:commit.trim(),archiveHash:MOCK_ARCHIVE,license:'MIT',publisher:'github-import'},attempt=createMutationAttempt('skill.import.discover',payload),result=await bridge.discover(payload,{attempt});setCandidate(result);setStep(2)}catch(e){setError(problem(e).message)}finally{setBusy(false)}}
 const inspect=async()=>{if(!candidate)return;setBusy(true);setError('');try{const payload={candidateId:candidate.candidateId,expectedVersion:candidate.version},attempt=createMutationAttempt('skill.import.inspect',payload),result=await bridge.inspect(payload,{attempt});setCandidate({...candidate,...result});setStep(3)}catch(e){setError(problem(e).message)}finally{setBusy(false)}}
 const submit=async()=>{if(!candidate)return;setBusy(true);setError('');try{const payload={candidateId:candidate.candidateId,expectedVersion:candidate.version,scanRefs:'mock-scan-ref',injectionScan:'clean',evaluationId:'mock-eval'},attempt=createMutationAttempt('skill.import.submit',payload),result=await bridge.submit(payload,{attempt});setCandidate({...candidate,...result});setStep(4)}catch(e){setError(problem(e).message)}finally{setBusy(false)}}
 const approve=async()=>{if(!candidate)return;setBusy(true);setError('');try{const payload={candidateId:candidate.candidateId,expectedVersion:candidate.version,approval:{source:'github-import',approvedAt:new Date().toISOString()}},attempt=createMutationAttempt('skill.import.approve',payload),result=await bridge.approve(payload,{attempt});setCandidate({...candidate,...result});setStep(5);onApproved?.()}catch(e){setError(problem(e).message)}finally{setBusy(false)}}

 return <Dialog open={open} title="从 GitHub 导入技能" description="发现 → 检查 → 提交扫描 → 审批" onClose={close} wide><div className="skill-import-wizard">{step===1&&<><label>GitHub 仓库 URL<input value={sourceUrl} onChange={e=>setSourceUrl(e.target.value)} placeholder="https://github.com/org/repo"/></label><label>Commit SHA<input value={commit} onChange={e=>setCommit(e.target.value)} placeholder="0123456789abcdef0123456789abcdef01234567"/></label><div className="dialog-actions"><button disabled={busy} onClick={close}>取消</button><button className="primary" disabled={busy||!sourceUrl.trim()||!commit.trim()} onClick={()=>void discover()}>{busy?'发现中…':'发现'}</button></div></>}
 {step===2&&candidate&&<><p className="gate-note">候选 <code>{candidate.candidateId}</code> · 状态 {candidate.state} · v{candidate.version}</p><div className="dialog-actions"><button disabled={busy} onClick={()=>setStep(1)}>上一步</button><button className="primary" disabled={busy} onClick={()=>void inspect()}>{busy?'检查中…':'检查'}</button></div></>}
 {step===3&&candidate&&<><p className="gate-note">已检查 · 状态 {candidate.state}</p><div className="dialog-actions"><button disabled={busy} onClick={()=>setStep(2)}>上一步</button><button className="primary" disabled={busy} onClick={()=>void submit()}>{busy?'提交中…':'提交扫描'}</button></div></>}
 {step===4&&candidate&&<><p className="gate-note">等待审批 · 状态 {candidate.state}</p><div className="dialog-actions"><button disabled={busy} onClick={()=>setStep(3)}>上一步</button><button className="primary" disabled={busy} onClick={()=>void approve()}>{busy?'审批中…':'批准导入'}</button></div></>}
 {step===5&&candidate&&<><p className="gate-note">导入已批准 · 状态 {candidate.state}</p><div className="dialog-actions"><button className="primary" onClick={close}>完成</button></div></>}
 {error&&<p className="error" role="alert"><b>{error}</b></p>}</div></Dialog>
}
