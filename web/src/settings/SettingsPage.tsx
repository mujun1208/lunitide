import React, { useEffect, useRef, useState } from 'react'
import{getAppUpdateBridge,getCollabGateBridge,getDiagnosticsBridge,getMcpBridge,getProviderBridge,getSystemHealthBridge,getTtsBridge,projectBridge,systemSettingsBridge,toolsPolicyBridge,conversationsBridge,type McpBridge,type ProviderBridge,type ToolsPolicyBridge,type TtsVoice,type TtsRefMeta}from'../bridge/client'
import type{Mcp6PresetsListResult,ProjectDTO}from'../generated/bridge'
import{microphoneConstraints,saveMicrophoneId,selectedMicrophoneId}from'./microphone'
import{ChoiceTiles}from'./ChoiceTiles'
import{VoicePathPicker}from'./VoicePathPicker'
import{VoicePersonaGrid}from'./VoicePersonaGrid'
import{filterSettingsNav,SETTINGS_NAV_GROUPS,SETTINGS_CATEGORIES,type SettingsCategory}from'./settingsNav'
import{MeetingNotesPanel}from'./MeetingNotesPanel'
import{REPLY_STYLE_OPTIONS,STRUCTURED_TEMPLATE_OPTIONS}from'./replySettings'
import{applyVoicePath,defaultCompanionSettings,formatInterruptHotkey,interruptHotkeyFromEvent,loadCompanionSettings,saveCompanionSettings,type CompanionSettings,type InterruptHotkey}from'../session/companion/companionSettings'
import{shownVoicePath,type VoicePersona}from'../session/companion/voicePersonas'
import{hasConfiguredVolcTts,pickTalkRealtimeModel}from'../provider/modelKind'
import{LocalAsrRow}from'./LocalAsrRow'
import{AsrCorrectionRow}from'./AsrCorrectionRow'
import{SubagentsPanel}from'./SubagentsPanel'
import{ProviderApp}from'../provider/ProviderApp'
import{PlanPage}from'../plan/PlanPage'
import{ReviewPage}from'../review/ReviewPage'
import{PersonalIntelligencePage}from'../m8/PersonalIntelligencePage'
import{ProfilePanel}from'./ProfilePanel'
import{useZh}from'../i18n/language'
import{leftoverArchivedNames}from'./leftoverMcp'
import { refEngineCaption, refLaunchPollStatus, refPreviewButtonLabel, refPreviewReady, refPreviewStatus } from './refEnginePreview'
import { Toggle, HotkeyRow, SelectRow } from './settingsControls'
import { BrowserPanel } from './BrowserPanel'
import { ComputerPanel } from './ComputerPanel'
import { ChannelsPanel } from './ChannelsPanel'
import { HooksPanel } from './HooksPanel'
export { BrowserPanel, ComputerPanel, ChannelsPanel, HooksPanel }


interface GeneralSettings {
  startupPage: 'new' | 'last' | 'projects'
  restoreUnfinished: boolean
  recentProjectCount: 5 | 8 | 10
  language: 'zh-CN' | 'en'
  timezone: string
  enterToSend: boolean
  autoTitle: boolean
  defaultMode: 'approval' | 'auto-edit' | 'full-access'
  toolProfile: '' | 'minimal' | 'coding' | 'colleague'
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
  toolProfile: '',
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

export function SettingsPage({ onNavigateExpert, onNavigateMcp, onBack, backLabel, initialCategory = 'general', providers, onPreferLLM }: { onNavigateExpert?: () => void; onNavigateMcp?: () => void; onBack?: () => void; backLabel?: string; initialCategory?: SettingsCategory; providers?: ProviderBridge; onPreferLLM?: (providerId: string, modelId: string) => void }): React.JSX.Element {
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
        <button className="settings-back" onClick={onBack}>← {backLabel ?? (zh ? '返回主页' : 'Home')}</button>
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
          {category === 'providers' && (providers ? <ProviderApp bridge={providers} embedded onPreferLLM={onPreferLLM} /> : <p className="setting-desc">供应商列表需要 Host 桥接。</p>)}
          {category === 'voice' && <VoicePanel />}
          {category === 'meetings' && <MeetingNotesPanel onSaved={() => setSaved(true)} />}
          {category === 'personal' && <PersonalIntelligencePage onNavigateExpert={onNavigateExpert} />}
          {category === 'security' && <div className="governance-stack">
            <div className="setting-group">
              <div className="setting-group-title">编码与权限</div>
              <div className="setting-row" style={{ gridTemplateColumns: '1fr' }}>
                <div className="setting-desc">命令白名单约束 command.run。Git 默认只读（写操作需确认）。工作区 AGENTS.md / .agents/skills 叠在月汐身份上，不替换身份。技能在技能中心，MCP 在 MCP 页，能力包单独入口。</div>
              </div>
            </div>
            <CommandPolicyPanel />
            <McpPresetsSection onOpenMcp={onNavigateMcp} />
            <HooksPanel />
            <ProjectScopedTabs tabs={[{ id: 'review', label: '审批', render: pid => <ReviewPage projectId={pid} embedded /> }, { id: 'plans', label: '计划', render: pid => <PlanPage projectId={pid} /> }]} />
          </div>}
          {category === 'browser' && <BrowserPanel />}
          {category === 'computer' && <ComputerPanel />}
          {category === 'channels' && <ChannelsPanel />}
          {category === 'subagents' && <SubagentsPanel onSaved={() => setSaved(true)} />}
          {category === 'collab' && <CollabGatePanel />}
          {category === 'diagnostics' && <DiagnosticsPanel />}
          {category === 'about' && <AboutPanel />}
        </div>
      </div>
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
          desc="新会话的默认审批模式；单个会话仍可临时切换。"
          value={settings.defaultMode}
          options={[
            { value: 'approval', label: '手动审批' },
            { value: 'auto-edit', label: '自动审批' },
            { value: 'full-access', label: '完全访问' },
          ]}
          onChange={v => onChange('defaultMode', v as GeneralSettings['defaultMode'])}
        />
        <SelectRow
          label="工具剖面"
          desc="默认保持现有全部工具。精简=检索与记忆；编程=工作区与命令；同事=专家工具集（不含电脑控制）。"
          value={settings.toolProfile || 'default'}
          options={[
            { value: 'default', label: '默认（当前工具）' },
            { value: 'minimal', label: '精简' },
            { value: 'coding', label: '编程' },
            { value: 'colleague', label: '同事' },
          ]}
          onChange={v => onChange('toolProfile', (v === 'default' ? '' : v) as GeneralSettings['toolProfile'])}
        />
        <div className="setting-desc">关窗口 ≠ 退出助手。助手在托盘继续运行；从托盘退出才会停工作台。引擎可在关掉窗口后继续接回。</div>
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
 const[volcTtsReady,setVolcTtsReady]=useState(false)
 const[talkReady,setTalkReady]=useState(false)
 const audioRef=useRef<HTMLAudioElement|undefined>(undefined)
 useEffect(()=>{setCompanion(loadCompanionSettings());return()=>{audioRef.current?.pause();audioRef.current=undefined}},[])
 useEffect(()=>{let cancelled=false;getProviderBridge().list().then(r=>{if(!cancelled){const items=r.items??[];setVolcTtsReady(hasConfiguredVolcTts(items));setTalkReady(!!pickTalkRealtimeModel(items))}}).catch(()=>{if(!cancelled){setVolcTtsReady(false);setTalkReady(false)}});return()=>{cancelled=true}},[])
 // Voice catalogue probes the machine for both engines (M95-001
 // degradation); natural leads with the OneCore neural pool. The ref
 // engine returns the built-in 18-voice character pack plus service
 // health in ref_meta.
 const refEndpointPayload=companion.engine==='ref'&&companion.refEndpoint?companion.refEndpoint:undefined
 const[showForeignEdgeVoices,setShowForeignEdgeVoices]=useState(false)
 useEffect(()=>{let cancelled=false;setEngineState('probing');getTtsBridge().voices({engine:companion.engine,refEndpoint:refEndpointPayload}).then(r=>{if(cancelled)return;setVoices(r.voices);setRefMeta(r.ref_meta);let available=r.voices.length>0;if(companion.engine==='ref'){const online=r.ref_meta?.server_online===true||r.ref_meta?.host_state==='online';available=online&&!(r.ref_meta&&r.ref_meta.pack_exists===false)}setEngineState(available?'available':'unavailable')}).catch(()=>{if(!cancelled){setVoices([]);setRefMeta(undefined);setEngineState('unavailable')}});return()=>{cancelled=true}},[companion.engine,companion.voicePath,refEndpointPayload])
 // Auto-host launcher: when the ref engine is selected but the service
 // is down, kick the backend ensureRefEngine (non-blocking spawn) once
 // and poll the voices probe until /docs answers or the budget runs out.
 const hostLaunching=refMeta?.host_state==='launching'
 useEffect(()=>{if(companion.engine!=='ref'||!refMeta||refMeta.server_online||!refMeta.host_script)return;let cancelled=false;void getTtsBridge().ensureRefEngine({refEndpoint:refEndpointPayload}).catch(()=>{});let tries=0;const poll=()=>{if(cancelled)return;tries++;getTtsBridge().voices({engine:'ref',refEndpoint:refEndpointPayload}).then(r=>{if(cancelled)return;if(r.ref_meta){setRefMeta(r.ref_meta);if(r.ref_meta.server_online){setStatus('语音引擎已就绪，50 种音色可直接试听。');return}}if(tries<40)setTimeout(poll,3000);else{const msg=refLaunchPollStatus(tries,40,r.ref_meta?.host_last_err);if(msg)setStatus(msg)}}).catch(()=>{if(!cancelled&&tries<40)setTimeout(poll,3000);else if(!cancelled){const msg=refLaunchPollStatus(tries,40);if(msg)setStatus(msg)}})};const timer=setTimeout(poll,3000);return()=>{cancelled=true;clearTimeout(timer)}},[companion.engine,refMeta?.server_online,refMeta?.host_script,refEndpointPayload])
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
  }catch(e){setStatus(refPreviewStatus(e))}finally{setBusy(false)}
 }
 const refDesc=refEngineCaption(refMeta,voices.length)
 const zhVoices=voices.filter(v=>(v.lang||'').toLowerCase()==='zh-cn')
 const zhMale=zhVoices.filter(v=>v.gender==='male').length
 const zhFemale=zhVoices.filter(v=>v.gender==='female').length
 const visibleVoices=companion.engine==='edge'&&!showForeignEdgeVoices?zhVoices:voices
 const volcPersonas:VoicePersona[]=voices.map(v=>({id:v.voice_id,name:v.display_name||v.voice_id,group:v.group||'官方音色',gender:v.gender==='male'?'male':'female'}))
 const engineDesc=companion.engine==='edge'?(engineState==='probing'?'正在获取微软云端音色…':engineState==='available'?`普通话音色 ${zhVoices.length} 种（男 ${zhMale} · 女 ${zhFemale}${showForeignEdgeVoices?` · 全部 ${voices.length}`:''}；微软 Edge Neural 风格扩展，免密钥，需联网）`:'无法连接微软云端语音（需联网）'):companion.engine==='natural'?(engineState==='probing'?'正在获取自然语音列表…':engineState==='available'?`自然语音音色 ${voices.length} 个（Windows OneCore 神经网络音色，本机离线合成），按角色风格分组`:'本机无自然语音（M95-001），月伴将自动切换字幕模式'):companion.engine==='ref'?(engineState==='probing'?'正在获取角色音色列表…':refDesc):companion.engine==='volc'?(engineState==='probing'?'正在获取火山官方音色…':engineState==='available'?`火山 seed-tts 2.0 官方音色 ${voices.length} 种（不是本机 50 种人生；密钥配在供应商「语音模型」）`:'火山朗读不可用。请在供应商里配置 Agent Plan 专属 API Key'):engineState==='probing'?'正在检测本机语音合成引擎…':engineState==='available'?`本机可用音色 ${voices.length} 个（Windows SAPI 桌面语音，离线合成）`:'本机无语音合成引擎（M95-001），月伴将自动切换字幕模式'
 const voiceDisabled=engineState!=='available'
 // Voice grouping: ref-engine presets carry an explicit group from the
 // backend; SAPI/OneCore voices fall back to the gender/lang heuristic
 // (zh-CN splits by gender, other zh-* are dialects, rest is foreign).
 const voiceGroups=(()=>{const g=new Map<string,typeof visibleVoices>();for(const v of visibleVoices){const lang=v.lang||'';const grp=v.group||(lang==='zh-CN'?(v.gender==='male'?'男声 · 阳光少年 / 沉稳大叔':'女声 · 温柔 / 活泼 / 甜美'):lang.startsWith('zh-')?'方言 · 东北 / 陕西 / 粤语 / 台湾':'外语 · 英语 / 日语');const arr=g.get(grp)||[];arr.push(v);g.set(grp,arr)}return[...g.entries()]})()
 const shownPath=shownVoicePath(companion.voicePath)
 return <><div className="voice-path-heading">语音通道</div><VoicePathPicker value={companion.voicePath} volcTtsReady={volcTtsReady} talkReady={talkReady} onChange={next=>save(applyVoicePath(companion,next,{volcTtsReady}))}/><Toggle label="启用月伴对话" desc="在普通聊天输入框显示月亮按钮，进入全屏语音对话舞台；关闭即回滚入口。月伴建议闪模型：用当前供应商里已有的最快文本模型（air / flash），不要开推理。" on={companion.enabled} onChange={v=>save({...companion,enabled:v})}/><HotkeyRow label="打断快捷键" desc="月汐说话或思考时，按此快捷键立刻停止；舞台上也有「打断」按钮。Esc 仍用于退出月伴。" hotkey={companion.interruptHotkey} onChange={hotkey=>save({...companion,interruptHotkey:hotkey})}/><Toggle label="回复自动朗读" desc="边生成边朗读（流式字幕同步更新）；关闭后仅显示字幕。" on={companion.autoSpeak} onChange={v=>save({...companion,autoSpeak:v})}/><Toggle label="先应一声" desc="默认关闭。打开后你说完立刻垫一句「嗯」；真正回答到了会接上。垫音容易被听写当成你的下一句，形成嗯嗯循环。" on={companion.instantAck} onChange={v=>save({...companion,instantAck:v})}/><Toggle label="全双工对话" desc="她说完后立刻接着听下一句，不必重新点麦克风。她思考和说话的整个回合里麦克风都静音，所以外放回声、电视声或旁人说话都不会把她的回答截断；想插话请点舞台上的「打断」或使用快捷键。云端/本地不能对着麦自动插话。" on={companion.fullDuplex} onChange={v=>save({...companion,fullDuplex:v})}/>{companion.voicePath==='volc'?<p className="voice-path-hint" role="note">火山卡：说话时可以对着麦打断；「打断」按钮同样可用。</p>:<p className="voice-path-hint" role="note">云端/本地：说完再答，答完再说。不想听完点「打断」。不能对着麦自动插话。</p>}<Toggle label="嘈杂环境模式" desc="提高麦克风能量门限、延长静音判定，减少旁人说话和背景声误触发。" on={companion.speechEnvironment==='noisy'} onChange={v=>save({...companion,speechEnvironment:v?'noisy':'normal'})}/><LocalAsrRow companion={companion} save={save}/><AsrCorrectionRow/>{shownPath==='local'&&<><VoicePersonaGrid caption="50 种人生已内置。音色走本机克隆引擎，点选即用。" value={companion.voiceId||companion.omniPersonaId} onChange={id=>save({...companion,voiceId:id,omniPersonaId:id})}/><div className="setting-row"><div><div className="setting-label">GPT-SoVITS 服务地址</div><div className="setting-desc">默认 http://127.0.0.1:9880。留空使用默认。</div></div><input className="setting-input" style={{flex:1,fontFamily:'var(--mono)',fontSize:12}} placeholder="http://127.0.0.1:9880" value={companion.refEndpoint} onChange={e=>save({...companion,refEndpoint:e.target.value.trim()})} aria-label="GPT-SoVITS 服务地址"/></div><div className="setting-row"><div><div className="setting-label">{engineDesc}</div></div><button disabled={busy||!refPreviewReady({engineState,hostState:refMeta?.host_state,serverOnline:refMeta?.server_online})} onClick={()=>void preview()}>{refPreviewButtonLabel({busy,launching:hostLaunching})}</button></div></>}{shownPath==='volc'&&volcTtsReady&&<><VoicePersonaGrid caption="火山 seed-tts 2.0 官方音色。不是本机 50 种人生，也不是豆包 App 温柔桃子。" personas={volcPersonas} filterPlaceholder="查找官方音色" filterLabel="查找火山音色" empty="没有匹配的音色。" value={companion.voiceId||''} onChange={id=>save({...companion,voiceId:id})}/><div className="setting-row"><div><div className="setting-label">{engineDesc}</div></div><button disabled={busy||engineState!=='available'} onClick={()=>void preview()}>{refPreviewButtonLabel({busy,launching:false})}</button></div><div className="setting-row"><div><div className="setting-label">语速</div><div className="setting-desc">当前 {companion.rate}</div></div><input type="range" min={-10} max={10} step={1} value={companion.rate} aria-label="朗读语速" onChange={e=>save({...companion,rate:Number(e.target.value)})} style={{accentColor:'var(--tide1)'}}/></div><div className="setting-row"><div><div className="setting-label">音量</div><div className="setting-desc">当前 {companion.volume}</div></div><input type="range" min={0} max={100} step={1} value={companion.volume} aria-label="朗读音量" onChange={e=>save({...companion,volume:Number(e.target.value)})} style={{accentColor:'var(--tide1)'}}/></div></>}{(shownPath==='cloud'||shownPath==='volc'&&!volcTtsReady)&&<><Toggle label="显示外语音色" desc="默认只列出中文；开启后可浏览全部云端 Neural 音色。" on={showForeignEdgeVoices} onChange={setShowForeignEdgeVoices}/><div className="setting-row"><div><div className="setting-label">朗读音色</div><div className="setting-desc">{engineDesc}</div></div><div style={{display:'flex',gap:8,alignItems:'center'}}><select className="setting-select" aria-label="朗读音色" disabled={voiceDisabled} value={companion.voiceId} onChange={e=>save({...companion,voiceId:e.target.value})}><option value="">默认音色</option>{voiceGroups.map(([grp,items])=><optgroup key={grp} label={grp}>{items.map(voice=><option key={voice.voice_id} value={voice.voice_id}>{voice.display_name}</option>)}</optgroup>)}</select><button disabled={busy||engineState!=='available'} onClick={()=>void preview()}>{refPreviewButtonLabel({busy,launching:false})}</button></div></div><div className="setting-row"><div><div className="setting-label">语速</div><div className="setting-desc">当前 {companion.rate}</div></div><input type="range" min={-10} max={10} step={1} value={companion.rate} aria-label="朗读语速" onChange={e=>save({...companion,rate:Number(e.target.value)})} style={{accentColor:'var(--tide1)'}}/></div><div className="setting-row"><div><div className="setting-label">音量</div><div className="setting-desc">当前 {companion.volume}</div></div><input type="range" min={0} max={100} step={1} value={companion.volume} aria-label="朗读音量" onChange={e=>save({...companion,volume:Number(e.target.value)})} style={{accentColor:'var(--tide1)'}}/></div></>}{status&&<p role="status" className="notice">{status}</p>}</>
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
          <div><dt>{zh ? '运行' : 'Runtime'}</dt><dd>{zh ? '关窗口 ≠ 退出助手，助手在托盘运行' : 'Closing the window keeps the tray assistant'}</dd></div>
        </dl>
        <div className="about-links">
          <span>{zh ? '产品定位：不只是一个“更好的界面”，而是一个理解项目语义、记得历史、可扩展、能规划、且有治理边界的 AI 开发伙伴。' : 'A local-first AI workbench that keeps project context, history, and governance boundaries — not just a prettier chat box.'}</span>
        </div>
      </div>
    </div>
  )
}

export type McpPresetItem = Mcp6PresetsListResult['items'][number]

export function McpPresetsSection({ bridge = getMcpBridge(), onOpenMcp }: { bridge?: McpBridge; onOpenMcp?: () => void }): React.JSX.Element {
  const [leftover, setLeftover] = useState<string[]>([])
  const [status, setStatus] = useState('')

  useEffect(() => {
    bridge.list().catch(() => ({ endpoints: [] }))
      .then(listed => {
        setLeftover(leftoverArchivedNames(listed?.endpoints ?? []))
        setStatus('')
      })
      .catch(e => setStatus(e instanceof Error ? e.message : 'MCP 清单加载失败'))
  }, [bridge])

  return (
    <div className="setting-row" style={{ gridTemplateColumns: '1fr' }}>
      <div className="setting-group-title" style={{ marginTop: 8 }}>MCP</div>
      <div className="setting-desc">预置服务器、手动 JSON 和连接状态都在左侧「MCP」。设置里不再安装第二套。</div>
      {leftover.length > 0 && (
        <p role="status" className="notice" style={{ color: 'var(--err)' }}>
          检测到已下架 MCP（{leftover.join('、')}）。请到 MCP 页卸载，设置里不再做第二套卸载。
        </p>
      )}
      {onOpenMcp ? <div style={{ margin: '8px 0' }}><button type="button" onClick={onOpenMcp}>去 MCP 页</button></div> : null}
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
