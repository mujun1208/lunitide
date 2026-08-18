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
const orgPill=(state:OrgState)=>`org-pill ${state}`

// M9 组织控制台导航（与设计文档 06-完整UI界面设计 · M9 原型一致）
type ConsoleTab='overview'|'spaces'|'members'|'policies'|'approvals'|'market'|'runners'|'budgets'|'audit'|'holds'|'operations'
const NAV:Array<{id:ConsoleTab;icon:string;label:string;live:boolean}>=[
 {id:'overview',icon:'⌂',label:'组织概览',live:true},
 {id:'spaces',icon:'◇',label:'TeamSpace',live:true},
 {id:'members',icon:'♙',label:'成员与身份',live:true},
 {id:'policies',icon:'⚖',label:'PolicyCenter',live:false},
 {id:'approvals',icon:'✓',label:'审批中心',live:false},
 {id:'market',icon:'✦',label:'能力市场',live:false},
 {id:'runners',icon:'◉',label:'Runner',live:false},
 {id:'budgets',icon:'¥',label:'预算中心',live:false},
 {id:'audit',icon:'▤',label:'审计与证据',live:false},
 {id:'holds',icon:'⚑',label:'Legal Hold',live:false},
 {id:'operations',icon:'⌁',label:'运营中心',live:false},
]
// 规划能力页合同（M9/03 UI · 路由契约 + 保护语义；未启用时仅展示概念，不得伪装可启用）
const PLANNED:Record<string,{route:string;desc:string;badge?:string;overlays:Array<[string,string]>;states:string[]}>={
 policies:{route:'/org/:orgId/policies',desc:'继承、模拟、版本发布；放宽项阻断并定位父规则。',overlays:[['策略发布向导','编辑、模拟、独立审批、发布'],['策略冲突','逐条显示父/子值与来源']],states:['concept','blocked','published']},
 approvals:{route:'/org/:orgId/approvals',desc:'SoD、撤回与到期；请求者不得自批，无权者不见正文。',overlays:[['待审批','N-of-M、剩余有效票和到期'],['撤回','已执行事实不可反转']],states:['waiting','approved','revoked','expired']},
 market:{route:'/org/:orgId/marketplace',desc:'组织级安装、策略模拟、审批、版本钉住和撤回影响。',overlays:[['市场隔离','隐藏安装，显示撤回原因编号'],['安装','签名、权限、审核记录']],states:['concept','quarantined','active']},
 runners:{route:'/org/:orgId/runners',desc:'信任、驻留、能力、心跳、排空与隔离；UNKNOWN 不伪装健康。',overlays:[['Runner 隔离','复述对象并通过 SoD'],['失联','红色 UNKNOWN，显示最后证明和影响']],states:['active','draining','offline','unknown']},
 budgets:{route:'/org/:orgId/budgets',desc:'层级额度、预留、实耗、预测、币种和周期。',overlays:[['预算不足','展示层级余额与预留'],['硬门槛','不得先执行后补批']],states:['reserved','exceeded']},
 audit:{route:'/org/:orgId/audit',desc:'筛选、链校验、脱敏查看、导出再授权及访问记录。',overlays:[['审计导出','敏感证据二次授权'],['脱敏','正文脱敏，访问留痕']],states:['redacted','held']},
 holds:{route:'/org/:orgId/legal-holds',desc:'范围、依据、保管人、到期、双人创建与解除，始终与删除分离。',badge:'SOD',overlays:[['Legal Hold','锁定清除，显示受限提示'],['解除','与删除分离且强审计']],states:['held','released']},
 operations:{route:'/org/:orgId/operations',desc:'采用、失败、审批时延、Runner、预算、安全；小群组抑制，不下钻个人内容。',overlays:[['低样本抑制','运营指标不下钻个人内容'],['样本不足','k 阈值口径下不展示']],states:['low-sample','error']},
}

export function OrgAdminPage({bridge=orgBridge}:{bridge?:OrgBridge}):React.JSX.Element{
 const[tab,setTab]=useState<ConsoleTab>('overview')
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
 const gate=summary&&!summary.boundOrgId&&summary.orgs.length>0
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

 const activeNav=NAV.find(item=>item.id===tab)!
 const planned=PLANNED[tab]
 const headMeta=bound?`已绑定「${bound.name}」 · ${bound.orgId} · 保留 ${bound.retentionDays} 天 · ${ORG_STATES[bound.state]}`:gate?'未绑定组织 · 隔离门禁生效（M9-003）':'org / space / 身份 / 环境明确可见 · M9 capability 未启用时显示概念或阻断'
 const OrgList=<div className="org-section"><h4>组织（{summary?.orgs.length??0}）</h4><div className="org-card">{loading?<p role="status">正在载入组织…</p>:summary?.orgs.length?summary.orgs.map(org=><button type="button" className="org-row" key={org.orgId} onClick={()=>void switchOrg(org.orgId)} disabled={busy}><span><b>{org.name}</b><small>{org.orgId}</small></span><i className={orgPill(org.state)}>{ORG_STATES[org.state]}</i><code>{summary.boundOrgId===org.orgId?'● 当前':'切换'}</code></button>):<div className="empty"><b>暂无组织</b><span>点击「新建组织」创建第一个组织。</span></div>}</div></div>

 return <main className="org-console-page"><header className="org-console-header"><div><h1>组织治理</h1><p>{summary?`${summary.orgs.length} 个组织 · 当前绑定 ${summary.boundOrgId?`「${summary.org?.name??summary.boundOrgId}」`:'无'}`:'加载中'} · 组织、空间与身份隔离遵循 ADR-011。</p></div><button className="primary" aria-label="新建组织" onClick={()=>{setCreating(true);setOrgName('')}}>＋ 新建组织</button></header>
  <div className="org-console">
   <aside className="org-nav" aria-label="组织治理导航">
    <div className="org-nav-logo"><span className="real-moon small" aria-hidden="true"><i/><b/><em/></span><b title={bound?.name??'未绑定组织'}>{bound?.name??'未绑定组织'}</b></div>
    {NAV.map(item=><button type="button" key={item.id} className={tab===item.id?'on':''} onClick={()=>setTab(item.id)}><i aria-hidden="true">{item.icon}</i>{item.label}{!item.live&&<em aria-label="规划能力">规划</em>}</button>)}
   </aside>
   <main className="org-main">
    <header className="org-main-head"><div><h3>{activeNav.label}</h3><span className="view-meta">{headMeta}</span></div><span className={`status-badge ${activeNav.live?'live':''}`}>{activeNav.live?'已实现':'规划能力'}</span></header>
    {error&&<p className="skill-center-error" role="alert">{error}</p>}
    {notice&&<p className="org-notice" role="status">{notice}</p>}
    {tab==='overview'&&<>
     {creating&&<div className="org-section"><form className="org-card org-create" onSubmit={e=>{e.preventDefault();void createOrg()}}><h4>新建组织</h4><label>组织名称<input value={orgName} maxLength={128} onChange={e=>setOrgName(e.target.value)} placeholder="如：月汐航空维修工程部"/></label><p>组织创建后自动绑定；草稿状态需激活后方可承载成员操作。</p><div className="org-form"><button type="button" disabled={busy} onClick={()=>setCreating(false)}>取消</button><button className="primary" disabled={busy||!orgName.trim()}>{busy?'创建中…':'创建并绑定'}</button></div></form></div>}
     {gate&&<div className="gate-box" role="alert"><b>未绑定组织（M9-003）</b><span>空间与成员数据已被隔离门禁关闭：请先在下方选择并绑定一个组织。</span></div>}
     {bound&&<div className="org-section"><h4>当前组织</h4><div className="org-card org-bound"><div><b>{bound.name}</b><small>{bound.orgId} · 保留 {bound.retentionDays} 天 · 创建于 {bound.createdAt.slice(0,10)}</small></div><i className={orgPill(bound.state)}>{ORG_STATES[bound.state]}</i><div className="org-bound-actions">{bound.state==='draft'&&<button className="primary" disabled={busy} onClick={()=>void lifecycle('activate')}>激活组织</button>}{bound.state==='active'&&<button disabled={busy} onClick={()=>void lifecycle('suspend')}>暂停组织</button>}{bound.state==='suspended'&&<button className="primary" disabled={busy} onClick={()=>void lifecycle('activate')}>恢复组织</button>}<button disabled={busy} onClick={()=>void load()}>↻ 刷新</button></div></div></div>}
     {OrgList}
    </>}
    {tab==='spaces'&&<>
     {gate&&<div className="gate-box" role="alert"><b>未绑定组织（M9-003）</b><span>空间数据已被隔离门禁关闭：请先在「组织概览」绑定一个组织。</span></div>}
     {bound&&<div className="org-section"><h4>新建空间{bound.state!=='active'&&<em className="view-meta"> · 仅活跃组织可创建</em>}</h4><form className="org-form" onSubmit={e=>{e.preventDefault();void addSpace()}}><input value={spaceName} maxLength={128} onChange={e=>setSpaceName(e.target.value)} placeholder="新空间名称" aria-label="新空间名称" disabled={busy||bound.state!=='active'}/><button disabled={busy||!spaceName.trim()||bound.state!=='active'}>创建空间</button></form></div>}
     {bound&&<div className="org-section"><h4>空间（{spaces?.length??0}）</h4><div className="org-card">{spaces===undefined?<p role="status">正在载入空间…</p>:spaces.length?spaces.map(space=><div className="org-row static" key={space.spaceId}><span><b>{space.name}</b><small>{space.spaceId} · 创建于 {space.createdAt.slice(0,10)}</small></span><i className={`org-pill ${space.state}`}>{space.state==='active'?'活跃':'已归档'}</i><code/></div>):<div className="empty"><b>暂无空间</b><span>组织激活后即可创建第一个团队空间。</span></div>}</div></div>}
    </>}
    {tab==='members'&&<>
     {gate&&<div className="gate-box" role="alert"><b>未绑定组织（M9-003）</b><span>成员数据已被隔离门禁关闭：请先在「组织概览」绑定一个组织。</span></div>}
     {bound&&<div className="org-section"><h4>邀请成员{bound.state!=='active'&&<em className="view-meta"> · 仅活跃组织可邀请</em>}</h4><form className="org-form" onSubmit={e=>{e.preventDefault();void invite()}}><input value={inviteName} maxLength={128} onChange={e=>setInviteName(e.target.value)} placeholder="成员名称" aria-label="成员名称" disabled={busy||bound.state!=='active'}/><input type="datetime-local" value={inviteExpiry} onChange={e=>setInviteExpiry(e.target.value)} aria-label="有效期（可选）" disabled={busy||bound.state!=='active'}/><button disabled={busy||!inviteName.trim()||bound.state!=='active'}>邀请成员</button></form><p className="view-meta">人类 / 服务 / 外部身份 · 撤销即时生效（revocation watermark）</p></div>}
     {bound&&<div className="org-section"><h4>成员（{members?.length??0}）</h4><div className="org-card">{members===undefined?<p role="status">正在载入成员…</p>:members.length?members.map(member=><div className="org-row static" key={member.principal.principalId}><span><b>{member.principal.displayName}</b><small>{member.bindings.filter(b=>b.state==='active').map(b=>ROLES[b.role]).join('、')||'无有效角色'} · {member.principal.principalId}{member.principal.expiresAt?` · 至 ${member.principal.expiresAt.slice(0,10)}`:''}</small></span><i className={`org-pill ${member.principal.state}`}>{PRINCIPAL_STATES[member.principal.state]}</i><code>{member.principal.state==='active'?<button type="button" disabled={busy} onClick={()=>setRevokeTarget(member)}>撤销</button>:null}</code></div>):<div className="empty"><b>暂无成员</b><span>邀请的成员将出现在这里并继承组织策略。</span></div>}</div></div>}
     {revokeTarget&&<div className="skill-center-error" role="alert"><p>撤销成员「{revokeTarget.principal.displayName}」的全部角色绑定？撤销后即时生效、不可恢复。</p><div className="org-form"><button className="danger" disabled={busy} onClick={()=>void revoke()}>确认撤销</button><button disabled={busy} onClick={()=>setRevokeTarget(undefined)}>取消</button></div></div>}
    </>}
    {planned&&<>
     <div className="blocked-banner" role="status">⚠ 规划能力 · 待实现 —— 依赖 M9 切片决策冻结与任务卡（ADR-011 后续切片），当前仅展示概念合同，不提供可启用控件。</div>
     <article className="screen-route" data-route={planned.route}>{planned.badge&&<span className="status-badge" style={{margin:'0 0 6px'}}>{planned.badge}</span>}<b>{activeNav.label}</b><p>{planned.desc}</p></article>
     <div className="overlay-strip">{planned.overlays.map(([title,desc])=><span key={title}><b>{title}</b>{desc}</span>)}</div>
     <div className="state-contract" aria-label="状态契约">{planned.states.map(state=><span key={state}>{state}</span>)}</div>
    </>}
   </main>
  </div>
 </main>
}
