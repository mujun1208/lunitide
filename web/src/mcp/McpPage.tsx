import React,{useCallback,useEffect,useMemo,useState}from'react'
import{mcBridge,mcpBridge,type McpBridge}from'../bridge/client'
import type{Mcp6PresetsListResult,McpListResult}from'../generated/bridge'
import{Dialog}from'../ui/Dialog'

type Preset=Mcp6PresetsListResult['items'][number]
type Endpoint=McpListResult['endpoints'][number]
type View='installed'|'market'

const STATE_LABEL:Record<string,string>={probe:'连接中',ready:'已连接',degraded:'连接异常',revoked:'已删除',quarantined:'连接失败'}
const CHROME_ATTACH_PRESETS=new Set(['chrome-devtools','browsermcp'])
const chromeAttachNote='人装才生效，不是默认电脑控制，月伴不会自动安装。默认网页自动化请用 Playwright。'
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
const installedKey=(item:Endpoint)=>`${item.command??''}|${packageName(item.args)}`
const presetKey=(preset:Preset)=>`${preset.command}|${packageName(preset.args)}`
const presetIdForEndpoint=(item:Endpoint,presets:readonly Preset[])=>presets.find(preset=>presetKey(preset)===installedKey(item))?.id??''

type ParsedServer={name:string;transport:'stdio'|'https';command?:string;args?:string[];url?:string}

function parseManualJson(raw:string):ParsedServer[]{
 const parsed=JSON.parse(raw)as Record<string,unknown>
 if(parsed.mcpServers&&typeof parsed.mcpServers==='object'&&!Array.isArray(parsed.mcpServers)){
  return Object.entries(parsed.mcpServers as Record<string,Record<string,unknown>>).map(([name,cfg])=>{
   const url=typeof cfg.url==='string'?cfg.url:typeof cfg.serverUrl==='string'?cfg.serverUrl:''
   const command=typeof cfg.command==='string'?cfg.command:undefined
   const args=Array.isArray(cfg.args)?cfg.args.map(String):undefined
   const transport=url.startsWith('https://')?'https' as const:(cfg.transport==='https'?'https' as const:'stdio' as const)
   return{name,transport,command,args,url:url||undefined}
  })
 }
 const url=typeof parsed.url==='string'?parsed.url:''
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
 const[argDraft,setArgDraft]=useState<{id:string;value:string}|null>(null)
 const[removeTarget,setRemoveTarget]=useState<Endpoint|null>(null)

 const load=useCallback(async()=>{
  try{
   const[presetResult,list]=await Promise.all([bridge.presets(),bridge.list({})])
   setPresets(presetResult.items);setEndpoints(list.endpoints)
  }catch(e){setError(e instanceof Error?e.message:'MCP 清单加载失败')}
 },[bridge])
 useEffect(()=>{void load()},[load])

 const installedKeys=useMemo(()=>new Set(endpoints.filter(item=>item.state!=='revoked').map(installedKey)),[endpoints])
 const categories=useMemo(()=>{const map=new Map<string,number>();for(const preset of presets)map.set(preset.category,(map.get(preset.category)??0)+1);return[...map.entries()]},[presets])
 const visiblePresets=useMemo(()=>{const q=query.trim().toLowerCase();return presets.filter(preset=>(!category||preset.category===category)&&(!q||`${preset.name} ${preset.description} ${preset.category} ${preset.args.join(' ')}`.toLowerCase().includes(q)))},[category,presets,query])
 const visibleInstalled=useMemo(()=>{const q=query.trim().toLowerCase();return endpoints.filter(item=>item.state!=='revoked'&&(!q||`${item.displayName??''} ${item.command??''} ${item.endpointId}`.toLowerCase().includes(q)))},[endpoints,query])
 const connected=visibleInstalled.filter(item=>item.enabled&&item.state==='ready').length
 const failed=visibleInstalled.filter(item=>item.state==='quarantined'||item.state==='degraded').length

 const resolveArgs=(preset:Preset,value:string)=>preset.args.map(item=>item===preset.argPlaceholder?value.trim().replaceAll('\\','/'):item)
 const installPreset=async(preset:Preset,value?:string)=>{
  const resolved=value?.trim()||preset.argDefault||''
  if(preset.needsArgs&&!resolved){setArgDraft({id:preset.id,value:''});return}
  setBusy(preset.id);setError('');setNotice('')
  try{
   const added=await bridge.add({origin:'manual',transport:'stdio',command:preset.command,args:resolveArgs(preset,resolved),riskConfirmed:true,requestId:crypto.randomUUID()})
   try{await bridge.toggle({endpointId:added.endpointId,enabled:true})}catch{/* still registered */}
   setArgDraft(null);setNotice(`已安装「${preset.name}」`);await load();setView('installed')
  }catch(e){setError(e instanceof Error?e.message:`${preset.name} 安装失败`)}finally{setBusy('')}
 }
 const reconnect=async(item:Endpoint)=>{
  setBusy(item.endpointId);setError('');setNotice('')
  try{
   if(!item.enabled)await bridge.toggle({endpointId:item.endpointId,enabled:true})
   const health=await bridge.health({endpointId:item.endpointId})
   setNotice(`${item.displayName||item.endpointId}：${STATE_LABEL[health.state]??health.state}${health.latencyMs?` · ${health.latencyMs}ms`:''}`)
   await load()
  }catch(e){setError(e instanceof Error?e.message:'重新连接失败')}finally{setBusy('')}
 }
 const remove=async()=>{
  if(!removeTarget)return
  setBusy(removeTarget.endpointId);setError('');setNotice('')
  try{
   const token=await mcBridge.confirmToken({method:'mc.connector.uninstall',target:removeTarget.endpointId})
   await mcBridge.uninstall({endpointId:removeTarget.endpointId,confirmToken:token.confirmToken})
   setNotice(`已删除「${removeTarget.displayName||removeTarget.endpointId}」`);setRemoveTarget(null);await load()
  }catch(e){setError(e instanceof Error?e.message:'删除失败')}finally{setBusy('')}
 }
 const saveManual=async()=>{
  setBusy('manual');setError('');setNotice('')
  try{
   const servers=parseManualJson(json)
   if(!servers.length)throw new Error('JSON 里没有可保存的 MCP')
   for(const server of servers){
    if(server.transport==='https'){
     if(!server.url?.startsWith('https://'))throw new Error(`${server.name} 需要 https:// URL`)
     const added=await bridge.add({origin:'manual',transport:'https',url:server.url,riskConfirmed:true,requestId:crypto.randomUUID()})
     try{await bridge.toggle({endpointId:added.endpointId,enabled:true})}catch{/* still registered */}
    }else{
     if(!server.command||!server.args?.length)throw new Error(`${server.name} 需要 command 和 args`)
     const added=await bridge.add({origin:'manual',transport:'stdio',command:server.command,args:server.args,riskConfirmed:true,requestId:crypto.randomUUID()})
     try{await bridge.toggle({endpointId:added.endpointId,enabled:true})}catch{/* still registered */}
    }
   }
   setCreateOpen(false);setRiskConfirmed(false);setNotice(`已保存 ${servers.length} 个 MCP`);await load();setView('installed')
  }catch(e){setError(e instanceof Error?e.message:'保存失败：请使用 mcpServers 或 command/args JSON')}finally{setBusy('')}
 }

 return <main className="skill-center mcp-page">
  <header className="skill-center-header">
   <div><h1>MCP</h1><p>已安装 {endpoints.filter(item=>item.state!=='revoked').length} 个 · 市场 {presets.length} 个可点选安装 · 已连接 {connected} · 失败 {failed}</p><small>点加号安装成熟 MCP；也可以手动填写 JSON 接到你的清单。同一市场项只安装一次。Chrome DevTools / Browser MCP 要人点安装，不是默认电脑控制。</small></div>
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
  {error&&<p className="skill-center-error" role="alert">{error}</p>}
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
       <div><b>{preset.name}</b><small>{preset.category} · {preset.command}</small></div>
       {installed?<span className="skill-market-installed">已安装</span>:<button type="button" className="skill-market-add" aria-label={`安装 ${preset.name}`} disabled={Boolean(busy)} onClick={()=>void installPreset(preset)}>{busy===preset.id?'…':'＋'}</button>}
      </header>
      <p>{preset.description}</p>
      {CHROME_ATTACH_PRESETS.has(preset.id)&&<p className="setting-desc">{chromeAttachNote}</p>}
      {argDraft?.id===preset.id&&!preset.argDefault&&<div className="mcp-arg-row"><input aria-label={`${preset.name} 参数`} placeholder={preset.argHint??'请输入参数'} value={argDraft.value} onChange={e=>setArgDraft({id:preset.id,value:e.target.value})}/><button className="primary" disabled={!argDraft.value.trim()||Boolean(busy)} onClick={()=>void installPreset(preset,argDraft.value)}>安装</button></div>}
      <footer><small>{preset.args.join(' ')}</small></footer>
     </article>
    })}</div>:<div className="empty"><b>没有匹配的 MCP</b><span>换个分类或关键字再试。</span></div>}
   </section>
  </>:<section className="expert-card-list" aria-label="已安装 MCP">
   {visibleInstalled.length?visibleInstalled.map(item=>{const status=statusOf(item);const presetId=presetIdForEndpoint(item,presets);return <article className="expert-card mcp-card" key={item.endpointId}>
    <div className="expert-card-main">
     <b>{item.displayName||packageName(item.args)||item.endpointId}</b>
     <small>{item.transport==='https'?'远程 HTTPS':'本地 stdio'} · {presetId?`策展预置 ${presetId}`:(item.origin==='market'?'市场':'手动')} · {item.command?`${item.command} ${item.args?.join(' ')??''}`:item.url}</small>
    </div>
    <i className={`skill-status status-${status.id==='ready'?'published':status.id==='off'||status.id==='degraded'?'disabled':status.id==='quarantined'?'deprecated':'draft'}`}>{status.label}</i>
    <div className="expert-card-actions">
     <button type="button" className="ui-btn" disabled={Boolean(busy)} onClick={()=>void reconnect(item)}>重新连接</button>
     <button type="button" className="ui-btn" disabled={Boolean(busy)} onClick={()=>setRemoveTarget(item)}>删除</button>
    </div>
   </article>}):<div className="empty"><b>还没有安装 MCP</b><span>去「MCP 市场」点加号，或点击「创建 MCP」粘贴 JSON。</span></div>}
  </section>}
  <Dialog open={createOpen} wide title="创建 MCP" description="粘贴 Cursor / Claude 风格的 mcpServers JSON，或单条 command/args。保存后进入已安装清单。" onClose={()=>{if(!busy)setCreateOpen(false)}}>
   <form className="editor-dialog" onSubmit={e=>{e.preventDefault();void saveManual()}}>
    <label>配置文件 JSON<textarea className="skill-manifest-editor" rows={14} value={json} onChange={e=>setJson(e.target.value)} aria-label="MCP JSON"/></label>
    <label className="mcp-trust"><input type="checkbox" checked={riskConfirmed} onChange={e=>setRiskConfirmed(e.target.checked)}/> 我确认信任此服务器来源，理解其工具将在本机运行</label>
    {error&&<p className="skill-center-error" role="alert">{error}</p>}
    <div className="dialog-actions"><button type="button" disabled={Boolean(busy)} onClick={()=>setCreateOpen(false)}>取消</button><button className="primary" disabled={Boolean(busy)||!json.trim()||!riskConfirmed}>{busy==='manual'?'保存中…':'保存'}</button></div>
   </form>
  </Dialog>
  <Dialog open={!!removeTarget} title={`删除「${removeTarget?.displayName||removeTarget?.endpointId||''}」`} description="删除后需重新从市场或 JSON 安装才能再用。" onClose={()=>{if(!busy)setRemoveTarget(null)}}>
   <div className="dialog-actions"><button type="button" disabled={Boolean(busy)} onClick={()=>setRemoveTarget(null)}>取消</button><button className="danger" disabled={Boolean(busy)} onClick={()=>void remove()}>{busy===removeTarget?.endpointId?'删除中…':'确认删除'}</button></div>
  </Dialog>
 </main>
}

export function __parseManualJsonForTest(raw:string){return parseManualJson(raw)}
