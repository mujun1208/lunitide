export type PluginKind='mcp'|'skill'|'workflow'|'template'|'tool'|'agent-pack'
export type PluginCategory='开发工具'|'效率提升'|'内容创作'|'系统能力'|'语音与界面'

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
}

const entry=(id:string,name:string,description:string,kind:PluginKind,category:PluginCategory,glyph:string,tint:string,publisher='lunitide',semver='1.0.0'):PluginMarketEntry=>({id,name,description,kind,category,publisher,semver,glyph,tint})

export const PLUGIN_MARKET:PluginMarketEntry[]=[
 entry('web-search','网页搜索','在对话里搜索公开网页，结果出现在工作区浏览器。','tool','效率提升','搜','#5ee0ff'),
 entry('web-search-deepseek','DeepSeek 网页搜索','走 DeepSeek 搜索插件通道的网页检索。','tool','效率提升','DS','#7c9cff'),
 entry('web-fetch','抓取网页','读取公开页面正文，供模型引用。','tool','效率提升','抓','#6bc5ff'),
 entry('workspace','工作区','浏览、读取和写入当前会话工作区文件。','tool','系统能力','区','#5ee0b5'),
 entry('filesystem','文件系统','在治理边界内访问文件树。','tool','系统能力','文','#f0c14a'),
 entry('git','Git','查看仓库状态、diff 与日志。','tool','开发工具','G','#ff8a6b'),
 entry('tool-pwsh','PowerShell','在命令白名单内执行 PowerShell（Windows 默认启用）。','tool','开发工具','PS','#3fd6ff'),
 entry('tool-cmd','CMD','在命令白名单内执行 cmd。','tool','开发工具','C','#d08cff'),
 entry('tool-bash','Bash','在命令白名单内执行 Bash（非 Windows 默认启用）。','tool','开发工具','B','#7c9cff'),
 entry('tool-python','Python','在命令白名单内运行 Python。','tool','开发工具','Py','#5ee0b5'),
 entry('browser','浏览器','页面导航与读取，可配合 Playwright MCP。','mcp','系统能力','浏','#6bc5ff'),
 entry('skills','技能','在对话中调用已安装技能。','skill','效率提升','技','#f0c14a'),
 entry('memory','记忆','跨会话记忆候选与确认。','tool','系统能力','忆','#d08cff'),
 entry('agent-loop','Agent 循环','多步工具循环，让助手把任务做到完成。','workflow','系统能力','环','#5ee0ff'),
 entry('thinking','思考链','展示模型推理过程。','tool','系统能力','思','#7c9cff'),
 entry('tts','语音合成','把回复读出来，服务月伴对话。','tool','语音与界面','说','#ff8a6b'),
 entry('stt','语音识别','把麦克风输入转成文字。','tool','语音与界面','听','#3fd6ff'),
 entry('clipboard','剪贴板','读写系统剪贴板。','tool','效率提升','贴','#5ee0b5'),
 entry('notification','通知','任务完成时弹出系统通知。','tool','语音与界面','通','#f0c14a'),
 entry('cron','定时任务','按日程触发自动化。','workflow','效率提升','时','#d08cff'),
 entry('jobs-local','本地任务','在本机排队执行后台任务。','workflow','开发工具','任','#6bc5ff'),
 entry('session','会话','会话元数据与历史衔接。','tool','系统能力','话','#5ee0ff'),
 entry('logger','日志','把运行轨迹写入诊断日志。','tool','开发工具','志','#7c9cff'),
 entry('inspector','检查器','检查工具调用与绑定状态。','tool','开发工具','检','#ff8a6b'),
 entry('hmr','HMR','开发时热替换插件能力。','tool','开发工具','H','#3fd6ff'),
 entry('include','Include','在对话上下文中引入片段。','tool','开发工具','In','#5ee0b5'),
 entry('timer','Timer','计时与延迟触发。','tool','效率提升','T','#f0c14a'),
 entry('llm','LLM','模型调用相关能力。','tool','系统能力','L','#d08cff'),
 entry('typert-registry','Typert Registry','类型注册与校验。','tool','开发工具','Ty','#6bc5ff'),
 entry('i18n','国际化','界面与回复的多语言处理。','tool','语音与界面','语','#5ee0ff'),
]

const LOGO_TINTS=['#5ee0ff','#7c9cff','#5ee0b5','#f0c14a','#ff8a6b','#d08cff','#6bc5ff','#3fd6ff']

export const FILLER_PLUGIN=/^(tool|mcp|skill|workflow|template|pack)-\d+$/
export const pluginTitle=(pluginId:string)=>PLUGIN_MARKET.find(item=>item.id===pluginId)?.name??pluginId
export const pluginEntry=(pluginId:string)=>PLUGIN_MARKET.find(item=>item.id===pluginId)
export function pluginLogo(pluginId:string):{glyph:string;tint:string}{
 const hit=pluginEntry(pluginId)
 if(hit)return{glyph:hit.glyph,tint:hit.tint}
 const name=pluginTitle(pluginId)
 let hash=0
 for(const ch of pluginId)hash=(hash*31+ch.charCodeAt(0))>>>0
 return{glyph:Array.from(name)[0]?.toUpperCase()||'P',tint:LOGO_TINTS[hash%LOGO_TINTS.length]}
}
