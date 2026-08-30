export type PluginKind='mcp'|'skill'|'workflow'|'template'|'tool'|'agent-pack'
export type PluginCategory='开发工具'|'效率提升'|'内容创作'|'系统能力'|'语音与界面'
export type PluginHonesty='builtin-toggle'

export interface PluginMarketEntry{
 id:string
 name:string
 description:string
 kind:PluginKind
 category:PluginCategory
 publisher:string
 semver:string
 glyph:string
 tint:string
 honesty:PluginHonesty
}

const entry=(id:string,name:string,description:string,kind:PluginKind,category:PluginCategory,glyph:string,tint:string,publisher='lunitide',semver='1.0.0'):PluginMarketEntry=>({id,name,description,kind,category,publisher,semver,glyph,tint,honesty:'builtin-toggle'})

export const PLUGIN_MARKET:PluginMarketEntry[]=[
 entry('web-search','网页搜索','对话里用 web.search。这里只是名单开关，不会再装一个搜索插件。','tool','效率提升','搜','#5ee0ff'),
 entry('web-search-deepseek','DeepSeek 网页搜索','DeepSeek 搜索通道的名单开关。真正检索仍走现有搜索工具。','tool','效率提升','DS','#7c9cff'),
 entry('web-fetch','抓取网页','对话里用 web.fetch。这里只是名单开关。','tool','效率提升','抓','#6bc5ff'),
 entry('workspace','工作区','对话里已经能读写当前会话工作区。这里只是名单开关。','tool','系统能力','区','#5ee0b5'),
 entry('filesystem','文件系统','对话里已经能在治理边界内访问文件。这里只是名单开关。','tool','系统能力','文','#f0c14a'),
 entry('git','Git','对话里用 command.run 看仓库。这里不会安装 Git 插件，也不是 GitHub MCP。','tool','开发工具','G','#ff8a6b'),
 entry('tool-pwsh','PowerShell','对话里用 command.run 跑 PowerShell。这里只是名单开关。','tool','开发工具','PS','#3fd6ff'),
 entry('tool-cmd','CMD','对话里用 command.run 跑 cmd。这里只是名单开关。','tool','开发工具','C','#d08cff'),
 entry('tool-bash','Bash','对话里用 command.run 跑 Bash。Windows 默认关。','tool','开发工具','B','#7c9cff'),
 entry('tool-python','Python','对话里用 command.run 跑 Python。这里不会新装解释器，也不是 tool-python 插件包。','tool','开发工具','Py','#5ee0b5'),
 entry('browser','浏览器','对话里用 browser.act；复杂页面配合设置里的 Playwright MCP。这里只是名单开关。','mcp','系统能力','浏','#6bc5ff'),
 entry('skills','技能','技能走技能中心。这里只是名单开关。','skill','效率提升','技','#f0c14a'),
 entry('memory','记忆','记忆走设置里的记忆预置。这里只是名单开关。','tool','系统能力','忆','#d08cff'),
 entry('agent-loop','Agent 循环','多步工具循环已经在对话引擎里。这里只是名单开关。','workflow','系统能力','环','#5ee0ff'),
 entry('thinking','思考链','思考过程由模型自己产出。这里只是名单开关。','tool','系统能力','思','#7c9cff'),
 entry('tts','语音合成','月伴读出来走本机/设置里的合成。这里只是名单开关。','tool','语音与界面','说','#ff8a6b'),
 entry('stt','语音识别','听写走月伴/会议的识别通道。这里只是名单开关。','tool','语音与界面','听','#3fd6ff'),
 entry('clipboard','剪贴板','对话里已经能读写剪贴板。这里不会新装剪贴板插件。','tool','效率提升','贴','#5ee0b5'),
 entry('notification','通知','对话里已经能弹系统通知。这里不会新装通知插件。','tool','语音与界面','通','#f0c14a'),
 entry('cron','定时任务','定时任务走自动化。这里只是名单开关。','workflow','效率提升','时','#d08cff'),
 entry('jobs-local','本地任务','本机排队任务走自动化。这里只是名单开关。','workflow','开发工具','任','#6bc5ff'),
 entry('session','会话','会话元数据已经在引擎里。这里只是名单开关。','tool','系统能力','话','#5ee0ff'),
 entry('llm','LLM','模型调用已经在引擎里。这里只是名单开关，不会新装模型。','tool','系统能力','L','#d08cff'),
]

const LOGO_TINTS=['#5ee0ff','#7c9cff','#5ee0b5','#f0c14a','#ff8a6b','#d08cff','#6bc5ff','#3fd6ff']

export const FILLER_PLUGIN=/^(tool|mcp|skill|workflow|template|pack)-\d+$/
export const pluginTitle=(pluginId:string)=>PLUGIN_MARKET.find(item=>item.id===pluginId)?.name??pluginId
export const pluginEntry=(pluginId:string)=>PLUGIN_MARKET.find(item=>item.id===pluginId)
export const pluginHonestyLabel=(pluginId:string)=>pluginEntry(pluginId)?.honesty==='builtin-toggle'?'已是内置工具':'已安装'
export const pluginOriginLabel=(origin:string)=>origin==='dev'?'对话创建':origin==='market'?'市场':'内置开关'
export function pluginLogo(pluginId:string):{glyph:string;tint:string}{
 const hit=pluginEntry(pluginId)
 if(hit)return{glyph:hit.glyph,tint:hit.tint}
 const name=pluginTitle(pluginId)
 let hash=0
 for(const ch of pluginId)hash=(hash*31+ch.charCodeAt(0))>>>0
 return{glyph:Array.from(name)[0]?.toUpperCase()||'P',tint:LOGO_TINTS[hash%LOGO_TINTS.length]}
}
