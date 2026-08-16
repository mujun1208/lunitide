import React, { useEffect, useRef, useState } from 'react'
import{getAppUpdateBridge,getCollabGateBridge,getDiagnosticsBridge,getMcpBridge,getPluginBridge,getTtsBridge,systemSettingsBridge,type TtsVoice}from'../bridge/client'
import{microphoneConstraints,saveMicrophoneId,selectedMicrophoneId}from'./microphone'
import{defaultCompanionSettings,loadCompanionSettings,saveCompanionSettings,type CompanionSettings}from'../session/companion/companionSettings'
type SettingsCategory = 'general' | 'appearance' | 'providers' | 'voice' | 'mcp' | 'plugins' | 'collab' | 'project' | 'connection' | 'security' | 'data' | 'shortcuts' | 'diagnostics' | 'about'

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
  { id: 'mcp', icon: '⧉', label: 'MCP 服务器' },
  { id: 'plugins', icon: '⬢', label: '插件' },
  { id: 'collab', icon: '⌘', label: '协作门禁' },
  { id: 'project', icon: '◇', label: '项目默认值' },
  { id: 'connection', icon: '⇄', label: '连接与代理' },
  { id: 'security', icon: '⛨', label: '安全与治理' },
  { id: 'data', icon: '❖', label: '数据与记忆' },
  { id: 'shortcuts', icon: '⌨', label: '快捷键' },
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
          {category === 'mcp' && <McpPanel />}
          {category === 'plugins' && <PluginsPanel />}
          {category === 'collab' && <CollabGatePanel />}
          {category === 'diagnostics' && <DiagnosticsPanel />}
          {category === 'about' && <AboutPanel />}
          {(['project', 'connection', 'security', 'data', 'shortcuts'].includes(category)) && <PlaceholderPanel category={category} />}
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
 const audioRef=useRef<HTMLAudioElement|undefined>(undefined)
 useEffect(()=>{setCompanion(loadCompanionSettings());let cancelled=false;getTtsBridge().voices().then(r=>{if(cancelled)return;setVoices(r.voices);setEngineState(r.voices.length?'available':'unavailable')}).catch(()=>{if(!cancelled)setEngineState('unavailable')});return()=>{cancelled=true;audioRef.current?.pause();audioRef.current=undefined}},[])
 const save=(next:CompanionSettings)=>{setCompanion(next);saveCompanionSettings(next);setStatus('设置已保存，立即生效。')}
 const preview=async()=>{if(!voices.length)return;setBusy(true);setStatus('正在合成试听…');try{const voiceId=companion.voiceId&&(voices.some(v=>v.voice_id===companion.voiceId)?companion.voiceId:(setStatus('所选音色不可用，已回退默认音色。'),undefined))||undefined;const result=await getTtsBridge().synthesize({text:'你好，我是月汐，很高兴与你同行。',voiceId,rate:companion.rate,volume:companion.volume});if(result.discarded||!result.wav_base64){setStatus('试听已取消。');return}const bytes=Uint8Array.from(atob(result.wav_base64),c=>c.charCodeAt(0)),url=URL.createObjectURL(new Blob([bytes],{type:'audio/wav'}));audioRef.current?.pause();const audio=new Audio(url);audioRef.current=audio;audio.onended=()=>URL.revokeObjectURL(url);await audio.play();setStatus(result.notice?`${result.notice}（M95-004）`:'试听播放中…')}catch(e){setStatus(e instanceof Error?`试听失败：${e.message}`:'试听失败，本机可能无语音合成引擎')}finally{setBusy(false)}}
 const disabled=engineState!=='available'
 return <><div className="setting-row" style={{gridTemplateColumns:'1fr'}}><div className="setting-group-title" style={{marginTop:8}}>月伴对话</div></div><Toggle label="启用月伴对话" desc="在普通聊天输入框显示月亮按钮，进入全屏语音对话舞台；关闭即回滚入口。" on={companion.enabled} onChange={v=>save({...companion,enabled:v})}/><Toggle label="回复自动朗读" desc="回复完成后自动用本机语音朗读；关闭后仅显示字幕。" on={companion.autoSpeak} onChange={v=>save({...companion,autoSpeak:v})}/><div className="setting-row"><div><div className="setting-label">朗读音色</div><div className="setting-desc">{engineState==='probing'?'正在检测本机语音合成引擎…':engineState==='available'?`本机可用音色 ${voices.length} 个（Windows SAPI，离线合成）`:'本机无语音合成引擎（M95-001），月伴将自动切换字幕模式'}</div></div><div style={{display:'flex',gap:8,alignItems:'center'}}><select className="setting-select" aria-label="朗读音色" disabled={disabled} value={companion.voiceId} onChange={e=>save({...companion,voiceId:e.target.value})}><option value="">默认音色</option>{voices.map(voice=><option key={voice.voice_id} value={voice.voice_id}>{voice.display_name} · {voice.lang}</option>)}</select><button disabled={disabled||busy} onClick={()=>void preview()}>{busy?'合成中…':'试听'}</button></div></div><div className="setting-row"><div><div className="setting-label">语速（SAPI rate {-10}~{10}）</div><div className="setting-desc">当前 {companion.rate}</div></div><input type="range" min={-10} max={10} step={1} disabled={disabled} value={companion.rate} aria-label="朗读语速" onChange={e=>save({...companion,rate:Number(e.target.value)})} style={{accentColor:'var(--tide1)'}}/></div><div className="setting-row"><div><div className="setting-label">音量（0~100）</div><div className="setting-desc">当前 {companion.volume}</div></div><input type="range" min={0} max={100} step={1} disabled={disabled} value={companion.volume} aria-label="朗读音量" onChange={e=>save({...companion,volume:Number(e.target.value)})} style={{accentColor:'var(--tide1)'}}/></div>{status&&<p role="status" className="notice">{status}</p>}</>
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

function PlaceholderPanel({ category }: { category: SettingsCategory }): React.JSX.Element {
  const descriptions: Partial<Record<SettingsCategory, string>> = {
    project: '项目级配置默认值：阶段策略、模板引用、自动审批边界、产物保留策略等。待 M3 九阶段与产物设计冻结后启用。',
    connection: '网络代理配置：HTTP/HTTPS 代理、SSL 证书策略、连接超时与重试、SSRF 防护白名单。待 M5 数据接口工作台阶段启用。',
    security: '安全与治理：审批边界配置、审计日志策略、高风险操作清单、凭据访问策略、信任根管理。待 M4 智能内核阶段启用。',
    data: '数据与记忆：记忆生命周期、备份恢复策略、隐私脱敏规则、数据删除与保留。待 M2 基础备份恢复与 M4 记忆中心完成后启用。',
    shortcuts: '键盘快捷键：全局与上下文快捷键自定义、冲突检测、预设方案。即将推出。',
    diagnostics: '诊断与更新：应用更新通道、运行健康检查、脱敏诊断包导出、日志级别配置。待 M6 CR 部署发行阶段启用。',
  }
  return (
    <div className="setting-group">
      <div className="setting-group-title">{CATEGORIES.find(c => c.id === category)?.label}</div>
      <div className="setting-placeholder">
        <div className="placeholder-icon">◇</div>
        <p>{descriptions[category] ?? '此分类正在规划中，即将推出。'}</p>
        <span className="placeholder-badge">规划中</span>
      </div>
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
