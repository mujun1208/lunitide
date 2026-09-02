import React, { useEffect, useState } from 'react'
import { brBridge, type BrBridge } from '../bridge/client'
import type { BrDataUsageResult, BrModeDetectResult, BrPermissionListResult, BrPermissionPolicyPayload, BrSessionListResult, BrSettingsGetResult, BrSettingsUpdatePayload } from '../generated/bridge'
import { Toggle } from './settingsControls'

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
          <div className="setting-desc">对话里的填表/点击走托管 Playwright。打开网页（navigate）可以在首次预置；click/type 未就绪会报 BROWSER_MCP_NOT_READY，不会空成功。登录墙、验证码、文件选择请你本地完成。个人 Chrome/Edge 仅在你选择对应连接模式时使用，不是默认电脑控制，也不会拷贝 Cookie。Chrome DevTools / Browser MCP 要人在 MCP 页安装。</div>
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
