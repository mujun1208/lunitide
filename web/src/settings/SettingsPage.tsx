import React, { useEffect, useState } from 'react'

type SettingsCategory = 'general' | 'appearance' | 'providers' | 'project' | 'connection' | 'security' | 'data' | 'skills' | 'shortcuts' | 'diagnostics' | 'about'

const CATEGORIES: { id: SettingsCategory; icon: string; label: string }[] = [
  { id: 'general', icon: '◌', label: '常规' },
  { id: 'appearance', icon: '◐', label: '外观' },
  { id: 'providers', icon: '◈', label: '模型与供应商' },
  { id: 'project', icon: '◇', label: '项目默认值' },
  { id: 'connection', icon: '⇄', label: '连接与代理' },
  { id: 'security', icon: '⛨', label: '安全与治理' },
  { id: 'data', icon: '❖', label: '数据与记忆' },
  { id: 'skills', icon: '✦', label: '技能' },
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

export function SettingsPage({ onNavigateProviders }: { onNavigateProviders?: () => void }): React.JSX.Element {
  const [category, setCategory] = useState<SettingsCategory>('general')
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
          {category === 'about' && <AboutPanel />}
          {(['project', 'connection', 'security', 'data', 'skills', 'shortcuts', 'diagnostics'].includes(category)) && <PlaceholderPanel category={category} />}
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
            首期支持 OpenAI-compatible 与 Anthropic 两协议族。API Key 编辑时默认显示掩码，不回传明文到 Renderer。BaseURL 的协议或 origin 改变时，旧 credential 不自动复用。
          </div>
        </div>
      </div>
      <div className="setting-row" style={{ gridTemplateColumns: '1fr' }}>
        <button className="primary" onClick={onNavigate} style={{ alignSelf: 'flex-start' }}>
          打开供应商管理 →
        </button>
      </div>
    </div>
  )
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

function PlaceholderPanel({ category }: { category: SettingsCategory }): React.JSX.Element {
  const descriptions: Partial<Record<SettingsCategory, string>> = {
    project: '项目级配置默认值：阶段策略、模板引用、自动审批边界、产物保留策略等。待 M3 九阶段与产物设计冻结后启用。',
    connection: '网络代理配置：HTTP/HTTPS 代理、SSL 证书策略、连接超时与重试、SSRF 防护白名单。待 M5 数据接口工作台阶段启用。',
    security: '安全与治理：审批边界配置、审计日志策略、高风险操作清单、凭据访问策略、信任根管理。待 M4 智能内核阶段启用。',
    data: '数据与记忆：记忆生命周期、备份恢复策略、隐私脱敏规则、数据删除与保留。待 M2 基础备份恢复与 M4 记忆中心完成后启用。',
    skills: '技能管理：已安装技能列表、权限配置、签名验证、自定义技能注册。待 M4 技能中心完成后启用。',
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
