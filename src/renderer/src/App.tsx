import { useEffect, useState } from 'react'
import { create } from 'zustand'
import type { EngineStatus } from '../../shared/engine'
import type { UpdateStatus } from '../../shared/update'
import { SettingsPage } from './pages/SettingsPage'

const initialStatus: EngineStatus = { state: 'starting', detail: '正在连接本地引擎…', restartCount: 0, updatedAt: new Date().toISOString() }
const initialUpdate: UpdateStatus = { state: 'idle', currentVersion: '0.2.0', detail: '可以检查新版本' }
const useEngine = create<{ status: EngineStatus; setStatus: (status: EngineStatus) => void }>((set) => ({ status: initialStatus, setStatus: (status) => set({ status }) }))
const useUpdate = create<{ status: UpdateStatus; setStatus: (status: UpdateStatus) => void }>((set) => ({ status: initialUpdate, setStatus: (status) => set({ status }) }))
const navItems = [['◈', '工作台', 'dashboard'], ['◎', '对话', 'chat'], ['◇', '项目', 'projects'], ['⌘', '设置', 'settings']] as const

export function App(): React.JSX.Element {
  const { status, setStatus } = useEngine(); const { status: update, setStatus: setUpdate } = useUpdate()
  const [page, setPage] = useState<'dashboard' | 'settings'>('dashboard')
  useEffect(() => {
    void window.lunitide.getEngineStatus().then(setStatus); void window.lunitide.getUpdateStatus().then(setUpdate)
    const offEngine = window.lunitide.onEngineStatus(setStatus); const offUpdate = window.lunitide.onUpdateStatus(setUpdate)
    return () => { offEngine(); offUpdate() }
  }, [setStatus, setUpdate])
  return <div className="shell"><aside className="sidebar">
    <div className="brand"><img className="brand-mark" src="./brand/lunitide-moon-logo.svg" alt="" /><b>Lunitide</b><span>月汐</span></div>
    <p className="nav-title">工作区</p><nav>{navItems.map(([icon, label, target]) => {
      const disabled = target === 'chat' || target === 'projects'; const active = page === target
      return <button className={active ? 'nav-item active' : 'nav-item'} key={target} disabled={disabled} onClick={() => !disabled && setPage(target as 'dashboard' | 'settings')}><span>{icon}</span>{label}{disabled && <small>即将开放</small>}</button>
    })}</nav>
    <div className="sidebar-foot"><span className={`status-dot ${status.state}`} /><div><b>本地引擎</b><small>{status.detail}</small></div></div>
  </aside><main>{page === 'settings' ? <SettingsPage /> : <Dashboard status={status} update={update} setStatus={setStatus} setUpdate={setUpdate} openSettings={() => setPage('settings')} />}</main></div>
}

function Dashboard({ status, update, setStatus, setUpdate, openSettings }: { status: EngineStatus; update: UpdateStatus; setStatus: (s: EngineStatus) => void; setUpdate: (s: UpdateStatus) => void; openSettings: () => void }): React.JSX.Element {
  const isReady = status.state === 'ready'; const checking = ['checking', 'available', 'downloading'].includes(update.state)
  return <><header className="topbar"><div><p className="eyebrow">LOCAL-FIRST AI WORKSPACE</p><h1>晚上好，欢迎回到月汐</h1></div><div className={`engine-pill ${status.state}`}><span />{isReady ? '引擎运行中' : '引擎未就绪'}</div></header>
    <section className="hero-card"><img className="moon" src="./brand/lunitide-moon-logo.svg" alt="" /><div className="hero-content"><p className="eyebrow">MILESTONE M2 · MODEL GATEWAY</p><h2>连接你的模型，开启月汐</h2><p>模型网关与 BYOK 安全密钥管理已经就绪。配置供应商后，即可验证真实模型连接。</p><button onClick={openSettings}>配置模型供应商</button>{!isReady && <button onClick={() => void window.lunitide.restartEngine().then(setStatus)}>重新启动引擎</button>}</div></section>
    <section className="content-grid"><article className="panel"><div className="panel-title"><span>系统状态</span><button onClick={() => void window.lunitide.exportDiagnostics()}>导出诊断</button></div><StatusRow label="Electron 桌面壳" state="运行中" ok /><StatusRow label="React 工作台" state="已加载" ok /><StatusRow label="Python 本地引擎" state={status.detail} ok={isReady} /><StatusRow label="模型网关" state={isReady ? '等待供应商配置' : '引擎未就绪'} ok={isReady} /></article>
    <article className="panel next-step"><div className="panel-title"><span>版本与更新</span><small>v{update.currentVersion}</small></div>{checking && <div className="update-progress"><i style={{ width: `${update.percent ?? 12}%` }} /></div>}<h3>{update.state === 'downloaded' ? `新版本 ${update.availableVersion ?? ''} 已就绪` : 'Lunitide 月汐'}</h3><p>{update.detail}</p><div className="update-actions">{update.state === 'downloaded' ? <button onClick={() => void window.lunitide.installUpdate()}>重启并安装</button> : <button disabled={checking || update.state === 'unavailable'} onClick={() => void window.lunitide.checkForUpdates().then(setUpdate)}>{checking ? '正在更新…' : '检查更新'}</button>}</div><div className="tags"><span>Electron</span><span>React</span><span>FastAPI</span></div></article></section></>
}
function StatusRow({ label, state, ok = false }: { label: string; state: string; ok?: boolean }): React.JSX.Element { return <div className="status-row"><span className={ok ? 'check ok' : 'check'}>{ok ? '✓' : '·'}</span><b>{label}</b><small>{state}</small></div> }
