import React, { useEffect, useRef, useState } from 'react'
import{getAppUpdateBridge,getCollabGateBridge,getDiagnosticsBridge,getMcpBridge,getPluginBridge,getTtsBridge,hooksPolicyBridge,projectBridge,systemSettingsBridge,toolsPolicyBridge,brBridge,type BrBridge,type HooksPolicyBridge,type McpBridge,type ToolsPolicyBridge,type TtsVoice}from'../bridge/client'
import type{BrDataUsageResult,BrModeDetectResult,BrPermissionListResult,BrPermissionPolicyPayload,BrSessionListResult,BrSettingsGetResult,BrSettingsUpdatePayload,Mcp6PresetsListResult,ProjectDTO,ToolsHooksPolicySetPayload,TtsRefAudiosResult}from'../generated/bridge'
import{microphoneConstraints,saveMicrophoneId,selectedMicrophoneId}from'./microphone'
import{defaultCompanionSettings,loadCompanionSettings,saveCompanionSettings,type CompanionSettings}from'../session/companion/companionSettings'
import{OrgAdminPage}from'../org/OrgAdminPage'
import{MemoryPage}from'../memory/MemoryPage'
import{OntologyPage}from'../ontology/OntologyPage'
import{PlanPage}from'../plan/PlanPage'
import{ReviewPage}from'../review/ReviewPage'
type SettingsCategory = 'general' | 'appearance' | 'providers' | 'voice' | 'org' | 'data' | 'security' | 'mcp' | 'browser' | 'plugins' | 'collab' | 'diagnostics' | 'about'

/** Reference-timbre picker start directory (user's voice collection). */
const DEFAULT_REF_DIR = 'E:\\AI电影漫剧\\800+音色合集'

// sha256Hex mirrors m8core.DigestOf for bridge confirm tokens (hex, 64).
const sha256Hex = async (value: string): Promise<string> => {
  const digest = await globalThis.crypto.subtle.digest('SHA-256', new TextEncoder().encode(value))
  return Array.from(new Uint8Array(digest)).map(byte => byte.toString(16).padStart(2, '0')).join('')
}

const CATEGORIES: { id: SettingsCategory; icon: string; label: string }[] = [
  { id: 'general', icon: '◌', label: '常规' },
  { id: 'appearance', icon: '◐', label: '外观' },
  { id: 'providers', icon: '◈', label: '模型与供应商' },
  { id: 'voice', icon: '◉', label: '语音与麦克风' },
  { id: 'org', icon: '⬡', label: '组织管理' },
  { id: 'data', icon: '❖', label: '数据与记忆' },
  { id: 'security', icon: '⛨', label: '安全与治理' },
  { id: 'mcp', icon: '⧉', label: 'MCP 服务器' },
  { id: 'browser', icon: '⬟', label: '浏览器' },
  { id: 'plugins', icon: '⬢', label: '插件' },
  { id: 'collab', icon: '⌘', label: '协作门禁' },
  { id: 'diagnostics', icon: '◉', label: '诊断与更新' },
  { id: 'about', icon: 'ⓘ', label: '关于' },
]

interface GeneralSettings {
  startupPage: 'new' | 'last' | 'projects'
  restoreUnfinished: boolean
  recentProjectCount: 5 | 8 | 10
  language: 'zh-CN' | 'en'
  timezone: string
  enterToSend: boolean
  autoTitle: boolean
  defaultMode: 'auto' | 'collab' | 'code'
}

interface AppearanceSettings {
  starlight: boolean
  moonlight: boolean
  density: 'compact' | 'standard' | 'roomy'
  reduceMotion: boolean
}

const DEFAULT_GENERAL: GeneralSettings = {
  startupPage: 'new',
  restoreUnfinished: true,
  recentProjectCount: 8,
  language: 'zh-CN',
  timezone: 'Asia/Shanghai',
  enterToSend: true,
  autoTitle: true,
  defaultMode: 'auto',
}

const DEFAULT_APPEARANCE: AppearanceSettings = {
  starlight: true,
  moonlight: true,
  density: 'standard',
  reduceMotion: false,
}

function loadSettings<T>(key: string, fallback: T): T {
  try {
    const raw = localStorage.getItem(`lunitide:${key}`)
    if (raw) return { ...fallback, ...JSON.parse(raw) }
  } catch { /* ignore */ }
  return fallback
}

function saveSettings<T>(key: string, value: T): void {
  try { localStorage.setItem(`lunitide:${key}`, JSON.stringify(value)) } catch { /* ignore */ }
}

export function SettingsPage({ onNavigateProviders, onBack, initialCategory = 'general' }: { onNavigateProviders?: () => void; onBack?: () => void; initialCategory?: SettingsCategory }): React.JSX.Element {
  const [category, setCategory] = useState<SettingsCategory>(initialCategory)
  const [general, setGeneral] = useState<GeneralSettings>(() => loadSettings('general', DEFAULT_GENERAL))
  const [appearance, setAppearance] = useState<AppearanceSettings>(() => loadSettings('appearance', DEFAULT_APPEARANCE))
  const [saved, setSaved] = useState(false)

  useEffect(() => {
    if (saved) { const t = setTimeout(() => setSaved(false), 2000); return () => clearTimeout(t) }
  }, [saved])

  const updateGeneral = <K extends keyof GeneralSettings>(key: K, value: GeneralSettings[K]): void => {
    const next = { ...general, [key]: value }
    setGeneral(next)
    saveSettings('general', next)
    setSaved(true)
  }

  const updateAppearance = <K extends keyof AppearanceSettings>(key: K, value: AppearanceSettings[K]): void => {
    const next = { ...appearance, [key]: value }
    setAppearance(next)
    saveSettings('appearance', next)
    applyAppearance(next)
    setSaved(true)
  }

  return (
    <div className="settings-shell">
      <nav className="settings-nav" aria-label="设置导航">
        <button className="settings-back" onClick={onBack}>← 返回主页</button>
        <div className="settings-search" role="search">
          <input type="search" placeholder="搜索设置…" aria-label="搜索设置" />
        </div>
        <div className="settings-menu">
          {CATEGORIES.map(c => (
            <button
              key={c.id}
              className={`settings-item ${category === c.id ? 'on' : ''}`}
              onClick={() => setCategory(c.id)}
              aria-current={category === c.id ? 'page' : undefined}
            >
              <span className="ic" aria-hidden="true">{c.icon}</span>
              {c.label}
            </button>
          ))}
        </div>
      </nav>
      <div className="settings-content">
        <div className="settings-top">
          <h2 style={{ margin: 0, fontFamily: 'var(--serif)', fontSize: '20px' }}>
            {CATEGORIES.find(c => c.id === category)?.label}
          </h2>
          {saved && <span className="save-indicator" role="status">✓ 已保存</span>}
        </div>
        <div className="settings-body">
          {category === 'general' && <GeneralPanel settings={general} onChange={updateGeneral} />}
          {category === 'appearance' && <AppearancePanel settings={appearance} onChange={updateAppearance} />}
          {category === 'providers' && <ProvidersPanel onNavigate={onNavigateProviders} />}
          {category === 'voice' && <VoicePanel />}
          {category === 'org' && <OrgAdminPage />}
          {category === 'data' && <ProjectScopedTabs tabs={[{ id: 'memory', label: '记忆', render: pid => <MemoryPage projectId={pid} /> }, { id: 'ontology', label: '本体', render: pid => <OntologyPage projectId={pid} /> }]} />}
          {category === 'security' && <>
            <CommandPolicyPanel />
            <HooksPanel />
            <ProjectScopedTabs tabs={[{ id: 'review', label: '审批', render: pid => <ReviewPage projectId={pid} /> }, { id: 'plans', label: '计划管理', render: pid => <PlanPage projectId={pid} /> }]} />
          </>}
          {category === 'mcp' && <McpPanel />}
          {category === 'browser' && <BrowserPanel />}
          {category === 'plugins' && <PluginsPanel />}
          {category === 'collab' && <CollabGatePanel />}
          {category === 'diagnostics' && <DiagnosticsPanel />}
          {category === 'about' && <AboutPanel />}
        </div>
      </div>
    </div>
  )
}

function Toggle({ on, onChange, label, desc }: { on: boolean; onChange: (v: boolean) => void; label: string; desc?: string }): React.JSX.Element {
  return (
    <div className="setting-row">
      <div>
        <div className="setting-label">{label}</div>
        {desc && <div className="setting-desc">{desc}</div>}
      </div>
      <button
        className={`toggle ${on ? 'on' : ''}`}
        onClick={() => onChange(!on)}
        role="switch"
        aria-checked={on}
        aria-label={label}
      >
        <span className="toggle-knob" />
      </button>
    </div>
  )
}

function SelectRow({ label, desc, value, options, onChange }: {
  label: string; desc?: string; value: string; options: { value: string; label: string }[]; onChange: (v: string) => void
}): React.JSX.Element {
  return (
    <div className="setting-row">
      <div>
        <div className="setting-label">{label}</div>
        {desc && <div className="setting-desc">{desc}</div>}
      </div>
      <select value={value} onChange={e => onChange(e.target.value)} className="setting-select">
        {options.map(o => <option key={o.value} value={o.value}>{o.label}</option>)}
      </select>
    </div>
  )
}

function GeneralPanel({ settings, onChange }: { settings: GeneralSettings; onChange: <K extends keyof GeneralSettings>(k: K, v: GeneralSettings[K]) => void }): React.JSX.Element {
  return (
    <>
      <div className="setting-group">
        <div className="setting-group-title">启动</div>
        <SelectRow
          label="启动时打开"
          desc="建议保持'新对话'，减少每次启动的决策负担。"
          value={settings.startupPage}
          options={[
            { value: 'new', label: '新对话' },
            { value: 'last', label: '上次会话' },
            { value: 'projects', label: '项目选择' },
          ]}
          onChange={v => onChange('startupPage', v as GeneralSettings['startupPage'])}
        />
        <Toggle
          label="恢复未完成运行"
          desc="启动后询问是否从最近 checkpoint 继续，不会自动执行副作用。"
          on={settings.restoreUnfinished}
          onChange={v => onChange('restoreUnfinished', v)}
        />
        <SelectRow
          label="最近项目数量"
          desc="控制首页左栏展示的最近项目数量。"
          value={String(settings.recentProjectCount)}
          options={[
            { value: '5', label: '5 个' },
            { value: '8', label: '8 个' },
            { value: '10', label: '10 个' },
          ]}
          onChange={v => onChange('recentProjectCount', Number(v) as GeneralSettings['recentProjectCount'])}
        />
      </div>
      <div className="setting-group">
        <div className="setting-group-title">语言与区域</div>
        <SelectRow
          label="界面语言"
          desc="重启 Renderer 后完整生效。"
          value={settings.language}
          options={[
            { value: 'zh-CN', label: '简体中文' },
            { value: 'en', label: 'English' },
          ]}
          onChange={v => onChange('language', v as GeneralSettings['language'])}
        />
        <SelectRow
          label="时区"
          desc="用于会话、审计、计划和发布记录。"
          value={settings.timezone}
          options={[
            { value: 'Asia/Shanghai', label: 'Asia/Shanghai (UTC+8)' },
            { value: 'UTC', label: 'UTC' },
            { value: 'America/Los_Angeles', label: 'America/Los_Angeles (UTC-8)' },
            { value: 'Europe/London', label: 'Europe/London (UTC+0)' },
          ]}
          onChange={v => onChange('timezone', v)}
        />
      </div>
      <div className="setting-group">
        <div className="setting-group-title">对话</div>
        <Toggle
          label="Enter 发送消息"
          desc="关闭后使用 Ctrl + Enter 发送，Enter 仅换行。"
          on={settings.enterToSend}
          onChange={v => onChange('enterToSend', v)}
        />
        <Toggle
          label="自动生成会话标题"
          desc="根据首轮目标生成，可随时手动修改。"
          on={settings.autoTitle}
          onChange={v => onChange('autoTitle', v)}
        />
        <SelectRow
          label="默认工作模式"
          desc="自动模式会根据任务选择协作或代码工具。"
          value={settings.defaultMode}
          options={[
            { value: 'auto', label: '自动模式' },
            { value: 'collab', label: '协作' },
            { value: 'code', label: '代码' },
          ]}
          onChange={v => onChange('defaultMode', v as GeneralSettings['defaultMode'])}
        />
      </div>
    </>
  )
}

function AppearancePanel({ settings, onChange }: { settings: AppearanceSettings; onChange: <K extends keyof AppearanceSettings>(k: K, v: AppearanceSettings[K]) => void }): React.JSX.Element {
  return (
    <>
      <div className="setting-group">
        <div className="setting-group-title">视觉效果</div>
        <Toggle
          label="星闪效果"
          desc="背景星空粒子动画，关闭可降低 GPU 占用。"
          on={settings.starlight}
          onChange={v => onChange('starlight', v)}
        />
        <Toggle
          label="月光辉光"
          desc="右上角月亮发光效果与浮动动画。"
          on={settings.moonlight}
          onChange={v => onChange('moonlight', v)}
        />
        <Toggle
          label="减少动态效果"
          desc="减弱或移除非必要的过渡动画和脉冲效果。"
          on={settings.reduceMotion}
          onChange={v => onChange('reduceMotion', v)}
        />
      </div>
      <div className="setting-group">
        <div className="setting-group-title">布局密度</div>
        <SelectRow
          label="界面密度"
          desc="控制组件间距和信息密度。"
          value={settings.density}
          options={[
            { value: 'compact', label: '紧凑' },
            { value: 'standard', label: '标准' },
            { value: 'roomy', label: '宽松' },
          ]}
          onChange={v => onChange('density', v as AppearanceSettings['density'])}
        />
      </div>
    </>
  )
}

function ProvidersPanel({ onNavigate }: { onNavigate?: () => void }): React.JSX.Element {
  return (
    <div className="setting-group">
      <div className="setting-group-title">模型与供应商</div>
      <div className="setting-row" style={{ gridTemplateColumns: '1fr' }}>
        <div>
          <div className="setting-label">供应商管理</div>
          <div className="setting-desc">
            首期支持 OpenAI-compatible 与 Anthropic 两协议族。API Key 默认由 Host 托管；仅在用户进入供应商编辑查看时短暂回填到受信页面，且不持久化。BaseURL 的协议或 origin 改变时，旧 credential 不自动复用。
          </div>
        </div>
      </div>
      <div className="setting-row" style={{ gridTemplateColumns: '1fr' }}>
        <button className="primary" onClick={onNavigate} style={{ alignSelf: 'flex-start' }} aria-label="打开供应商管理">
          打开供应商管理 →
        </button>
      </div>
    </div>
  )
}

function VoicePanel():React.JSX.Element{
 const[devices,setDevices]=useState<MediaDeviceInfo[]>([]),[deviceId,setDeviceId]=useState(selectedMicrophoneId),[status,setStatus]=useState(''),[busy,setBusy]=useState(false)
 const refresh=async()=>{setBusy(true);setStatus('正在检测麦克风…');try{if(!navigator.mediaDevices?.enumerateDevices)throw new Error('当前 WebView 不支持设备检测');const items=(await navigator.mediaDevices.enumerateDevices()).filter(item=>item.kind==='audioinput');setDevices(items);if(deviceId&&!items.some(item=>item.deviceId===deviceId))setStatus('当前设备列表中未显示已选麦克风；获得权限或重新连接设备后再确认。');else setStatus(items.length?`检测到 ${items.length} 个麦克风输入设备。`:'未检测到麦克风，请检查连接和 Windows 设备设置。')}catch(e){setStatus(e instanceof Error?e.message:'无法检测麦克风')}finally{setBusy(false)}}
 useEffect(()=>{void refresh()},[])
 const choose=(value:string)=>{setDeviceId(value);saveMicrophoneId(value);setStatus(value?'已保存所选麦克风。':'已使用系统默认麦克风。')}
 const test=async()=>{setBusy(true);setStatus('正在请求麦克风权限…');let stream:MediaStream|undefined;try{stream=await navigator.mediaDevices.getUserMedia(microphoneConstraints());setStatus('麦克风可用，测试成功。');const items=(await navigator.mediaDevices.enumerateDevices()).filter(item=>item.kind==='audioinput');setDevices(items)}catch(e){const name=e instanceof DOMException?e.name:'';setStatus(name==='NotAllowedError'||name==='SecurityError'?'权限被拒绝，请打开 Windows 麦克风设置并允许桌面应用访问。':name==='NotFoundError'?'未检测到可用麦克风。':'无法启动麦克风，请检查设备是否被占用或驱动异常。')}finally{stream?.getTracks().forEach(track=>track.stop());setBusy(false)}}
 const openSettings=async()=>{setBusy(true);try{await systemSettingsBridge.open({page:'privacy-microphone'});setStatus('已打开 Windows 麦克风隐私设置。请开启“麦克风访问”和“允许桌面应用访问麦克风”。')}catch(e){setStatus(e instanceof Error?e.message:'无法打开 Windows 麦克风设置')}finally{setBusy(false)}}
 return <div className="setting-group"><div className="setting-group-title">语音与麦克风</div><div className="setting-row"><div><div className="setting-label">输入设备</div><div className="setting-desc">选择语音输入使用的麦克风；设备失效时自动回退到系统默认。</div></div><select className="setting-select" aria-label="麦克风输入设备" value={deviceId} onChange={e=>choose(e.target.value)}><option value="">系统默认麦克风</option>{devices.map((device,index)=><option key={device.deviceId} value={device.deviceId}>{device.label||`麦克风 ${index+1}`}</option>)}</select></div><div className="setting-row" style={{gridTemplateColumns:'1fr'}}><div className="setting-desc">Windows 隐私权限不能由软件自动开启。请确保“麦克风访问”和“允许桌面应用访问麦克风”均已开启；语音转文字还依赖 Windows 在线语音识别服务。</div><div className="microphone-setting-actions"><button disabled={busy} onClick={()=>void refresh()}>刷新设备</button><button disabled={busy} onClick={()=>void test()}>测试麦克风</button><button className="primary" disabled={busy} onClick={()=>void openSettings()}>打开 Windows 麦克风设置</button></div>{status&&<p role="status" className="notice">{status}</p>}</div><CompanionSection/></div>
}
function CompanionSection():React.JSX.Element{
 const[companion,setCompanion]=useState<CompanionSettings>(defaultCompanionSettings)
 const[voices,setVoices]=useState<TtsVoice[]>([])
 const[engineState,setEngineState]=useState<'probing'|'available'|'unavailable'>('probing')
 const[status,setStatus]=useState('')
 const[busy,setBusy]=useState(false)
 const[refDirInput,setRefDirInput]=useState('')
 const[refEntries,setRefEntries]=useState<TtsRefAudiosResult['entries']|null>(null)
 const[refDir,setRefDir]=useState('')
 const[refMissing,setRefMissing]=useState(false)
 const audioRef=useRef<HTMLAudioElement|undefined>(undefined)
 useEffect(()=>{setCompanion(loadCompanionSettings());setRefDirInput(loadCompanionSettings().refDir||DEFAULT_REF_DIR);return()=>{audioRef.current?.pause();audioRef.current=undefined}},[])
 // Voice catalogue follows the selected engine: edge/ref are static
 // catalogues; sapi probes the machine (M95-001 degradation).
 useEffect(()=>{let cancelled=false;setEngineState('probing');getTtsBridge().voices({engine:companion.engine}).then(r=>{if(cancelled)return;setVoices(r.voices);setEngineState(r.voices.length?'available':'unavailable')}).catch(()=>{if(!cancelled){setVoices([]);setEngineState('unavailable')}});return()=>{cancelled=true}},[companion.engine])
 const save=(next:CompanionSettings)=>{setCompanion(next);saveCompanionSettings(next);setStatus('设置已保存，立即生效。')}
 const browseRefDir=async(dir:string)=>{if(!dir.trim()){setStatus('请输入参考音频目录路径。');return}setBusy(true);setStatus('正在读取目录…');try{const r=await getTtsBridge().refAudios(dir.trim());setRefEntries(r.entries);setRefDir(r.dir);setRefDirInput(r.dir);setRefMissing(!r.exists);setStatus(r.exists?`目录已读取：${r.dir}`:'目录不存在，请检查路径。')}catch(e){setStatus(e instanceof Error?`读取目录失败：${e.message}`:'读取目录失败')}finally{setBusy(false)}}
 const preview=async()=>{
  if(companion.engine==='ref'&&(!companion.refWavPath||!companion.refEndpoint)){setStatus('参考音色引擎需要先填写服务地址并选择参考音频。');return}
  setBusy(true);setStatus('正在合成试听…')
  try{
   const voiceId=companion.engine==='ref'?undefined:companion.voiceId&&(voices.some(v=>v.voice_id===companion.voiceId)?companion.voiceId:(setStatus('所选音色不可用，已回退默认音色。'),undefined))
   const result=await getTtsBridge().synthesize({text:'你好，我是月汐，很高兴与你同行。',voiceId,rate:companion.rate,volume:companion.volume,engine:companion.engine,refEndpoint:companion.engine==='ref'?companion.refEndpoint:undefined,refWavPath:companion.engine==='ref'?companion.refWavPath:undefined,refPromptText:companion.engine==='ref'?companion.refPromptText:undefined})
   if(result.discarded||!result.wav_base64){setStatus('试听已取消。');return}
   const bytes=Uint8Array.from(atob(result.wav_base64),c=>c.charCodeAt(0)),url=URL.createObjectURL(new Blob([bytes],{type:'audio/wav'}))
   audioRef.current?.pause();const audio=new Audio(url);audioRef.current=audio;audio.onended=()=>URL.revokeObjectURL(url);await audio.play()
   setStatus(result.notice==='TTS_ENGINE_FALLBACK'?'网络不可用，本次试听已回退本机 SAPI 语音（联网后自动恢复自然语音）。':result.notice==='TTS_VOICE_NOT_FOUND'?'所选音色不可用，已回退默认音色（M95-004）。':'试听播放中…')
  }catch(e){setStatus(e instanceof Error?`试听失败：${e.message}`:'试听失败，请检查引擎配置')}finally{setBusy(false)}
 }
 const engineDesc=companion.engine==='edge'?(engineState==='probing'?'正在获取自然语音列表…':engineState==='available'?`自然语音音色池 ${voices.length} 个，按角色风格分组（女声/男声/方言/外语），联网使用，断网自动回退本机语音`:'自然语音列表不可用'):companion.engine==='ref'?'参考音色克隆：朗读音色跟随所选参考音频（需本地 GPT-SoVITS 兼容服务）':engineState==='probing'?'正在检测本机语音合成引擎…':engineState==='available'?`本机可用音色 ${voices.length} 个（Windows SAPI，离线合成）`:'本机无语音合成引擎（M95-001），月伴将自动切换字幕模式'
 const voiceDisabled=companion.engine==='ref'||engineState!=='available'
 const fmtSize=(n:number)=>n>=1048576?`${(n/1048576).toFixed(1)} MB`:n>=1024?`${Math.round(n/1024)} KB`:`${n} B`
 // Role-play voice pool grouping: zh-CN splits by gender, other zh-* are
 // dialects, everything else is foreign-language. Generic enough to also
 // group the SAPI catalogue the same way.
 const voiceGroups=(()=>{const g=new Map<string,typeof voices>();for(const v of voices){const lang=v.lang||'';const grp=lang==='zh-CN'?(v.gender==='male'?'男声 · 阳光少年 / 沉稳大叔':'女声 · 温柔 / 活泼 / 甜美'):lang.startsWith('zh-')?'方言 · 东北 / 陕西 / 粤语 / 台湾':'外语 · 英语 / 日语';const arr=g.get(grp)||[];arr.push(v);g.set(grp,arr)}return[...g.entries()]})()
 return <><div className="setting-row" style={{gridTemplateColumns:'1fr'}}><div className="setting-group-title" style={{marginTop:8}}>月伴对话</div></div><Toggle label="启用月伴对话" desc="在普通聊天输入框显示月亮按钮，进入全屏语音对话舞台；关闭即回滚入口。" on={companion.enabled} onChange={v=>save({...companion,enabled:v})}/><Toggle label="语音唤醒（你好，月汐）" desc="在首页待命：说「你好，月汐」自动进入月伴对话，唤醒词后的话会作为提问直接回答；依赖 Windows 在线语音识别与麦克风权限。" on={companion.wakeWord} onChange={v=>save({...companion,wakeWord:v})}/><Toggle label="回复自动朗读" desc="回复完成后自动用本机语音朗读；关闭后仅显示字幕。" on={companion.autoSpeak} onChange={v=>save({...companion,autoSpeak:v})}/><div className="setting-row"><div><div className="setting-label">朗读引擎</div><div className="setting-desc">自然语音更接近真人；参考音色可用本地音色库克隆任意声音。</div></div><select className="setting-select" aria-label="朗读引擎" value={companion.engine} onChange={e=>save({...companion,engine:e.target.value as CompanionSettings['engine']})}><option value="edge">自然语音（在线 · 推荐）</option><option value="sapi">本机语音（离线 SAPI）</option><option value="ref">参考音色克隆（本地 GPT-SoVITS）</option></select></div><div className="setting-row"><div><div className="setting-label">朗读音色</div><div className="setting-desc">{engineDesc}</div></div><div style={{display:'flex',gap:8,alignItems:'center'}}>{companion.engine!=='ref'&&<select className="setting-select" aria-label="朗读音色" disabled={voiceDisabled} value={companion.voiceId} onChange={e=>save({...companion,voiceId:e.target.value})}><option value="">默认音色</option>{voiceGroups.map(([grp,items])=><optgroup key={grp} label={grp}>{items.map(voice=><option key={voice.voice_id} value={voice.voice_id}>{voice.display_name}</option>)}</optgroup>)}</select>}<button disabled={busy||(companion.engine!=='ref'&&engineState!=='available')} onClick={()=>void preview()}>{busy?'合成中…':'试听'}</button></div></div>
 {companion.engine==='ref'&&<>
  <div className="setting-row"><div><div className="setting-label">参考音色服务地址</div><div className="setting-desc">GPT-SoVITS api_v2 兼容服务，默认 http://127.0.0.1:9880；服务与本应用需能访问同一份参考音频。</div></div><input className="setting-input" style={{width:280}} aria-label="参考音色服务地址" value={companion.refEndpoint} placeholder="http://127.0.0.1:9880" onChange={e=>save({...companion,refEndpoint:e.target.value})}/></div>
  <div className="setting-row" style={{gridTemplateColumns:'1fr'}}><div><div className="setting-label">参考音频</div><div className="setting-desc">选择一段 5~10 秒的清晰人声作为音色参考，例如音色合集目录中的文件；目录与文件均保存在本机。</div><div style={{display:'flex',gap:8,alignItems:'center',marginTop:8,flexWrap:'wrap'}}><input className="setting-input" style={{flex:1,minWidth:260}} aria-label="参考音频目录" value={refDirInput} placeholder="E:\AI电影漫剧\800+音色合集" onChange={e=>setRefDirInput(e.target.value)}/><button disabled={busy} onClick={()=>void browseRefDir(refDirInput)}>{busy?'读取中…':'浏览目录'}</button></div>
  {refEntries&&<div aria-label="参考音频文件列表" style={{marginTop:8,border:'1px solid var(--border)',borderRadius:8,maxHeight:200,overflowY:'auto'}}>{refMissing&&<div style={{padding:'6px 10px',color:'var(--muted)'}}>目录不存在，请检查路径。</div>}{refEntries.map(entry=><button key={entry.path} onClick={entry.is_dir?()=>void browseRefDir(entry.path):()=>save({...companion,refWavPath:entry.path,refDir})} style={{display:'flex',justifyContent:'space-between',gap:8,width:'100%',padding:'5px 10px',border:'none',background:entry.is_dir?'transparent':entry.path===companion.refWavPath?'var(--tide1-soft, rgba(120,140,200,.18))':'transparent',color:entry.is_dir?'var(--tide1)':'inherit',textAlign:'left',cursor:'pointer',fontSize:13}}><span>{entry.is_dir?'📁 ':entry.path===companion.refWavPath?'✓ ':''}{entry.name}</span>{!entry.is_dir&&<span style={{color:'var(--muted)',flexShrink:0}}>{fmtSize(entry.size_bytes)}</span>}</button>)}</div>}
  {companion.refWavPath&&<div className="setting-desc" style={{marginTop:6}}>已选参考音频:{companion.refWavPath}</div>}</div></div>
  <div className="setting-row"><div><div className="setting-label">参考音频文本</div><div className="setting-desc">参考音频里说的话（转写文本），克隆效果依赖它；留空时部分服务会自动推断。</div></div><input className="setting-input" style={{width:280}} aria-label="参考音频文本" value={companion.refPromptText} placeholder="例如：今天天气真不错，我们出去走走吧。" onChange={e=>save({...companion,refPromptText:e.target.value})}/></div>
 </>}
 <div className="setting-row"><div><div className="setting-label">语速{companion.engine==='ref'?'（参考音色引擎暂不支持）':`（rate {-10}~{10}）`}</div><div className="setting-desc">当前 {companion.rate}</div></div><input type="range" min={-10} max={10} step={1} disabled={companion.engine==='ref'||engineState!=='available'&&companion.engine==='sapi'} value={companion.rate} aria-label="朗读语速" onChange={e=>save({...companion,rate:Number(e.target.value)})} style={{accentColor:'var(--tide1)'}}/></div><div className="setting-row"><div><div className="setting-label">音量{companion.engine==='ref'?'（参考音色引擎暂不支持）':''}（0~100）</div><div className="setting-desc">当前 {companion.volume}</div></div><input type="range" min={0} max={100} step={1} disabled={companion.engine==='ref'||engineState!=='available'&&companion.engine==='sapi'} value={companion.volume} aria-label="朗读音量" onChange={e=>save({...companion,volume:Number(e.target.value)})} style={{accentColor:'var(--tide1)'}}/></div>{status&&<p role="status" className="notice">{status}</p>}</>
}

function AboutPanel(): React.JSX.Element {
  return (
    <div className="setting-group">
      <div className="setting-group-title">关于 Lunitide 月汐</div>
      <div className="about-content">
        <div className="about-logo">
          <div className="moon-logo" aria-hidden="true" />
          <div>
            <h3 style={{ margin: 0, fontFamily: 'var(--serif)', fontSize: '22px' }}>Lunitide <span style={{ color: 'var(--tide1)', fontWeight: 400 }}>月汐</span></h3>
            <p style={{ margin: '4px 0 0', color: 'var(--muted)', fontSize: '13px' }}>本地优先 · BYOK · AI 软件生命周期工作台</p>
          </div>
        </div>
        <dl className="about-info">
          <div><dt>版本</dt><dd>0.3.0-dev</dd></div>
          <div><dt>架构</dt><dd>Go Core Engine + WebView2 Host + React/TypeScript Renderer</dd></div>
          <div><dt>存储</dt><dd>SQLite WAL + Windows DPAPI</dd></div>
          <div><dt>协议族</dt><dd>OpenAI-compatible · Anthropic</dd></div>
          <div><dt>里程碑</dt><dd>M1 Go 模型底座（已完成）</dd></div>
        </dl>
        <div className="about-links">
          <span>产品定位：不只是一个"更好的界面"，而是一个理解项目语义、记得历史、可扩展、能规划、且有治理边界的 AI 开发伙伴。</span>
        </div>
      </div>
    </div>
  )
}

// M7 T-7.8.5 — MCP 服务器设置页：列表启停、健康检查、手动添加（风险确认）、市场搜索。
function McpPanel(): React.JSX.Element {
  const bridge = getMcpBridge()
  const [endpoints, setEndpoints] = useState<Array<{ endpointId: string; transport: 'stdio' | 'https'; state: string; enabled: boolean; origin?: string; lastHealthAt?: string }>>([])
  const [status, setStatus] = useState('')
  const [busy, setBusy] = useState(false)
  const [json, setJson] = useState('')
  const [riskConfirmed, setRiskConfirmed] = useState(false)
  const [marketQuery, setMarketQuery] = useState('')
  const [marketItems, setMarketItems] = useState<Array<{ itemId: string; name: string; publisher: string; description: string; transportHint: string; signed: boolean }>>([])

  const refresh = async () => {
    setBusy(true)
    try {
      const r = await bridge.list()
      setEndpoints(r.endpoints)
      setStatus('')
    } catch (e) {
      setStatus(e instanceof Error ? e.message : 'MCP 列表加载失败')
    } finally { setBusy(false) }
  }
  useEffect(() => { void refresh() }, [])

  const toggle = async (endpointId: string, enabled: boolean) => {
    setBusy(true); setStatus('')
    try {
      await bridge.toggle({ endpointId, enabled })
      await refresh()
    } catch (e) { setStatus(e instanceof Error ? e.message : '启停失败') } finally { setBusy(false) }
  }
  const health = async (endpointId: string) => {
    setBusy(true); setStatus('')
    try {
      const r = await bridge.health({ endpointId })
      setStatus(`${endpointId.slice(0, 8)}… 状态 ${r.state}${r.latencyMs !== undefined ? ` · 延迟 ${r.latencyMs}ms` : ''}${r.driftDetected ? ' · 能力漂移已检测（fail-closed）' : ''}`)
    } catch (e) { setStatus(e instanceof Error ? e.message : '健康检查失败') } finally { setBusy(false) }
  }
  const addManual = async () => {
    setBusy(true); setStatus('')
    try {
      const parsed = JSON.parse(json) as { transport?: 'stdio' | 'https'; command?: string; args?: string[]; url?: string }
      await bridge.add({ origin: 'manual', transport: parsed.transport, command: parsed.command, args: parsed.args, url: parsed.url, riskConfirmed, requestId: crypto.randomUUID() })
      setJson(''); setRiskConfirmed(false)
      setStatus('已添加，进入 probe 探测。')
      await refresh()
    } catch (e) { setStatus(e instanceof Error ? e.message : '添加失败：JSON 需含 transport(stdio|https) 及 command/args 或 url') } finally { setBusy(false) }
  }
  const searchMarket = async () => {
    setBusy(true); setStatus('')
    try {
      const r = await bridge.marketSearch({ query: marketQuery })
      setMarketItems(r.items)
      setStatus(r.fresh ? `市场返回 ${r.items.length} 项` : `市场缓存返回 ${r.items.length} 项（离线）`)
    } catch (e) { setStatus(e instanceof Error ? e.message : '市场搜索失败') } finally { setBusy(false) }
  }
  const addFromMarket = async (itemId: string) => {
    setBusy(true); setStatus('')
    try {
      await bridge.add({ origin: 'market', marketItemId: itemId, riskConfirmed: true, requestId: crypto.randomUUID() })
      setStatus('市场服务器已添加，进入 probe 探测。')
      await refresh()
    } catch (e) { setStatus(e instanceof Error ? e.message : '市场添加失败') } finally { setBusy(false) }
  }

  return (
    <div className="setting-group">
      <div className="setting-group-title">MCP 服务器</div>
      <div className="setting-row" style={{ gridTemplateColumns: '1fr' }}>
        <div className="setting-desc">stdio/HTTPS 双通道；能力 pin 漂移 fail-closed；凭据覆盖式更新、零回显。当前 {endpoints.length} 个端点。</div>
      </div>
      {endpoints.map(e => (
        <div className="setting-row" key={e.endpointId}>
          <div>
            <div className="setting-label">{e.endpointId.slice(0, 12)}… · {e.transport}{e.origin ? ` · ${e.origin === 'market' ? '市场' : '手动'}` : ''}</div>
            <div className="setting-desc">状态 {e.state}{e.lastHealthAt ? ` · 上次检查 ${new Date(e.lastHealthAt).toLocaleString()}` : ''}</div>
          </div>
          <div style={{ display: 'flex', gap: 8 }}>
            <button disabled={busy} onClick={() => void health(e.endpointId)}>检查</button>
            <button disabled={busy} aria-pressed={e.enabled} onClick={() => void toggle(e.endpointId, !e.enabled)}>{e.enabled ? '停用' : '启用'}</button>
          </div>
        </div>
      ))}
      <div className="setting-row" style={{ gridTemplateColumns: '1fr' }}>
        <div className="setting-group-title" style={{ marginTop: 8 }}>手动添加（JSON）</div>
        <textarea className="setting-textarea" style={{ width: '100%', minHeight: 84, fontFamily: 'var(--mono)', fontSize: 12 }} placeholder='{"transport":"stdio","command":"npx","args":["-y","some-mcp-server"]}' value={json} onChange={ev => setJson(ev.target.value)} aria-label="MCP 服务器 JSON" />
        <label style={{ display: 'flex', gap: 6, alignItems: 'center', fontSize: 12, color: 'var(--muted)' }}>
          <input type="checkbox" checked={riskConfirmed} onChange={ev => setRiskConfirmed(ev.target.checked)} /> 我确认信任此服务器来源，理解其工具将进入本机沙箱执行
        </label>
        <div><button disabled={busy || !json.trim() || !riskConfirmed} onClick={() => void addManual()}>添加服务器</button></div>
      </div>
      <div className="setting-row" style={{ gridTemplateColumns: '1fr' }}>
        <div className="setting-group-title" style={{ marginTop: 8 }}>市场</div>
        <div style={{ display: 'flex', gap: 8 }}>
          <input className="setting-input" style={{ flex: 1 }} placeholder="搜索 MCP 服务器" value={marketQuery} onChange={ev => setMarketQuery(ev.target.value)} aria-label="市场搜索" />
          <button disabled={busy || !marketQuery.trim()} onClick={() => void searchMarket()}>搜索</button>
        </div>
        {marketItems.map(m => (
          <div className="setting-row" key={m.itemId} style={{ borderTop: '1px solid var(--rule)', paddingTop: 8 }}>
            <div>
              <div className="setting-label">{m.name} {m.signed ? '· 已签名' : '· 未签名'} · {m.transportHint}</div>
              <div className="setting-desc">{m.publisher} — {m.description}</div>
            </div>
            <button disabled={busy} onClick={() => void addFromMarket(m.itemId)}>添加</button>
          </div>
        ))}
      </div>
      <McpPresetsSection />
      {status && <p role="status" className="notice">{status}</p>}
    </div>
  )
}

// c3-mcp — 预置免费官方 MCP server 目录：卡片展示 + 一键注册（stdio / npx），
// needsArgs 条目先把占位符替换为用户输入再注册；不放宽任何 stdio 白名单。
export type McpPresetItem = Mcp6PresetsListResult['items'][number]

export function McpPresetsSection({ bridge = getMcpBridge() }: { bridge?: McpBridge }): React.JSX.Element {
  const [items, setItems] = useState<McpPresetItem[]>([])
  const [status, setStatus] = useState('')
  const [busy, setBusy] = useState(false)
  const [argDraft, setArgDraft] = useState<{ id: string; value: string } | null>(null)

  useEffect(() => {
    setBusy(true)
    bridge.presets()
      .then(r => { setItems(r.items); setStatus('') })
      .catch(e => setStatus(e instanceof Error ? e.message : '预置目录加载失败'))
      .finally(() => setBusy(false))
  }, [bridge])

  // 反斜杠属于 stdio 白名单元字符：Windows 路径先归一为正斜杠再替换占位符。
  const resolveArgs = (preset: McpPresetItem, value: string): string[] =>
    preset.args.map(a => a === preset.argPlaceholder ? value.trim().replaceAll('\\', '/') : a)

  const register = async (preset: McpPresetItem, value?: string) => {
    setBusy(true); setStatus('')
    try {
      await bridge.add({ origin: 'manual', transport: 'stdio', command: preset.command, args: resolveArgs(preset, value ?? ''), riskConfirmed: true, requestId: crypto.randomUUID() })
      setArgDraft(null)
      setStatus(`已注册 ${preset.name}，进入 probe 探测。`)
    } catch (e) { setStatus(e instanceof Error ? e.message : `${preset.name} 注册失败`) } finally { setBusy(false) }
  }

  return (
    <div className="setting-row" style={{ gridTemplateColumns: '1fr' }}>
      <div className="setting-group-title" style={{ marginTop: 8 }}>预置服务器（免费官方）</div>
      <div className="setting-desc">官方 @modelcontextprotocol 参考 server，一键注册（stdio / npx），无需手填 URL 或命令。</div>
      {items.map(p => (
        <div className="setting-row" key={p.id} style={{ borderTop: '1px solid var(--rule)', paddingTop: 8 }}>
          <div style={{ minWidth: 0 }}>
            <div className="setting-label">{p.name} · {p.category}{p.needsArgs ? ' · 需补充参数' : ''}</div>
            <div className="setting-desc">{p.description} — <span style={{ fontFamily: 'var(--mono)' }}>{p.command} {p.args.join(' ')}</span></div>
            {argDraft?.id === p.id && (
              <div style={{ display: 'flex', gap: 8, marginTop: 8 }}>
                <input className="setting-input" style={{ flex: 1 }} placeholder={p.argHint ?? '请输入参数'} value={argDraft.value}
                  onChange={ev => setArgDraft({ id: p.id, value: ev.target.value })} aria-label={`${p.name} 参数`} />
                <button disabled={busy || !argDraft.value.trim()} aria-label={`确认注册 ${p.name}`} onClick={() => void register(p, argDraft.value)}>确认注册</button>
              </div>
            )}
          </div>
          {argDraft?.id === p.id
            ? <button disabled={busy} onClick={() => setArgDraft(null)}>取消</button>
            : <button disabled={busy} aria-label={`注册 ${p.name}`} onClick={() => (p.needsArgs ? setArgDraft({ id: p.id, value: '' }) : void register(p))}>注册</button>}
        </div>
      ))}
      {status && <p role="status" className="notice">{status}</p>}
    </div>
  )
}

// M10 wave-3 — 浏览器多模式设置：5 种连接模式卡片（builtin/chrome/edge/extension/ask）、
// 可执行路径 / 扩展端口 / 数据留存 / 私网拦截配置、导航白名单管理、CDP 会话生命周期
// 与 ask/allow/deny 权限审批队列。
type BrMode = 'builtin' | 'chrome' | 'edge' | 'extension' | 'ask'
const BR_MODE_META: Record<BrMode, { label: string; desc: string }> = {
  builtin: { label: '内置 WebView2', desc: '应用内嵌渲染，始终可用；导航走 browser.act 通道' },
  chrome: { label: 'Chrome', desc: '独立 profile 启动本机 Chrome 并接管 CDP 调试端口' },
  edge: { label: 'Edge', desc: '独立 profile 启动本机 Edge 并接管 CDP 调试端口' },
  extension: { label: '浏览器扩展', desc: '接管已装扩展桥（默认端口 9222）的外部浏览器' },
  ask: { label: '每次询问', desc: '不固定浏览器，操作前弹出选择' },
}
const BR_PERM_LABELS: Record<string, string> = {
  geolocation: '地理位置', camera: '摄像头', microphone: '麦克风',
  notifications: '通知', 'clipboard-read': '剪贴板读取', downloads: '下载',
}
type BrSessionRow = BrSessionListResult['sessions'][number]
type BrPermissionRow = BrPermissionListResult['permissions'][number]

export function BrowserPanel({ bridge = brBridge }: { bridge?: BrBridge }): React.JSX.Element {
  const [settings, setSettings] = useState<BrSettingsGetResult | null>(null)
  const [detect, setDetect] = useState<BrModeDetectResult | null>(null)
  const [sessions, setSessions] = useState<BrSessionRow[]>([])
  const [pending, setPending] = useState<BrPermissionRow[]>([])
  const [usage, setUsage] = useState<BrDataUsageResult['usage']>([])
  const [status, setStatus] = useState('')
  const [busy, setBusy] = useState(false)
  const [chromePathDraft, setChromePathDraft] = useState('')
  const [edgePathDraft, setEdgePathDraft] = useState('')
  const [portDraft, setPortDraft] = useState('')
  const [retentionDraft, setRetentionDraft] = useState('')
  const [allowDraft, setAllowDraft] = useState('')

  const refresh = async () => {
    setBusy(true)
    try {
      const [s, sess, perms] = await Promise.all([
        bridge.getSettings(),
        bridge.listSessions(),
        bridge.listPermissions({ state: 'pending' }),
      ])
      setSettings(s)
      setChromePathDraft(s.chromePath)
      setEdgePathDraft(s.edgePath)
      setPortDraft(String(s.extensionPort))
      setRetentionDraft(String(s.dataRetentionDays))
      setSessions(sess.sessions)
      setPending(perms.permissions)
      setStatus('')
    } catch (e) {
      setStatus(e instanceof Error ? e.message : '浏览器设置加载失败')
    } finally { setBusy(false) }
  }
  useEffect(() => { void refresh() }, [])

  const detectModes = async () => {
    setBusy(true); setStatus('')
    try {
      const r = await bridge.detectModes()
      setDetect(r)
      setStatus(`探测完成：内置 ${r.builtin ? '✓' : '✗'} · Chrome ${r.chrome.available ? '✓' : '✗'} · Edge ${r.edge.available ? '✓' : '✗'} · 扩展桥 ${r.extension.available ? '✓' : '✗'}（端口 ${r.extension.port}）`)
    } catch (e) { setStatus(e instanceof Error ? e.message : '模式探测失败') } finally { setBusy(false) }
  }
  const patch = async (p: BrSettingsUpdatePayload, okMsg: string) => {
    setBusy(true); setStatus('')
    try {
      const s = await bridge.updateSettings(p)
      setSettings(s)
      setStatus(okMsg)
    } catch (e) { setStatus(e instanceof Error ? e.message : '设置更新失败') } finally { setBusy(false) }
  }
  const connect = async (mode?: BrMode) => {
    setBusy(true); setStatus('')
    try {
      const sess = await bridge.connect(mode && mode !== 'ask' ? { mode } : {})
      setStatus(`会话 ${sess.sessionId.slice(0, 12)}… ${sess.mode} · ${sess.state}`)
      const list = await bridge.listSessions()
      setSessions(list.sessions)
    } catch (e) { setStatus(e instanceof Error ? e.message : '连接失败') } finally { setBusy(false) }
  }
  const disconnect = async (sessionId: string) => {
    setBusy(true); setStatus('')
    try {
      await bridge.disconnect({ sessionId })
      const list = await bridge.listSessions()
      setSessions(list.sessions)
    } catch (e) { setStatus(e instanceof Error ? e.message : '断开失败') } finally { setBusy(false) }
  }
  const refreshUsage = async () => {
    setBusy(true); setStatus('')
    try {
      const r = await bridge.dataUsage({})
      setUsage(r.usage)
      setStatus(`已刷新 ${r.usage.length} 个会话的存储快照`)
    } catch (e) { setStatus(e instanceof Error ? e.message : '用量查询失败') } finally { setBusy(false) }
  }
  const clearData = async () => {
    setBusy(true); setStatus('')
    try {
      const r = await bridge.clearData({})
      setStatus(`已清理 ${r.clearedSessions.length} 个会话，释放 ${(r.freedBytes / 1024).toFixed(1)} KB`)
    } catch (e) { setStatus(e instanceof Error ? e.message : '清理失败') } finally { setBusy(false) }
  }
  const decide = async (permissionId: string, decision: 'grant' | 'deny') => {
    setBusy(true); setStatus('')
    try {
      await bridge.decidePermission({ permissionId, decision })
      const perms = await bridge.listPermissions({ state: 'pending' })
      setPending(perms.permissions)
    } catch (e) { setStatus(e instanceof Error ? e.message : '审批失败') } finally { setBusy(false) }
  }
  const setPolicy = async (origin: string, permission: string, policy: 'ask' | 'allow' | 'deny') => {
    setBusy(true); setStatus('')
    try {
      await bridge.setPermissionPolicy({ origin: origin as BrPermissionPolicyPayload['origin'], permission: permission as BrPermissionPolicyPayload['permission'], policy })
      const perms = await bridge.listPermissions({ state: 'pending' })
      setPending(perms.permissions)
      setStatus('策略已更新')
    } catch (e) { setStatus(e instanceof Error ? e.message : '策略更新失败') } finally { setBusy(false) }
  }
  const addAllow = async () => {
    if (!settings) return
    const entry = allowDraft.trim().replace(/\/+$/, '')
    if (!entry) return
    if (!/^https?:\/\/[^\s/]+(:\d+)?(\/.*)?$/.test(entry)) { setStatus('白名单条目需为 http(s)://host[:port][/前缀]'); return }
    if (settings.allowlist.includes(entry)) { setStatus('条目已存在'); return }
    await patch({ allowlist: [...settings.allowlist, entry] }, `已添加 ${entry}`)
    setAllowDraft('')
  }
  const removeAllow = async (entry: string) => {
    if (!settings) return
    await patch({ allowlist: settings.allowlist.filter(x => x !== entry) }, `已移除 ${entry}`)
  }

  return (
    <div className="setting-group">
      <div className="setting-group-title">浏览器多模式</div>
      <div className="setting-row" style={{ gridTemplateColumns: '1fr' }}>
        <div className="setting-desc">五种连接模式共用一套导航白名单与私网拦截策略；切换模式会断开当前活动会话。当前 {sessions.filter(s => s.state === 'connected').length}/{sessions.length} 个会话在线。</div>
        <div style={{ display: 'flex', gap: 8, flexWrap: 'wrap' }}>
          <button disabled={busy} onClick={() => void detectModes()}>探测本机浏览器</button>
          <button disabled={busy} onClick={() => void connect()}>新建会话（当前模式）</button>
        </div>
      </div>

      <div className="setting-row" style={{ gridTemplateColumns: '1fr' }}>
        <div className="setting-group-title" style={{ marginTop: 8 }}>连接模式</div>
        <div className="br-mode-grid" role="radiogroup" aria-label="浏览器连接模式">
          {(Object.keys(BR_MODE_META) as BrMode[]).map(mode => {
            const available = mode === 'builtin' ? detect?.builtin
              : mode === 'chrome' ? detect?.chrome.available
                : mode === 'edge' ? detect?.edge.available
                  : mode === 'extension' ? detect?.extension.available : undefined
            return (
              <button
                key={mode}
                type="button"
                role="radio"
                aria-checked={settings?.mode === mode}
                disabled={busy}
                className={`br-mode-card ${settings?.mode === mode ? 'on' : ''}`}
                onClick={() => void patch({ mode }, `连接模式已切换为 ${BR_MODE_META[mode].label}`)}
              >
                <b>{BR_MODE_META[mode].label}</b>
                <small>{BR_MODE_META[mode].desc}</small>
                {detect && <code className={available ? 'ok' : 'no'}>{available === undefined ? '' : available ? '可用' : '未检测到'}</code>}
              </button>
            )
          })}
        </div>
      </div>

      <div className="setting-row" style={{ gridTemplateColumns: '1fr' }}>
        <div className="setting-group-title" style={{ marginTop: 8 }}>可执行文件与端口</div>
        <div className="setting-desc">探测未命中时可手动指定 chrome.exe / msedge.exe 完整路径；扩展桥端口需与浏览器扩展一致（1024–65535）。</div>
        <div style={{ display: 'grid', gap: 8, maxWidth: 560 }}>
          <label style={{ display: 'grid', gap: 4, fontSize: 12, color: 'var(--muted)' }}>Chrome 路径
            <div style={{ display: 'flex', gap: 8 }}>
              <input className="setting-input" style={{ flex: 1, fontFamily: 'var(--mono)', fontSize: 12 }} placeholder={detect?.chrome.path || 'C:\\Program Files\\Google\\Chrome\\Application\\chrome.exe'} value={chromePathDraft} onChange={ev => setChromePathDraft(ev.target.value)} aria-label="Chrome 可执行路径" />
              <button disabled={busy || chromePathDraft === (settings?.chromePath ?? '')} onClick={() => void patch({ chromePath: chromePathDraft.trim() }, 'Chrome 路径已保存')}>保存</button>
            </div>
          </label>
          <label style={{ display: 'grid', gap: 4, fontSize: 12, color: 'var(--muted)' }}>Edge 路径
            <div style={{ display: 'flex', gap: 8 }}>
              <input className="setting-input" style={{ flex: 1, fontFamily: 'var(--mono)', fontSize: 12 }} placeholder={detect?.edge.path || 'C:\\Program Files (x86)\\Microsoft\\Edge\\Application\\msedge.exe'} value={edgePathDraft} onChange={ev => setEdgePathDraft(ev.target.value)} aria-label="Edge 可执行路径" />
              <button disabled={busy || edgePathDraft === (settings?.edgePath ?? '')} onClick={() => void patch({ edgePath: edgePathDraft.trim() }, 'Edge 路径已保存')}>保存</button>
            </div>
          </label>
          <label style={{ display: 'grid', gap: 4, fontSize: 12, color: 'var(--muted)' }}>扩展桥端口
            <div style={{ display: 'flex', gap: 8 }}>
              <input className="setting-input" style={{ width: 120, fontFamily: 'var(--mono)' }} inputMode="numeric" value={portDraft} onChange={ev => setPortDraft(ev.target.value.replace(/\D/g, ''))} aria-label="扩展桥端口" />
              <button disabled={busy || !portDraft || Number(portDraft) === settings?.extensionPort} onClick={() => void patch({ extensionPort: Number(portDraft) }, '扩展桥端口已保存')}>保存</button>
            </div>
          </label>
        </div>
      </div>

      <div className="setting-row" style={{ gridTemplateColumns: '1fr' }}>
        <div className="setting-group-title" style={{ marginTop: 8 }}>导航白名单{settings && settings.allowlist.length > 0 ? `（${settings.allowlist.length}/256）` : ''}</div>
        <div className="setting-desc">白名单非空时，仅允许导航到列表内源（scheme://host[:port] 前缀匹配）；留空表示不限。拦截 file:、非标准端口与私网地址。</div>
        <div style={{ display: 'flex', gap: 8, maxWidth: 560 }}>
          <input className="setting-input" style={{ flex: 1, fontFamily: 'var(--mono)', fontSize: 12 }} placeholder="https://docs.example.com" value={allowDraft} onChange={ev => setAllowDraft(ev.target.value)} aria-label="白名单新条目" onKeyDown={ev => { if (ev.key === 'Enter') { ev.preventDefault(); void addAllow() } }} />
          <button disabled={busy || !allowDraft.trim()} onClick={() => void addAllow()}>添加</button>
        </div>
        {settings?.allowlist.map(entry => (
          <div className="setting-row" key={entry} style={{ borderTop: '1px solid var(--rule)', paddingTop: 8 }}>
            <code style={{ fontFamily: 'var(--mono)', fontSize: 12, overflowWrap: 'anywhere' }}>{entry}</code>
            <button disabled={busy} onClick={() => void removeAllow(entry)}>移除</button>
          </div>
        ))}
      </div>

      <div className="setting-row" style={{ gridTemplateColumns: '1fr' }}>
        <div className="setting-group-title" style={{ marginTop: 8 }}>数据与隐私</div>
        <Toggle on={settings?.blockPrivateNetworks ?? true} onChange={v => void patch({ blockPrivateNetworks: v }, v ? '已开启私网拦截' : '已关闭私网拦截（不推荐）')} label="拦截私网与本地地址" desc="开启后导航拒绝 127.0.0.1、192.168.x.x、10.x.x.x、172.16-31.x.x、*.localhost 及解析到私网的域名" />
        <label style={{ display: 'grid', gap: 4, fontSize: 12, color: 'var(--muted)', maxWidth: 560 }}>缓存数据留存天数（0–365，0 表示立即过期）
          <div style={{ display: 'flex', gap: 8 }}>
            <input className="setting-input" style={{ width: 120, fontFamily: 'var(--mono)' }} inputMode="numeric" value={retentionDraft} onChange={ev => setRetentionDraft(ev.target.value.replace(/\D/g, ''))} aria-label="缓存数据留存天数" />
            <button disabled={busy || retentionDraft === '' || Number(retentionDraft) === settings?.dataRetentionDays} onClick={() => void patch({ dataRetentionDays: Number(retentionDraft) }, '留存窗口已保存')}>保存</button>
          </div>
        </label>
        <div style={{ display: 'flex', gap: 8, flexWrap: 'wrap' }}>
          <button disabled={busy} onClick={() => void refreshUsage()}>刷新存储用量</button>
          <button disabled={busy} onClick={() => void clearData()}>按留存窗口清理</button>
        </div>
        {usage.map(u => (
          <div className="setting-desc" key={u.sessionId} style={{ fontFamily: 'var(--mono)' }}>
            {u.sessionId.slice(0, 12)}… · profile {(u.profileBytes / 1024).toFixed(1)} KB · cache {(u.cacheBytes / 1024).toFixed(1)} KB · cookies {(u.cookiesBytes / 1024).toFixed(1)} KB
          </div>
        ))}
      </div>

      <div className="setting-row" style={{ gridTemplateColumns: '1fr' }}>
        <div className="setting-group-title" style={{ marginTop: 8 }}>CDP 会话</div>
        {sessions.length === 0 && <div className="setting-desc">暂无会话。点击「新建会话」以当前模式发起 CDP 连接。</div>}
        {sessions.map(s => (
          <div className="setting-row" key={s.sessionId}>
            <div style={{ minWidth: 0 }}>
              <div className="setting-label">{s.sessionId.slice(0, 14)}… · {BR_MODE_META[s.mode as BrMode]?.label ?? s.mode} · <span style={{ color: s.state === 'connected' ? 'var(--ok)' : s.state === 'error' ? 'var(--red)' : 'var(--muted)' }}>{s.state}</span></div>
              <div className="setting-desc">{s.connectedAt ? `连接于 ${new Date(s.connectedAt).toLocaleString()}` : '未连接'}{s.detail ? ` · ${s.detail}` : ''}</div>
            </div>
            <div style={{ display: 'flex', gap: 8 }}>
              {s.state === 'connected' && <button disabled={busy} onClick={() => void disconnect(s.sessionId)}>断开</button>}
              {(s.state === 'disconnected' || s.state === 'error') && <button disabled={busy} onClick={() => void connect(s.mode as BrMode)}>重连</button>}
            </div>
          </div>
        ))}
      </div>

      <div className="setting-row" style={{ gridTemplateColumns: '1fr' }}>
        <div className="setting-group-title" style={{ marginTop: 8 }}>权限审批{pending.length > 0 ? `（${pending.length} 待决）` : ''}</div>
        {pending.length === 0 && <div className="setting-desc">暂无待审批的站点权限请求。</div>}
        {pending.map(p => (
          <div className="setting-row" key={p.permissionId}>
            <div style={{ minWidth: 0 }}>
              <div className="setting-label">{BR_PERM_LABELS[p.permission] ?? p.permission} · <code style={{ fontFamily: 'var(--mono)', fontSize: 11 }}>{p.origin}</code></div>
              <div className="setting-desc">{new Date(p.createdAt).toLocaleString()} · 策略 {p.policy}</div>
            </div>
            <div style={{ display: 'flex', gap: 8, flexWrap: 'wrap' }}>
              <button disabled={busy} onClick={() => void decide(p.permissionId, 'grant')}>允许</button>
              <button disabled={busy} onClick={() => void decide(p.permissionId, 'deny')}>拒绝</button>
              <button disabled={busy} onClick={() => void setPolicy(p.origin, p.permission, 'allow')}>始终允许</button>
              <button disabled={busy} onClick={() => void setPolicy(p.origin, p.permission, 'deny')}>始终拒绝</button>
            </div>
          </div>
        ))}
      </div>
      {status && <p role="status" className="notice">{status}</p>}
    </div>
  )
}

// M8 T-8.9.7 — 插件设置页：已安装启停/卸载/升级 + 市场浏览安装。
function PluginsPanel(): React.JSX.Element {
  const bridge = getPluginBridge()
  const [plugins, setPlugins] = useState<Array<{ installId: string; pluginId: string; semver: string; publisher: string; kind: string; origin: string; state: string; bindingCount: number }>>([])
  const [status, setStatus] = useState('')
  const [busy, setBusy] = useState(false)
  const [query, setQuery] = useState('')
  const [items, setItems] = useState<Array<{ itemId: string; pluginId: string; name: string; publisher: string; description: string; kind: string; semver: string; signed: boolean }>>([])
  // 三步安装确认（M8 P1）：1 详情 → 2 权限确认 → 3 安装
  const [wizard, setWizard] = useState<{ step: 1 | 2; item: { itemId: string; pluginId: string; name: string; publisher: string; description: string; kind: string; semver: string; signed: boolean }; detail?: { manifest: object; permissions: string[]; requires: object; signature: object; downloads: number }; grant: Record<string, string[]>; agreed: boolean } | null>(null)
  // 开发者工具视图（plugin.dev.create）
  const [devOpen, setDevOpen] = useState(false)
  const [devWorkspace, setDevWorkspace] = useState('')
  const [devEntrypoint, setDevEntrypoint] = useState('')
  const [devManifest, setDevManifest] = useState('')
  const quarantined = plugins.filter(p => p.state === 'quarantined')

  const refresh = async () => {
    setBusy(true)
    try { setPlugins((await bridge.list()).plugins); setStatus('') } catch (e) { setStatus(e instanceof Error ? e.message : '插件列表加载失败') } finally { setBusy(false) }
  }
  useEffect(() => { void refresh() }, [])

  const toggle = async (installId: string, enabled: boolean) => {
    setBusy(true); setStatus('')
    try { await bridge.toggle({ installId, enabled }); await refresh() } catch (e) { setStatus(e instanceof Error ? e.message : '启停失败') } finally { setBusy(false) }
  }
  const uninstall = async (installId: string) => {
    if (!window.confirm('确认卸载该插件？能力绑定将同步撤销，并留存墓碑记录。')) return
    setBusy(true); setStatus('')
    try {
      const confirmToken = await sha256Hex(`plugin.uninstall|${installId}`)
      await bridge.uninstall({ installId, confirmToken }); await refresh(); setStatus('已卸载，能力绑定同步撤销。')
    } catch (e) { setStatus(e instanceof Error ? e.message : '卸载失败') } finally { setBusy(false) }
  }
  const upgrade = async (installId: string) => {
    setBusy(true); setStatus('')
    try { const r = await bridge.upgrade({ installId }); setStatus(`${r.fromSemver} → ${r.toSemver}${r.permissionExpansion ? '（权限有扩展，请复核）' : ''}`); await refresh() } catch (e) { setStatus(e instanceof Error ? e.message : '升级失败') } finally { setBusy(false) }
  }
  const search = async () => {
    setBusy(true); setStatus('')
    try { setItems((await bridge.marketSearch({ query })).items); setStatus('') } catch (e) { setStatus(e instanceof Error ? e.message : '市场搜索失败') } finally { setBusy(false) }
  }
  // 步骤 1：读取详情并把声明权限解析为授权文档（grant = requested，不扩权）
  const openWizard = async (item: { itemId: string; pluginId: string; name: string; publisher: string; description: string; kind: string; semver: string; signed: boolean }) => {
    setBusy(true); setStatus('')
    try {
      const detail = await bridge.marketDetail({ itemId: item.itemId })
      const docPermissions = (detail.manifest as { permissions?: unknown } | null)?.permissions
      const grant: Record<string, string[]> = {}
      if (docPermissions && typeof docPermissions === 'object' && !Array.isArray(docPermissions)) {
        for (const [scope, actions] of Object.entries(docPermissions as Record<string, unknown>)) grant[scope] = Array.isArray(actions) ? actions.map(String) : [String(actions)]
      } else {
        for (const permission of detail.permissions) {
          const [scope, ...rest] = String(permission).split(/[.:]/)
          const action = rest.join(':') || scope
          grant[scope] = [...(grant[scope] ?? []), action]
        }
      }
      setWizard({ step: 1, item, detail, grant, agreed: false })
    } catch (e) { setStatus(e instanceof Error ? e.message : '插件详情加载失败') } finally { setBusy(false) }
  }
  // 步骤 3：携带确认后的授权文档安装（空权限插件也走同一链路）
  const install = async () => {
    if (!wizard || !wizard.agreed) return
    setBusy(true); setStatus('')
    try {
      const r = await bridge.install({ origin: 'market', source: wizard.item.itemId, permissionGrant: wizard.grant, requestId: crypto.randomUUID() })
      setStatus(r.state === 'quarantined' ? '安装已隔离：签名/哈希校验未通过，未注册任何能力。' : `已安装（${r.state}，绑定 ${r.bindings.length} 项能力）`)
      setWizard(null); await refresh()
    } catch (e) { setStatus(e instanceof Error ? e.message : '安装失败') } finally { setBusy(false) }
  }
  const devCreate = async () => {
    setBusy(true); setStatus('')
    try {
      const manifest = JSON.parse(devManifest) as object
      const r = await bridge.devCreate({ workspaceId: devWorkspace.trim(), manifest, entrypoint: devEntrypoint.trim() })
      setStatus(r.state === 'quarantined' ? `开发包已创建并隔离（bundle ${r.bundleId}）` : `开发包已创建并通过校验（bundle ${r.bundleId}）`)
      setDevOpen(false); setDevWorkspace(''); setDevEntrypoint(''); setDevManifest('')
    } catch (e) { setStatus(e instanceof Error ? e.message : '开发包创建失败（清单需为合法 JSON）') } finally { setBusy(false) }
  }

  return (
    <div className="setting-group">
      <div className="setting-group-title">插件</div>
      <div className="setting-row" style={{ gridTemplateColumns: '1fr' }}>
        <div className="setting-desc">签名验证 → 隔离安装 → 能力热注册（复用既有注册表，无插件专用 Runner）。当前 {plugins.length} 个。</div>
      </div>
      {quarantined.length > 0 && (
        <div className="setting-row" style={{ gridTemplateColumns: '1fr', border: '1px solid #f59e0b', borderRadius: 8, background: 'rgba(245, 158, 11, 0.08)' }} role="alert">
          <div className="setting-label">⛔ {quarantined.length} 个插件处于隔离状态</div>
          <div className="setting-desc">{quarantined.map(p => p.pluginId).join('、')} — 签名/哈希校验未通过，未注册任何能力；可卸载留存取证。</div>
        </div>
      )}
      {plugins.map(p => (
        <div className="setting-row" key={p.installId}>
          <div>
            <div className="setting-label">{p.pluginId} · v{p.semver} · {p.kind}{p.state === 'quarantined' ? ' · ⛔ 隔离' : ''}</div>
            <div className="setting-desc">{p.publisher} · {p.state} · 绑定 {p.bindingCount} 项 · {p.origin}</div>
          </div>
          <div style={{ display: 'flex', gap: 8 }}>
            {p.state === 'enabled' || p.state === 'disabled' ? <button disabled={busy} onClick={() => void toggle(p.installId, p.state !== 'enabled')}>{p.state === 'enabled' ? '停用' : '启用'}</button> : null}
            {p.state !== 'quarantined' && p.state !== 'uninstalled' && <button disabled={busy} onClick={() => void upgrade(p.installId)}>升级</button>}
            {p.state !== 'uninstalled' && <button disabled={busy} onClick={() => void uninstall(p.installId)}>卸载</button>}
          </div>
        </div>
      ))}
      <div className="setting-row" style={{ gridTemplateColumns: '1fr' }}>
        <div className="setting-group-title" style={{ marginTop: 8 }}>插件市场</div>
        <div style={{ display: 'flex', gap: 8 }}>
          <input className="setting-input" style={{ flex: 1 }} placeholder="搜索插件（mcp/skill/workflow/template/tool/agent-pack）" value={query} onChange={e => setQuery(e.target.value)} aria-label="插件市场搜索" />
          <button disabled={busy || !query.trim()} onClick={() => void search()}>搜索</button>
        </div>
        {items.map(m => (
          <div className="setting-row" key={m.itemId} style={{ borderTop: '1px solid var(--rule)', paddingTop: 8 }}>
            <div>
              <div className="setting-label">{m.name} v{m.semver} · {m.kind} {m.signed ? '· 已签名' : '· 未签名'}</div>
              <div className="setting-desc">{m.publisher} — {m.description}</div>
            </div>
            <button disabled={busy} onClick={() => void openWizard(m)}>安装…</button>
          </div>
        ))}
      </div>
      <div className="setting-row" style={{ gridTemplateColumns: '1fr' }}>
        <div className="setting-group-title" style={{ marginTop: 8 }}>开发者工具</div>
        <div style={{ display: 'flex', gap: 8 }}>
          <button disabled={busy} onClick={() => setDevOpen(open => !open)} aria-expanded={devOpen}>{devOpen ? '收起' : '创建开发包'}</button>
        </div>
        {devOpen && (
          <div style={{ display: 'grid', gap: 8, borderTop: '1px solid var(--rule)', paddingTop: 8 }}>
            <div className="setting-desc">本地工作区直连：plugin.dev.create 走隔离校验链，未通过前不注册能力。</div>
            <input className="setting-input" placeholder="workspaceId（本地工作区标识）" value={devWorkspace} onChange={e => setDevWorkspace(e.target.value)} aria-label="开发包工作区" />
            <input className="setting-input" placeholder="入口（entrypoint，如 plugin/main.ts）" value={devEntrypoint} onChange={e => setDevEntrypoint(e.target.value)} aria-label="开发包入口" />
            <textarea className="setting-input" style={{ minHeight: 120, fontFamily: 'var(--mono, monospace)' }} placeholder='插件清单 JSON，如 {"pluginId":"demo","semver":"0.1.0","publisher":"local","kind":"tool","permissions":{"fs":["read"]}}' value={devManifest} onChange={e => setDevManifest(e.target.value)} aria-label="开发包清单 JSON" />
            <div><button className="primary" disabled={busy || !devWorkspace.trim() || !devEntrypoint.trim() || !devManifest.trim()} onClick={() => void devCreate()}>提交开发包</button></div>
          </div>
        )}
      </div>
      {wizard && wizard.detail && (
        <div className="setting-row" style={{ gridTemplateColumns: '1fr', border: '1px solid var(--rule)', borderRadius: 8, padding: 12 }} role="dialog" aria-label={`安装确认 ${wizard.item.name}`}>
          <div className="setting-group-title">安装 {wizard.item.name} v{wizard.item.semver} · 第 {wizard.step}/2 步</div>
          {wizard.step === 1 ? (
            <>
              <div className="setting-desc">{wizard.item.publisher} — {wizard.item.description} · 下载 {wizard.detail.downloads} 次 · {wizard.item.signed ? '已签名' : '未签名'}</div>
              <div className="setting-label" style={{ marginTop: 8 }}>清单</div>
              <pre style={{ margin: 0, whiteSpace: 'pre-wrap', fontSize: 12 }}>{JSON.stringify(wizard.detail.manifest, null, 2)}</pre>
              <div style={{ display: 'flex', gap: 8, marginTop: 8 }}>
                <button onClick={() => setWizard({ ...wizard, step: 2 })}>下一步：确认权限</button>
                <button onClick={() => setWizard(null)}>取消</button>
              </div>
            </>
          ) : (
            <>
              <div className="setting-label" style={{ marginTop: 8 }}>该插件声明以下权限（安装即按此精确授权，不扩权）</div>
              {Object.keys(wizard.grant).length ? Object.entries(wizard.grant).map(([scope, actions]) => (
                <div className="setting-desc" key={scope}><code>{scope}</code> → {actions.join('、')}</div>
              )) : <div className="setting-desc">（无权限声明）</div>}
              <label style={{ display: 'flex', gap: 8, alignItems: 'center', marginTop: 8 }}>
                <input type="checkbox" checked={wizard.agreed} onChange={e => setWizard({ ...wizard, agreed: e.target.checked })} />
                <span className="setting-desc">我已了解并同意授予以上权限</span>
              </label>
              <div style={{ display: 'flex', gap: 8, marginTop: 8 }}>
                <button className="primary" disabled={busy || !wizard.agreed} onClick={() => void install()}>确认并安装</button>
                <button onClick={() => setWizard({ ...wizard, step: 1 })}>上一步</button>
                <button onClick={() => setWizard(null)}>取消</button>
              </div>
            </>
          )}
        </div>
      )}
      {status && <p role="status" className="notice">{status}</p>}
    </div>
  )
}

// M8 FR-17 协作门禁 — 能力状态快照 / 证据评估 / 决策确认（collabGate.*）。
// 评估窗口为毫秒且 ≥30 天；确认令牌经带外途径（引擎审计事件）下发。
function CollabGatePanel(): React.JSX.Element {
  const bridge = getCollabGateBridge()
  const [subjectId, setSubjectId] = useState('local-user')
  const [status, setStatus] = useState('')
  const [busy, setBusy] = useState(false)
  const [snapshot, setSnapshot] = useState<{ capability: string; evaluationId?: string; decisionId?: string; policyVersion?: string; capabilityDigest?: string; effectiveAt?: string } | undefined>()
  const [evaluation, setEvaluation] = useState<{ evaluationId: string; outcome: string; failedCriteria: string[]; evidenceDigest: string } | undefined>()
  const [criteriaVersion, setCriteriaVersion] = useState('v1')
  const [decisionId, setDecisionId] = useState('')
  const [decisionToken, setDecisionToken] = useState('')

  const refresh = async () => {
    if (!subjectId.trim()) return
    setBusy(true); setStatus('')
    try { setSnapshot(await bridge.status({ subjectId: subjectId.trim() })) } catch (e) { setStatus(e instanceof Error ? e.message : '门禁状态查询失败') } finally { setBusy(false) }
  }
  useEffect(() => { void refresh() }, [])
  const evaluate = async () => {
    if (!subjectId.trim()) return
    setBusy(true); setStatus('')
    try {
      const now = Date.now(), windowStart = now - 45 * 24 * 3600 * 1000
      const r = await bridge.evaluate({ subjectId: subjectId.trim(), windowStart, windowEnd: now, criteriaVersion: criteriaVersion.trim() || 'v1' })
      setEvaluation(r)
      setStatus(r.outcome === 'pass' ? '评估通过：已生成待确认决策（令牌经审计事件带外下发）。' : r.outcome === 'fail' ? `评估未通过：${r.failedCriteria.join('、')}` : '证据不足：窗口内运行样本不够。')
      await refresh()
    } catch (e) { setStatus(e instanceof Error ? e.message : '评估失败') } finally { setBusy(false) }
  }
  const confirm = async () => {
    setBusy(true); setStatus('')
    try {
      const r = await bridge.confirm({ decisionId: decisionId.trim(), decisionToken: decisionToken.trim() })
      setStatus(`已确认：协作能力 ${r.capability === 'enabled' ? '开启' : '关闭'}（生效 ${r.effectiveAt}）。`)
      setDecisionId(''); setDecisionToken('')
      await refresh()
    } catch (e) { setStatus(e instanceof Error ? e.message : '确认失败（令牌或决策无效）') } finally { setBusy(false) }
  }

  return (
    <div className="setting-group">
      <div className="setting-group-title">协作门禁</div>
      <div className="setting-row" style={{ gridTemplateColumns: '1fr' }}>
        <div className="setting-desc">协作编排能力按主体门禁：先证据评估，再决策确认；未开启时不暴露任何编排细节（M8 FR-17）。</div>
      </div>
      <div className="setting-row">
        <div>
          <div className="setting-label">主体标识</div>
          <input className="setting-input" style={{ width: '100%' }} value={subjectId} maxLength={128} onChange={e => setSubjectId(e.target.value)} aria-label="门禁主体标识" />
        </div>
        <div style={{ display: 'flex', gap: 8 }}>
          <button disabled={busy || !subjectId.trim()} onClick={() => void refresh()}>查询状态</button>
        </div>
      </div>
      {snapshot && (
        <div className="setting-row" style={{ gridTemplateColumns: '1fr' }}>
          <div className="setting-label">能力状态：{snapshot.capability === 'enabled' ? '✅ 已开启' : '⛔ 未开启'}</div>
          {snapshot.capability === 'enabled' ? (
            <div className="setting-desc">决策 {snapshot.decisionId} · 策略 {snapshot.policyVersion} · 摘要 {snapshot.capabilityDigest?.slice(0, 12)}… · 生效 {snapshot.effectiveAt}</div>
          ) : (
            <div className="setting-desc">未开启状态下不暴露任何编排细节。</div>
          )}
        </div>
      )}
      <div className="setting-row">
        <div>
          <div className="setting-label">证据评估</div>
          <div className="setting-desc">按最近 45 天窗口聚合运行证据（窗口须 ≥30 天）。</div>
          <input className="setting-input" style={{ marginTop: 6 }} placeholder="判据版本（如 v1）" value={criteriaVersion} onChange={e => setCriteriaVersion(e.target.value)} aria-label="判据版本" />
          {evaluation && <div className="setting-desc" style={{ marginTop: 6 }}>评估 {evaluation.evaluationId} · 结论 {evaluation.outcome} · 证据摘要 {evaluation.evidenceDigest.slice(0, 12)}…</div>}
        </div>
        <div style={{ display: 'flex', gap: 8 }}>
          <button disabled={busy || !subjectId.trim()} onClick={() => void evaluate()}>发起评估</button>
        </div>
      </div>
      <div className="setting-row">
        <div>
          <div className="setting-label">决策确认</div>
          <div className="setting-desc">评估通过后生成待确认决策；确认令牌经审计事件带外下发。</div>
          <input className="setting-input" style={{ marginTop: 6 }} placeholder="decisionId" value={decisionId} onChange={e => setDecisionId(e.target.value)} aria-label="决策 ID" />
          <input className="setting-input" style={{ marginTop: 6 }} placeholder="decisionToken（64 位十六进制）" value={decisionToken} onChange={e => setDecisionToken(e.target.value)} aria-label="决策令牌" />
        </div>
        <div style={{ display: 'flex', gap: 8 }}>
          <button className="primary" disabled={busy || !decisionId.trim() || !decisionToken.trim()} onClick={() => void confirm()}>确认决策</button>
        </div>
      </div>
      {status && <p role="status" className="notice">{status}</p>}
    </div>
  )
}

// 0.3.5 命令白名单 — command.run 用户可配只读命令集（tools.commandPolicy.*）。
// 内置 git/go 只读规则不可移除；此处编辑的是叠加在其上的用户白名单，
// 保存即校验并热生效（fail-closed：非法文档整体拒绝，不影响现运行规则）。
interface PolicyEntry { prefix: string; maxArgs: number; timeoutMs: number }
export function CommandPolicyPanel({ bridge = toolsPolicyBridge }: { bridge?: ToolsPolicyBridge }): React.JSX.Element {
  const [entries, setEntries] = useState<PolicyEntry[]>([])
  const [status, setStatus] = useState('')
  const [busy, setBusy] = useState(false)
  const [loaded, setLoaded] = useState(false)

  const parsePrefix = (raw: string): string[] => raw.trim().split(/\s+/).filter(Boolean)
  const formatPrefix = (prefix: string[]): string => prefix.join(' ')

  useEffect(() => {
    const load = async () => {
      setBusy(true)
      try {
        const r = await bridge.getCommandPolicy()
        setEntries(r.commands.map(c => ({ prefix: formatPrefix(c.prefix), maxArgs: c.maxArgs ?? 0, timeoutMs: c.timeoutMs ?? 10_000 })))
        setLoaded(true)
      } catch (e) { setStatus(e instanceof Error ? e.message : '命令白名单读取失败') } finally { setBusy(false) }
    }
    void load()
  }, [])

  const save = async () => {
    setBusy(true); setStatus('')
    try {
      const commands = entries.map(e => {
        const prefix = parsePrefix(e.prefix)
        const doc: { prefix: string[]; maxArgs?: number; timeoutMs?: number } = { prefix }
        if (e.maxArgs > 0) doc.maxArgs = e.maxArgs
        if (e.timeoutMs > 0) doc.timeoutMs = e.timeoutMs
        return doc
      }).filter(c => c.prefix.length > 0)
      const r = await bridge.setCommandPolicy({ commands })
      setStatus(`已保存并热生效：${r.applied} 条用户规则（叠加内置 git/go 只读集）。`)
      setEntries(commands.map(c => ({ prefix: formatPrefix(c.prefix), maxArgs: c.maxArgs ?? 0, timeoutMs: c.timeoutMs ?? 10_000 })))
    } catch (e) { setStatus(e instanceof Error ? e.message : '命令白名单保存失败（文档被整体拒绝，现运行规则不变）') } finally { setBusy(false) }
  }

  return (
    <div className="setting-group">
      <div className="setting-group-title">命令白名单（command.run）</div>
      <div className="setting-row" style={{ gridTemplateColumns: '1fr' }}>
        <div className="setting-desc">聊天中 command.run 仅允许白名单内的只读命令。内置 git/go 只读集恒生效；下方为用户附加规则，保存即校验并热生效，非法文档整体拒绝（fail-closed）。超时范围 1s–300s，argv 总长上限 16。</div>
      </div>
      {entries.map((entry, i) => (
        <div className="setting-row" key={i} style={{ gridTemplateColumns: '1fr auto' }}>
          <div style={{ display: 'flex', gap: 8, flexWrap: 'wrap', alignItems: 'center' }}>
            <input className="setting-input" style={{ width: 260 }} placeholder="命令前缀（如 node --version）" value={entry.prefix} maxLength={128} onChange={e => setEntries(entries.map((x, j) => j === i ? { ...x, prefix: e.target.value } : x))} aria-label={`规则 ${i + 1} 命令前缀`} />
            <input className="setting-input" style={{ width: 90 }} type="number" min={0} max={16} placeholder="maxArgs" value={entry.maxArgs || ''} onChange={e => setEntries(entries.map((x, j) => j === i ? { ...x, maxArgs: Math.max(0, Math.min(16, Number(e.target.value) || 0)) } : x))} aria-label={`规则 ${i + 1} 最大参数数`} />
            <input className="setting-input" style={{ width: 110 }} type="number" min={1000} max={300000} step={500} placeholder="超时 ms" value={entry.timeoutMs || ''} onChange={e => setEntries(entries.map((x, j) => j === i ? { ...x, timeoutMs: Math.max(1000, Math.min(300000, Number(e.target.value) || 10_000)) } : x))} aria-label={`规则 ${i + 1} 超时毫秒`} />
          </div>
          <button disabled={busy} onClick={() => setEntries(entries.filter((_, j) => j !== i))} aria-label={`删除规则 ${i + 1}`}>删除</button>
        </div>
      ))}
      <div className="setting-row">
        <div className="setting-desc">{loaded ? `共 ${entries.length} 条用户规则（不含内置 git/go 只读集）` : '正在读取当前白名单…'}</div>
        <div style={{ display: 'flex', gap: 8 }}>
          <button disabled={busy} onClick={() => setEntries([...entries, { prefix: '', maxArgs: 0, timeoutMs: 10_000 }])}>添加规则</button>
          <button className="primary" disabled={busy || !loaded} onClick={() => void save()}>保存并热生效</button>
        </div>
      </div>
      {status && <p role="status" className="notice">{status}</p>}
    </div>
  )
}

// P3-B Hooks — 工具调用拦截规则（tools.hooksPolicy.*）。规则在工具执行前
// 评估：block 拒绝并回显原因；requireApproval 强制审批（即使自动模式）；
// allow 免除审批往返（不放宽命令白名单）。多规则命中时 block 最优先。
// 保存即校验并热生效（fail-closed：非法文档整体拒绝，不影响现运行规则）。
const HOOK_TOOLS = ['workspace.list', 'workspace.read', 'workspace.write', 'workspace.search', 'workspace.edit', 'todo.write', 'command.run', 'web.fetch', 'web.search', 'excel.gen', 'excel.parse', 'docx.gen', 'pptx.gen', 'pdf.gen'] as const
const HOOK_DECISIONS = [
  { value: 'block', label: '拦截（拒绝执行）' },
  { value: 'requireApproval', label: '强制审批' },
  { value: 'allow', label: '免审批放行' },
] as const
interface HookEntry { id: string; events: string[]; tools: string[]; decision: string; message: string }
export function HooksPanel({ bridge = hooksPolicyBridge }: { bridge?: HooksPolicyBridge }): React.JSX.Element {
  const [entries, setEntries] = useState<HookEntry[]>([])
  const [events, setEvents] = useState<{ sessionId: string; toolName: string; hookId: string; event: string; decision: string; createdAt: string }[]>([])
  const [status, setStatus] = useState('')
  const [busy, setBusy] = useState(false)
  const [loaded, setLoaded] = useState(false)

  useEffect(() => {
    const load = async () => {
      setBusy(true)
      try {
        const [policy, recent] = await Promise.all([bridge.getHooksPolicy(), bridge.listHookEvents({ limit: 20 })])
        setEntries(policy.hooks.map(h => ({ id: h.id, events: [...h.events], tools: [...h.tools], decision: h.decision, message: h.message ?? '' })))
        setEvents(recent.events)
        setLoaded(true)
      } catch (e) { setStatus(e instanceof Error ? e.message : 'Hooks 规则读取失败') } finally { setBusy(false) }
    }
    void load()
  }, [])

  const refreshEvents = async () => {
    try { const r = await bridge.listHookEvents({ limit: 20 }); setEvents(r.events) } catch { /* 保持现状 */ }
  }

  const save = async () => {
    setBusy(true); setStatus('')
    try {
      const hooks = entries
        .map(e => ({ ...e, id: e.id.trim(), tools: e.tools.filter(Boolean) }))
        .filter(e => e.id && e.tools.length > 0)
      const r = await bridge.setHooksPolicy({ hooks: hooks as ToolsHooksPolicySetPayload['hooks'] })
      setStatus(`已保存并热生效：${r.applied} 条 Hook 规则。`)
      setEntries(hooks)
    } catch (e) { setStatus(e instanceof Error ? e.message : 'Hooks 规则保存失败（文档被整体拒绝，现运行规则不变）') } finally { setBusy(false) }
  }

  const toggleIn = (list: string[], value: string): string[] => list.includes(value) ? list.filter(x => x !== value) : [...list, value]
  const update = (i: number, patch: Partial<HookEntry>) => setEntries(entries.map((x, j) => j === i ? { ...x, ...patch } : x))

  return (
    <div className="setting-group">
      <div className="setting-group-title">Hooks 拦截规则（工具调用）</div>
      <div className="setting-row" style={{ gridTemplateColumns: '1fr' }}>
        <div className="setting-desc">在工具执行前拦截：拦截=拒绝并回显原因；强制审批=即使自动编辑/完全访问也走审批；免审批=跳过审批往返（命令白名单仍生效）。多条规则命中时按 拦截 &gt; 强制审批 &gt; 免审批 取最严。保存即校验并热生效，非法文档整体拒绝（fail-closed）。</div>
      </div>
      {entries.map((entry, i) => (
        <div className="setting-row" key={i} style={{ gridTemplateColumns: '1fr auto' }}>
          <div style={{ display: 'flex', gap: 8, flexWrap: 'wrap', alignItems: 'center' }}>
            <input className="setting-input" style={{ width: 150 }} placeholder="规则 ID（如 no-docx）" value={entry.id} maxLength={64} onChange={e => update(i, { id: e.target.value })} aria-label={`规则 ${i + 1} ID`} />
            <select className="setting-input" style={{ width: 150 }} value={entry.decision} onChange={e => update(i, { decision: e.target.value, message: e.target.value === 'block' ? entry.message : '' })} aria-label={`规则 ${i + 1} 动作`}>
              {HOOK_DECISIONS.map(d => <option key={d.value} value={d.value}>{d.label}</option>)}
            </select>
            {entry.decision === 'block' && <input className="setting-input" style={{ width: 220 }} placeholder="拒绝原因（必填）" value={entry.message} maxLength={256} onChange={e => update(i, { message: e.target.value })} aria-label={`规则 ${i + 1} 拒绝原因`} />}
          </div>
          <div style={{ display: 'flex', gap: 8, flexWrap: 'wrap', alignItems: 'center' }}>
            {HOOK_TOOLS.map(tool => (
              <label key={tool} style={{ display: 'inline-flex', gap: 4, alignItems: 'center', fontSize: 12 }}>
                <input type="checkbox" checked={entry.tools.includes(tool)} onChange={() => update(i, { tools: toggleIn(entry.tools, tool) })} aria-label={`规则 ${i + 1} 工具 ${tool}`} />
                {tool}
              </label>
            ))}
            <label style={{ display: 'inline-flex', gap: 4, alignItems: 'center', fontSize: 12 }}>
              <input type="checkbox" checked={entry.events.includes('afterToolCall')} onChange={() => update(i, { events: toggleIn(entry.events.includes('beforeToolCall') ? entry.events : ['beforeToolCall', ...entry.events], 'afterToolCall') })} aria-label={`规则 ${i + 1} 执行后审计`} />
              执行后审计
            </label>
            <button disabled={busy} onClick={() => setEntries(entries.filter((_, j) => j !== i))} aria-label={`删除规则 ${i + 1}`}>删除</button>
          </div>
        </div>
      ))}
      <div className="setting-row">
        <div className="setting-desc">{loaded ? `共 ${entries.length} 条规则` : '正在读取当前 Hooks 规则…'}</div>
        <div style={{ display: 'flex', gap: 8 }}>
          <button disabled={busy} onClick={() => setEntries([...entries, { id: '', events: ['beforeToolCall'], tools: [], decision: 'block', message: '' }])}>添加规则</button>
          <button className="primary" disabled={busy || !loaded} onClick={() => void save()}>保存并热生效</button>
        </div>
      </div>
      {loaded && (
        <div className="setting-row" style={{ gridTemplateColumns: '1fr' }}>
          <div className="setting-label">最近命中记录</div>
          <div style={{ display: 'flex', justifyContent: 'flex-end' }}>
            <button disabled={busy} onClick={() => void refreshEvents()}>刷新</button>
          </div>
          {events.length === 0 ? <div className="setting-desc">暂无命中记录。</div> : (
            <ul style={{ margin: 0, paddingLeft: 18, fontSize: 12, lineHeight: 1.9 }}>
              {events.map((ev, k) => (
                <li key={k}>{ev.createdAt} · {ev.hookId} · {ev.toolName} · {ev.event}{ev.decision ? ` · ${ev.decision}` : ''} · 会话 {ev.sessionId.slice(-6)}</li>
              ))}
            </ul>
          )}
        </div>
      )}
      {status && <p role="status" className="notice">{status}</p>}
    </div>
  )
}

// M7 appUpdate — 诊断与更新：检查更新通道与安装（回滚状态可见）。
function DiagnosticsPanel(): React.JSX.Element {
  const bridge = getAppUpdateBridge()
  const diagnostics = getDiagnosticsBridge()
  const [channel, setChannel] = useState<'stable' | 'beta'>('stable')
  const [status, setStatus] = useState('')
  const [busy, setBusy] = useState(false)
  const [update, setUpdate] = useState<{ updateId: string; version: string; digest: string; mandatory: boolean } | undefined>()
  const [exportResult, setExportResult] = useState<{ path: string; createdAt: string } | undefined>(undefined)
  const [includeLogs, setIncludeLogs] = useState(false)
  const [redactPaths, setRedactPaths] = useState(true)

  const check = async () => {
    setBusy(true); setStatus('正在检查更新…')
    try {
      const r = await bridge.check({ channel, currentVersion: '0.3.0-dev' })
      if (!r.updateId) { setUpdate(undefined); setStatus('已是最新版本。') }
      else { setUpdate({ updateId: r.updateId, version: r.version, digest: r.digest, mandatory: r.mandatory }); setStatus(`发现新版本 ${r.version}${r.mandatory ? '（强制更新）' : ''}`) }
    } catch (e) { setStatus(e instanceof Error ? e.message : '检查更新失败') } finally { setBusy(false) }
  }
  const install = async () => {
    if (!update) return
    setBusy(true); setStatus('正在安装更新…')
    try {
      const r = await bridge.install({ updateId: update.updateId, expectedDigest: update.digest })
      setStatus(r.state === 'installed' ? '更新已安装。' : `更新已回滚（${r.state}），请查看诊断日志。`)
      setUpdate(undefined)
    } catch (e) { setStatus(e instanceof Error ? e.message : '安装失败') } finally { setBusy(false) }
  }
  // diagnostics.export — 脱敏诊断包导出（M8 低风险残留项补齐）
  const exportDiagnostics = async () => {
    setBusy(true); setStatus('正在导出诊断包…')
    try {
      const r = await diagnostics.exportDiagnostics({ includeLogs, redactPaths })
      setExportResult({ path: r.path, createdAt: r.createdAt })
      setStatus(`诊断包已导出：${r.path}`)
    } catch (e) { setStatus(e instanceof Error ? e.message : '诊断包导出失败') } finally { setBusy(false) }
  }

  return (
    <div className="setting-group">
      <div className="setting-group-title">诊断与更新</div>
      <SelectRow label="更新通道" desc="stable 稳定通道；beta 抢先体验（含未完成特性）" value={channel} options={[{ value: 'stable', label: 'stable（稳定）' }, { value: 'beta', label: 'beta（抢先）' }]} onChange={v => { setChannel(v as 'stable' | 'beta'); setUpdate(undefined) }} />
      <div className="setting-row">
        <div>
          <div className="setting-label">应用更新</div>
          <div className="setting-desc">{update ? `新版本 ${update.version} · 摘要 ${update.digest.slice(0, 12)}…` : '检查本机应用版本与新版本可用性；安装失败自动回滚。'}</div>
        </div>
        <div style={{ display: 'flex', gap: 8 }}>
          <button disabled={busy} onClick={() => void check()}>检查更新</button>
          {update && <button disabled={busy} onClick={() => void install()}>安装</button>}
        </div>
      </div>
      <div className="setting-row">
        <div>
          <div className="setting-label">诊断包导出</div>
          <div className="setting-desc">{exportResult ? `已导出 ${exportResult.createdAt}（路径已脱敏）` : '导出脱敏诊断包，供问题定位使用。'}</div>
          <label style={{ display: 'flex', gap: 6, alignItems: 'center', marginTop: 6 }}>
            <input type="checkbox" checked={includeLogs} onChange={e => setIncludeLogs(e.target.checked)} />
            <span className="setting-desc">包含日志</span>
          </label>
          <label style={{ display: 'flex', gap: 6, alignItems: 'center', marginTop: 4 }}>
            <input type="checkbox" checked={redactPaths} onChange={e => setRedactPaths(e.target.checked)} />
            <span className="setting-desc">脱敏文件路径</span>
          </label>
        </div>
        <div style={{ display: 'flex', gap: 8 }}>
          <button disabled={busy} onClick={() => void exportDiagnostics()}>导出诊断包</button>
        </div>
      </div>
      {status && <p role="status" className="notice">{status}</p>}
    </div>
  )
}

function ProjectScopedTabs({ tabs }: { tabs: Array<{ id: string; label: string; render: (projectId: string) => React.JSX.Element }> }): React.JSX.Element {
  const [projects, setProjects] = useState<ProjectDTO[]>([])
  const [projectId, setProjectId] = useState('')
  const [tab, setTab] = useState(tabs[0]?.id ?? '')
  const [error, setError] = useState('')
  useEffect(() => {
    let alive = true
    projectBridge.list().then(result => {
      if (!alive) return
      setProjects(result.items)
      setProjectId(current => (result.items.some(item => item.id === current) ? current : (result.items[0]?.id ?? '')))
    }).catch(e => { if (alive) setError(e instanceof Error ? e.message : '项目列表载入失败') })
    return () => { alive = false }
  }, [])
  const active = tabs.find(item => item.id === tab) ?? tabs[0]
  return (
    <div className="project-scoped-panel">
      <div className="project-scoped-toolbar">
        <label>作用域项目
          <select aria-label="选择作用域项目" value={projectId} onChange={e => setProjectId(e.target.value)}>
            {projects.map(item => <option key={item.id} value={item.id}>{item.name}</option>)}
          </select>
        </label>
        <div className="project-scoped-tabs" role="tablist" aria-label="子分类">
          {tabs.map(item => (
            <button type="button" role="tab" key={item.id} aria-selected={item.id === active?.id} onClick={() => setTab(item.id)}>{item.label}</button>
          ))}
        </div>
      </div>
      {error ? <p className="notice" role="alert">{error}</p> : !projectId ? <p className="notice">还没有可用项目；请先在“项目管理”中创建。</p> : active?.render(projectId)}
    </div>
  )
}

export function applyAppearance(settings: AppearanceSettings): void {
  const root = document.documentElement
  root.classList.toggle('no-starlight', !settings.starlight)
  root.classList.toggle('no-moonlight', !settings.moonlight)
  root.classList.toggle('reduce-motion', settings.reduceMotion)
  root.dataset.density = settings.density
}

/** 应用启动时从 localStorage 恢复外观设置，应在 App 挂载时调用 */
export function initAppearance(): void {
  applyAppearance(loadSettings('appearance', DEFAULT_APPEARANCE))
}
