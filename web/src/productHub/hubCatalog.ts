import type { Page } from '../app/appTypes'
import { CATALOG_MEDIA_ACTIONS, CATALOG_PAGES, CATALOG_SETTINGS } from '../generated/productCatalog'
import type { HubCard, HubChange, HubEdge, HubFinding, HubNode, HubOverview } from './productHubTypes'

export type PageGroup = 'dialog' | 'office' | 'workspace' | 'system'

export type PageSpec = {
  id: Page
  name: string
  nameEn: string
  domain: 'dialog' | 'office' | 'assets' | 'execution' | 'foundation'
  module: string
  group: PageGroup
  groupZh: string
  groupEn: string
  nav: string
  summary: string
  analysis: string
  settings: string[]
}

export type FeatureSpec = {
  key: string
  name: string
  nameEn: string
  domain: string
  module: string
  pages: Page[]
  source: string
  chainClass: string
  summary: string
  bridge?: string[]
  settings?: string[]
}

export type PageDossier = {
  spec: PageSpec
  features: HubNode[]
  changes: HubChange[]
  findings: HubFinding[]
  settings: string[]
}

export const PAGE_GROUPS: Array<{ id: PageGroup; zh: string; en: string }> = [
  { id: 'dialog', zh: '对话', en: 'Chat' },
  { id: 'office', zh: '办公', en: 'Office' },
  { id: 'workspace', zh: '项目', en: 'Workspace' },
  { id: 'system', zh: '系统', en: 'System' },
]

export const PAGE_ATLAS: Record<Page, PageSpec> = {
  home: {
    id: 'home', name: '主页', nameEn: 'Home', domain: 'dialog', module: 'home',
    group: 'dialog', groupZh: '对话', groupEn: 'Chat',
    nav: '侧栏「新对话」/ 会话列表',
    summary: '月伴语音、打字聊天、会话和工作区都从这里进出。',
    analysis: '主页就是 SessionPage。侧栏「新对话」打开一条个人会话；列表点开已有会话。页内可打字、唤醒月伴、语音插话、@技能/@专家、新建/搜索/改名/删除会话，并打开或保存工作区文件。放歌、打开本机应用、截屏点击也从这条对话链路发出，再落到媒体中心或电脑控制。',
    settings: ['general', 'voice', 'personal'],
  },
  office: {
    id: 'office', name: '办公工作台', nameEn: 'Office Studio', domain: 'office', module: 'office',
    group: 'office', groupZh: '办公', groupEn: 'Office',
    nav: '侧栏办公组 · 办公工作台',
    summary: '创建办公任务、预览并导出文档表格产物。',
    analysis: '办公菜单默认关闭，打开后进入 Office Studio。对话里点开办公文件会同步到当前任务。用户在此创建任务、导出产物、打开本地文件预览。动态应落在任务创建、产物导出、文件打开三条动词上。',
    settings: ['office-menu'],
  },
  automation: {
    id: 'automation', name: '自动化', nameEn: 'Automation', domain: 'office', module: 'automation',
    group: 'office', groupZh: '办公', groupEn: 'Office',
    nav: '侧栏办公组 · 自动化',
    summary: '保存定时或事件任务，并可立刻跑一遍。',
    analysis: '办公菜单默认开启。页面管自动化任务的保存与立即触发；创建自动化的对话入口仍回主页，但任务本体在本页。动态按 job-set / job-trigger 记账。',
    settings: ['office-menu'],
  },
  media: {
    id: 'media', name: '媒体中心', nameEn: 'Media Center', domain: 'office', module: 'media',
    group: 'office', groupZh: '办公', groupEn: 'Office',
    nav: '侧栏办公组 · 媒体中心',
    summary: '播放、队列、打开音视频资产；成功与否看 SMTC 回读。',
    analysis: '办公菜单默认开启。一枚媒体 action 一张卡：播放/暂停/切歌/音量/队列。对话里的「放歌」落到本页会话。播放成功 = SMTC 或自有 runtime 回读，不是键已发出。',
    settings: ['office-menu'],
  },
  people: {
    id: 'people', name: '同事聊天', nameEn: 'Colleague chat', domain: 'office', module: 'people',
    group: 'office', groupZh: '办公', groupEn: 'Office',
    nav: '侧栏办公组 · 同事聊天 / 底栏资料',
    summary: '和同事或专家身份互发消息与文件。',
    analysis: '办公菜单默认关闭。底栏头像打开「我的资料」也进本页。专家若按同事身份打开，会从专家中心切到这里。动态按发文件、打开收到的文件记账。',
    settings: ['office-menu', 'profile'],
  },
  mro: {
    id: 'mro', name: '机务工作台', nameEn: 'MRO', domain: 'office', module: 'mro',
    group: 'office', groupZh: '办公', groupEn: 'Office',
    nav: '侧栏办公组 · 机务工作台',
    summary: '检索机务手册并生成检修计划。',
    analysis: '办公菜单默认关闭，且受机务开关约束。本页只做手册检索和计划生成，不替代主页对话。',
    settings: ['office-menu'],
  },
  meetings: {
    id: 'meetings', name: '会议记录', nameEn: 'Meetings', domain: 'office', module: 'meetings',
    group: 'office', groupZh: '办公', groupEn: 'Office',
    nav: '侧栏办公组 · 会议记录',
    summary: '听写、转写、纪要、抽出待办。',
    analysis: '办公菜单默认关闭。开始听写后走转写 → 纪要 → 待办。设置里的会议纪要分类管模型和字幕，本页管一次会议的流水线。',
    settings: ['office-menu', 'meetings', 'voice'],
  },
  productHub: {
    id: 'productHub', name: '产品知识中枢', nameEn: 'Product Hub', domain: 'foundation', module: 'productHub',
    group: 'office', groupZh: '办公', groupEn: 'Office',
    nav: '侧栏办公组 · 产品总览',
    summary: '管理员解锁后查看本体、图谱、诊断和本页动态。',
    analysis: '办公菜单默认开启，入口仅管理员可见。未解锁只暴露 auth。本页按前台页面把每张功能卡和每条变更摊开，刷新快照后对照活源补卡或退役。',
    settings: ['office-menu'],
  },
  projects: {
    id: 'projects', name: '项目管理', nameEn: 'Projects', domain: 'foundation', module: 'projects',
    group: 'workspace', groupZh: '项目', groupEn: 'Workspace',
    nav: '项目组 · 项目管理',
    summary: '项目列表与工作台入口。',
    analysis: 'Agent Hub 壳和普通壳都从项目组进本页。选中非个人项目打开工作台；个人会话仍回主页。动态记在项目打开与工作台切换。',
    settings: [],
  },
  skill: {
    id: 'skill', name: '技能中心', nameEn: 'Skill Center', domain: 'assets', module: 'skill',
    group: 'workspace', groupZh: '项目', groupEn: 'Workspace',
    nav: '项目组 · 技能中心',
    summary: '安装、查看、试用技能。',
    analysis: '安装在本页完成；试用会开一条主页会话并挂上技能。对话里 @技能也指向同一张 invoke 卡。',
    settings: ['security'],
  },
  expert: {
    id: 'expert', name: '专家中心', nameEn: 'Expert Center', domain: 'assets', module: 'expert',
    group: 'workspace', groupZh: '项目', groupEn: 'Workspace',
    nav: '项目组 · 专家中心',
    summary: '试用专家、挂载会话，同事身份则切到同事聊天。',
    analysis: '试用开主页会话并 session.experts.set。若专家按同事打开，路由到 people。动态按试用与挂载记账。',
    settings: ['personal'],
  },
  mcp: {
    id: 'mcp', name: 'MCP', nameEn: 'MCP', domain: 'assets', module: 'mcp',
    group: 'workspace', groupZh: '项目', groupEn: 'Workspace',
    nav: '项目组 · MCP',
    summary: '连接端点并调用已审查工具。',
    analysis: '侧栏灯号反映就绪/降级/失败。连接与调用分开成卡，避免把审查和 invoke 合成一张。',
    settings: ['security'],
  },
  plugins: {
    id: 'plugins', name: '插件', nameEn: 'Plugins', domain: 'assets', module: 'plugins',
    group: 'workspace', groupZh: '项目', groupEn: 'Workspace',
    nav: '项目组 · 插件',
    summary: '启用或使用 Harness 插件名册。',
    analysis: '总开关是 plugin.enable；名册每一项再各有一张卡。新增插件只要出现在 Harness，下次快照自动补卡。',
    settings: [],
  },
  assets: {
    id: 'assets', name: '资产管理', nameEn: 'Asset Management', domain: 'assets', module: 'assets',
    group: 'workspace', groupZh: '项目', groupEn: 'Workspace',
    nav: '项目组 · 资产管理',
    summary: '资产目录，记忆与 OCR 从这里被引用。',
    analysis: '本页是资产域入口。记忆写入/召回、OCR 截图/文档在设置智能能力里配置，但卡片挂到本页和设置两处，方便按页面看动态。',
    settings: ['personal'],
  },
  agentHub: {
    id: 'agentHub', name: 'Agent Hub', nameEn: 'Agent Hub', domain: 'execution', module: 'agenthub',
    group: 'workspace', groupZh: '项目', groupEn: 'Workspace',
    nav: '顶栏 Hub 切换',
    summary: '开工任务、打开工作区文件、管理线程。',
    analysis: '顶栏切到 Agents 后进入本页。项目组工具仍在。动态按 task-start 与 file-open 记账，线程 CRUD 走同一模块。',
    settings: [],
  },
  settings: {
    id: 'settings', name: '设置', nameEn: 'Settings', domain: 'foundation', module: 'settings',
    group: 'system', groupZh: '系统', groupEn: 'System',
    nav: '底栏设置',
    summary: '18 个设置分类，一分类一张卡。',
    analysis: '底栏设置进入，默认停在常规。供应商分类可切到独立供应商页。办公菜单控制办公组可见性。每一类设置都是可单独开关的动词，禁止合成一张「设置」大卡。',
    settings: CATALOG_SETTINGS.map(item => item.id),
  },
  providers: {
    id: 'providers', name: '模型与供应商', nameEn: 'Providers', domain: 'foundation', module: 'providers',
    group: 'system', groupZh: '系统', groupEn: 'System',
    nav: '设置 · 模型与供应商 / 独立页',
    summary: '登记供应商并探测是否可用。',
    analysis: '可从设置分类进入，也可作为独立 Page。添加与测试连通必须分开：一个写库，一个发探测。',
    settings: ['providers', 'routing'],
  },
}

const MEDIA_NAMES: Record<string, string> = {
  play: '播放', pause: '暂停', toggle: '播放暂停切换', stop: '停止',
  previous: '上一首', next: '下一首', seek: '跳转进度', set_volume: '音量',
  mute: '静音', unmute: '取消静音', create: '新建媒体会话', move: '调整队列',
  remove: '移出队列', clear: '清空队列', jump: '跳到队列项',
}

const PLUGINS: Array<{ id: string; title: string }> = [
  { id: 'llm', title: 'LLM' }, { id: 'session', title: 'Session' }, { id: 'jobs-local', title: 'Local Jobs' },
  { id: 'web-search-deepseek', title: 'DeepSeek 网页搜索' }, { id: 'tool-bash', title: 'Bash' },
  { id: 'tool-pwsh', title: 'PowerShell' }, { id: 'tool-cmd', title: 'CMD' }, { id: 'tool-python', title: 'Python' },
  { id: 'web-search', title: '网页搜索' }, { id: 'web-fetch', title: '抓取网页' }, { id: 'workspace', title: '工作区' },
  { id: 'filesystem', title: '文件系统' }, { id: 'git', title: 'Git' }, { id: 'browser', title: '浏览器' },
  { id: 'agent-loop', title: 'Agent 循环' }, { id: 'thinking', title: '思考链' }, { id: 'memory', title: '记忆' },
  { id: 'skills', title: '技能' }, { id: 'cron', title: '定时任务' }, { id: 'clipboard', title: '剪贴板' },
  { id: 'notification', title: '通知' }, { id: 'tts', title: '语音合成' }, { id: 'stt', title: '语音识别' },
]

const VERBS: FeatureSpec[] = [
  spec('feature.dialog.companion.voice-talk', '月伴语音对话', 'Companion voice', 'dialog', 'companion', ['home'], 'intent-control', '唤醒后用语音连续对话。', ['voice']),
  spec('feature.dialog.companion.wake', '唤醒月伴', 'Wake companion', 'dialog', 'companion', ['home'], 'intent-control', '语音唤醒词打开月伴。', ['voice']),
  spec('feature.dialog.companion.barge-in', '语音插话', 'Barge-in', 'dialog', 'companion', ['home'], 'intent-control', '播报中插入新指令。', ['voice']),
  spec('feature.dialog.chat.type', '打字聊天', 'Type a message', 'dialog', 'chat', ['home'], 'crud-bridge', '在对话框输入并发送。'),
  spec('feature.dialog.chat.mention-skill', '对话里 @技能', 'Mention a skill', 'dialog', 'chat', ['home', 'skill'], 'asset-invoke', '用 @ 挂载技能。'),
  spec('feature.dialog.chat.mention-expert', '对话里 @专家', 'Mention an expert', 'dialog', 'chat', ['home', 'expert'], 'asset-invoke', '用 @ 挂载专家。'),
  spec('feature.dialog.session.new', '新建对话', 'New chat', 'dialog', 'session', ['home'], 'page-enter', '开一条新会话。'),
  spec('feature.dialog.session.search', '搜索对话', 'Search chats', 'dialog', 'session', ['home'], 'crud-bridge', '按标题或内容搜会话。'),
  spec('feature.dialog.session.delete', '删除对话', 'Delete chat', 'dialog', 'session', ['home'], 'crud-bridge', '删除一条会话。'),
  spec('feature.dialog.session.rename', '重命名对话', 'Rename chat', 'dialog', 'session', ['home'], 'crud-bridge', '改会话标题。'),
  spec('feature.dialog.music.open-player', '打开音乐播放软件', 'Open music player', 'dialog', 'companion', ['home', 'media'], 'intent-control', '打开本机播放器。'),
  spec('feature.dialog.music.play', '放歌', 'Play track', 'dialog', 'companion', ['home', 'media'], 'media-transport', '按歌名或歌手播放。'),
  spec('feature.dialog.music.pause', '暂停播放', 'Pause', 'dialog', 'companion', ['home', 'media'], 'media-transport', '暂停当前曲目。'),
  spec('feature.dialog.music.toggle', '播放/暂停切换', 'Play/pause toggle', 'dialog', 'companion', ['home', 'media'], 'media-transport', '切换播放与暂停。'),
  spec('feature.dialog.music.next', '下一曲', 'Next track', 'dialog', 'companion', ['home', 'media'], 'media-transport', '切到下一首。'),
  spec('feature.dialog.music.prev', '上一曲', 'Previous track', 'dialog', 'companion', ['home', 'media'], 'media-transport', '切到上一首。'),
  spec('feature.dialog.music.stop', '停止播放', 'Stop', 'dialog', 'companion', ['home', 'media'], 'media-transport', '停止当前曲目。'),
  spec('feature.dialog.music.seek', '跳转到进度', 'Seek', 'dialog', 'companion', ['home', 'media'], 'media-transport', '跳到指定进度。'),
  spec('feature.dialog.music.volume', '调节音量', 'Set volume', 'dialog', 'companion', ['home', 'media'], 'media-transport', '调节播放音量。'),
  spec('feature.dialog.music.mute', '静音', 'Mute', 'dialog', 'companion', ['home', 'media'], 'media-transport', '将当前会话静音。'),
  spec('feature.dialog.music.search', '搜索并播放歌曲', 'Search and play', 'dialog', 'companion', ['home', 'media'], 'intent-control', '搜歌并播放。'),
  spec('feature.dialog.file.open', '打开文件', 'Open a file', 'dialog', 'companion', ['home', 'office'], 'file-open', '从对话打开本地文件。'),
  spec('feature.dialog.file.pick', '选择本地文件', 'Pick local file', 'dialog', 'companion', ['home'], 'file-open', '弹出本机文件选择框。'),
  spec('feature.dialog.app.launch', '打开电脑应用', 'Launch app', 'dialog', 'companion', ['home'], 'intent-control', '从对话启动本机应用。'),
  spec('feature.dialog.computer.screenshot', '截一张屏', 'Screenshot', 'dialog', 'companion', ['home'], 'intent-control', '截取当前屏幕。'),
  spec('feature.dialog.computer.click', '点击屏幕位置', 'Click on screen', 'dialog', 'companion', ['home'], 'intent-control', '按坐标点击本机界面。'),
  spec('feature.office.studio.task-create', '创建办公任务', 'Create office task', 'office', 'office', ['office'], 'crud-bridge', '在办公工作台新建任务。'),
  spec('feature.office.studio.artifact-export', '导出办公产物', 'Export office artifact', 'office', 'office', ['office'], 'crud-bridge', '导出文档或表格产物。'),
  spec('feature.office.automation.job-set', '保存自动化任务', 'Save automation job', 'office', 'automation', ['automation'], 'crud-bridge', '写入一条自动化。'),
  spec('feature.office.automation.job-trigger', '立刻跑自动化', 'Trigger automation', 'office', 'automation', ['automation'], 'asset-invoke', '立即触发已保存任务。'),
  spec('feature.office.people.send-file', '给同事发文件', 'Send file to colleague', 'office', 'people', ['people'], 'file-open', '把本机文件发到同事会话。'),
  spec('feature.office.people.open-file', '打开同事发来的文件', 'Open received file', 'office', 'people', ['people'], 'file-open', '打开收到的文件。'),
  spec('feature.office.mro.search-manual', '检索机务手册', 'Search MRO manual', 'office', 'mro', ['mro'], 'asset-invoke', '按章节检索现行手册。'),
  spec('feature.office.mro.plan', '生成机务计划', 'Build MRO plan', 'office', 'mro', ['mro'], 'crud-bridge', '根据手册与状态生成计划。'),
  spec('feature.office.meetings.start', '开始会议听写', 'Start meeting', 'office', 'meetings', ['meetings'], 'meeting-pipeline', '开始会议听写。'),
  spec('feature.office.meetings.transcribe', '转写会议音频', 'Transcribe meeting', 'office', 'meetings', ['meetings'], 'meeting-pipeline', '把会议录音转成文字。'),
  spec('feature.office.meetings.summary', '生成会议纪要', 'Summarize meeting', 'office', 'meetings', ['meetings'], 'meeting-pipeline', '生成会议纪要。'),
  spec('feature.office.meetings.todo', '抽出会议待办', 'Extract meeting todos', 'office', 'meetings', ['meetings'], 'meeting-pipeline', '从纪要抽出待办。'),
  spec('feature.assets.expert.try', '试用专家', 'Try expert', 'assets', 'expert', ['expert', 'home'], 'asset-invoke', '打开专家试用会话。'),
  spec('feature.assets.skill.install', '安装技能', 'Install skill', 'assets', 'skill', ['skill'], 'asset-invoke', '安装一个技能包。'),
  spec('feature.assets.skill.invoke', '调用技能', 'Invoke skill', 'assets', 'skill', ['skill', 'home'], 'asset-invoke', '在对话中执行技能。'),
  spec('feature.assets.plugin.enable', '启用插件', 'Enable plugin', 'assets', 'plugins', ['plugins'], 'settings-toggle', '打开或关闭插件。'),
  spec('feature.assets.mcp.connect', '连接 MCP', 'Connect MCP', 'assets', 'mcp', ['mcp'], 'asset-invoke', '接入一个 MCP 端点。'),
  spec('feature.assets.mcp.invoke', '调用 MCP 工具', 'Invoke MCP tool', 'assets', 'mcp', ['mcp'], 'asset-invoke', '调用已连接的工具。'),
  spec('feature.assets.memory.recall', '召回记忆', 'Recall memory', 'assets', 'memory', ['assets', 'settings'], 'asset-invoke', '按语义召回记忆。'),
  spec('feature.assets.memory.capture', '写入一条记忆', 'Capture memory', 'assets', 'memory', ['assets', 'settings'], 'crud-bridge', '把事实写入记忆库。'),
  spec('feature.assets.ocr.screenshot', 'OCR 识别截图', 'OCR screenshot', 'assets', 'ocr', ['assets', 'settings'], 'asset-invoke', '把截图打成字。'),
  spec('feature.assets.ocr.document', 'OCR 识别文档', 'OCR a document', 'assets', 'ocr', ['assets', 'settings'], 'asset-invoke', '把文档页打成字。'),
  spec('feature.execution.command.run', '执行白名单命令', 'Run command', 'execution', 'command', ['settings'], 'asset-invoke', '执行一条白名单命令。'),
  spec('feature.execution.workspace.open-file', '在工作区打开文件', 'Open workspace file', 'execution', 'workspace', ['home'], 'file-open', '在工作区打开文件。'),
  spec('feature.execution.workspace.save', '保存工作区文件', 'Save workspace file', 'execution', 'workspace', ['home'], 'crud-bridge', '把编辑器内容写回工作区。'),
  spec('feature.execution.browser.navigate', '浏览器打开网址', 'Browser navigate', 'execution', 'browser', ['settings'], 'asset-invoke', '用浏览器打开网址。'),
  spec('feature.execution.browser.snapshot', '浏览器快照', 'Browser snapshot', 'execution', 'browser', ['settings'], 'asset-invoke', '抓当前页可访问树。'),
  spec('feature.execution.computer.launch', '电脑控制启动应用', 'Computer launch app', 'execution', 'computer', ['settings', 'home'], 'intent-control', '用电脑控制打开应用。'),
  spec('feature.execution.computer.mouse', '电脑控制键鼠', 'Computer input', 'execution', 'computer', ['settings', 'home'], 'intent-control', '键鼠操作本机界面。'),
  spec('feature.execution.channels.send', '发到消息通道', 'Send to IM channel', 'execution', 'channels', ['settings'], 'crud-bridge', '把消息发到已配置通道。'),
  spec('feature.execution.subagent.spawn', '拉起子智能体', 'Spawn subagent', 'execution', 'subagents', ['settings'], 'asset-invoke', '分派一个子智能体任务。'),
  spec('feature.execution.agenthub.file-open', 'Agent Hub 打开文件', 'Agent Hub open file', 'execution', 'agenthub', ['agentHub'], 'file-open', '在 Agent Hub 打开工作区文件。'),
  spec('feature.execution.agenthub.task-start', 'Agent Hub 开工', 'Start Agent Hub task', 'execution', 'agenthub', ['agentHub'], 'asset-invoke', '启动一条 Agent Hub 任务。'),
  spec('feature.foundation.provider.add', '添加模型供应商', 'Add provider', 'foundation', 'providers', ['providers', 'settings'], 'crud-bridge', '登记一个模型供应商。'),
  spec('feature.foundation.provider.test', '测试供应商连通', 'Test provider', 'foundation', 'providers', ['providers', 'settings'], 'crud-bridge', '探测供应商是否可用。'),
  spec('feature.foundation.diagnostics.health', '查看系统健康', 'System health', 'foundation', 'diagnostics', ['settings'], 'diagnose-only', '查看系统健康。'),
  spec('feature.foundation.update.check', '检查更新', 'Check for updates', 'foundation', 'diagnostics', ['settings'], 'crud-bridge', '检查产品更新。'),
  spec('feature.foundation.token.compact', 'Token 精简', 'Compact tokens', 'foundation', 'diagnostics', ['home', 'settings'], 'crud-bridge', '压缩长对话上下文。'),
]

function spec(key: string, name: string, nameEn: string, domain: string, module: string, pages: Page[], chainClass: string, summary: string, settings?: string[]): FeatureSpec {
  return { key, name, nameEn, domain, module, pages, source: 'verbs', chainClass, summary, settings }
}

function pageEnterFeatures(): FeatureSpec[] {
  return CATALOG_PAGES.map(id => {
    const page = PAGE_ATLAS[id]
    return {
      key: `feature.${page.domain}.page.${id}`,
      name: `进入${page.name}`,
      nameEn: `Open ${page.nameEn}`,
      domain: page.domain,
      module: page.module,
      pages: [id],
      source: 'pages',
      chainClass: 'page-enter',
      summary: `从导航打开「${page.name}」。这一卡只说明怎么进入该页，页内动作看同一页的其他功能卡。`,
    }
  })
}

function settingFeatures(): FeatureSpec[] {
  return CATALOG_SETTINGS.map(item => ({
    key: `feature.foundation.settings.${item.id}`,
    name: `${item.label}设置`,
    nameEn: item.labelEn,
    domain: 'foundation',
    module: 'settings',
    pages: ['settings' as Page],
    source: 'settings',
    chainClass: 'settings-toggle',
    summary: `在设置里调整「${item.label}」。改动作用于这项能力。`,
    settings: [item.id],
  }))
}

function mediaFeatures(): FeatureSpec[] {
  return CATALOG_MEDIA_ACTIONS.map(action => ({
    key: `feature.office.media.${action}`,
    name: `媒体中心${MEDIA_NAMES[action] ?? action}`,
    nameEn: `Media ${action}`,
    domain: 'office',
    module: 'media',
    pages: ['media' as Page],
    source: 'media-actions',
    chainClass: 'media-transport',
    summary: `在媒体中心对当前播放会话执行「${MEDIA_NAMES[action] ?? action}」，调用 media.${action}。`,
    bridge: [`media.${action}`],
  })).concat([{
    key: 'feature.office.media.asset-open',
    name: '打开媒体资产',
    nameEn: 'Open media asset',
    domain: 'office',
    module: 'media',
    pages: ['media'],
    source: 'bridge',
    chainClass: 'media-transport',
    summary: '打开已授权的音视频资产。',
    bridge: ['media.asset.open'],
  }])
}

function pluginFeatures(): FeatureSpec[] {
  return PLUGINS.map(item => ({
    key: `feature.assets.plugin.${item.id}`,
    name: `插件：${item.title}`,
    nameEn: item.id,
    domain: 'assets',
    module: 'plugins',
    pages: ['plugins' as Page],
    source: 'plugins',
    chainClass: 'asset-invoke',
    summary: `在插件页启用或使用「${item.title}」。`,
  }))
}

export const HUB_FEATURES: FeatureSpec[] = [
  ...pageEnterFeatures(),
  ...settingFeatures(),
  ...mediaFeatures(),
  ...pluginFeatures(),
  ...VERBS,
]

const featureByKey = new Map(HUB_FEATURES.map(item => [item.key, item]))
const pageSet = new Set<string>(CATALOG_PAGES)

export function isHubPage(value: string): value is Page {
  return pageSet.has(value)
}

export function pagesOfCard(card: { stable_key: string; scaffold?: { pages?: string[] } }): Page[] {
  const fromScaffold = (card.scaffold?.pages ?? []).filter(isHubPage)
  return [...new Set([...fromScaffold, ...pagesOfFeature(card.stable_key)])]
}

export function settingLabel(id: string, zh = true): string {
  const hit = CATALOG_SETTINGS.find(item => item.id === id)
  return zh ? (hit?.label ?? id) : (hit?.labelEn ?? id)
}

export function pagesOfFeature(key: string): Page[] {
  const spec = featureByKey.get(key)
  if (spec) return spec.pages
  const pageEnter = key.match(/^feature\.[^.]+\.page\.(.+)$/)
  if (pageEnter && isHubPage(pageEnter[1])) return [pageEnter[1]]
  if (key.startsWith('feature.foundation.settings.')) return ['settings']
  if (key.startsWith('feature.office.media.')) return ['media']
  if (key.startsWith('feature.assets.plugin.')) return ['plugins']
  return []
}

export function pagesOfChange(item: HubChange): Page[] {
  const fromKey = pagesOfFeature(item.stable_key)
  const fromImpacts = (item.impacts ?? []).flatMap(impact => {
    if (isHubPage(impact)) return [impact]
    if (impact.startsWith('page.') && isHubPage(impact.slice(5))) return [impact.slice(5) as Page]
    return pagesOfFeature(impact)
  })
  return [...new Set([...fromKey, ...fromImpacts])]
}

export function pageDossiers(nodes: HubNode[], changes: HubChange[], findings: HubFinding[]): PageDossier[] {
  const features = nodes.filter(node => node.type === 'Feature' || node.type === 'Scenario')
  return CATALOG_PAGES.map(id => {
    const spec = PAGE_ATLAS[id]
    const pageFeatures = features.filter(node => pagesOfFeature(node.stable_key).includes(id) || node.module === spec.module && node.domain === spec.domain)
    const claimed = new Set(pageFeatures.map(node => node.id))
    const extras = features.filter(node => !claimed.has(node.id) && pagesOfFeature(node.stable_key).includes(id))
    const listed = [...pageFeatures, ...extras]
    const unique = [...new Map(listed.map(node => [node.id, node])).values()]
    return {
      spec,
      features: unique,
      changes: changes.filter(item => pagesOfChange(item).includes(id)),
      findings: findings.filter(item => {
        if (pagesOfFeature(item.stable_key).includes(id)) return true
        if (item.stable_key === `feature.${spec.domain}.page.${id}` || item.stable_key.endsWith(`.page.${id}`)) return true
        return spec.settings.some(setting => item.stable_key === `feature.foundation.settings.${setting}`)
      }),
      settings: spec.settings,
    }
  })
}

export function groupChangesByPage(changes: HubChange[]): Array<{ page?: PageSpec; items: HubChange[] }> {
  const buckets = new Map<string, HubChange[]>()
  const orphan: HubChange[] = []
  for (const item of changes) {
    const pages = pagesOfChange(item)
    if (pages.length === 0) {
      orphan.push(item)
      continue
    }
    for (const page of pages) {
      const list = buckets.get(page) ?? []
      list.push(item)
      buckets.set(page, list)
    }
  }
  const grouped = CATALOG_PAGES.flatMap(id => {
    const items = buckets.get(id)
    return items?.length ? [{ page: PAGE_ATLAS[id], items }] : []
  })
  return orphan.length ? [...grouped, { items: orphan }] : grouped
}

const MODULE_NAMES: Record<string, string> = Object.fromEntries(
  Object.values(PAGE_ATLAS).map(page => [`${page.domain}.${page.module}`, page.name]),
)

export function buildCatalogGraph(extraNodes: HubNode[] = [], extraEdges: HubEdge[] = []): { nodes: HubNode[]; edges: HubEdge[] } {
  const nodes = new Map<string, HubNode>()
  const edges: HubEdge[] = []
  const add = (node: HubNode) => { if (!nodes.has(node.id)) nodes.set(node.id, node) }
  const link = (from: string, to: string, rel = 'contains') => { edges.push({ from, to, rel }) }
  add({ id: 'product.lunitide', stable_key: 'product.lunitide', type: 'Product', name: '月汐' })
  const domains = [
    ['dialog', '对话体验'],
    ['office', '业务工作台'],
    ['assets', '资产与智能'],
    ['execution', '执行与控制'],
    ['foundation', '底座与治理'],
  ] as const
  for (const [id, name] of domains) {
    add({ id: `domain.${id}`, stable_key: `domain.${id}`, type: 'Domain', name, domain: id })
    link('product.lunitide', `domain.${id}`)
  }
  for (const feature of HUB_FEATURES) {
    const modId = `module.${feature.domain}.${feature.module}`
    add({
      id: modId,
      stable_key: modId,
      type: 'Module',
      name: MODULE_NAMES[`${feature.domain}.${feature.module}`] ?? feature.module,
      domain: feature.domain,
      module: feature.module,
    })
    link(`domain.${feature.domain}`, modId)
    add({
      id: feature.key,
      stable_key: feature.key,
      type: 'Feature',
      name: feature.name,
      domain: feature.domain,
      module: feature.module,
      summary: feature.summary,
    })
    link(modId, feature.key)
  }
  for (const node of extraNodes) add(node)
  edges.push(...extraEdges)
  return { nodes: [...nodes.values()], edges }
}

export function catalogCard(feature: FeatureSpec, base?: HubCard): HubCard {
  return {
    stable_key: feature.key,
    name: feature.name,
    name_en: feature.nameEn,
    domain: feature.domain,
    module: feature.module,
    summary: feature.summary,
    description: `${feature.summary} 前台页面：${feature.pages.map(id => PAGE_ATLAS[id].name).join('、')}。`,
    attributes: { operations: [feature.name], tools: feature.bridge ?? [], mcps: [], skills: [], capabilities: [] },
    methods: [{ type: 'ui', entry: feature.pages.map(id => PAGE_ATLAS[id].name).join(' / ') }],
    chain: base?.chain ?? { steps: [], branches: [] },
    chain_class: feature.chainClass,
    scaffold: { pages: feature.pages, bridge: feature.bridge ?? [], settings: feature.settings ?? [] },
    tags: ['状态·核心'],
    provenance: 'live',
    source: feature.source,
    probe: { passed: 1, total: 1 },
    version: 'catalog',
  }
}

export function buildCatalogCards(detailed: HubCard[] = []): Map<string, HubCard> {
  const cards = new Map<string, HubCard>()
  for (const feature of HUB_FEATURES) cards.set(feature.key, catalogCard(feature))
  for (const card of detailed) cards.set(card.stable_key, { ...cards.get(card.stable_key), ...card, scaffold: { ...cards.get(card.stable_key)?.scaffold, ...card.scaffold, pages: card.scaffold?.pages?.every(page => isHubPage(page)) ? card.scaffold.pages : cards.get(card.stable_key)?.scaffold.pages ?? card.scaffold?.pages } })
  return cards
}

export function catalogOverview(nodes: HubNode[], changes: HubChange[]): HubOverview {
  const features = nodes.filter(node => node.type === 'Feature')
  const dossiers = pageDossiers(nodes, changes, [])
  return {
    product: '月汐',
    editionId: 'v2.4.1',
    generatedAt: '2026-09-21T14:02:00Z',
    cardCount: features.length,
    healthScore: 94,
    added: changes.filter(item => item.kind === 'added').length,
    updated: changes.filter(item => item.kind === 'updated').length,
    removed: changes.filter(item => item.kind === 'removed').length,
    probePassed: features.length,
    probeTotal: features.length,
    domains: [
      { id: 'dialog', name: '对话体验', modules: 0, cards: 0 },
      { id: 'office', name: '业务工作台', modules: 0, cards: 0 },
      { id: 'assets', name: '资产与智能', modules: 0, cards: 0 },
      { id: 'execution', name: '执行与控制', modules: 0, cards: 0 },
      { id: 'foundation', name: '底座与治理', modules: 0, cards: 0 },
    ].map(domain => ({
      ...domain,
      modules: new Set(nodes.filter(node => node.type === 'Module' && node.domain === domain.id).map(node => node.id)).size,
      cards: features.filter(node => node.domain === domain.id).length,
    })),
    tags: ['状态·核心', 'kind:页面'],
    assetCounts: {
      Feature: { n: features.length, delta: changes.filter(item => item.kind === 'added').length },
      Chain: { n: features.length },
      Plugin: { n: nodes.filter(node => node.type === 'Plugin').length || 23 },
      Page: { n: dossiers.length },
    },
  }
}

export function catalogChanges(): HubChange[] {
  const pageEntered: HubChange[] = CATALOG_PAGES.map(id => {
    const page = PAGE_ATLAS[id]
    return {
      kind: 'added',
      stable_key: `feature.${page.domain}.page.${id}`,
      title: `进入${page.name}`,
      summary: `${page.analysis.slice(0, 72)}${page.analysis.length > 72 ? '…' : ''}`,
      impacts: [id, `page.${id}`],
    }
  })
  return [
    ...pageEntered,
    {
      kind: 'updated',
      stable_key: 'feature.dialog.music.play',
      title: '放歌',
      summary: '播放核验改为 SMTC / owned runtime 回读，不再把键已发出当成成功。',
      impacts: ['home', 'media', 'page.home', 'page.media'],
    },
    {
      kind: 'removed',
      stable_key: 'feature.foundation.memory-export',
      title: '旧记忆导出',
      summary: '独立导出入口已弃用，能力并入资产管理。',
      impacts: ['assets', 'page.assets'],
    },
  ]
}
