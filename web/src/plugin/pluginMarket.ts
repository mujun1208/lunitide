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
}

export const PLUGIN_MARKET:PluginMarketEntry[]=[
 {id:'web-search',name:'网页搜索',description:'在对话里搜索公开网页，结果出现在工作区浏览器。',kind:'tool',category:'效率提升',publisher:'lunitide',semver:'1.0.0'},
 {id:'web-search-deepseek',name:'DeepSeek 网页搜索',description:'走 DeepSeek 搜索插件通道的网页检索。',kind:'tool',category:'效率提升',publisher:'lunitide',semver:'1.0.0'},
 {id:'web-fetch',name:'抓取网页',description:'读取公开页面正文，供模型引用。',kind:'tool',category:'效率提升',publisher:'lunitide',semver:'1.0.0'},
 {id:'workspace',name:'工作区',description:'浏览、读取和写入当前会话工作区文件。',kind:'tool',category:'系统能力',publisher:'lunitide',semver:'1.0.0'},
 {id:'filesystem',name:'文件系统',description:'在治理边界内访问文件树。',kind:'tool',category:'系统能力',publisher:'lunitide',semver:'1.0.0'},
 {id:'git',name:'Git',description:'查看仓库状态、diff 与日志。',kind:'tool',category:'开发工具',publisher:'lunitide',semver:'1.0.0'},
 {id:'tool-pwsh',name:'PowerShell',description:'在命令白名单内执行 PowerShell（Windows 默认启用）。',kind:'tool',category:'开发工具',publisher:'lunitide',semver:'1.0.0'},
 {id:'tool-cmd',name:'CMD',description:'在命令白名单内执行 cmd。',kind:'tool',category:'开发工具',publisher:'lunitide',semver:'1.0.0'},
 {id:'tool-bash',name:'Bash',description:'在命令白名单内执行 Bash（非 Windows 默认启用）。',kind:'tool',category:'开发工具',publisher:'lunitide',semver:'1.0.0'},
 {id:'tool-python',name:'Python',description:'在命令白名单内运行 Python。',kind:'tool',category:'开发工具',publisher:'lunitide',semver:'1.0.0'},
 {id:'browser',name:'浏览器',description:'页面导航与读取，可配合 Playwright MCP。',kind:'mcp',category:'系统能力',publisher:'lunitide',semver:'1.0.0'},
 {id:'skills',name:'技能',description:'在对话中调用已安装技能。',kind:'skill',category:'效率提升',publisher:'lunitide',semver:'1.0.0'},
 {id:'memory',name:'记忆',description:'跨会话记忆候选与确认。',kind:'tool',category:'系统能力',publisher:'lunitide',semver:'1.0.0'},
 {id:'agent-loop',name:'Agent 循环',description:'多步工具循环，让助手把任务做到完成。',kind:'workflow',category:'系统能力',publisher:'lunitide',semver:'1.0.0'},
 {id:'thinking',name:'思考链',description:'展示模型推理过程。',kind:'tool',category:'系统能力',publisher:'lunitide',semver:'1.0.0'},
 {id:'tts',name:'语音合成',description:'把回复读出来，服务月伴对话。',kind:'tool',category:'语音与界面',publisher:'lunitide',semver:'1.0.0'},
 {id:'stt',name:'语音识别',description:'把麦克风输入转成文字。',kind:'tool',category:'语音与界面',publisher:'lunitide',semver:'1.0.0'},
 {id:'clipboard',name:'剪贴板',description:'读写系统剪贴板。',kind:'tool',category:'效率提升',publisher:'lunitide',semver:'1.0.0'},
 {id:'notification',name:'通知',description:'任务完成时弹出系统通知。',kind:'tool',category:'语音与界面',publisher:'lunitide',semver:'1.0.0'},
 {id:'cron',name:'定时任务',description:'按日程触发自动化。',kind:'workflow',category:'效率提升',publisher:'lunitide',semver:'1.0.0'},
 {id:'jobs-local',name:'本地任务',description:'在本机排队执行后台任务。',kind:'workflow',category:'开发工具',publisher:'lunitide',semver:'1.0.0'},
 {id:'session',name:'会话',description:'会话元数据与历史衔接。',kind:'tool',category:'系统能力',publisher:'lunitide',semver:'1.0.0'},
 {id:'logger',name:'日志',description:'把运行轨迹写入诊断日志。',kind:'tool',category:'开发工具',publisher:'lunitide',semver:'1.0.0'},
 {id:'inspector',name:'检查器',description:'检查工具调用与绑定状态。',kind:'tool',category:'开发工具',publisher:'lunitide',semver:'1.0.0'},
]

export const FILLER_PLUGIN=/^(tool|mcp|skill|workflow|template|pack)-\d+$/
export const pluginTitle=(pluginId:string)=>PLUGIN_MARKET.find(item=>item.id===pluginId)?.name??pluginId
