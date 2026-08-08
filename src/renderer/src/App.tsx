import { useEffect } from 'react'
import { create } from 'zustand'
import type { EngineStatus } from '../../shared/engine'

const initialStatus: EngineStatus = {
  state: 'starting',
  detail: '正在连接本地引擎…',
  restartCount: 0,
  updatedAt: new Date().toISOString()
}

const useEngine = create<{ status: EngineStatus; setStatus: (status: EngineStatus) => void }>((set) => ({
  status: initialStatus,
  setStatus: (status) => set({ status })
}))

const navItems = [
  ['◈', '工作台'],
  ['◎', '对话'],
  ['◇', '项目'],
  ['⌘', '设置']
]

export function App(): React.JSX.Element {
  const { status, setStatus } = useEngine()

  useEffect(() => {
    void window.lunitide.getEngineStatus().then(setStatus)
    return window.lunitide.onEngineStatus(setStatus)
  }, [setStatus])

  const isReady = status.state === 'ready'

  return (
    <div className="shell">
      <aside className="sidebar">
        <div className="brand"><span className="brand-mark">◐</span><b>Lunitide</b><span>月汐</span></div>
        <p className="nav-title">工作区</p>
        <nav>
          {navItems.map(([icon, label], index) => (
            <button className={index === 0 ? 'nav-item active' : 'nav-item'} key={label} disabled={index > 0}>
              <span>{icon}</span>{label}{index > 0 && <small>即将开放</small>}
            </button>
          ))}
        </nav>
        <div className="sidebar-foot">
          <span className={`status-dot ${status.state}`} />
          <div><b>本地引擎</b><small>{status.detail}</small></div>
        </div>
      </aside>

      <main>
        <header className="topbar">
          <div><p className="eyebrow">LOCAL-FIRST AI WORKSPACE</p><h1>晚上好，欢迎回到月汐</h1></div>
          <div className={`engine-pill ${status.state}`}><span />{isReady ? '引擎运行中' : '引擎未就绪'}</div>
        </header>

        <section className="hero-card">
          <div className="moon"><i /></div>
          <div className="hero-content">
            <p className="eyebrow">MILESTONE M1 · ENGINE ONLINE</p>
            <h2>以月为灯，随潮汐流向灵感</h2>
            <p>桌面框架与本地 Python 引擎已经连接。下一步将在这里配置你的模型，开启第一场对话。</p>
            {!isReady && <button onClick={() => void window.lunitide.restartEngine().then(setStatus)}>重新启动引擎</button>}
          </div>
        </section>

        <section className="content-grid">
          <article className="panel">
            <div className="panel-title"><span>系统状态</span><small>实时</small></div>
            <StatusRow label="Electron 桌面壳" state="运行中" ok />
            <StatusRow label="React 工作台" state="已加载" ok />
            <StatusRow label="Python 本地引擎" state={status.detail} ok={isReady} />
            <StatusRow label="模型网关" state="M2 开放" />
          </article>
          <article className="panel next-step">
            <div className="panel-title"><span>开发进度</span><small>1 / 4</small></div>
            <div className="progress"><i /></div>
            <h3>M1 · 工程脚手架</h3>
            <p>安全进程桥、健康检查、异常恢复与工作台空壳。</p>
            <div className="tags"><span>Electron</span><span>React</span><span>FastAPI</span></div>
          </article>
        </section>
      </main>
    </div>
  )
}

function StatusRow({ label, state, ok = false }: { label: string; state: string; ok?: boolean }): React.JSX.Element {
  return <div className="status-row"><span className={ok ? 'check ok' : 'check'}>{ok ? '✓' : '·'}</span><b>{label}</b><small>{state}</small></div>
}
