import React,{useCallback,useEffect,useState}from'react'
import{createMutationAttempt,mcBridge,type McBridge}from'../bridge/client'
import type{McConfigValidateResult,McConnectorUsageResult,McMarketDetailResult,McMarketListResult,McTombstoneCheckResult}from'../generated/bridge'

type MarketItem=McMarketListResult['items'][number]
type UsageStat=McConnectorUsageResult['stats'][number]
type CheckItem=McConfigValidateResult['checks'][number]

const RULE_LABELS:Record<string,string>={
 'MC-VR-01':'传输类型','MC-VR-02':'stdio 命令','MC-VR-03':'stdio 参数',
 'MC-VR-04':'https URL','MC-VR-05':'SSRF 防护','MC-VR-06':'密钥引用','MC-VR-07':'长度上限','MC-VR-08':'配额指纹'}
const STATES:Record<UsageStat['state'],string>={probe:'探测中',ready:'就绪',degraded:'降级',revoked:'已吊销',quarantined:'隔离'}
const TRANSPORTS:Record<MarketItem['transportHint'],string>={stdio:'本地 stdio',https:'远程 https'}
const MANUAL_TEMPLATE='{\n  "transport": "https",\n  "url": "https://mcp.example.com/sse"\n}'

const CheckList=({checks}:{checks:CheckItem[]})=><ul className="mc-check-chain" aria-label="校验链">{checks.map(check=><li key={check.rule} className={check.passed?'passed':'failed'}><b>{check.passed?'✓':'✗'} {RULE_LABELS[check.rule]??check.rule}</b>{check.reason&&<small>{check.reason}</small>}</li>)}</ul>

export function ConnectorPage({bridge=mcBridge}:{bridge?:McBridge}):React.JSX.Element{
 const[tab,setTab]=useState<'market'|'manual'|'installed'>('market')
 const[items,setItems]=useState<MarketItem[]>([]),[fresh,setFresh]=useState(true),[nextCursor,setNextCursor]=useState('')
 const[query,setQuery]=useState(''),[transportFilter,setTransportFilter]=useState<'stdio'|'https'|''>('')
 const[loading,setLoading]=useState(true),[busy,setBusy]=useState(false),[error,setError]=useState(''),[notice,setNotice]=useState('')
 const[detail,setDetail]=useState<McMarketDetailResult>()
 const[confirmInstall,setConfirmInstall]=useState<{origin:'market'|'manual';marketItemId?:string;config:McMarketDetailResult['config'];token:string;digest:string}|null>(null)
 const[manualJson,setManualJson]=useState(MANUAL_TEMPLATE),[manualChecks,setManualChecks]=useState<McConfigValidateResult>()
 const[stats,setStats]=useState<UsageStat[]>([])
 const[tombstone,setTombstone]=useState<McTombstoneCheckResult>()
 const[uninstallTarget,setUninstallTarget]=useState<UsageStat|null>(null)

 const loadMarket=useCallback(async(cursor?:string)=>{setLoading(true);setError('');try{const result=await bridge.marketList({query:query.trim()||undefined,transportHint:transportFilter||undefined,...(cursor?{cursor,limit:50}:{limit:50})});setItems(cursor?[...items,...result.items]:result.items);setFresh(result.fresh);setNextCursor(result.nextCursor??'')}catch(e){setError(e instanceof Error?e.message:'市场目录加载失败')}finally{setLoading(false)}},[bridge,query,transportFilter,items])
 useEffect(()=>{void loadMarket()},[loadMarket])
 const loadStats=useCallback(async()=>{try{const result=await bridge.usage({});setStats(result.stats)}catch{setStats([])}},[bridge])
 useEffect(()=>{void loadStats()},[loadStats])

 const openDetail=async(item:MarketItem)=>{setBusy(true);setError('');try{setDetail(await bridge.marketDetail({itemId:item.id}))}catch(e){setError(e instanceof Error?e.message:'市场详情加载失败')}finally{setBusy(false)}}
 const requestInstall=async()=>{if(!detail)return;setBusy(true);setError('');try{const target=detail.item.id,digest=await sha256Text(`mc.connector.install|${target}|${detail.item.catalogDigest}`),token=await bridge.confirmToken({method:'mc.connector.install',target,digest});setConfirmInstall({origin:'market',marketItemId:target,config:detail.config,token:token.confirmToken,digest})}catch(e){setError(e instanceof Error?e.message:'确认令牌签发失败')}finally{setBusy(false)}}
 const install=async()=>{if(!confirmInstall)return;setBusy(true);setError('');try{const base={origin:confirmInstall.origin,...(confirmInstall.marketItemId?{marketItemId:confirmInstall.marketItemId}:{}),...(confirmInstall.origin==='manual'?normalizeManualConfig(confirmInstall.config):{}),confirmToken:confirmInstall.token,requestId:crypto.randomUUID()},attempt=createMutationAttempt('mc.connector.install',base);const result=await bridge.install(attempt.payload,{attempt});setNotice(`连接器已安装：${result.endpointId}（${STATES[result.state]??result.state}）`);setConfirmInstall(null);setDetail(undefined);await loadStats()}catch(e){setError(e instanceof Error?e.message:'连接器安装失败')}finally{setBusy(false)}}
 const validateManual=async()=>{setBusy(true);setError('');try{const config=parseManualJson(manualJson);if(!config)return;setManualChecks(await bridge.configValidate(config))}catch(e){if(e instanceof SyntaxErrorLike)setError('手动配置需为合法 JSON 对象');else setError(e instanceof Error?e.message:'校验请求失败')}finally{setBusy(false)}}
 const requestManualInstall=async()=>{setBusy(true);setError('');try{const config=parseManualJson(manualJson);if(!config)return;const digest=await sha256Text(`mc.connector.install|manual|${JSON.stringify(config)}`),token=await bridge.confirmToken({method:'mc.connector.install',target:`fp:${digest}`,digest});setConfirmInstall({origin:'manual',config,token:token.confirmToken,digest})}catch(e){setError(e instanceof Error?e.message:'确认令牌签发失败')}finally{setBusy(false)}}
 const requestUninstall=async()=>{if(!uninstallTarget)return;setBusy(true);setError('');try{const token=await bridge.confirmToken({method:'mc.connector.uninstall',target:uninstallTarget.endpointId});const base={endpointId:uninstallTarget.endpointId,confirmToken:token.confirmToken},attempt=createMutationAttempt('mc.connector.uninstall',base);await bridge.uninstall(attempt.payload,{attempt});setNotice(`连接器 ${uninstallTarget.endpointId} 已吊销`);setUninstallTarget(null);await loadStats()}catch(e){setError(e instanceof Error?e.message:'连接器卸载失败')}finally{setBusy(false)}}
 const runTombstone=async()=>{setBusy(true);setError('');try{setTombstone(await bridge.tombstoneCheck())}catch(e){setError(e instanceof Error?e.message:'墓碑检测失败')}finally{setBusy(false)}}

 return <main className="skill-center"><header className="skill-center-header"><div><h1>连接器市场</h1><p>{items.length} 个目录条目 · {stats.filter(stat=>stat.state!=='revoked').length} 个已安装端点</p><small>MCP 连接器市场：目录浏览、8 规则校验链、确认令牌与端点生命周期（M10 第三波）。</small></div><div className="skill-detail-actions"><button onClick={()=>void runTombstone()} disabled={busy||loading}>墓碑检测</button><button className="primary" onClick={()=>setTab('manual')}>＋ 手动配置</button></div></header>
  <section className="skill-center-toolbar"><div className="skill-status-tabs" role="tablist" aria-label="连接器视图"><button type="button" role="tab" aria-selected={tab==='market'} onClick={()=>setTab('market')}>市场目录</button><button type="button" role="tab" aria-selected={tab==='manual'} onClick={()=>setTab('manual')}>手动配置</button><button type="button" role="tab" aria-selected={tab==='installed'} onClick={()=>setTab('installed')}>已安装（{stats.length}）</button></div>
   {tab==='market'&&<><label className="skill-search">搜索目录<input value={query} onChange={e=>setQuery(e.target.value)} placeholder="名称 / 发布者 / 描述"/></label><select aria-label="传输筛选" value={transportFilter} onChange={e=>setTransportFilter(e.target.value as'stdio'|'https'|'')}><option value="">全部传输</option><option value="stdio">本地 stdio</option><option value="https">远程 https</option></select></>}
   {tab==='market'&&<button aria-label="刷新目录" onClick={()=>void loadMarket()} disabled={loading}>↻</button>}
   {tab==='installed'&&<button aria-label="刷新统计" onClick={()=>void loadStats()}>↻</button>}</section>
  {error&&<p className="skill-center-error" role="alert">{error}</p>}
  {notice&&<p role="status">{notice}</p>}

  {tab==='market'&&<>
   {!fresh&&<p role="status">市场目录不可达，当前展示本地只读缓存。</p>}
   <div className="mc-market-grid" aria-label="市场目录">
    {loading&&items.length===0?<p role="status">正在载入市场目录…</p>:items.length?items.map(item=><article className="mc-market-card" key={item.id}><div><b>{item.name}</b><code>{TRANSPORTS[item.transportHint]}</code></div><p>{item.description||'（无描述）'}</p><small>{item.publisher} · 摘要 {item.catalogDigest.slice(0,12)}…</small><div className="skill-detail-actions"><button className="primary" disabled={busy} onClick={()=>void openDetail(item)}>查看并安装</button></div></article>):<div className="empty"><b>目录为空</b><span>{query?'没有匹配的目录条目。':'市场目录暂无条目，或注册表不可达。'}</span></div>}
   </div>
   {nextCursor&&<div className="skill-detail-actions"><button disabled={loading} onClick={()=>void loadMarket(nextCursor)}>载入更多</button></div>}
  </>}

  {tab==='manual'&&<form className="mc-manual" onSubmit={e=>{e.preventDefault();void validateManual()}}>
   <label>连接器配置 JSON（stdio 需 command/args；https 需 url；密钥仅允许 secret-lease:// 引用）<textarea className="skill-manifest-editor" value={manualJson} maxLength={8192} onChange={e=>setManualJson(e.target.value)}/></label>
   <div className="skill-detail-actions"><button className="primary" disabled={busy}>{busy?'校验中…':'运行校验链'}</button></div>
   {manualChecks&&<><p aria-live="polite">{manualChecks.valid?'配置通过全部 8 条规则，可申请安装令牌。':'配置未通过校验链，安装前需修正以下规则：'}</p><CheckList checks={manualChecks.checks}/><div className="skill-detail-actions">{manualChecks.valid&&<button type="button" disabled={busy} onClick={()=>void requestManualInstall()}>申请令牌并安装</button>}</div></>}
  </form>}

  {tab==='installed'&&<>
   <div className="skill-table" aria-label="已安装端点"><div className="skill-table-head"><span>端点</span><span>传输</span><span>状态</span><span>用量</span></div>
    {stats.length?stats.map(stat=><div className="skill-row" key={stat.endpointId}><span><b>{stat.endpointId}</b><small>{stat.origin==='market'?'市场安装':'手动安装'}{stat.enabled?'':' · 已停用'}</small></span><code>{TRANSPORTS[stat.transport]}</code><i className={`skill-status status-${stat.state==='ready'?'published':stat.state==='degraded'?'disabled':stat.state==='probe'?'draft':'deprecated'}`}>{STATES[stat.state]}</i><span><small>装 {stat.installs} · 更 {stat.updates} · 卸 {stat.uninstalls}</small>{stat.state!=='revoked'&&<button type="button" disabled={busy} onClick={()=>setUninstallTarget(stat)}>卸载…</button>}</span></div>):<div className="empty"><b>暂无已安装端点</b><span>从市场目录或手动配置安装第一个连接器。</span></div>}
   </div>
   {tombstone&&<section aria-label="墓碑检测"><h3>墓碑检测（{tombstone.fresh?'注册表可达':'注册表不可达'}）</h3>
    {tombstone.revoked.length?<div className="skill-center-error" role="alert"><b>发现 {tombstone.revoked.length} 个已撤销条目</b>{tombstone.revoked.map(item=><p key={item.marketItemId}>{item.name}（{item.marketItemId}）→ 影响端点 {item.endpointIds.join('、')||'（无）'}，建议卸载。</p>)}</div>:<p>无已撤销条目。</p>}
    {tombstone.drifted.length?<div role="status"><b>{tombstone.drifted.length} 个条目上游摘要变化</b>{tombstone.drifted.map(item=><p key={item.marketItemId}>{item.name}：{item.cachedDigest.slice(0,12)}… → {item.registryDigest.slice(0,12)}…，建议更新。</p>)}</div>:<p>无摘要漂移。</p>}
   </section>}
  </>}

  {detail&&<div className="model-manager-overlay" role="dialog" aria-modal="true" aria-label={`市场条目 ${detail.item.name}`}><div className="mc-detail-dialog">
   <div className="skill-detail-title"><div><h2>{detail.item.name}</h2><code>{detail.item.publisher} · {TRANSPORTS[detail.item.transportHint]}</code></div><button type="button" onClick={()=>{setDetail(undefined)}}>关闭</button></div>
   <p>{detail.item.description||'（无描述）'}</p>
   <h3>安装配置预览</h3>
   <pre className="mc-config-preview">{JSON.stringify(detail.config,null,2)}</pre>
   <h3>服务端校验链</h3>
   {detail.checks.length?<CheckList checks={detail.checks}/>:<p>（无校验记录）</p>}
   <div className="skill-detail-actions"><button className="primary" disabled={busy||detail.checks.some(check=>!check.passed)} onClick={()=>void requestInstall()}>{busy?'签发令牌中…':'申请令牌并安装'}</button></div>
  </div></div>}

  {confirmInstall&&<div className="model-manager-overlay" role="dialog" aria-modal="true" aria-label="确认安装连接器"><div className="mc-detail-dialog">
   <div className="skill-detail-title"><h2>确认安装连接器</h2><button type="button" onClick={()=>setConfirmInstall(null)}>取消</button></div>
   <p>来源：{confirmInstall.origin==='market'?'市场条目':'手动配置'}。确认令牌（一次性，5 分钟内有效）：<code>{confirmInstall.token.slice(0,16)}…</code></p>
   <pre className="mc-config-preview">{JSON.stringify(confirmInstall.config,null,2)}</pre>
   <div className="skill-detail-actions"><button className="primary" disabled={busy} onClick={()=>void install()}>{busy?'安装中…':'确认安装'}</button><button disabled={busy} onClick={()=>setConfirmInstall(null)}>取消</button></div>
  </div></div>}

  {uninstallTarget&&<div className="model-manager-overlay" role="dialog" aria-modal="true" aria-label="确认卸载连接器"><div className="mc-detail-dialog">
   <div className="skill-detail-title"><h2>卸载连接器</h2><button type="button" onClick={()=>setUninstallTarget(null)}>取消</button></div>
   <p>端点 <code>{uninstallTarget.endpointId}</code>（{TRANSPORTS[uninstallTarget.transport]} · {STATES[uninstallTarget.state]}）将被吊销：停用并进入终态，用量统计保留。操作需要签发一次性确认令牌。</p>
   <div className="skill-detail-actions"><button className="danger" disabled={busy} onClick={()=>void requestUninstall()}>{busy?'卸载中…':'签发令牌并卸载'}</button><button disabled={busy} onClick={()=>setUninstallTarget(null)}>取消</button></div>
  </div></div>}
 </main>
}

const sha256Text=async(text:string):Promise<string>=>{const bytes=new TextEncoder().encode(text),digest=await globalThis.crypto.subtle.digest('SHA-256',bytes);return Array.from(new Uint8Array(digest)).map(byte=>byte.toString(16).padStart(2,'0')).join('')}
class SyntaxErrorLike extends Error{}
const parseManualJson=(raw:string):McMarketDetailResult['config']|undefined=>{let parsed:unknown;try{parsed=JSON.parse(raw)}catch{throw new SyntaxErrorLike('invalid json')}if(!parsed||typeof parsed!=='object'||Array.isArray(parsed))throw new SyntaxErrorLike('not object');return parsed as McMarketDetailResult['config']}
const normalizeManualConfig=(config:McMarketDetailResult['config'])=>({transport:config.transport,command:config.command||undefined,args:config.args||undefined,url:config.url||undefined,envSecretRefs:config.envSecretRefs||undefined})
