import React,{useCallback,useEffect,useMemo,useRef,useState}from'react'
import{mcBridge,mcpBridge,type McpBridge}from'../bridge/client'
import type{Mcp6PresetsListResult,McpListResult}from'../generated/bridge'
import{leftoverArchivedMcp,leftoverArchivedNames,mcpCountsAsInstalled}from'../settings/leftoverMcp'
import{Dialog}from'../ui/Dialog'
import{McpCredentialDialog}from'./McpCredentialDialog'
import{McpSecurityReviewDialog}from'./McpSecurityReviewDialog'

type Preset=Mcp6PresetsListResult['items'][number]
type Endpoint=McpListResult['endpoints'][number]
type View='installed'|'market'

const STATE_LABEL:Record<string,string>={probe:'连接中',ready:'已连接',degraded:'连接异常',revoked:'已删除',quarantined:'连接失败'}
const CHROME_ATTACH_PRESETS=new Set(['chrome-devtools'])
const chromeAttachNote='人装才生效，不是默认电脑控制，月伴不会自动安装。默认网页自动化请用 Playwright。'
const GDRIVE_SETUP='https://github.com/modelcontextprotocol/servers-archived/tree/main/src/gdrive'
const MANUAL_TEMPLATE=`{
  "mcpServers": {
    "example": {
      "command": "npx",
      "args": ["-y", "@modelcontextprotocol/server-memory"]
    }
  }
}`

const statusOf=(item:Endpoint)=>{
 if(!item.enabled)return{id:'off',label:'未连接'}
 return{id:item.state,label:STATE_LABEL[item.state]??item.state}
}
const packageName=(args?:string[])=>args?.find(item=>item.startsWith('@')||item.includes('mcp'))??''
const leftoverGdrive=(args?:string[])=>(args??[]).some(item=>item.includes('server-gdrive'))
const needsUv=(code?:string,message?:string)=>code==='MCP_UV_UNAVAILABLE'||/未找到 uv/.test(message??'')
const UV_POLL_MS=400
const installedKey=(item:Endpoint)=>item.transport==='https'?`https|${item.url}`:`${item.command??''}|${packageName(item.args)||item.args?.[0]||''}`
const presetKey=(preset:Preset)=>preset.transport==='https'?`https|${preset.url}`:`${preset.command}|${packageName(preset.args)||preset.args[0]||''}`
const presetIdForEndpoint=(item:Endpoint,presets:readonly Preset[])=>presets.find(preset=>presetKey(preset)===installedKey(item))?.id??''
function endpointTitle(item:Endpoint,presets:readonly Preset[]):string{
 const fromPreset=presets.find(preset=>presetKey(preset)===installedKey(item))?.name
 const raw=item.displayName?.trim()
 return raw||fromPreset||packageName(item.args)||item.command?.trim()||item.url?.trim()||item.endpointId||'未命名 MCP'
}

function mcpUserError(err:unknown,fallback:string):string{
 const detail=err instanceof Error?err.message.trim():''
 return /[\u4e00-\u9fff]/.test(detail)?detail:fallback
}

type ParsedServer={name:string;transport:'stdio'|'https';command?:string;args?:string[];url?:string}

function parseManualJson(raw:string):ParsedServer[]{
 const parsed=JSON.parse(raw)as Record<string,unknown>
 if(parsed.mcpServers&&typeof parsed.mcpServers==='object'&&!Array.isArray(parsed.mcpServers)){
  return Object.entries(parsed.mcpServers as Record<string,Record<string,unknown>>).map(([name,cfg])=>{
   if(cfg.env||cfg.headers||cfg.auth)throw new Error(`${name}：请先保存服务器，再通过“凭据”按钮配置；不要在 JSON 中填写凭据`)
   const url=typeof cfg.url==='string'?cfg.url:typeof cfg.serverUrl==='string'?cfg.serverUrl:''
   const command=typeof cfg.command==='string'?cfg.command:undefined
   const args=Array.isArray(cfg.args)?cfg.args.map(String):undefined
   const transport=url.startsWith('https://')?'https' as const:(cfg.transport==='https'?'https' as const:'stdio' as const)
   return{name,transport,command,args,url:url||undefined}
  })
 }
 const url=typeof parsed.url==='string'?parsed.url:''
 if(parsed.env||parsed.headers||parsed.auth)throw new Error('请先保存服务器，再通过“凭据”按钮配置；不要在 JSON 中填写凭据')
 const command=typeof parsed.command==='string'?parsed.command:undefined
 const args=Array.isArray(parsed.args)?parsed.args.map(String):undefined
 const transport=url.startsWith('https://')?'https' as const:(parsed.transport==='https'?'https' as const:'stdio' as const)
 return[{name:typeof parsed.name==='string'?parsed.name:'manual',transport,command,args,url:url||undefined}]
}

export function McpPage({bridge=mcpBridge}:{bridge?:McpBridge}):React.JSX.Element{
 const[view,setView]=useState<View>('market')
 const[presets,setPresets]=useState<Preset[]>([])
 const[endpoints,setEndpoints]=useState<Endpoint[]>([])
 const[query,setQuery]=useState('')
 const[category,setCategory]=useState('')
 const[busy,setBusy]=useState('')
 const[error,setError]=useState('')
 const[notice,setNotice]=useState('')
 const[createOpen,setCreateOpen]=useState(false)
 const[json,setJson]=useState(MANUAL_TEMPLATE)
 const[riskConfirmed,setRiskConfirmed]=useState(false)
 const[removeTarget,setRemoveTarget]=useState<Endpoint|null>(null)
 const[cleanupOpen,setCleanupOpen]=useState(false)
 const[credentialTarget,setCredentialTarget]=useState<Endpoint|null>(null)
 const[reviewTarget,setReviewTarget]=useState<Endpoint|null>(null)
 const[uvProgress,setUvProgress]=useState<{state:'idle'|'downloading'|'ready'|'failed';percent:number;lastError?:string}|undefined>()
 const leftoverRedirected=useRef(false)

 const load=useCallback(async()=>{
  try{
   const[presetResult,list]=await Promise.all([bridge.presets(),bridge.list({})])
   setPresets(presetResult.items);setEndpoints(list.endpoints)
   if(!leftoverRedirected.current&&leftoverArchivedNames(list.endpoints).length){
    leftoverRedirected.current=true
    setView('installed')
   }
   return list.endpoints
  }catch(e){setError(mcpUserError(e,'MCP 清单加载失败'))}
 },[bridge])
 useEffect(()=>{void load()},[load])

 const leftoverNames=useMemo(()=>leftoverArchivedNames(endpoints),[endpoints])
 const leftoverEndpoints=useMemo(()=>endpoints.filter(item=>item.state!=='revoked'&&leftoverArchivedMcp(item.args,item.url).length>0),[endpoints])
 const installedKeys=useMemo(()=>new Set(endpoints.filter(mcpCountsAsInstalled).map(installedKey)),[endpoints])
 const categories=useMemo(()=>{const map=new Map<string,number>();for(const preset of presets)map.set(preset.category,(map.get(preset.category)??0)+1);return[...map.entries()]},[presets])
 const visiblePresets=useMemo(()=>{const q=query.trim().toLowerCase();return presets.filter(preset=>(!category||preset.category===category)&&(!q||`${preset.name} ${preset.description} ${preset.category} ${preset.args.join(' ')}`.toLowerCase().includes(q)))},[category,presets,query])
 const visibleInstalled=useMemo(()=>{const q=query.trim().toLowerCase();return endpoints.filter(item=>item.state!=='revoked'&&(!q||`${item.displayName??''} ${item.command??''} ${item.endpointId} ${item.url??''} ${leftoverArchivedMcp(item.args,item.url).join(' ')}`.toLowerCase().includes(q))).sort((a,b)=>(leftoverArchivedMcp(a.args,a.url).length?0:1)-(leftoverArchivedMcp(b.args,b.url).length?0:1))},[endpoints,query])
 const connected=visibleInstalled.filter(item=>item.enabled&&item.state==='ready').length
 const failed=visibleInstalled.filter(item=>item.state==='quarantined'||item.state==='degraded').length

 const resolveArgs=(preset:Preset,value:string)=>preset.args.map(item=>item===preset.argPlaceholder?value.trim().replaceAll('\\','/'):item)
 const installPreset=async(preset:Preset,value?:string)=>{
  const resolved=value?.trim()||preset.argDefault||''
  if(preset.needsArgs&&!resolved){setError(`「${preset.name}」无法一键安装。`);return}
  setBusy(preset.id);setError('');setNotice('')
  try{
   const added=await bridge.add({origin:'manual',transport:preset.transport,...(preset.transport==='https'?{url:preset.url}:{command:preset.command,args:resolveArgs(preset,resolved)}),riskConfirmed:true,configureOnly:Boolean(preset.needsCredential),requestId:crypto.randomUUID()})
   if(!preset.needsCredential && added.state!=='ready'){
    const refreshed=await load()
    const row=refreshed?.find(item=>item.endpointId===added.endpointId)
    setError(`「${preset.name}」${row?.diagnosticMessage||'未能完成连接。请检查 uv / Node 运行环境及服务器版本后重试。'}`)
    return
   }
   if(!preset.needsCredential)await bridge.toggle({endpointId:added.endpointId,enabled:true})
   setView('installed')
   const refreshed=await load()
   if(preset.needsCredential){
    setNotice(`已保存「${preset.name}」，配置凭据后连接`)
    const target=refreshed?.find(item=>item.endpointId===added.endpointId)
    if(target&&bridge.credentialSet)setCredentialTarget(target)
   }else setNotice(`已安装「${preset.name}」`)
  }catch(e){await load();setError(mcpUserError(e,`${preset.name} 安装失败`))}finally{setBusy('')}
 }
 const reconnect=async(item:Endpoint)=>{
  const title=endpointTitle(item,presets)
  setBusy(item.endpointId);setError('');setNotice('')
  try{
   if(!item.enabled)await bridge.toggle({endpointId:item.endpointId,enabled:true})
   const health=await bridge.health({endpointId:item.endpointId})
   if(health.state==='ready')setNotice(`${title}：已连接${health.latencyMs?` · ${health.latencyMs}ms`:''}`)
   else setError(`${title}：${health.diagnosticMessage||'未能建立连接，请检查启动配置后重试。'}`)
   await load()
  }catch(e){setError(`${title}：${mcpUserError(e,'重新连接失败')}`)}finally{await load();setBusy('')}
 }
 const remove=async()=>{
  if(!removeTarget)return
  setBusy(removeTarget.endpointId);setError('');setNotice('')
  try{
   const token=await mcBridge.confirmToken({method:'mc.connector.uninstall',target:removeTarget.endpointId})
   await mcBridge.uninstall({endpointId:removeTarget.endpointId,confirmToken:token.confirmToken})
   setNotice(`已删除「${endpointTitle(removeTarget,presets)}」`);setRemoveTarget(null);await load()
  }catch(e){setError(mcpUserError(e,'删除失败'))}finally{setBusy('')}
 }
 const cleanupLeftovers=async()=>{
  if(!leftoverEndpoints.length)return
  setBusy('cleanup');setError('');setNotice('')
  try{
   for(const item of leftoverEndpoints){
    const token=await mcBridge.confirmToken({method:'mc.connector.uninstall',target:item.endpointId})
    await mcBridge.uninstall({endpointId:item.endpointId,confirmToken:token.confirmToken})
   }
   setNotice(`已卸载 ${leftoverEndpoints.length} 个无法使用的下架 MCP`);setCleanupOpen(false);await load()
  }catch(e){setError(mcpUserError(e,'卸载下架 MCP 失败'))}finally{setBusy('')}
 }
 const installUv=async()=>{
  if(!bridge.uvInstall||busy==='uv')return
  setBusy('uv');setError('');setNotice('')
  const pump=async()=>{
   try{
    const snap=await bridge.uvInstall!()
    setUvProgress({state:snap.state,percent:snap.percent,lastError:snap.lastError})
    if(snap.state==='downloading'){window.setTimeout(()=>{void pump()},UV_POLL_MS);return}
    if(snap.state==='failed'){setError(snap.lastError||'uv 安装失败');setBusy('');return}
    setNotice('uv 已安装，可重新连接 Python MCP')
    await load()
    setBusy('')
   }catch(e){setError(mcpUserError(e,'uv 安装失败'));setBusy('')}
  }
  await pump()
 }
 const saveManual=async()=>{
  setBusy('manual');setError('');setNotice('')
  try{
   const servers=parseManualJson(json)
   if(!servers.length)throw new Error('JSON 里没有可保存的 MCP')
   for(const server of servers){
    if(server.transport==='https'){
     if(!server.url?.startsWith('https://'))throw new Error(`${server.name} 需要 https:// URL`)
     await bridge.add({origin:'manual',transport:'https',url:server.url,riskConfirmed:true,configureOnly:true,requestId:crypto.randomUUID()})
    }else{
     if(!server.command||!server.args?.length)throw new Error(`${server.name} 需要 command 和 args`)
     await bridge.add({origin:'manual',transport:'stdio',command:server.command,args:server.args,riskConfirmed:true,configureOnly:true,requestId:crypto.randomUUID()})
    }
   }
   setCreateOpen(false);setRiskConfirmed(false);setNotice(`已保存 ${servers.length} 个 MCP，请配置所需凭据后连接`);await load();setView('installed')
  }catch(e){await load();setError(mcpUserError(e,'保存失败：请使用 mcpServers 或 command/args JSON'))}finally{setBusy('')}
 }

 return <main className="skill-center mcp-page">
  <header className="skill-center-header">
   <div><h1>MCP</h1><p>已安装 {endpoints.filter(item=>item.state!=='revoked').length} 个 · 市场 {presets.length} 个可点选安装 · 已连接 {connected} · 失败 {failed}</p><small>市场只保留一键安装的免费服务，点加号即可，不用填密钥或路径。需要 Token、付费或自备连接串的服务已下架。也可以手动填写 JSON。Chrome DevTools 要人点安装，不是默认电脑控制。</small></div>
   <button className="primary skill-chat-create" onClick={()=>setCreateOpen(true)}>＋ 创建 MCP</button>
  </header>
  <section className="skill-center-toolbar">
   <div className="skill-status-tabs" role="tablist" aria-label="MCP 视图">
    <button type="button" role="tab" aria-selected={view==='installed'} onClick={()=>setView('installed')}>已安装（{endpoints.filter(item=>item.state!=='revoked').length}）</button>
    <button type="button" role="tab" aria-selected={view==='market'} onClick={()=>setView('market')}>MCP 市场</button>
   </div>
   <label className="skill-search">搜索<input value={query} onChange={e=>setQuery(e.target.value)} placeholder={view==='market'?'名称或分类':'已安装 MCP'}/></label>
   <button aria-label="刷新 MCP" onClick={()=>void load()}>↻</button>
  </section>
  {error&&<p className="skill-center-error" role="alert">{error}{needsUv('',error)&&bridge.uvInstall?<button type="button" className="ui-btn" disabled={Boolean(busy)} onClick={()=>void installUv()}>{uvProgress?.state==='downloading'?`下载中 ${uvProgress.percent}%`:'安装 uv'}</button>:null}</p>}
  {leftoverNames.length>0&&<p role="status" className="notice" style={{color:'var(--err)'}}>检测到已下架且无法继续使用的 MCP（{leftoverNames.join('、')}）。<button type="button" className="ui-btn" disabled={Boolean(busy)} onClick={()=>setCleanupOpen(true)}>一键卸载</button></p>}
  {notice&&<p role="status">{notice}</p>}
  {view==='market'?<>
   <nav className="skill-market-cats" aria-label="MCP 分类">
    <button type="button" aria-pressed={!category} onClick={()=>setCategory('')}>全部<small>{presets.length}</small></button>
    {categories.map(([id,count])=><button type="button" key={id} aria-pressed={category===id} onClick={()=>setCategory(id)}>{id}<small>{count}</small></button>)}
   </nav>
   <section className="skill-market-shelf" aria-label="可安装 MCP">
    {visiblePresets.length?<div className="skill-market mcp-market">{visiblePresets.map(preset=>{
     const installed=installedKeys.has(presetKey(preset))
     return <article className={`skill-market-card ${installed?'is-installed':''}`} key={preset.id}>
      <header>
       <span className="skill-market-glyph" aria-hidden="true">{preset.name.slice(0,1)}</span>
       <div><b>{preset.name}</b><small>{preset.category} · {preset.transport==='https'?'远程服务':preset.command}</small></div>
       {installed?<span className="skill-market-installed-row"><span className="skill-market-installed">已安装</span><button type="button" className="skill-market-remove" aria-label={`卸载 ${preset.name}`} disabled={Boolean(busy)} onClick={()=>{const item=endpoints.find(endpoint=>endpoint.state!=='revoked'&&installedKey(endpoint)===presetKey(preset));if(item)setRemoveTarget(item)}}>卸载</button></span>:<button type="button" className="skill-market-add" aria-label={`安装 ${preset.name}`} disabled={Boolean(busy)} onClick={()=>void installPreset(preset)}>{busy===preset.id?'…':'＋'}</button>}
      </header>
      <p>{preset.description}</p>
      {CHROME_ATTACH_PRESETS.has(preset.id)&&<p className="setting-desc">{chromeAttachNote}</p>}
      {preset.setupUrl&&<a href={preset.setupUrl} target="_blank" rel="noreferrer">官方配置说明</a>}
      <footer><small>{preset.url||preset.args.join(' ')}</small></footer>
     </article>
    })}</div>:<div className="empty"><b>没有匹配的 MCP</b><span>换个分类或关键字再试。</span></div>}
   </section>
  </>:<section className="expert-card-list" aria-label="已安装 MCP">
   {visibleInstalled.length?visibleInstalled.map(item=>{const status=statusOf(item);const presetId=presetIdForEndpoint(item,presets);const preset=presets.find(entry=>entry.id===presetId);const leftover=leftoverArchivedMcp(item.args,item.url);const gdrive=leftover.includes('Google Drive')||leftoverGdrive(item.args);const needsCredential=Boolean((preset?.needsCredential||gdrive)&&!item.credentialConfigured);const title=endpointTitle(item,presets);return <article className="expert-card mcp-card" key={item.endpointId||title}>
    <div className="expert-card-main">
     <b>{title}</b>
     {leftover.length>0&&<small>已下架 · {leftover.join('、')}</small>}
     <small>{item.transport==='https'?'远程 HTTPS':'本地 stdio'} · {presetId?`策展预置 ${presetId}`:(item.origin==='market'?'市场':'手动')} · {item.command?`${item.command} ${item.args?.join(' ')??''}`:item.url||'未记录启动命令'}</small>
     {item.lockedArgs?.length? <small>已锁定：{item.lockedArgs.join(' ')}</small>:null}
     {needsCredential&&<p className="setting-desc">此服务需要凭据。{gdrive?'Google Drive 需要先完成 OAuth 授权，再填写 GDRIVE_CREDENTIALS_PATH；普通文件目录不能代替授权。':'请先完成服务授权，再配置凭据并重新连接。'}</p>}
     {item.state!=='ready'&&item.diagnosticMessage&&<p className="setting-desc" role="status">{item.diagnosticMessage}</p>}
     {needsUv(item.diagnosticCode,item.diagnosticMessage)&&bridge.uvInstall&&<button type="button" className="ui-btn" disabled={Boolean(busy)} onClick={()=>void installUv()}>{busy==='uv'||uvProgress?.state==='downloading'?`下载 uv ${uvProgress?.percent??0}%`:'安装 uv'}</button>}
     {(preset?.setupUrl||gdrive)&&<a href={preset?.setupUrl||GDRIVE_SETUP} target="_blank" rel="noreferrer">官方配置说明</a>}
    </div>
    <i className={`skill-status status-${status.id==='ready'?'published':status.id==='off'||status.id==='degraded'?'disabled':status.id==='quarantined'?'deprecated':'draft'}`}>{status.label}</i>
    <div className="expert-card-actions">
     {bridge.credentialSet&&<button type="button" className="ui-btn" disabled={Boolean(busy)} onClick={()=>setCredentialTarget(item)}>凭据</button>}
     {item.state==='quarantined'&&bridge.securityReview&&<button type="button" className="ui-btn" disabled={Boolean(busy)} onClick={()=>setReviewTarget(item)}>复核变更</button>}
     <button type="button" className="ui-btn" disabled={Boolean(busy)} onClick={()=>void reconnect(item)}>{busy===item.endpointId?'连接中…':'重新连接'}</button>
     <button type="button" className="ui-btn" disabled={Boolean(busy)} onClick={()=>setRemoveTarget(item)}>删除</button>
    </div>
   </article>}):<div className="empty"><b>还没有安装 MCP</b><span>去「MCP 市场」点加号，或点击「创建 MCP」粘贴 JSON。</span></div>}
  </section>}
  <Dialog open={createOpen} wide title="创建 MCP" description="粘贴 Cursor / Claude 风格的 mcpServers JSON，或单条 command/args。保存后进入已安装清单。" onClose={()=>{if(!busy)setCreateOpen(false)}}>
   <form className="editor-dialog" onSubmit={e=>{e.preventDefault();void saveManual()}}>
    <label>配置文件 JSON<textarea className="skill-manifest-editor" rows={14} value={json} onChange={e=>setJson(e.target.value)} aria-label="MCP JSON"/></label>
    <p>本地包支持 npx 包名（可带 -y）和 uvx 包名，首次连接后锁定精确版本；远程支持标准 MCP HTTPS 地址，并兼容原有服务。需认证时先保存，再使用“凭据”配置。</p>
    <label className="mcp-trust"><input type="checkbox" checked={riskConfirmed} onChange={e=>setRiskConfirmed(e.target.checked)}/> 我确认信任此服务器来源，理解其工具将在本机运行</label>
    {error&&<p className="skill-center-error" role="alert">{error}</p>}
    <div className="dialog-actions"><button type="button" disabled={Boolean(busy)} onClick={()=>setCreateOpen(false)}>取消</button><button className="primary" disabled={Boolean(busy)||!json.trim()||!riskConfirmed}>{busy==='manual'?'保存中…':'保存'}</button></div>
   </form>
  </Dialog>
  {credentialTarget&&bridge.credentialSet&&<McpCredentialDialog endpoint={credentialTarget} suggestedEnvs={presets.find(preset=>presetKey(preset)===installedKey(credentialTarget))?.credentialEnvs??(leftoverGdrive(credentialTarget.args)?['GDRIVE_CREDENTIALS_PATH']:undefined)} save={bridge.credentialSet} onClose={()=>setCredentialTarget(null)} onSaved={()=>{setNotice('凭据已更新，请重新连接以验证');void load()}}/>}
  {reviewTarget&&bridge.securityReview&&<McpSecurityReviewDialog endpoint={reviewTarget} review={bridge.securityReview} onClose={()=>setReviewTarget(null)} onSaved={()=>{setNotice('变更已确认，请重新连接');void load()}}/>}
  <Dialog open={cleanupOpen} title="卸载无法使用的 MCP" description={`将删除 ${leftoverNames.join('、')}。这些服务已下架或必须单独授权，无法在一键市场中继续使用。`} onClose={()=>{if(!busy)setCleanupOpen(false)}}>
   <div className="dialog-actions"><button type="button" disabled={Boolean(busy)} onClick={()=>setCleanupOpen(false)}>取消</button><button className="danger" disabled={Boolean(busy)} onClick={()=>void cleanupLeftovers()}>{busy==='cleanup'?'卸载中…':'确认卸载'}</button></div>
  </Dialog>
  <Dialog open={!!removeTarget} title={`删除「${removeTarget?endpointTitle(removeTarget,presets):''}」`} description="删除后需重新从市场或 JSON 安装才能再用。" onClose={()=>{if(!busy)setRemoveTarget(null)}}>
   <div className="dialog-actions"><button type="button" disabled={Boolean(busy)} onClick={()=>setRemoveTarget(null)}>取消</button><button className="danger" disabled={Boolean(busy)} onClick={()=>void remove()}>{busy===removeTarget?.endpointId?'删除中…':'确认删除'}</button></div>
  </Dialog>
 </main>
}

export function __parseManualJsonForTest(raw:string){return parseManualJson(raw)}
