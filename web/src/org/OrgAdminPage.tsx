import React,{useCallback,useEffect,useState}from'react'
import{createMutationAttempt,orgBridge,type OrgBridge}from'../bridge/client'
import type{OrgMemberListResult,OrgSpaceListResult,OrgSummaryResult}from'../generated/bridge'

type OrgRow=OrgSummaryResult['orgs'][number]
type OrgState=OrgRow['state']
type SpaceRow=OrgSpaceListResult['spaces'][number]
type MemberRow=OrgMemberListResult['members'][number]
type Role=MemberRow['bindings'][number]['role']
type PrincipalState=MemberRow['principal']['state']

const ORG_STATES:Record<OrgState,string>={draft:'草稿',active:'活跃',suspended:'已暂停',closed:'已关闭'}
const PRINCIPAL_STATES:Record<PrincipalState,string>={active:'活跃',suspended:'已暂停',expired:'已过期',revoked:'已撤销'}
const ROLES:Record<Role,string>={'org-admin':'组织管理员','space-admin':'空间管理员',operator:'操作者',approver:'审批者',auditor:'审计者','legal-officer':'法务专员',member:'成员'}
const statusClass=(state:OrgState)=>state==='active'?'published':state==='suspended'?'deprecated':'disabled'

export function OrgAdminPage({bridge=orgBridge}:{bridge?:OrgBridge}):React.JSX.Element{
 const[summary,setSummary]=useState<OrgSummaryResult>(),[loading,setLoading]=useState(true),[busy,setBusy]=useState(false),[error,setError]=useState(''),[notice,setNotice]=useState('')
 const[spaces,setSpaces]=useState<SpaceRow[]>(),[members,setMembers]=useState<MemberRow[]>()
 const[creating,setCreating]=useState(false),[orgName,setOrgName]=useState('')
 const[spaceName,setSpaceName]=useState(''),[inviteName,setInviteName]=useState(''),[inviteExpiry,setInviteExpiry]=useState('')
 const[revokeTarget,setRevokeTarget]=useState<MemberRow>()

 const loadScoped=useCallback(async(current:string)=>{
  if(!current){setSpaces(undefined);setMembers(undefined);return}
  const[nextSpaces,nextMembers]=await Promise.all([bridge.spaceList(),bridge.memberList()])
  setSpaces(nextSpaces.spaces);setMembers(nextMembers.members)
 },[bridge])

 const load=useCallback(async()=>{
  setLoading(true);setError('')
  try{
   const next=await bridge.summary()
   setSummary(next)
   await loadScoped(next.boundOrgId)
  }catch(e){setError(e instanceof Error?e.message:'组织概览加载失败')}
  finally{setLoading(false)}
 },[bridge,loadScoped])

 useEffect(()=>{void load()},[load])

 const bound=summary?.org
 const switchOrg=async(orgId:string)=>{
  if(busy||orgId===summary?.boundOrgId)return
  setBusy(true);setError('');setNotice('')
  try{
   const base={orgId},attempt=createMutationAttempt('org.switch',base)
   await bridge.switch(base,{attempt})
   setNotice(`已切换到组织「${summary?.orgs.find(o=>o.orgId===orgId)?.name??orgId}」，数据范围随之切换`)
   const next=await bridge.summary()
   setSummary(next)
   setSpaces(undefined);setMembers(undefined)
   await loadScoped(next.boundOrgId)
  }catch(e){setError(e instanceof Error?e.message:'组织切换失败')}finally{setBusy(false)}
 }
 const createOrg=async()=>{
  const name=orgName.trim()
  if(name.length<1||name.length>128){setError('组织名称需为 1–128 个字符');return}
  setBusy(true);setError('');setNotice('')
  try{
   const base={name},attempt=createMutationAttempt('org.create',base)
   const created=await bridge.create(base,{attempt})
   setNotice(`组织「${created.name}」已创建（${ORG_STATES[created.state]}），切换后即可启用`)
   setCreating(false);setOrgName('')
   const next=await bridge.summary();setSummary(next)
   const baseSwitch={orgId:created.orgId},attemptSwitch=createMutationAttempt('org.switch',baseSwitch)
   await bridge.switch(baseSwitch,{attempt:attemptSwitch})
   const switched=await bridge.summary();setSummary(switched)
   await loadScoped(switched.boundOrgId)
  }catch(e){setError(e instanceof Error?e.message:'组织创建失败')}finally{setBusy(false)}
 }
 const lifecycle=async(action:'activate'|'suspend')=>{
  setBusy(true);setError('');setNotice('')
  try{
   if(action==='activate'){const attempt=createMutationAttempt('org.activate',{});const r=await bridge.activate({},{attempt});setNotice(`组织「${r.name}」已激活`)}
   else{const attempt=createMutationAttempt('org.suspend',{});const r=await bridge.suspend({},{attempt});setNotice(`组织「${r.name}」已暂停（保留审计与法务持有）`)}
   const next=await bridge.summary();setSummary(next)
  }catch(e){setError(e instanceof Error?e.message:action==='activate'?'组织激活失败':'组织暂停失败')}finally{setBusy(false)}
 }
 const addSpace=async()=>{
  const name=spaceName.trim()
  if(name.length<1||name.length>128){setError('空间名称需为 1–128 个字符');return}
  setBusy(true);setError('');setNotice('')
  try{
   const base={name},attempt=createMutationAttempt('org.space.create',base)
   const created=await bridge.spaceCreate(base,{attempt})
   setNotice(`空间「${created.name}」已创建`)
   setSpaceName('')
   const next=await bridge.spaceList();setSpaces(next.spaces)
  }catch(e){setError(e instanceof Error?e.message:'空间创建失败')}finally{setBusy(false)}
 }
 const invite=async()=>{
  const displayName=inviteName.trim()
  if(displayName.length<1||displayName.length>128){setError('成员名称需为 1–128 个字符');return}
  let expiresAt:string|undefined
  if(inviteExpiry){const expiry=new Date(inviteExpiry);if(Number.isNaN(expiry.getTime())||expiry.getTime()<=Date.now()){setError('成员有效期需为将来的时间');return}expiresAt=expiry.toISOString()}
  setBusy(true);setError('');setNotice('')
  try{
   const base:{displayName:string;expiresAt?:string}=expiresAt?{displayName,expiresAt}:{displayName}
   const attempt=createMutationAttempt('org.member.invite',base)
   const created=await bridge.memberInvite(base,{attempt})
   setNotice(`成员「${created.displayName}」已加入（${PRINCIPAL_STATES[created.state]}）`)
   setInviteName('');setInviteExpiry('')
   const next=await bridge.memberList();setMembers(next.members)
  }catch(e){setError(e instanceof Error?e.message:'成员邀请失败')}finally{setBusy(false)}
 }
 const revoke=async()=>{
  if(!revokeTarget)return
  setBusy(true);setError('');setNotice('')
  try{
   const base={principalId:revokeTarget.principal.principalId},attempt=createMutationAttempt('org.member.revoke',base)
   const result=await bridge.memberRevoke(base,{attempt})
   setNotice(`成员「${revokeTarget.principal.displayName}」已撤销（${PRINCIPAL_STATES[result.state]}），其角色绑定即时失效`)
   setRevokeTarget(undefined)
   const next=await bridge.memberList();setMembers(next.members)
  }catch(e){setError(e instanceof Error?e.message:'成员撤销失败')}finally{setBusy(false)}
 }

 return <main className="skill-center"><header className="skill-center-header"><div><h1>组织管理</h1><p>{summary?`${summary.orgs.length} 个组织 · 当前绑定 ${summary.boundOrgId?`「${summary.org?.name??summary.boundOrgId}」`:'无'}`:'加载中'}</p><small>多租户组织、空间与成员管理及隔离门禁（M9 T-9.1.3）。</small></div><button className="primary skill-chat-create" aria-label="新建组织" onClick={()=>{setCreating(true);setOrgName('')}}>＋ 新建组织</button></header>
  {error&&<p className="skill-center-error" role="alert">{error}</p>}
  {notice&&<p role="status">{notice}</p>}
  <div className="skill-center-layout"><section className="skill-table" aria-label="组织列表"><div className="skill-table-head"><span>组织</span><span>状态</span><span>绑定</span></div>
   {loading?<p role="status">正在载入组织…</p>:summary?.orgs.length?summary.orgs.map(org=><button type="button" className={`skill-row ${summary.boundOrgId===org.orgId?'active':''}`} key={org.orgId} onClick={()=>void switchOrg(org.orgId)} disabled={busy}><span><b>{org.name}</b><small>{org.orgId}</small></span><i className={`skill-status status-${statusClass(org.state)}`}>{ORG_STATES[org.state]}</i><code>{summary.boundOrgId===org.orgId?'● 当前':'切换'}</code></button>):<div className="empty"><b>暂无组织</b><span>点击「新建组织」创建第一个组织。</span></div>}
  </section>
  <aside className="skill-detail" aria-label="组织详情">
   {creating?(
    <form onSubmit={e=>{e.preventDefault();void createOrg()}}><div className="skill-detail-title"><h2>新建组织</h2><button type="button" onClick={()=>setCreating(false)}>取消</button></div>
     <label>组织名称<input value={orgName} maxLength={128} onChange={e=>setOrgName(e.target.value)} placeholder="如：月汐航空维修工程部"/></label>
     <p>组织创建后自动绑定；草稿状态需激活后方可承载成员操作。</p>
     <button className="primary" disabled={busy||!orgName.trim()}>{busy?'创建中…':'创建并绑定'}</button></form>
   ):bound?(
    <>
     <div className="skill-detail-title"><div><h2>{bound.name}</h2><code>{bound.orgId} · 保留 {bound.retentionDays} 天</code></div><span className={`skill-status status-${statusClass(bound.state)}`}>{ORG_STATES[bound.state]}</span></div>
     <div className="skill-detail-actions">
      {bound.state==='draft'&&<button className="primary" disabled={busy} onClick={()=>void lifecycle('activate')}>激活组织</button>}
      {bound.state==='active'&&<button disabled={busy} onClick={()=>void lifecycle('suspend')}>暂停组织</button>}
      {bound.state==='suspended'&&<button className="primary" disabled={busy} onClick={()=>void lifecycle('activate')}>恢复组织</button>}
      <button disabled={busy} onClick={()=>void load()}>↻ 刷新</button>
     </div>
     <h3>空间（{spaces?.length??0}）</h3>
     <form className="skill-path" onSubmit={e=>{e.preventDefault();void addSpace()}}><input value={spaceName} maxLength={128} onChange={e=>setSpaceName(e.target.value)} placeholder="新空间名称" aria-label="新空间名称" disabled={busy||bound.state!=='active'}/><button disabled={busy||!spaceName.trim()||bound.state!=='active'}>创建空间</button></form>
     {spaces===undefined?<p role="status">正在载入空间…</p>:spaces.length?spaces.map(space=><div className="skill-path" key={space.spaceId}><b>{space.name}</b><span>{space.state==='active'?'活跃':'已归档'} · {space.createdAt.slice(0,10)}</span></div>):<p>暂无空间</p>}
     <h3>成员（{members?.length??0}）</h3>
     <div className="skill-path"><input value={inviteName} maxLength={128} onChange={e=>setInviteName(e.target.value)} placeholder="成员名称" aria-label="成员名称" disabled={busy||bound.state!=='active'}/><input type="datetime-local" value={inviteExpiry} onChange={e=>setInviteExpiry(e.target.value)} aria-label="有效期（可选）" disabled={busy||bound.state!=='active'}/><button disabled={busy||!inviteName.trim()||bound.state!=='active'} onClick={()=>void invite()}>邀请成员</button></div>
     {members===undefined?<p role="status">正在载入成员…</p>:members.length?members.map(member=><div className="skill-path" key={member.principal.principalId}><b>{member.principal.displayName}</b><span>{member.bindings.filter(b=>b.state==='active').map(b=>ROLES[b.role]).join('、')||'无有效角色'} · {PRINCIPAL_STATES[member.principal.state]}{member.principal.expiresAt?` · 至 ${member.principal.expiresAt.slice(0,10)}`:''}</span>{member.principal.state==='active'&&<button disabled={busy} onClick={()=>setRevokeTarget(member)}>撤销</button>}</div>):<p>暂无成员</p>}
     {revokeTarget&&<div className="skill-center-error" role="alert"><p>撤销成员「{revokeTarget.principal.displayName}」的全部角色绑定？撤销后即时生效、不可恢复。</p><div className="skill-detail-actions"><button className="danger" disabled={busy} onClick={()=>void revoke()}>确认撤销</button><button disabled={busy} onClick={()=>setRevokeTarget(undefined)}>取消</button></div></div>}
    </>
   ):summary?(
    <div className="empty"><b>未绑定组织（M9-003）</b><span>空间与成员数据已被隔离门禁关闭：请先在左侧选择并绑定一个组织。</span></div>
   ):<div className="empty"><b>选择组织查看详情</b></div>}
  </aside></div></main>
}
