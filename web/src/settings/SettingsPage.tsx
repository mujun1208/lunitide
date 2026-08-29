import React, { useEffect, useRef, useState } from 'react'
import{getAppUpdateBridge,getCollabGateBridge,getDiagnosticsBridge,getMcpBridge,getSystemHealthBridge,getTtsBridge,hooksPolicyBridge,projectBridge,systemSettingsBridge,toolsPolicyBridge,brBridge,ccBridge,conversationsBridge,type BrBridge,type CcBridge,type HooksPolicyBridge,type McpBridge,type ProviderBridge,type ToolsPolicyBridge,type TtsVoice,type TtsRefMeta}from'../bridge/client'
import type{BrDataUsageResult,BrModeDetectResult,BrPermissionListResult,BrPermissionPolicyPayload,BrSessionListResult,BrSettingsGetResult,BrSettingsUpdatePayload,CcGetAuditLogResult,CcGetConfigResult,CcUpdateConfigPayload,Mcp6PresetsListResult,ProjectDTO,ToolsHooksPolicySetPayload}from'../generated/bridge'
import{microphoneConstraints,saveMicrophoneId,selectedMicrophoneId}from'./microphone'
import{ChoiceTiles}from'./ChoiceTiles'
import{VoicePathPicker}from'./VoicePathPicker'
import{VoicePersonaGrid}from'./VoicePersonaGrid'
import{filterSettingsNav,SETTINGS_NAV_GROUPS,SETTINGS_CATEGORIES,type SettingsCategory}from'./settingsNav'
import{REPLY_STYLE_OPTIONS,STRUCTURED_TEMPLATE_OPTIONS}from'./replySettings'
import{applyVoicePath,defaultCompanionSettings,formatInterruptHotkey,interruptHotkeyFromEvent,loadCompanionSettings,saveCompanionSettings,type CompanionSettings,type InterruptHotkey}from'../session/companion/companionSettings'
import{shownVoicePath}from'../session/companion/voicePersonas'
import{LocalAsrRow}from'./LocalAsrRow'
import{SubagentsPanel}from'./SubagentsPanel'
import{ProviderApp}from'../provider/ProviderApp'
import{PlanPage}from'../plan/PlanPage'
import{ReviewPage}from'../review/ReviewPage'
import{PersonalIntelligencePage}from'../m8/PersonalIntelligencePage'
import{ProfilePanel}from'./ProfilePanel'
import{useZh}from'../i18n/language'


interface GeneralSettings {
  startupPage: 'new' | 'last' | 'projects'
  restoreUnfinished: boolean
  recentProjectCount: 5 | 8 | 10
  language: 'zh-CN' | 'en'
  timezone: string
  enterToSend: boolean
  autoTitle: boolean
  defaultMode: 'auto' | 'collab' | 'code' | 'full-access'
  replyStyle: 'default' | 'assistant' | 'support' | 'teacher' | 'npc'
  structuredTemplate: 'off' | 'event' | 'form' | 'kv'
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
  defaultMode: 'full-access', // 默认提升为完全访问权限
  replyStyle: 'default',
  structuredTemplate: 'off',
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
  try {
    localStorage.setItem(`lunitide:${key}`, JSON.stringify(value))
    if (key === 'general') window.dispatchEvent(new Event('lunitide:general'))
  } catch { /* ignore */ }
}

export function SettingsPage({ onNavigateExpert, onBack, initialCategory = 'general', providers }: { onNavigateExpert?: () => void; onBack?: () => void; initialCategory?: SettingsCategory; providers?: ProviderBridge }): React.JSX.Element {
  const zh = useZh()
  const [category, setCategory] = useState<SettingsCategory>(initialCategory)
  const [search, setSearch] = useState('')
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
      <nav className="settings-nav" aria-label={zh ? '设置导航' : 'Settings'}>
        <button className="settings-back" onClick={onBack}>← {zh ? '返回主页' : 'Home'}</button>
        <div className="settings-search" role="search">
          <input type="search" placeholder={zh ? '搜索设置…' : 'Search settings…'} aria-label={zh ? '搜索设置' : 'Search settings'} value={search} onChange={e => setSearch(e.target.value)} />
        </div>
        <div className="settings-menu">
          {SETTINGS_NAV_GROUPS.map(group => {
            const items = filterSettingsNav(search).filter(c => group.ids.includes(c.id))
            if (!items.length) return null
            const meta = SETTINGS_CATEGORIES
            return (
              <div key={group.label} className="settings-nav-group">
                <div className="settings-nav-group-label">{zh ? group.label : group.labelEn}</div>
                {items.map(c => {
                  const item = meta.find(x => x.id === c.id) ?? c
                  return (
                    <button
                      key={c.id}
                      className={`settings-item ${category === c.id ? 'on' : ''}`}
                      onClick={() => setCategory(c.id)}
                      aria-current={category === c.id ? 'page' : undefined}
                    >
                      <span className="ic" aria-hidden="true">{item.icon}</span>
                      {zh ? item.label : item.labelEn}
                    </button>
                  )
                })}
              </div>
            )
          })}
          {filterSettingsNav(search).length === 0 && <p className="settings-nav-empty">{zh ? '没有匹配的设置' : 'No matching settings'}</p>}
        </div>
      </nav>
      <div className="settings-content">
        <div className="settings-top">
          <h2 style={{ margin: 0, fontFamily: 'var(--serif)', fontSize: '20px' }}>
            {(() => { const item = SETTINGS_CATEGORIES.find(c => c.id === category); return item ? (zh ? item.label : item.labelEn) : '' })()}
          </h2>
          {saved && <span className="save-indicator" role="status">✓ 已保存</span>}
        </div>
        <div className={`settings-body${category === 'providers' ? ' settings-body-providers' : ''}${category === 'voice' ? ' settings-body-voice' : ''}`}>
          {category === 'general' && <GeneralPanel settings={general} onChange={updateGeneral} />}
          {category === 'appearance' && <AppearancePanel settings={appearance} onChange={updateAppearance} />}
          {category === 'profile' && <ProfilePanel />}
          {category === 'providers' && (providers ? <ProviderApp bridge={providers} embedded /> : <p className="setting-desc">供应商列表需要 Host 桥接。</p>)}
          {category === 'voice' && <VoicePanel />}
          {category === 'personal' && <PersonalIntelligencePage onNavigateExpert={onNavigateExpert} />}
          {category === 'security' && <div className="governance-stack">
            <div className="setting-group">
              <div className="setting-group-title">编码与权限</div>
              <div className="setting-row" style={{ gridTemplateColumns: '1fr' }}>
                <div className="setting-desc">命令白名单约束 command.run。Git 默认只读（写操作需确认）。工作区 AGENTS.md / .agents/skills 叠在月汐身份上，不替换身份。技能与 MCP 在能力中心维护。</div>
              </div>
            </div>
            <CommandPolicyPanel />
            <HooksPanel />
            <ProjectScopedTabs tabs={[{ id: 'review', label: '审批', render: pid => <ReviewPage projectId={pid} embedded /> }, { id: 'plans', label: '计划', render: pid => <PlanPage projectId={pid} /> }]} />
          </div>}
          {category === 'browser' && <BrowserPanel />}
          {category === 'computer' && <ComputerPanel />}
          {category === 'subagents' && <SubagentsPanel onSaved={() => setSaved(true)} />}
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

function HotkeyRow({ label, desc, hotkey, onChange }: {
  label: string
  desc: string
  hotkey: InterruptHotkey
  onChange: (next: InterruptHotkey) => void
}): React.JSX.Element {
  const [capturing, setCapturing] = useState(false)
  return (
    <div className="setting-row">
      <div>
        <div className="setting-label">{label}</div>
        <div className="setting-desc">{desc}</div>
      </div>
      <button
        type="button"
        className={`setting-hotkey${capturing ? ' capturing' : ''}`}
        aria-label={label}
        aria-pressed={capturing}
        onClick={() => setCapturing(true)}
        onBlur={() => setCapturing(false)}
        onKeyDown={event => {
          if (!capturing) {
            if (event.key === 'Enter' || event.key === ' ') {
              event.preventDefault()
              setCapturing(true)
            }
            return
          }
          event.preventDefault()
          event.stopPropagation()
          const next = interruptHotkeyFromEvent(event.nativeEvent)
          if (!next) return
          onChange(next)
          setCapturing(false)
        }}
      >
        {capturing ? '按下要设置的快捷键…' : formatInterruptHotkey(hotkey)}
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
            { value: 'full-access', label: '完全访问' },
          ]}
          onChange={v => onChange('defaultMode', v as GeneralSettings['defaultMode'])}
        />
      </div>
      <div className="setting-group">
        <div className="setting-group-title">人设与输出</div>
        <ChoiceTiles
          legend="说话风格"
          name="replyStyle"
          value={settings.replyStyle}
          options={REPLY_STYLE_OPTIONS}
          onChange={v => onChange('replyStyle', v)}
        />
        <ChoiceTiles
          legend="结构化输出"
          name="structuredTemplate"
          value={settings.structuredTemplate}
          options={STRUCTURED_TEMPLATE_OPTIONS}
          onChange={v => onChange('structuredTemplate', v)}
        />
      </div>
      <ConversationsStorageSection />
    </>
  )
}

function ConversationsStorageSection(): React.JSX.Element {
  const [path, setPath] = useState('')
  const [configured, setConfigured] = useState(false)
  const [legacyPath, setLegacyPath] = useState('')
  const [busy, setBusy] = useState(false)
  const [notice, setNotice] = useState('')
  const load = async () => {
    try {
      const status = await conversationsBridge.get()
      setPath(status.path ?? '')
      setConfigured(!!status.configured)
      setLegacyPath(status.legacyPath ?? '')
    } catch (e) {
      setNotice(e instanceof Error ? e.message : '无法读取对话存储路径')
    }
  }
  useEffect(() => {
    void load()
  }, [])
  const choose = async () => {
    setBusy(true)
    setNotice('')
    try {
      const picked = await conversationsBridge.select()
      if (picked.path) setPath(picked.path)
    } catch (e) {
      setNotice(e instanceof Error ? e.message : '未选择文件夹')
    } finally {
      setBusy(false)
    }
  }
  const save = async () => {
    const next = path.trim()
    if (!next) {
      setNotice('请先选择或填写存储目录')
      return
    }
    setBusy(true)
    setNotice('正在保存并迁移历史对话数据…')
    try {
      const result = await conversationsBridge.set({ path: next })
      setPath(result.path)
      setConfigured(result.configured)
      setLegacyPath(result.legacyPath ?? legacyPath)
      setNotice(
        result.migratedSessions > 0
          ? `已保存。已迁移 ${result.migratedSessions} 个历史会话文件夹。`
          : '已保存。每个新对话都会在此目录下自动创建独立子文件夹。',
      )
    } catch (e) {
      setNotice(e instanceof Error ? e.message : '保存失败')
    } finally {
      setBusy(false)
    }
  }
  return (
    <div className="setting-group">
      <div className="setting-group-title">数据存储</div>
      <div className="setting-row conversations-storage-row" style={{ gridTemplateColumns: '1fr' }}>
        <div>
          <div className="setting-label">数据存储路径</div>
          <div className="setting-desc">
            所有对话产物的根目录。修改后会将现有会话文件夹复制到新位置；每个对话会自动创建以会话编号命名的子文件夹，本次对话生成的 Word、PDF、HTML 等文件默认保存在其中。
          </div>
          {!configured && legacyPath && (
            <div className="setting-desc">当前使用默认位置：{legacyPath}</div>
          )}
        </div>
        <div className="conversations-storage-controls">
          <label className="conversations-storage-path">
            <span aria-hidden="true">📁</span>
            <input
              aria-label="对话数据存储路径"
              value={path}
              placeholder="选择或输入目录，例如 E:\\Trae-Work-Projects"
              onChange={e => setPath(e.target.value)}
            />
          </label>
          <div className="conversations-storage-actions">
            <button type="button" disabled={busy} onClick={() => void choose()}>
              选择文件夹
            </button>
            <button type="button" className="primary" disabled={busy || !path.trim()} onClick={() => void save()}>
              {busy ? '处理中…' : '保存'}
            </button>
          </div>
          {notice && (
            <p className="notice" role={notice.includes('失败') ? 'alert' : 'status'}>
              {notice}
            </p>
          )}
        </div>
      </div>
    </div>
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

function VoicePanel():React.JSX.Element{
 const[devices,setDevices]=useState<MediaDeviceInfo[]>([]),[deviceId,setDeviceId]=useState(selectedMicrophoneId),[status,setStatus]=useState(''),[busy,setBusy]=useState(false)
 const refresh=async()=>{setBusy(true);setStatus('正在检测麦克风…');try{if(!navigator.mediaDevices?.enumerateDevices)throw new Error('当前 WebView 不支持设备检测');const items=(await navigator.mediaDevices.enumerateDevices()).filter(item=>item.kind==='audioinput');setDevices(items);if(deviceId&&!items.some(item=>item.deviceId===deviceId))setStatus('当前设备列表中未显示已选麦克风；获得权限或重新连接设备后再确认。');else setStatus(items.length?`检测到 ${items.length} 个麦克风输入设备。`:'未检测到麦克风，请检查连接和 Windows 设备设置。')}catch(e){setStatus(e instanceof Error?e.message:'无法检测麦克风')}finally{setBusy(false)}}
 useEffect(()=>{void refresh()},[])
 const choose=(value:string)=>{setDeviceId(value);saveMicrophoneId(value);setStatus(value?'已保存所选麦克风。':'已使用系统默认麦克风。')}
 const test=async()=>{setBusy(true);setStatus('正在请求麦克风权限…');let stream:MediaStream|undefined;try{stream=await navigator.mediaDevices.getUserMedia(microphoneConstraints());setStatus('麦克风可用，测试成功。');const items=(await navigator.mediaDevices.enumerateDevices()).filter(item=>item.kind==='audioinput');setDevices(items)}catch(e){const name=e instanceof DOMException?e.name:'';setStatus(name==='NotAllowedError'||name==='SecurityError'?'权限被拒绝，请打开 Windows 麦克风设置并允许桌面应用访问。':name==='NotFoundError'?'未检测到可用麦克风。':'无法启动麦克风，请检查设备是否被占用或驱动异常。')}finally{stream?.getTracks().forEach(track=>track.stop());setBusy(false)}}
 const openSettings=async()=>{setBusy(true);try{await systemSettingsBridge.open({page:'privacy-microphone'});setStatus('已打开 Windows 麦克风隐私设置。请开启“麦克风访问”和“允许桌面应用访问麦克风”。')}catch(e){setStatus(e instanceof Error?e.message:'无法打开 Windows 麦克风设置')}finally{setBusy(false)}}
 return <div className="setting-group"><div className="setting-group-title">语音与麦克风</div><div className="setting-row"><div><div className="setting-label">输入设备</div><div className="setting-desc">选择语音输入使用的麦克风；设备失效时自动回退到系统默认。</div></div><select className="setting-select" aria-label="麦克风输入设备" value={deviceId} onChange={e=>choose(e.target.value)}><option value="">系统默认麦克风</option>{devices.map((device,index)=><option key={device.deviceId} value={device.deviceId}>{device.label||`麦克风 ${index+1}`}</option>)}</select></div><div className="setting-row" style={{gridTemplateColumns:'1fr'}}><div className="setting-desc">Windows 隐私权限不能由软件自动开启。请确保“麦克风访问”和“允许桌面应用访问麦克风”均已开启；语音转文字还依赖 Windows 在线语音识别服务。</div><div className="microphone-setting-actions"><button disabled={busy} onClick={()=>void refresh()}>刷新设备</button><button disabled={busy} onClick={()=>void test()}>测试麦克风</button><button className="primary" disabled={busy} onClick={()=>void openSettings()}>打开 Windows 麦克风设置</button></div>{status&&<p role="status" className="notice">{status}</p>}</div><CompanionSection/></div>
}
export function CompanionSection():React.JSX.Element{
 const[companion,setCompanion]=useState<CompanionSettings>(defaultCompanionSettings)
 const[voices,setVoices]=useState<TtsVoice[]>([])
 const[refMeta,setRefMeta]=useState<TtsRefMeta|undefined>(undefined)
 const[engineState,setEngineState]=useState<'probing'|'available'|'unavailable'>('probing')
 const[status,setStatus]=useState('')
 const[busy,setBusy]=useState(false)
 const audioRef=useRef<HTMLAudioElement|undefined>(undefined)
 useEffect(()=>{setCompanion(loadCompanionSettings());return()=>{audioRef.current?.pause();audioRef.current=undefined}},[])
 // Voice catalogue probes the machine for both engines (M95-001
 // degradation); natural leads with the OneCore neural pool. The ref
 // engine returns the built-in 18-voice character pack plus service
 // health in ref_meta.
 const refEndpointPayload=companion.engine==='ref'&&companion.refEndpoint?companion.refEndpoint:undefined
 const[showForeignEdgeVoices,setShowForeignEdgeVoices]=useState(false)
 useEffect(()=>{let cancelled=false;setEngineState('probing');getTtsBridge().voices({engine:companion.engine,refEndpoint:refEndpointPayload}).then(r=>{if(cancelled)return;setVoices(r.voices);setRefMeta(r.ref_meta);let available=r.voices.length>0;if(companion.engine==='ref'&&r.ref_meta&&!r.ref_meta.pack_exists){available=false}setEngineState(available?'available':'unavailable')}).catch(()=>{if(!cancelled){setVoices([]);setRefMeta(undefined);setEngineState('unavailable')}});return()=>{cancelled=true}},[companion.engine,companion.voicePath,refEndpointPayload])
 // Auto-host launcher: when the ref engine is selected but the service
 // is down, kick the backend ensureRefEngine (non-blocking spawn) once
 // and poll the voices probe until /docs answers or the budget runs out.
 const hostLaunching=refMeta?.host_state==='launching'
 useEffect(()=>{if(companion.engine!=='ref'||!refMeta||refMeta.server_online||!refMeta.host_script)return;let cancelled=false;void getTtsBridge().ensureRefEngine({refEndpoint:refEndpointPayload}).catch(()=>{});let tries=0;const poll=()=>{if(cancelled)return;tries++;getTtsBridge().voices({engine:'ref',refEndpoint:refEndpointPayload}).then(r=>{if(cancelled)return;if(r.ref_meta){setRefMeta(r.ref_meta);if(r.ref_meta.server_online){setStatus('语音引擎已就绪，50 种音色可直接试听。');return}}if(tries<40)setTimeout(poll,3000)}).catch(()=>{if(!cancelled&&tries<40)setTimeout(poll,3000)})};const timer=setTimeout(poll,3000);return()=>{cancelled=true;clearTimeout(timer)}},[companion.engine,refMeta?.server_online,refMeta?.host_script,refEndpointPayload])
 const save=(next:CompanionSettings)=>{setCompanion(next);saveCompanionSettings(next);setStatus('设置已保存，立即生效。')}
 const preview=async()=>{
  setBusy(true);setStatus('正在合成试听…')
  try{
   const voiceId=companion.voiceId&&(voices.some(v=>v.voice_id===companion.voiceId)?companion.voiceId:(setStatus('所选音色不可用，已回退默认音色。'),undefined))
   const result=await getTtsBridge().synthesize({text:'你好，我是月汐，很高兴与你同行。',voiceId,rate:companion.rate,volume:companion.volume,engine:companion.engine,refEndpoint:refEndpointPayload})
   if(result.discarded||!result.wav_base64){setStatus('试听已取消。');return}
   const bytes=Uint8Array.from(atob(result.wav_base64),c=>c.charCodeAt(0))
   const mime=bytes[0]===0xff||(bytes[0]===0x49&&bytes[1]===0x44&&bytes[2]===0x33)?'audio/mpeg':'audio/wav'
   const url=URL.createObjectURL(new Blob([bytes],{type:mime}))
   audioRef.current?.pause();const audio=new Audio(url);audioRef.current=audio;audio.onended=()=>URL.revokeObjectURL(url);await audio.play()
   setStatus(result.notice==='TTS_VOICE_NOT_FOUND'?'所选音色不可用，已回退默认音色（M95-004）。':'试听播放中…')
  }catch(e){setStatus(e instanceof Error?`试听失败：${e.message}`:'试听失败，请检查引擎配置')}finally{setBusy(false)}
 }
 const refDesc=refMeta?(refMeta.server_online?(refMeta.pack_exists?(refMeta.missing_files&&refMeta.missing_files.length>0?`音色 ${voices.length} 个，但 ${refMeta.missing_files.length} 个参考音频缺失（${refMeta.missing_files.slice(0,3).join('、')}${refMeta.missing_files.length>3?'…':''}），请检查音色包目录`:`音色 ${voices.length} 个（GPT-SoVITS 本地克隆，引擎在线，按风格分组）`):`引擎在线，但音色包目录缺失（${refMeta.pack_dir}），请检查音色文件`):refMeta.host_script?(hostLaunching?'语音引擎启动中…（首次加载模型约 30-90 秒，就绪后 50 种音色自动可用）':`检测到本机 GPT-SoVITS，服务未运行——已自动在后台启动语音引擎（首次加载模型约 30-90 秒）`):`未检测到 GPT-SoVITS 启动脚本（E:\GPT-SoVITS\start-api-cpu.bat）——请手动启动 api_v2 服务（默认端口 9880；WebUI 的 9874 端口不提供合成 API）`):'正在检测 GPT-SoVITS 服务…'
 const zhVoices=voices.filter(v=>(v.lang||'').toLowerCase()==='zh-cn')
 const zhMale=zhVoices.filter(v=>v.gender==='male').length
 const zhFemale=zhVoices.filter(v=>v.gender==='female').length
 const visibleVoices=companion.engine==='edge'&&!showForeignEdgeVoices?zhVoices:voices
 const engineDesc=companion.engine==='edge'?(engineState==='probing'?'正在获取微软云端音色…':engineState==='available'?`普通话音色 ${zhVoices.length} 种（男 ${zhMale} · 女 ${zhFemale}${showForeignEdgeVoices?` · 全部 ${voices.length}`:''}；微软 Edge Neural 风格扩展，免密钥，需联网）`:'无法连接微软云端语音（需联网）'):companion.engine==='natural'?(engineState==='probing'?'正在获取自然语音列表…':engineState==='available'?`自然语音音色 ${voices.length} 个（Windows OneCore 神经网络音色，本机离线合成），按角色风格分组`:'本机无自然语音（M95-001），月伴将自动切换字幕模式'):companion.engine==='ref'?(engineState==='probing'?'正在获取角色音色列表…':refDesc):engineState==='probing'?'正在检测本机语音合成引擎…':engineState==='available'?`本机可用音色 ${voices.length} 个（Windows SAPI 桌面语音，离线合成）`:'本机无语音合成引擎（M95-001），月伴将自动切换字幕模式'
 const voiceDisabled=engineState!=='available'
 // Voice grouping: ref-engine presets carry an explicit group from the
 // backend; SAPI/OneCore voices fall back to the gender/lang heuristic
 // (zh-CN splits by gender, other zh-* are dialects, rest is foreign).
 const voiceGroups=(()=>{const g=new Map<string,typeof visibleVoices>();for(const v of visibleVoices){const lang=v.lang||'';const grp=v.group||(lang==='zh-CN'?(v.gender==='male'?'男声 · 阳光少年 / 沉稳大叔':'女声 · 温柔 / 活泼 / 甜美'):lang.startsWith('zh-')?'方言 · 东北 / 陕西 / 粤语 / 台湾':'外语 · 英语 / 日语');const arr=g.get(grp)||[];arr.push(v);g.set(grp,arr)}return[...g.entries()]})()
 const shownPath=shownVoicePath(companion.voicePath)
 return <><div className="voice-path-heading">语音通道</div><VoicePathPicker value={companion.voicePath} onChange={next=>save(applyVoicePath(companion,next))}/><Toggle label="启用月伴对话" desc="在普通聊天输入框显示月亮按钮，进入全屏语音对话舞台；关闭即回滚入口。" on={companion.enabled} onChange={v=>save({...companion,enabled:v})}/><HotkeyRow label="打断快捷键" desc="月汐说话或思考时，按此快捷键立刻停止；舞台上也有「打断」按钮。Esc 仍用于退出月伴。" hotkey={companion.interruptHotkey} onChange={hotkey=>save({...companion,interruptHotkey:hotkey})}/><Toggle label="回复自动朗读" desc="边生成边朗读（流式字幕同步更新）；关闭后仅显示字幕。" on={companion.autoSpeak} onChange={v=>save({...companion,autoSpeak:v})}/><Toggle label="全双工对话" desc="她说完后立刻接着听下一句，不必重新点麦克风。她思考和说话的整个回合里麦克风都静音，所以外放回声、电视声或旁人说话都不会把她的回答截断；想插话请点舞台上的「打断」或使用快捷键。" on={companion.fullDuplex} onChange={v=>save({...companion,fullDuplex:v})}/><Toggle label="嘈杂环境模式" desc="提高麦克风能量门限、延长静音判定，减少旁人说话和背景声误触发。" on={companion.speechEnvironment==='noisy'} onChange={v=>save({...companion,speechEnvironment:v?'noisy':'normal'})}/><LocalAsrRow companion={companion} save={save}/>{shownPath==='local'&&<><VoicePersonaGrid caption="50 种人生已内置。音色走本机克隆引擎，点选即用。" value={companion.voiceId||companion.omniPersonaId} onChange={id=>save({...companion,voiceId:id,omniPersonaId:id})}/><div className="setting-row"><div><div className="setting-label">GPT-SoVITS 服务地址</div><div className="setting-desc">默认 http://127.0.0.1:9880。留空使用默认。</div></div><input className="setting-input" style={{flex:1,fontFamily:'var(--mono)',fontSize:12}} placeholder="http://127.0.0.1:9880" value={companion.refEndpoint} onChange={e=>save({...companion,refEndpoint:e.target.value.trim()})} aria-label="GPT-SoVITS 服务地址"/></div><div className="setting-row"><div><div className="setting-label">{engineDesc}</div></div><button disabled={busy||engineState!=='available'} onClick={()=>void preview()}>{busy?'合成中…':'试听'}</button></div></>}{shownPath==='cloud'&&<><Toggle label="显示外语音色" desc="默认只列出中文；开启后可浏览全部云端 Neural 音色。" on={showForeignEdgeVoices} onChange={setShowForeignEdgeVoices}/><div className="setting-row"><div><div className="setting-label">朗读音色</div><div className="setting-desc">{engineDesc}</div></div><div style={{display:'flex',gap:8,alignItems:'center'}}><select className="setting-select" aria-label="朗读音色" disabled={voiceDisabled} value={companion.voiceId} onChange={e=>save({...companion,voiceId:e.target.value})}><option value="">默认音色</option>{voiceGroups.map(([grp,items])=><optgroup key={grp} label={grp}>{items.map(voice=><option key={voice.voice_id} value={voice.voice_id}>{voice.display_name}</option>)}</optgroup>)}</select><button disabled={busy||engineState!=='available'} onClick={()=>void preview()}>{busy?'合成中…':'试听'}</button></div></div><div className="setting-row"><div><div className="setting-label">语速</div><div className="setting-desc">当前 {companion.rate}</div></div><input type="range" min={-10} max={10} step={1} value={companion.rate} aria-label="朗读语速" onChange={e=>save({...companion,rate:Number(e.target.value)})} style={{accentColor:'var(--tide1)'}}/></div><div className="setting-row"><div><div className="setting-label">音量</div><div className="setting-desc">当前 {companion.volume}</div></div><input type="range" min={0} max={100} step={1} value={companion.volume} aria-label="朗读音量" onChange={e=>save({...companion,volume:Number(e.target.value)})} style={{accentColor:'var(--tide1)'}}/></div></>}{status&&<p role="status" className="notice">{status}</p>}</>
}

function AboutPanel(): React.JSX.Element {
  const zh = useZh()
  const [version, setVersion] = useState('')
  useEffect(() => {
    getSystemHealthBridge().health().then(result => setVersion(result.version)).catch(() => setVersion(''))
  }, [])
  return (
    <div className="setting-group">
      <div className="setting-group-title">{zh ? '关于月汐' : 'About Lunitide'}</div>
      <div className="about-content">
        <div className="about-logo">
          <div className="moon-logo" aria-hidden="true" />
          <div>
            <h3 style={{ margin: 0, fontFamily: 'var(--serif)', fontSize: '22px' }}>{zh ? '月汐' : 'Lunitide'}</h3>
            <p style={{ margin: '4px 0 0', color: 'var(--muted)', fontSize: '13px' }}>{zh ? '本地优先 · BYOK · AI 软件生命周期工作台' : 'Local-first · BYOK · AI software lifecycle workbench'}</p>
          </div>
        </div>
        <dl className="about-info">
          <div><dt>{zh ? '版本' : 'Version'}</dt><dd>{version || '—'}</dd></div>
          <div><dt>{zh ? '作者' : 'Author'}</dt><dd>Yy.MJ</dd></div>
        </dl>
        <div className="about-links">
          <span>{zh ? '产品定位：不只是一个“更好的界面”，而是一个理解项目语义、记得历史、可扩展、能规划、且有治理边界的 AI 开发伙伴。' : 'A local-first AI workbench that keeps project context, history, and governance boundaries — not just a prettier chat box.'}</span>
        </div>
      </div>
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
      const added = await bridge.add({ origin: 'manual', transport: 'stdio', command: preset.command, args: resolveArgs(preset, value ?? ''), riskConfirmed: true, requestId: crypto.randomUUID() })
      try { await bridge.toggle({ endpointId: added.endpointId, enabled: true }) } catch { /* still registered */ }
      setArgDraft(null)
      setStatus(`已启用 ${preset.name}，对话里可直接调用其工具。`)
    } catch (e) { setStatus(e instanceof Error ? e.message : `${preset.name} 注册失败`) } finally { setBusy(false) }
  }

  const kitIds = new Set(['memory', 'sequentialthinking'])
  const enableKit = async () => {
    const kit = items.filter(p => kitIds.has(p.id) && !p.needsArgs)
    if (!kit.length) return
    setBusy(true); setStatus('')
    try {
      for (const preset of kit) {
        const added = await bridge.add({ origin: 'manual', transport: 'stdio', command: preset.command, args: preset.args, riskConfirmed: true, requestId: crypto.randomUUID() })
        try { await bridge.toggle({ endpointId: added.endpointId, enabled: true }) } catch { /* still registered */ }
      }
      setStatus('已启用推荐套件（记忆 + 结构化推理），对话可直接调用。')
    } catch (e) { setStatus(e instanceof Error ? e.message : '推荐套件启用失败') } finally { setBusy(false) }
  }

  return (
    <div className="setting-row" style={{ gridTemplateColumns: '1fr' }}>
      <div className="setting-group-title" style={{ marginTop: 8 }}>预置服务器（免费直连）</div>
      <div className="setting-desc">官方 / 微软开源 MCP，npx 一键拉起，无需 API Key。注册后会自动启用并进入对话工具表。推荐套件不含要登录的 GitHub。</div>
      <div style={{ margin: '8px 0' }}>
        <button disabled={busy || items.length === 0} onClick={() => void enableKit()}>一键启用推荐套件</button>
      </div>
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
      const [s, sess, perms, modes] = await Promise.all([
        bridge.getSettings(),
        bridge.listSessions(),
        bridge.listPermissions({ state: 'pending' }),
        bridge.detectModes().catch(() => null),
      ])
      setSettings(s)
      setChromePathDraft(s.chromePath)
      setEdgePathDraft(s.edgePath)
      setPortDraft(String(s.extensionPort))
      setRetentionDraft(String(s.dataRetentionDays))
      setSessions(sess.sessions)
      setPending(perms.permissions)
      if (modes) setDetect(modes)
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
      <div className="setting-group-title">就绪检查</div>
      <div className="setting-row" style={{ gridTemplateColumns: '1fr' }}>
        <div>
          <div className="setting-label">本机浏览器自动化</div>
          <div className="setting-desc">对话里的填表/点击走托管 Playwright（首次自动安装）。操作前 snapshot，动作后会带回新页面树。登录墙、验证码、文件选择请你本地完成，月汐不会代点。个人 Chrome/Edge 仅在你选择对应连接模式时使用，不会拷贝 Cookie。</div>
        </div>
        <div style={{ display: 'flex', gap: 8, flexWrap: 'wrap', marginTop: 8 }}>
          <button disabled={busy} onClick={() => void detectModes()}>重新探测</button>
          <button disabled={busy} onClick={() => void connect()}>新建会话（当前模式）</button>
        </div>
        {detect && (
          <p className="setting-desc" role="status" style={{ marginTop: 8 }}>
            内置 WebView2 {detect.builtin ? '可用' : '不可用'} · Chrome {detect.chrome.available ? '可用' : '未检测到'} · Edge {detect.edge.available ? '可用' : '未检测到'} · 扩展桥 {detect.extension.available ? '可用' : '未检测到'}（端口 {detect.extension.port}）
          </p>
        )}
      </div>
      <div className="setting-group-title">浏览器多模式</div>
      <div className="setting-row" style={{ gridTemplateColumns: '1fr' }}>
        <div className="setting-desc">五种连接模式共用一套导航白名单与私网拦截策略；切换模式会断开当前活动会话。当前 {sessions.filter(s => s.state === 'connected').length}/{sessions.length} 个会话在线。</div>
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

// M10 wave-4 — 电脑控制设置：三步启用流（风险告知 → 安全级别 → 确认）、
// 安全级别 / 高危操作 / 进程黑名单（操作范围）/ 频率与确认超时、
// 紧急停止闩锁与操作审计台账（CSV 导出）。
type CcAuditRow = CcGetAuditLogResult['items'][number]
const CC_RISK_LABELS: Record<string, string> = { low: '低', medium: '中', high: '高', critical: '极高' }
const CC_STATUS_LABELS: Record<string, string> = { executed: '已执行', blocked: '已拦截', denied: '已拒绝', failed: '失败', stopped: '已停止' }
const CC_LAYER_LABELS: Record<string, string> = { intent: '意图识别', 'input-filter': '输入过滤', 'process-monitor': '进程监控' }
const CC_TOOL_LABELS: Record<string, string> = {
  'cc.mouse_move': '移动鼠标', 'cc.mouse_click': '点击鼠标', 'cc.mouse_drag': '拖拽',
  'cc.keyboard_type': '键盘输入',
  'cc.keyboard_shortcut': '组合键', 'cc.screen_capture': '全桌面截图', 'cc.get_active_window': '活动窗口',
  'cc.window_list': '窗口列表', 'cc.window_focus': '聚焦窗口',
  'cc.observe_dialog': '观察对话框', 'cc.confirm_dialog': '确认对话框',
  'cc.observe_ui': '界面节点', 'cc.wait': '等待界面', 'cc.clipboard': '剪贴板',
  'cc.window_action': '窗口操作', 'cc.app_list': '应用列表', 'cc.app_quit': '退出应用',
  'cc.paste': '粘贴', 'cc.press': '按键', 'cc.menu_click': '菜单', 'cc.set_value': '填写控件',
}
const CC_LEVEL_META: Record<'standard' | 'strict', { label: string; desc: string }> = {
  standard: { label: '标准', desc: '高危操作（组合键等）需人工确认；极高风险默认拦截' },
  strict: { label: '严格', desc: '仅允许低/中风险操作；高危一律拦截，适合首次试用' },
}

export function ComputerPanel({ bridge = ccBridge }: { bridge?: CcBridge }): React.JSX.Element {
  const [settings, setSettings] = useState<CcGetConfigResult | null>(null)
  const [entries, setEntries] = useState<CcAuditRow[]>([])
  const [statusFilter, setStatusFilter] = useState('')
  const [status, setStatus] = useState('')
  const [busy, setBusy] = useState(false)
  // 三步启用向导：1 风险告知 → 2 安全级别 → 3 确认启用。
  const [wizard, setWizard] = useState<{ step: 1 | 2; agreed: boolean; level: 'standard' | 'strict'; allowCritical: boolean } | null>(null)
  const [blockDraft, setBlockDraft] = useState('')
  const [rateDraft, setRateDraft] = useState('')
  const [timeoutDraft, setTimeoutDraft] = useState('')

  const refresh = async (filter = statusFilter) => {
    setBusy(true)
    try {
      const [cfg, log] = await Promise.all([
        bridge.getConfig(),
        bridge.getAuditLog({ limit: 50, ...(filter ? { status: filter as CcAuditRow['status'] } : {}) }),
      ])
      setSettings(cfg)
      setRateDraft(String(cfg.maxActionsPerMinute))
      setTimeoutDraft(String(cfg.confirmTimeoutSeconds))
      setEntries(log.items)
      setStatus('')
    } catch (e) {
      setStatus(e instanceof Error ? e.message : '电脑控制配置加载失败')
    } finally { setBusy(false) }
  }
  useEffect(() => { void refresh() }, [])

  const patch = async (p: CcUpdateConfigPayload, okMsg: string) => {
    setBusy(true); setStatus('')
    try {
      const cfg = await bridge.updateConfig(p)
      setSettings(cfg)
      setStatus(okMsg)
    } catch (e) { setStatus(e instanceof Error ? e.message : '设置更新失败') } finally { setBusy(false) }
  }

  const confirmEnable = async () => {
    if (!wizard) return
    setBusy(true); setStatus('')
    try {
      const cfg = await bridge.updateConfig({ enabled: true, securityLevel: wizard.level, allowCritical: wizard.allowCritical })
      setSettings(cfg)
      setWizard(null)
      setStatus('电脑控制已启用')
    } catch (e) { setStatus(e instanceof Error ? e.message : '启用失败') } finally { setBusy(false) }
  }

  const emergencyStop = async () => {
    if (!window.confirm('确认紧急停止？所有电脑控制操作将立即失效，直到重新走启用流程。')) return
    setBusy(true); setStatus('')
    try {
      const cfg = await bridge.emergencyStop({ actor: 'renderer', reason: '设置页手动急停' })
      setSettings(cfg)
      setStatus('紧急停止已激活')
    } catch (e) { setStatus(e instanceof Error ? e.message : '紧急停止失败') } finally { setBusy(false) }
  }

  const addBlock = async () => {
    if (!settings) return
    const entry = blockDraft.trim().toLowerCase()
    if (!entry) return
    if (entry.includes('/') || entry.includes('\\')) { setStatus('黑名单条目为进程名（如 cmd.exe），不能含路径分隔符'); return }
    if (settings.processBlocklist.includes(entry)) { setStatus('条目已存在'); return }
    await patch({ processBlocklist: [...settings.processBlocklist, entry] }, `已添加 ${entry}`)
    setBlockDraft('')
  }
  const removeBlock = async (entry: string) => {
    if (!settings) return
    await patch({ processBlocklist: settings.processBlocklist.filter(x => x !== entry) }, `已移除 ${entry}`)
  }

  const exportCsv = () => {
    const header = '时间,会话,工具,动作,风险,状态,拦截层,详情'
    const esc = (v: string) => `"${v.replace(/"/g, '""')}"`
    const lines = entries.map(e => [
      e.createdAt, e.sessionId, CC_TOOL_LABELS[e.tool] ?? e.tool, e.action,
      CC_RISK_LABELS[e.riskLevel] ?? e.riskLevel, CC_STATUS_LABELS[e.status] ?? e.status,
      e.layer ? CC_LAYER_LABELS[e.layer] ?? e.layer : '', e.detail,
    ].map(esc).join(','))
    const blob = new Blob(['\ufeff' + [header, ...lines].join('\r\n')], { type: 'text/csv;charset=utf-8' })
    const url = URL.createObjectURL(blob)
    const a = document.createElement('a')
    a.href = url
    a.download = `cc-audit-${new Date().toISOString().slice(0, 10)}.csv`
    a.click()
    URL.revokeObjectURL(url)
  }

  return (
    <div className="setting-group">
      <div className="setting-group-title">电脑控制</div>
      <div className="setting-row" style={{ gridTemplateColumns: '1fr' }}>
          <div className="setting-desc">允许模型操作本机：截图 / 界面节点 / 对话框 / 窗口列表与聚焦 / 鼠标（点击、拖拽、滚轮）/ 键盘（对指定应用先聚焦再输入）/ 剪贴板（纯文本）。点击坐标与截图像素一致；点按钮优先用界面节点或对话框观察，不要盲点。所有操作经过意图识别、输入过滤、进程监控三层拦截，并写入不可篡改的审计台账。紧急停止后月伴不会自动重新打开。</div>
        {settings && (
          <div className="setting-label" style={{ display: 'flex', gap: 10, alignItems: 'center' }}>
            <span>当前状态：</span>
            <span style={{ color: settings.emergencyStopped ? 'var(--red)' : settings.enabled ? 'var(--ok)' : 'var(--muted)', fontWeight: 600 }}>
              {settings.emergencyStopped ? '紧急停止' : settings.enabled ? '已启用' : '已停用'}
            </span>
          </div>
        )}
        {settings?.emergencyStopped && (
          <p role="alert" className="notice" style={{ color: 'var(--red)' }}>紧急停止已激活（{settings.emergencyStoppedAt ? new Date(settings.emergencyStoppedAt).toLocaleString() : ''}）：所有电脑控制操作一律拒绝，需重新走启用流程。</p>
        )}
        <div style={{ display: 'flex', gap: 8, flexWrap: 'wrap' }}>
          {settings && !settings.enabled && !wizard && <button disabled={busy} onClick={() => setWizard({ step: 1, agreed: false, level: 'standard', allowCritical: false })}>三步启用…</button>}
          {settings?.enabled && <button disabled={busy} onClick={() => void patch({ enabled: false }, '电脑控制已停用')}>停用</button>}
          {settings?.enabled && !settings.emergencyStopped && <button disabled={busy} onClick={() => void emergencyStop()} style={{ color: 'var(--red)' }}>紧急停止</button>}
        </div>
      </div>

      {wizard && (
        <div className="setting-row" style={{ gridTemplateColumns: '1fr', border: '1px solid var(--rule)', borderRadius: 8, padding: 12 }}>
          <div className="setting-group-title">启用电脑控制（步骤 {wizard.step}/2）</div>
          {wizard.step === 1 && (
            <>
              <div className="setting-desc">电脑控制会授予模型操作本机输入的能力。请确认：</div>
              <ul className="setting-desc" style={{ margin: '0 0 0 18px', display: 'grid', gap: 4 }}>
                <li>高危操作（组合键、关闭窗口等）默认需要人工逐次确认；</li>
                <li>cmd / powershell / regedit 等系统进程默认在黑名单中，不会被操作；</li>
                <li>随时可在会话中或本页执行「紧急停止」，立即熔断全部操作。</li>
              </ul>
              <label style={{ display: 'flex', gap: 8, alignItems: 'center', fontSize: 13 }}>
                <input type="checkbox" checked={wizard.agreed} onChange={ev => setWizard({ ...wizard, agreed: ev.target.checked })} aria-label="已了解风险" />
                我已了解上述风险与防护机制
              </label>
              <div style={{ display: 'flex', gap: 8 }}>
                <button disabled={!wizard.agreed} onClick={() => setWizard({ ...wizard, step: 2 })}>下一步：选择安全级别</button>
                <button onClick={() => setWizard(null)}>取消</button>
              </div>
            </>
          )}
          {wizard.step === 2 && (
            <>
              <div className="br-mode-grid" role="radiogroup" aria-label="电脑控制安全级别">
                {(['standard', 'strict'] as const).map(level => (
                  <button key={level} type="button" role="radio" aria-checked={wizard.level === level}
                    className={`br-mode-card ${wizard.level === level ? 'on' : ''}`}
                    onClick={() => setWizard({ ...wizard, level })}>
                    <b>{CC_LEVEL_META[level].label}</b>
                    <small>{CC_LEVEL_META[level].desc}</small>
                  </button>
                ))}
              </div>
              <label style={{ display: 'flex', gap: 8, alignItems: 'center', fontSize: 13 }}>
                <input type="checkbox" checked={wizard.allowCritical} onChange={ev => setWizard({ ...wizard, allowCritical: ev.target.checked })} aria-label="允许极高风险操作" />
                允许极高风险操作（Alt+F4 / Win+R 等，仍需逐次人工确认；不建议开启）
              </label>
              <div style={{ display: 'flex', gap: 8 }}>
                <button disabled={busy} onClick={() => void confirmEnable()}>确认启用</button>
                <button disabled={busy} onClick={() => setWizard({ ...wizard, step: 1 })}>上一步</button>
                <button disabled={busy} onClick={() => setWizard(null)}>取消</button>
              </div>
            </>
          )}
        </div>
      )}

      {settings?.enabled && (
        <>
          <div className="setting-row" style={{ gridTemplateColumns: '1fr' }}>
            <div className="setting-group-title" style={{ marginTop: 8 }}>安全级别</div>
            <div className="br-mode-grid" role="radiogroup" aria-label="电脑控制安全级别">
              {(['standard', 'strict'] as const).map(level => (
                <button key={level} type="button" role="radio" aria-checked={settings.securityLevel === level}
                  className={`br-mode-card ${settings.securityLevel === level ? 'on' : ''}`}
                  onClick={() => void patch({ securityLevel: level }, `安全级别已切换为 ${CC_LEVEL_META[level].label}`)}>
                  <b>{CC_LEVEL_META[level].label}</b>
                  <small>{CC_LEVEL_META[level].desc}</small>
                </button>
              ))}
            </div>
            <Toggle on={settings.allowCritical} onChange={v => void patch({ allowCritical: v }, v ? '已允许极高风险操作（仍需逐次确认）' : '已禁止极高风险操作')} label="允许极高风险操作" desc="Alt+F4 / Win+R / Win+L 等系统级组合键；开启后每次执行仍需人工确认" />
          </div>

          <div className="setting-row" style={{ gridTemplateColumns: '1fr' }}>
            <div className="setting-group-title" style={{ marginTop: 8 }}>操作范围{settings.processBlocklist.length > 0 ? `（黑名单 ${settings.processBlocklist.length}/128）` : ''}</div>
            <div className="setting-desc">前台进程命中黑名单时，鼠标 / 键盘 / 截图操作一律拦截（M10-CC-009）。条目为进程名，不区分大小写。</div>
            <div style={{ display: 'flex', gap: 8, maxWidth: 560 }}>
              <input className="setting-input" style={{ flex: 1, fontFamily: 'var(--mono)', fontSize: 12 }} placeholder="例如 taskmgr.exe" value={blockDraft} onChange={ev => setBlockDraft(ev.target.value)} aria-label="黑名单新条目" onKeyDown={ev => { if (ev.key === 'Enter') { ev.preventDefault(); void addBlock() } }} />
              <button disabled={busy || !blockDraft.trim()} onClick={() => void addBlock()}>添加</button>
            </div>
            <div style={{ display: 'flex', gap: 8, flexWrap: 'wrap', maxWidth: 640 }}>
              {settings.processBlocklist.map(entry => (
                <code key={entry} style={{ display: 'inline-flex', gap: 6, alignItems: 'center', border: '1px solid var(--rule)', borderRadius: 6, padding: '2px 8px', fontFamily: 'var(--mono)', fontSize: 12 }}>
                  {entry}
                  <button disabled={busy} style={{ border: 'none', background: 'none', cursor: 'pointer', padding: 0, color: 'var(--muted)' }} aria-label={`移除 ${entry}`} onClick={() => void removeBlock(entry)}>✕</button>
                </code>
              ))}
            </div>
          </div>

          <div className="setting-row" style={{ gridTemplateColumns: '1fr' }}>
            <div className="setting-group-title" style={{ marginTop: 8 }}>频率与确认</div>
            <label style={{ display: 'grid', gap: 4, fontSize: 12, color: 'var(--muted)', maxWidth: 560 }}>每分钟最大操作数（1–120，默认 30）
              <div style={{ display: 'flex', gap: 8 }}>
                <input className="setting-input" style={{ width: 120, fontFamily: 'var(--mono)' }} inputMode="numeric" value={rateDraft} onChange={ev => setRateDraft(ev.target.value.replace(/\D/g, ''))} aria-label="每分钟最大操作数" />
                <button disabled={busy || rateDraft === '' || Number(rateDraft) === settings.maxActionsPerMinute} onClick={() => void patch({ maxActionsPerMinute: Number(rateDraft) }, '频率上限已保存')}>保存</button>
              </div>
            </label>
            <label style={{ display: 'grid', gap: 4, fontSize: 12, color: 'var(--muted)', maxWidth: 560 }}>高危确认超时（秒，10–600，默认 60）
              <div style={{ display: 'flex', gap: 8 }}>
                <input className="setting-input" style={{ width: 120, fontFamily: 'var(--mono)' }} inputMode="numeric" value={timeoutDraft} onChange={ev => setTimeoutDraft(ev.target.value.replace(/\D/g, ''))} aria-label="高危确认超时秒数" />
                <button disabled={busy || timeoutDraft === '' || Number(timeoutDraft) === settings.confirmTimeoutSeconds} onClick={() => void patch({ confirmTimeoutSeconds: Number(timeoutDraft) }, '确认超时已保存')}>保存</button>
              </div>
            </label>
          </div>
        </>
      )}

      <div className="setting-row" style={{ gridTemplateColumns: '1fr' }}>
        <div className="setting-group-title" style={{ marginTop: 8 }}>操作审计（最近 {entries.length} 条）</div>
        <div style={{ display: 'flex', gap: 8, flexWrap: 'wrap', alignItems: 'center' }}>
          <select className="setting-input" style={{ width: 160 }} value={statusFilter} onChange={ev => { setStatusFilter(ev.target.value); void refresh(ev.target.value) }} aria-label="状态筛选">
            <option value="">全部状态</option>
            <option value="executed">已执行</option>
            <option value="blocked">已拦截</option>
            <option value="denied">已拒绝</option>
            <option value="failed">失败</option>
            <option value="stopped">已停止</option>
          </select>
          <button disabled={busy} onClick={() => void refresh()}>刷新</button>
          <button disabled={busy || entries.length === 0} onClick={exportCsv}>导出 CSV</button>
        </div>
        {entries.length === 0 && <div className="setting-desc">暂无操作记录。</div>}
        {entries.length > 0 && (
          <div style={{ overflowX: 'auto' }}>
            <table style={{ width: '100%', borderCollapse: 'collapse', fontSize: 12 }}>
              <thead>
                <tr style={{ textAlign: 'left', color: 'var(--muted)' }}>
                  <th style={{ padding: '6px 10px 6px 0' }}>时间</th>
                  <th style={{ padding: '6px 10px 6px 0' }}>工具</th>
                  <th style={{ padding: '6px 10px 6px 0' }}>风险</th>
                  <th style={{ padding: '6px 10px 6px 0' }}>状态</th>
                  <th style={{ padding: '6px 10px 6px 0' }}>拦截层</th>
                  <th style={{ padding: '6px 10px 6px 0' }}>详情</th>
                </tr>
              </thead>
              <tbody>
                {entries.map(e => (
                  <tr key={e.entryId} style={{ borderTop: '1px solid var(--rule)' }}>
                    <td style={{ padding: '6px 10px 6px 0', whiteSpace: 'nowrap', fontFamily: 'var(--mono)' }}>{new Date(e.createdAt).toLocaleString()}</td>
                    <td style={{ padding: '6px 10px 6px 0' }}>{CC_TOOL_LABELS[e.tool] ?? e.tool}</td>
                    <td style={{ padding: '6px 10px 6px 0' }}>{CC_RISK_LABELS[e.riskLevel] ?? e.riskLevel}</td>
                    <td style={{ padding: '6px 10px 6px 0', color: e.status === 'executed' ? 'var(--ok)' : e.status === 'blocked' || e.status === 'stopped' ? 'var(--red)' : 'var(--muted)' }}>{CC_STATUS_LABELS[e.status] ?? e.status}</td>
                    <td style={{ padding: '6px 10px 6px 0' }}>{e.layer ? CC_LAYER_LABELS[e.layer] ?? e.layer : '—'}</td>
                    <td style={{ padding: '6px 0', overflowWrap: 'anywhere', fontFamily: 'var(--mono)', fontSize: 11, color: 'var(--muted)' }}>{e.detail}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
      </div>
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
  const [fullAccess, setFullAccess] = useState(false)
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
        setFullAccess(r.fullAccess ?? false)
        setLoaded(true)
      } catch (e) { setStatus(e instanceof Error ? e.message : '命令白名单读取失败') } finally { setBusy(false) }
    }
    void load()
  }, [])

  const toggleFullAccess = async (on: boolean) => {
    setFullAccess(on); setStatus('')
    try {
      const commands = entries.map(e => {
        const prefix = parsePrefix(e.prefix)
        const doc: { prefix: string[]; maxArgs?: number; timeoutMs?: number } = { prefix }
        if (e.maxArgs > 0) doc.maxArgs = e.maxArgs
        if (e.timeoutMs > 0) doc.timeoutMs = e.timeoutMs
        return doc
      }).filter(c => c.prefix.length > 0)
      await bridge.setCommandPolicy({ commands, fullAccess: on })
      setStatus(on ? '全盘完全访问已开启并热生效。' : '全盘完全访问已关闭并热生效。')
    } catch (e) {
      setFullAccess(!on)
      setStatus(e instanceof Error ? e.message : '全盘访问开关保存失败（现运行规则不变）')
    }
  }

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
      const r = await bridge.setCommandPolicy(fullAccess ? { commands, fullAccess: true } : { commands })
      setStatus(`已保存并热生效：${r.applied} 条用户规则（叠加内置 git/go 只读集）。`)
      setEntries(commands.map(c => ({ prefix: formatPrefix(c.prefix), maxArgs: c.maxArgs ?? 0, timeoutMs: c.timeoutMs ?? 10_000 })))
    } catch (e) { setStatus(e instanceof Error ? e.message : '命令白名单保存失败（文档被整体拒绝，现运行规则不变）') } finally { setBusy(false) }
  }

  return (
    <div className="setting-group">
      <div className="setting-group-title">命令白名单（command.run）</div>
      <div className="setting-row" style={{ gridTemplateColumns: '1fr auto', alignItems: 'center', background: fullAccess ? 'rgba(194,65,12,.08)' : undefined, borderRadius: 8, padding: '4px 12px' }}>
        <div>
          <div className="setting-label" style={{ color: fullAccess ? 'var(--warn, #c2410c)' : undefined }}>全盘完全访问</div>
          <div className="setting-desc">开启后：聊天选择「完全访问」模式时，AI 可执行任意命令并读写所有盘符的任意路径（含桌面、文档、其他硬盘）。审批/自动编辑模式仍走白名单；子代理始终受限。<strong>请仅在信任模型输出时开启，风险自负。</strong></div>
        </div>
        <Toggle on={fullAccess} onChange={v => void toggleFullAccess(v)} label="全盘完全访问" />
      </div>
      <div className="setting-row" style={{ gridTemplateColumns: '1fr' }}>
        <div className="setting-desc">聊天中 command.run 仅允许白名单内的只读命令（完全访问 + 上方开关开启时除外）。内置 git/go 只读集恒生效；下方为用户附加规则，保存即校验并热生效，非法文档整体拒绝（fail-closed）。超时范围 1s–300s，argv 总长上限 16。</div>
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
const HOOK_TOOLS = ['workspace.list', 'workspace.read', 'workspace.write', 'workspace.search', 'workspace.edit', 'todo.write', 'command.run', 'web.fetch', 'web.search', 'excel.gen', 'excel.parse', 'docx.gen', 'pptx.gen', 'pdf.gen', 'html.gen', 'desktop.open'] as const
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
      const health = await getSystemHealthBridge().health().catch(() => undefined)
      const r = await bridge.check({ channel, currentVersion: health?.version || '0.0.0' })
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
  const projectName = projects.find(item => item.id === projectId)?.name
  return (
    <div className="gov-scope">
      <header className="gov-scope-head">
        <div>
          <div className="setting-group-title">项目治理</div>
          <p className="setting-desc">{projectName ? `当前项目：${projectName}` : '审批与计划按项目查看。'}</p>
        </div>
        <div className="gov-scope-controls">
          <label className="gov-project-select">项目
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
      </header>
      {error ? <p className="notice" role="alert">{error}</p> : !projectId ? <p className="notice">还没有可用项目；请先在“项目管理”中创建。</p> : <div className="gov-scope-body">{active?.render(projectId)}</div>}
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
