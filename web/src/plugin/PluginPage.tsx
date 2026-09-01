import React,{useCallback,useEffect,useMemo,useState}from'react'
import{mcpBridge,pluginBridge,skillBridge,type McpBridge,type PluginBridge,type SkillBridge}from'../bridge/client'
import type{PluginListResult}from'../generated/bridge'
import{Dialog}from'../ui/Dialog'
import{CAPABILITY_PACKS,capabilityPack,exportCapabilityPackJSON,installCapabilityPack,isPackPluginId,mergedPackLedger,parseCapabilityPackJSON,uninstallCapabilityPack,type CapabilityPackSpec,type PackLedgerEntry}from'./capabilityPacks'
import{FILLER_PLUGIN,PLUGIN_MARKET,pluginHonestyLabel,pluginLogo,pluginOriginLabel,pluginTitle,type PluginCategory}from'./pluginMarket'

type Plugin=PluginListResult['plugins'][number]
type View='installed'|'market'
const KIND_LABEL:Record<string,string>={mcp:'MCP',skill:'技能',workflow:'工作流',template:'模板',tool:'工具','agent-pack':'AgentPack'}
const STATE_LABEL:Record<Plugin['state'],string>={installed:'已安装未启用',enabled:'已安装',disabled:'未启用',quarantined:'安装失败',uninstalled:'已卸载'}
const isFiller=(pluginId:string)=>FILLER_PLUGIN.test(pluginId)

export function PluginPage({bridge=pluginBridge,skills=skillBridge,mcp=mcpBridge,onCreateInChat,highlightId}:{bridge?:PluginBridge;skills?:SkillBridge;mcp?:McpBridge;onCreateInChat?:()=>void;highlightId?:string}):React.JSX.Element{
 const[view,setView]=useState<View>('market')
 const[plugins,setPlugins]=useState<Plugin[]>([])
 const[query,setQuery]=useState('')
 const[category,setCategory]=useState<PluginCategory|''>('')
 const[busy,setBusy]=useState('')
 const[error,setError]=useState('')
 const[notice,setNotice]=useState('')
 const[manualOpen,setManualOpen]=useState(false)
 const[manifest,setManifest]=useState('{\n  "pluginId": "my-plugin",\n  "semver": "1.0.0",\n  "publisher": "local",\n  "kind": "tool",\n  "permissions": {}\n}')
 const[entrypoint,setEntrypoint]=useState('pack://manifest')
 const[importedPacks,setImportedPacks]=useState<CapabilityPackSpec[]>([])
 const[workspaceId,setWorkspaceId]=useState('chat')
 const[removeTarget,setRemoveTarget]=useState<Plugin|null>(null)
 const[removePackId,setRemovePackId]=useState('')
 const[packLedger,setPackLedger]=useState<PackLedgerEntry[]>([])

 const load=useCallback(async()=>{try{const listed=(await bridge.list()).plugins;setPlugins(listed);setPackLedger(mergedPackLedger(listed));setError('')}catch(e){setError(e instanceof Error?e.message:'插件清单加载失败')}},[bridge])
 useEffect(()=>{void load()},[load])

 const byId=useMemo(()=>new Map(plugins.map(item=>[item.pluginId,item])),[plugins])
 const shelfPacks=useMemo(()=>{const extra=importedPacks.filter(pack=>!CAPABILITY_PACKS.some(item=>item.id===pack.id));return [...CAPABILITY_PACKS,...extra]},[importedPacks])
 const installedVisible=useMemo(()=>{const q=query.trim().toLowerCase();return plugins.filter(item=>item.state!=='uninstalled'&&!isFiller(item.pluginId)&&(!q||`${pluginTitle(item.pluginId)} ${item.pluginId} ${item.kind} ${item.state}`.toLowerCase().includes(q)))},[plugins,query])
 const chatPacks=useMemo(()=>installedVisible.filter(item=>isPackPluginId(item.pluginId)),[installedVisible])
 const failed=installedVisible.filter(item=>item.state==='quarantined').length
 const enabled=installedVisible.filter(item=>item.state==='enabled').length
 const categories=useMemo(()=>{const map=new Map<PluginCategory,number>();for(const item of PLUGIN_MARKET)map.set(item.category,(map.get(item.category)??0)+1);return[...map.entries()]},[])
 const visibleMarket=useMemo(()=>{const q=query.trim().toLowerCase();return PLUGIN_MARKET.filter(item=>(!category||item.category===category)&&(!q||`${item.name} ${item.description} ${item.category}`.toLowerCase().includes(q)))},[category,query])

 const findPack=(id:string)=>shelfPacks.find(item=>item.id===id)??capabilityPack(id)
 const enable=async(pluginId:string)=>{
  const pack=findPack(pluginId)
  if(pack){
   setBusy(pluginId);setError('');setNotice('')
   try{
    const result=await installCapabilityPack(pack,{skills,mcp,plugins:bridge,installed:plugins})
    setNotice(result.ok?`已安装「${pack.name}」：${result.notes.join('；')}`:`「${pack.name}」未装完：${result.notes.join('；')}`)
    await load();setView('installed')
   }catch(e){setError(e instanceof Error?e.message:'能力包安装失败')}finally{setBusy('')}
   return
  }
  const hit=byId.get(pluginId)
  if(!hit){setError(`还没有「${pluginTitle(pluginId)}」的安装记录`);return}
  setBusy(pluginId);setError('');setNotice('')
  try{
   await bridge.toggle({installId:hit.installId,enabled:hit.state!=='enabled'})
   setNotice(hit.state==='enabled'?`已停用「${pluginTitle(pluginId)}」`:`已启用「${pluginTitle(pluginId)}」`)
   await load();if(hit.state!=='enabled')setView('installed')
  }catch(e){setError(e instanceof Error?e.message:'安装失败')}finally{setBusy('')}
 }
 const remove=async()=>{
  if(!removeTarget)return
  setBusy(removeTarget.installId);setError('');setNotice('')
  try{
   const {confirmToken}=await bridge.confirmToken({installId:removeTarget.installId})
   await bridge.uninstall({installId:removeTarget.installId,confirmToken})
   setNotice(`已删除「${pluginTitle(removeTarget.pluginId)}」`);setRemoveTarget(null);await load()
  }catch(e){setError(e instanceof Error?e.message:'删除失败')}finally{setBusy('')}
 }
 const removePack=async()=>{
  const pack=findPack(removePackId)
  if(!pack)return
  setBusy(pack.id);setError('');setNotice('')
  try{
   const result=await uninstallCapabilityPack(pack,{mcp,plugins:bridge,listedPlugins:plugins})
   setNotice(`已撤下「${pack.name}」：${result.notes.join('；')}`)
   setRemovePackId('');await load()
  }catch(e){setError(e instanceof Error?e.message:'删除失败')}finally{setBusy('')}
 }
 const createManual=async()=>{
  setBusy('manual');setError('');setNotice('')
  try{
   const parsed=JSON.parse(manifest)as object
   const result=await bridge.devCreate({workspaceId:workspaceId.trim()||'chat',manifest:parsed,entrypoint:entrypoint.trim()})
   setNotice(result.state==='quarantined'?`插件已创建但安装失败（隔离校验未通过）`:`插件已创建（${result.state}）`)
   setManualOpen(false);await load();setView('installed')
  }catch(e){setError(e instanceof Error?e.message:'创建失败：清单需为合法 JSON')}finally{setBusy('')}
 }

 return <main className="skill-center plugin-page">
  <header className="skill-center-header">
   <div><h1>能力包</h1><p>组合包 {shelfPacks.length} 个 · 已启用门闸 {enabled} 个 · 失败 {failed} 个</p><small>能力包市场是本机捆绑目录，不是在线商店。组合包会安装技能和 MCP、打开门闸，不会执行外部脚本。要可调用技能去技能中心；要连服务器去 MCP。</small></div>
   <div className="view-actions"><button type="button" className="ui-btn" onClick={()=>{const raw=window.prompt('粘贴能力包 JSON');if(!raw)return;try{const pack=parseCapabilityPackJSON(raw);setImportedPacks(current=>[...current.filter(item=>item.id!==pack.id),pack]);setNotice(`已读入「${pack.name}」，不会执行脚本。`);setView('market')}catch(e){setError(e instanceof Error?e.message:'能力包 JSON 无效')}}}>导入 JSON</button><button type="button" className="ui-btn" onClick={()=>setManualOpen(true)}>手动填写</button>{onCreateInChat&&<button type="button" className="ui-btn primary" onClick={onCreateInChat}>＋ 创建能力包</button>}</div>
  </header>
  <section className="skill-center-toolbar">
   <div className="skill-status-tabs" role="tablist" aria-label="能力包视图">
    <button type="button" role="tab" aria-selected={view==='installed'} onClick={()=>setView('installed')}>已安装（{installedVisible.length}）</button>
    <button type="button" role="tab" aria-selected={view==='market'} onClick={()=>setView('market')}>能力包市场</button>
   </div>
   <label className="skill-search">搜索<input value={query} onChange={e=>setQuery(e.target.value)} placeholder={view==='market'?'名称或分类':'已安装能力包'}/></label>
   <button aria-label="刷新能力包" onClick={()=>void load()}>↻</button>
  </section>
  {error&&<p className="skill-center-error" role="alert">{error}</p>}
  {notice&&<p role="status">{notice}</p>}
  {view==='market'?<>
   <nav className="skill-market-cats" aria-label="插件分类">
    <button type="button" aria-pressed={!category} onClick={()=>setCategory('')}>全部<small>{PLUGIN_MARKET.length}</small></button>
    {categories.map(([id,count])=><button type="button" key={id} aria-pressed={category===id} onClick={()=>setCategory(id)}>{id}<small>{count}</small></button>)}
   </nav>
   <section className="skill-market-shelf" aria-label="组合能力包">
    <header><b>组合包</b><small>一次装上技能 + MCP + 门闸</small></header>
    <div className="skill-market">{shelfPacks.filter(pack=>!query.trim()||`${pack.name} ${pack.description}`.toLowerCase().includes(query.trim().toLowerCase())).map(pack=>{
     const logo=pluginLogo(pack.id)
     const record=packLedger.find(item=>item.packId===pack.id)
     const installed=!!record&&!record.failed
     return <article className={`skill-market-card ${highlightId===pack.id?'is-highlight':''} ${installed?'is-installed':''}`} key={pack.id}>
      <header>
       <span className="plugin-logo" style={{'--plugin-tint':logo.tint} as React.CSSProperties} aria-hidden="true">{logo.glyph}</span>
       <div><b>{pack.name}</b><small>技能 {pack.skills.length} · MCP {pack.mcpPresetIds.length} · 门闸 {pack.toolGates.length}{record?.failed?' · 安装失败':''}</small></div>
       {installed?<span className="skill-market-installed">已安装</span>:record?.failed?<button type="button" className="ui-btn" disabled={Boolean(busy)} onClick={()=>setRemovePackId(pack.id)}>删除重来</button>:<button type="button" className="skill-market-add" aria-label={`安装 ${pack.name}`} disabled={Boolean(busy)} onClick={()=>void enable(pack.id)}>{busy===pack.id?'…':'＋'}</button>}
       <button type="button" className="ui-btn" onClick={()=>void navigator.clipboard.writeText(exportCapabilityPackJSON(pack)).then(()=>setNotice(`已复制「${pack.name}」JSON`))}>导出</button>
      </header>
      <p>{pack.description}</p>
     </article>})}</div>
   </section>
   <section className="skill-market-shelf" aria-label="内置能力开关">
    {visibleMarket.length?<div className="skill-market">{visibleMarket.map(entry=>{
     const hit=byId.get(entry.id);const on=hit?.state==='enabled';const failedInstall=hit?.state==='quarantined'
     const logo=pluginLogo(entry.id)
     return <article className={`skill-market-card ${on?'is-installed':''}`} key={entry.id}>
      <header>
       <span className="plugin-logo" style={{'--plugin-tint':logo.tint} as React.CSSProperties} aria-hidden="true">{logo.glyph}</span>
       <div><b>{entry.name}</b><small>{entry.category} · {KIND_LABEL[entry.kind]}{failedInstall?' · 安装失败':''}</small></div>
       {on?<span className="skill-market-installed">{pluginHonestyLabel(entry.id)}</span>:<button type="button" className="skill-market-add" aria-label={`启用 ${entry.name}`} disabled={Boolean(busy)} onClick={()=>void enable(entry.id)}>{busy===entry.id?'…':'＋'}</button>}
      </header>
      <p>{entry.description}</p>
      <footer><small>v{entry.semver} · {entry.publisher}</small></footer>
     </article>
    })}</div>:<div className="empty"><b>没有匹配的能力包</b><span>换个分类或关键字再试。</span></div>}
   </section>
  </>:<section className="skill-market-shelf" aria-label="已安装能力包">
   {packLedger.length?<div className="skill-market">{packLedger.map(entry=>{
    const pack=findPack(entry.packId)
    if(!pack)return null
    const logo=pluginLogo(pack.id)
    return <article className={`skill-market-card ${entry.failed?'':'is-installed'}`} key={pack.id}>
     <header>
      <span className="plugin-logo" style={{'--plugin-tint':logo.tint} as React.CSSProperties} aria-hidden="true">{logo.glyph}</span>
      <div><b>{pack.name}</b><small>组合包{entry.failed?' · 安装失败':''}</small></div>
      <i className={`skill-status status-${entry.failed?'deprecated':'published'}`}>{entry.failed?'安装失败':'已安装'}</i>
     </header>
     <p>{entry.failed?entry.failed:pack.description}</p>
     <footer>
      <small>删除只撤本包加过的门闸和 MCP，不卸技能</small>
      <div className="expert-card-actions">
       <button type="button" className="ui-btn" disabled={Boolean(busy)} onClick={()=>setRemovePackId(pack.id)}>删除</button>
      </div>
     </footer>
    </article>})}</div>:null}
   {installedVisible.filter(item=>!isPackPluginId(item.pluginId)).length?<div className="skill-market">{installedVisible.filter(item=>!isPackPluginId(item.pluginId)).map(item=>{
    const logo=pluginLogo(item.pluginId)
    const market=PLUGIN_MARKET.find(entry=>entry.id===item.pluginId)
    return <article className={`skill-market-card ${item.state==='enabled'?'is-installed':''}`} key={item.installId}>
     <header>
      <span className="plugin-logo" style={{'--plugin-tint':logo.tint} as React.CSSProperties} aria-hidden="true">{logo.glyph}</span>
      <div><b>{pluginTitle(item.pluginId)}</b><small>{KIND_LABEL[item.kind]??item.kind} · {item.publisher||'local'} · 绑定 {item.bindingCount} 项</small></div>
      <i className={`skill-status status-${item.state==='enabled'?'published':item.state==='quarantined'?'deprecated':item.state==='disabled'?'disabled':'draft'}`}>{STATE_LABEL[item.state]}</i>
     </header>
     <p>{market?.description??`能力包标识 ${item.pluginId}`}</p>
     <footer>
      <small>v{item.semver} · {pluginOriginLabel(item.origin)}</small>
      <div className="expert-card-actions">
       {(item.state==='enabled'||item.state==='disabled'||item.state==='installed')&&<button type="button" className="ui-btn" disabled={Boolean(busy)} onClick={()=>void enable(item.pluginId)}>{item.state==='enabled'?'停用':'启用'}</button>}
       <button type="button" className="ui-btn" disabled={Boolean(busy)} onClick={()=>setRemoveTarget(item)}>删除</button>
      </div>
     </footer>
    </article>})}</div>:!packLedger.length?<div className="empty"><b>还没有启用的门闸</b><span>去「能力包市场」点加号启用内置能力。</span></div>:null}
  </section>}
  <Dialog open={manualOpen} wide title="手动创建能力包" description="填写清单 JSON。保存后出现在已安装清单；校验失败会标成安装失败。不会执行外部脚本。" onClose={()=>{if(!busy)setManualOpen(false)}}>
   <form className="editor-dialog" onSubmit={e=>{e.preventDefault();void createManual()}}>
    <label>工作区标识<input value={workspaceId} onChange={e=>setWorkspaceId(e.target.value)}/></label>
    <label>入口路径<input value={entrypoint} onChange={e=>setEntrypoint(e.target.value)}/></label>
    <label>插件清单 JSON<textarea className="skill-manifest-editor" rows={12} value={manifest} onChange={e=>setManifest(e.target.value)} aria-label="插件清单 JSON"/></label>
    {error&&<p className="skill-center-error" role="alert">{error}</p>}
    <div className="dialog-actions"><button type="button" disabled={Boolean(busy)} onClick={()=>setManualOpen(false)}>取消</button><button className="primary" disabled={Boolean(busy)||!manifest.trim()||!entrypoint.trim()}>{busy==='manual'?'保存中…':'保存'}</button></div>
   </form>
  </Dialog>
  <Dialog open={!!removeTarget} title={`删除「${removeTarget?pluginTitle(removeTarget.pluginId):''}」`} description="删除后能力绑定一并撤销，可从市场重新安装。" onClose={()=>{if(!busy)setRemoveTarget(null)}}>
   <div className="dialog-actions"><button type="button" disabled={Boolean(busy)} onClick={()=>setRemoveTarget(null)}>取消</button><button className="danger" disabled={Boolean(busy)} onClick={()=>void remove()}>确认删除</button></div>
  </Dialog>
  <Dialog open={!!removePackId} title={`撤下「${findPack(removePackId)?.name??'能力包'}」`} description="只关闭本包打开的门闸和本包新加的 MCP。技能留在技能中心，其他包还在用的绑定会保留。" onClose={()=>{if(!busy)setRemovePackId('')}}>
   <div className="dialog-actions"><button type="button" disabled={Boolean(busy)} onClick={()=>setRemovePackId('')}>取消</button><button className="danger" disabled={Boolean(busy)} onClick={()=>void removePack()}>确认删除</button></div>
  </Dialog>
 </main>
}
