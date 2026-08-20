import React,{useCallback,useEffect,useMemo,useState}from'react'
import{pluginBridge,type PluginBridge}from'../bridge/client'
import type{PluginListResult}from'../generated/bridge'
import{Dialog}from'../ui/Dialog'
import{FILLER_PLUGIN,PLUGIN_MARKET,pluginLogo,pluginTitle,type PluginCategory}from'./pluginMarket'

type Plugin=PluginListResult['plugins'][number]
type View='installed'|'market'
const KIND_LABEL:Record<string,string>={mcp:'MCP',skill:'技能',workflow:'工作流',template:'模板',tool:'工具','agent-pack':'AgentPack'}
const STATE_LABEL:Record<Plugin['state'],string>={installed:'已安装未启用',enabled:'已安装',disabled:'未启用',quarantined:'安装失败',uninstalled:'已卸载'}
const sha256Hex=async(value:string)=>{const digest=await globalThis.crypto.subtle.digest('SHA-256',new TextEncoder().encode(value));return Array.from(new Uint8Array(digest)).map(byte=>byte.toString(16).padStart(2,'0')).join('')}
const isFiller=(pluginId:string)=>FILLER_PLUGIN.test(pluginId)

export function PluginPage({bridge=pluginBridge,onCreateInChat}:{bridge?:PluginBridge;onCreateInChat?:()=>void}):React.JSX.Element{
 const[view,setView]=useState<View>('market')
 const[plugins,setPlugins]=useState<Plugin[]>([])
 const[query,setQuery]=useState('')
 const[category,setCategory]=useState<PluginCategory|''>('')
 const[busy,setBusy]=useState('')
 const[error,setError]=useState('')
 const[notice,setNotice]=useState('')
 const[manualOpen,setManualOpen]=useState(false)
 const[manifest,setManifest]=useState('{\n  "pluginId": "my-plugin",\n  "semver": "1.0.0",\n  "publisher": "local",\n  "kind": "tool",\n  "permissions": {}\n}')
 const[entrypoint,setEntrypoint]=useState('plugin/main.ts')
 const[workspaceId,setWorkspaceId]=useState('chat')
 const[removeTarget,setRemoveTarget]=useState<Plugin|null>(null)

 const load=useCallback(async()=>{try{setPlugins((await bridge.list()).plugins);setError('')}catch(e){setError(e instanceof Error?e.message:'插件清单加载失败')}},[bridge])
 useEffect(()=>{void load()},[load])

 const byId=useMemo(()=>new Map(plugins.map(item=>[item.pluginId,item])),[plugins])
 const installedVisible=useMemo(()=>{const q=query.trim().toLowerCase();return plugins.filter(item=>item.state!=='uninstalled'&&!isFiller(item.pluginId)&&(!q||`${pluginTitle(item.pluginId)} ${item.pluginId} ${item.kind} ${item.state}`.toLowerCase().includes(q)))},[plugins,query])
 const failed=installedVisible.filter(item=>item.state==='quarantined').length
 const enabled=installedVisible.filter(item=>item.state==='enabled').length
 const categories=useMemo(()=>{const map=new Map<PluginCategory,number>();for(const item of PLUGIN_MARKET)map.set(item.category,(map.get(item.category)??0)+1);return[...map.entries()]},[])
 const visibleMarket=useMemo(()=>{const q=query.trim().toLowerCase();return PLUGIN_MARKET.filter(item=>(!category||item.category===category)&&(!q||`${item.name} ${item.description} ${item.category}`.toLowerCase().includes(q)))},[category,query])

 const enable=async(pluginId:string)=>{
  const hit=byId.get(pluginId)
  if(!hit){setError(`还没有「${pluginTitle(pluginId)}」的安装记录`);return}
  setBusy(pluginId);setError('');setNotice('')
  try{
   await bridge.toggle({installId:hit.installId,enabled:hit.state!=='enabled'})
   setNotice(hit.state==='enabled'?`已停用「${pluginTitle(pluginId)}」`:`已安装并启用「${pluginTitle(pluginId)}」`)
   await load();if(hit.state!=='enabled')setView('installed')
  }catch(e){setError(e instanceof Error?e.message:'安装失败')}finally{setBusy('')}
 }
 const remove=async()=>{
  if(!removeTarget)return
  setBusy(removeTarget.installId);setError('');setNotice('')
  try{
   const confirmToken=await sha256Hex(`plugin.uninstall|${removeTarget.installId}`)
   await bridge.uninstall({installId:removeTarget.installId,confirmToken})
   setNotice(`已删除「${pluginTitle(removeTarget.pluginId)}」`);setRemoveTarget(null);await load()
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
   <div><h1>插件</h1><p>已安装 {enabled} 个可用 · 失败 {failed} 个 · 市场 {PLUGIN_MARKET.length} 个</p><small>点加号启用内置 Harness 插件；也可以对话创建，或粘贴清单手动接入。GitHub Marketplace / Claude 插件市场的包格式与月汐插件契约不同，不能直接安装。</small></div>
   <div className="view-actions"><button type="button" className="ui-btn" onClick={()=>setManualOpen(true)}>手动填写</button>{onCreateInChat&&<button type="button" className="ui-btn primary" onClick={onCreateInChat}>＋ 创建插件</button>}</div>
  </header>
  <section className="skill-center-toolbar">
   <div className="skill-status-tabs" role="tablist" aria-label="插件视图">
    <button type="button" role="tab" aria-selected={view==='installed'} onClick={()=>setView('installed')}>已安装（{installedVisible.length}）</button>
    <button type="button" role="tab" aria-selected={view==='market'} onClick={()=>setView('market')}>插件市场</button>
   </div>
   <label className="skill-search">搜索<input value={query} onChange={e=>setQuery(e.target.value)} placeholder={view==='market'?'名称或分类':'已安装插件'}/></label>
   <button aria-label="刷新插件" onClick={()=>void load()}>↻</button>
  </section>
  {error&&<p className="skill-center-error" role="alert">{error}</p>}
  {notice&&<p role="status">{notice}</p>}
  {view==='market'?<>
   <nav className="skill-market-cats" aria-label="插件分类">
    <button type="button" aria-pressed={!category} onClick={()=>setCategory('')}>全部<small>{PLUGIN_MARKET.length}</small></button>
    {categories.map(([id,count])=><button type="button" key={id} aria-pressed={category===id} onClick={()=>setCategory(id)}>{id}<small>{count}</small></button>)}
   </nav>
   <section className="skill-market-shelf" aria-label="可安装插件">
    {visibleMarket.length?<div className="skill-market">{visibleMarket.map(entry=>{
     const hit=byId.get(entry.id);const on=hit?.state==='enabled';const failedInstall=hit?.state==='quarantined'
     const logo=pluginLogo(entry.id)
     return <article className={`skill-market-card ${on?'is-installed':''}`} key={entry.id}>
      <header>
       <span className="plugin-logo" style={{'--plugin-tint':logo.tint} as React.CSSProperties} aria-hidden="true">{logo.glyph}</span>
       <div><b>{entry.name}</b><small>{entry.category} · {KIND_LABEL[entry.kind]}{failedInstall?' · 安装失败':''}</small></div>
       {on?<span className="skill-market-installed">已安装</span>:<button type="button" className="skill-market-add" aria-label={`安装 ${entry.name}`} disabled={Boolean(busy)} onClick={()=>void enable(entry.id)}>{busy===entry.id?'…':'＋'}</button>}
      </header>
      <p>{entry.description}</p>
      <footer><small>v{entry.semver} · {entry.publisher}</small></footer>
     </article>
    })}</div>:<div className="empty"><b>没有匹配的插件</b><span>换个分类或关键字再试。</span></div>}
   </section>
  </>:<section className="skill-market-shelf" aria-label="已安装插件">
   {installedVisible.length?<div className="skill-market">{installedVisible.map(item=>{
    const logo=pluginLogo(item.pluginId)
    const market=PLUGIN_MARKET.find(entry=>entry.id===item.pluginId)
    return <article className={`skill-market-card ${item.state==='enabled'?'is-installed':''}`} key={item.installId}>
     <header>
      <span className="plugin-logo" style={{'--plugin-tint':logo.tint} as React.CSSProperties} aria-hidden="true">{logo.glyph}</span>
      <div><b>{pluginTitle(item.pluginId)}</b><small>{KIND_LABEL[item.kind]??item.kind} · {item.publisher||'local'} · 绑定 {item.bindingCount} 项</small></div>
      <i className={`skill-status status-${item.state==='enabled'?'published':item.state==='quarantined'?'deprecated':item.state==='disabled'?'disabled':'draft'}`}>{STATE_LABEL[item.state]}</i>
     </header>
     <p>{market?.description??`插件标识 ${item.pluginId}`}</p>
     <footer>
      <small>v{item.semver} · {item.origin==='dev'?'对话创建':item.origin==='market'?'市场':'本机'}</small>
      <div className="expert-card-actions">
       {(item.state==='enabled'||item.state==='disabled'||item.state==='installed')&&<button type="button" className="ui-btn" disabled={Boolean(busy)} onClick={()=>void enable(item.pluginId)}>{item.state==='enabled'?'停用':'启用'}</button>}
       <button type="button" className="ui-btn" disabled={Boolean(busy)} onClick={()=>setRemoveTarget(item)}>删除</button>
      </div>
     </footer>
    </article>})}</div>:<div className="empty"><b>还没有安装插件</b><span>去「插件市场」点加号，或通过对话创建。</span></div>}
  </section>}
  <Dialog open={manualOpen} wide title="手动创建插件" description="填写 Harness 兼容的插件清单 JSON。保存后出现在已安装清单；校验失败会标成安装失败。" onClose={()=>{if(!busy)setManualOpen(false)}}>
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
 </main>
}
