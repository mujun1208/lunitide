import React,{useEffect,useState}from'react'
import{projectBridge}from'../bridge/client'
import type{ProjectDTO}from'../generated/bridge'
import{MemoryPage}from'../memory/MemoryPage'
import{MemoryOpsPanel}from'../memory/MemoryOpsPanel'

// M8 个人长期智能控制台（对齐设计文档 06-完整UI界面设计 · M8 原型：lunitide://personal-intelligence）
type M8Tab='inbox'|'facts'|'knowledge'|'handoff'|'automation'|'runs'|'sync'|'privacy'|'experts'
const NAV:Array<{id:M8Tab;icon:string;label:string;live:boolean}>=[
 {id:'inbox',icon:'◌',label:'记忆收件箱',live:true},
 {id:'facts',icon:'◆',label:'已确认事实',live:true},
 {id:'knowledge',icon:'▤',label:'KnowledgeBase',live:false},
 {id:'handoff',icon:'⇥',label:'Handoff',live:false},
 {id:'automation',icon:'⚡',label:'自动化',live:false},
 {id:'runs',icon:'◷',label:'运行中心',live:false},
 {id:'sync',icon:'⇄',label:'同步冲突',live:false},
 {id:'privacy',icon:'⌾',label:'隐私与设备',live:false},
 {id:'experts',icon:'✧',label:'专家中心',live:true},
]
// 概念合同（设计文档规定：无公开 RPC 的画板主操作必须禁用并标"概念预览"）
const PLANNED:Record<string,{route:string;desc:string;overlays:Array<[string,string]>;states:string[]}>={
 knowledge:{route:'/knowledge',desc:'集合、文档、索引、引用及 indexing、stale、redacted 状态。',overlays:[['索引','集合、文档与引用追溯'],['状态','indexing / stale / redacted']],states:['indexing','stale','redacted']},
 handoff:{route:'/handoffs/new',desc:'接收人、明确清单、裁剪差异、有效期和接受状态；发送二次确认。',overlays:[['发送','明确清单 + 二次确认'],['裁剪','差异预览与有效期']],states:['draft','sent','accepted','expired']},
 automation:{route:'/automations/:id/edit',desc:'触发器、步骤、权限、预算、试运行与版本差异。',overlays:[['试运行','先试跑再启用'],['预算','步骤级额度与版本差异']],states:['draft','trial','enabled','versioned']},
 runs:{route:'/automations/runs',desc:'waiting、running、compensating、quarantined、done。',overlays:[['补偿','失败步骤补偿回滚'],['隔离','quarantined 人工处置']],states:['waiting','running','compensating','quarantined','done']},
 sync:{route:'/sync/conflicts',desc:'本机、远端、自定义的字段级合并和最终预览。',overlays:[['字段级合并','本机 / 远端 / 自定义'],['最终预览','合并结果先行预览']],states:['local','remote','merged']},
 privacy:{route:'/privacy/devices',desc:'设备信任、同步、导出、吊销、清除及删除证明进度。',overlays:[['设备吊销','与清除数据分开确认'],['删除证明','设备回执和证明编号']],states:['trusted','revoked','erasing','proven']},
}

export function PersonalIntelligencePage({onNavigateExpert}:{onNavigateExpert?:()=>void}):React.JSX.Element{
 const[tab,setTab]=useState<M8Tab>('inbox')
 const[projects,setProjects]=useState<ProjectDTO[]>([]),[projectId,setProjectId]=useState('')
 useEffect(()=>{try{projectBridge.list().then(result=>{const visible=result.items.filter(item=>!item.name.startsWith('⁣'));setProjects(visible);setProjectId(current=>current||visible[0]?.id||'')}).catch(()=>{})}catch{/* bridge unavailable outside WebView2 */}},[])
 const activeNav=NAV.find(item=>item.id===tab)!
 const planned=PLANNED[tab]
 return <main className="org-console-page"><div className="org-console">
  <aside className="org-nav" aria-label="个人智能导航">
   <div className="org-nav-logo"><span className="real-moon small" aria-hidden="true"><i/><b/><em/></span><b>个人智能</b></div>
   {NAV.map(item=><button type="button" key={item.id} className={tab===item.id?'on':''} onClick={()=>setTab(item.id)}><i aria-hidden="true">{item.icon}</i>{item.label}{!item.live&&<em aria-label="概念预览">概念</em>}</button>)}
  </aside>
  <main className="org-main">
   <header className="org-main-head"><div><h3>{activeNav.label}</h3><span className="view-meta">候选必须人工确认 · 可解释召回 · 可撤回与递归删除</span></div><span className={`status-badge ${activeNav.live?'live':''}`}>{activeNav.live?'M8 · 已实现':'概念预览'}</span></header>
   {tab==='inbox'&&<div className="org-section" style={{marginTop:6}}>
    <div className="org-form" style={{marginBottom:10}}><label className="view-meta" htmlFor="m8-memory-project">记忆作用域（项目）</label><select id="m8-memory-project" value={projectId} onChange={e=>setProjectId(e.target.value)}>{projects.length?projects.map(project=><option key={project.id} value={project.id}>{project.name}</option>):<option value="">（暂无项目）</option>}</select></div>
    {projectId?<MemoryPage projectId={projectId}/>:<div className="gate-box"><b>需要项目上下文</b><span>记忆以项目为作用域；请先创建或选择一个项目。</span></div>}
   </div>}
   {tab==='facts'&&<div className="org-section" style={{marginTop:6}}><MemoryOpsPanel/></div>}
   {tab==='experts'&&<div className="org-section" style={{marginTop:6}}>
    <article className="screen-route" data-route="/experts"><b>专家中心</b><p>division 八类分组、来源/状态筛选；六段式详情与 append-only 版本历史；启停/归档；九阶段挂载矩阵（每阶段 ≤4）。</p></article>
    <div className="overlay-strip"><span><b>六段式专家</b>身份 / 使命 / 规则 / 流程 / 交付模板 / 成功指标</span><span><b>只读派发</b>personaRef 不携带授权，结果物化 Evidence</span><span><b>版本链</b>append-only，修订须填 change_note</span><span><b>挂载矩阵</b>默认 M7 映射，可逐阶段调整</span></div>
    <div className="org-form" style={{marginTop:12}}><button className="primary" onClick={onNavigateExpert}>前往专家中心 →</button></div>
   </div>}
   {planned&&<>
    <div className="blocked-banner" role="status">⚠ 概念预览 —— 该子域的 M8 RPC 未开放（FR 对应切片待实现），主操作禁用，仅展示界面合同。</div>
    <article className="screen-route" data-route={planned.route}><b>{activeNav.label}</b><p>{planned.desc}</p></article>
    <div className="overlay-strip">{planned.overlays.map(([title,desc])=><span key={title}><b>{title}</b>{desc}</span>)}</div>
    <div className="state-contract" aria-label="状态契约">{planned.states.map(state=><span key={state}>{state}</span>)}</div>
   </>}
  </main>
 </div></main>
}
