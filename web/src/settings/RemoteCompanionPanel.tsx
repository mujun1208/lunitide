import React, { useCallback, useEffect, useRef, useState } from 'react'
import { remoteCompanionBridge, type RemoteCompanionBridge } from '../bridge/client'
import type { RemoteAccessStatusResult, RemoteDevicesListResult, RemotePairCodeResult, RemoteSessionsListResult } from '../generated/bridge'
import { Toggle } from './settingsControls'
import { useConfirmDialog } from '../ui/useAskDialog'

// 移动伴侣设置面板：远程开关（enable 后展示配对二维码与地址）、已配对
// 设备列表（吊销）、在线会话与审计、防休眠开关。全部状态由引擎侧
// remote.db 管理，本面板只读 + 显式操作，不做本地镜像。
type DeviceRow = RemoteDevicesListResult['devices'][number]
type SessionRow = RemoteSessionsListResult['sessions'][number]
type AuditRow = RemoteSessionsListResult['audit'][number]

const AUDIT_KIND_LABELS: Record<string, string> = {
  pair: '配对成功', 'pair-fail': '配对失败', 'pair-locked': '配对锁定',
  'auth-fail': '鉴权失败', 'scope-denied': '越权拦截',
  method: '方法调用', revoke: '吊销',
}

function rcUserError(err: unknown, fallback: string): string {
  const detail = err instanceof Error ? err.message.trim() : ''
  return /[\u4e00-\u9fff]/.test(detail) ? detail : fallback
}

// 桌面当前界面语言：配对二维码 URL 携带，配对页据此把语言写入手机，
// 手机首次打开即与桌面同语言（见 pairApp.tsx）。
function desktopLang(): 'zh-CN' | 'en' {
  try { return localStorage.getItem('lunitide:language') === 'en' ? 'en' : 'zh-CN' } catch { return 'zh-CN' }
}

function formatTime(value: string | undefined): string {
  if (!value) return '—'
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return value
  return date.toLocaleString('zh-CN', { hour12: false })
}

// 码有效倒计时（5 分钟 TTL，前端按签发时刻推算展示）。
function useCountdown(expiresAt: string | undefined): string {
  const [, tick] = useState(0)
  useEffect(() => {
    if (!expiresAt) return
    const timer = window.setInterval(() => tick(v => v + 1), 1_000)
    return () => clearInterval(timer)
  }, [expiresAt])
  if (!expiresAt) return ''
  const remain = Math.max(0, Math.floor((Date.parse(expiresAt) - Date.now()) / 1000))
  const minutes = String(Math.floor(remain / 60)).padStart(2, '0')
  const seconds = String(remain % 60).padStart(2, '0')
  return `${minutes}:${seconds}`
}

// 地址分类（hostAddresses 已剔除环回/链路本地）：局域网 IPv4 = 同一 Wi-Fi
// 可达；全局 IPv6 = 手机蜂窝流量直连家里电脑的候选（APP 端自动轮换）；
// 100.64.0.0/10（CGNAT 段）= Tailscale/ZeroTier 等尾网地址——依赖手机也
// 登录同一虚拟组网（国内环境登录/中继常被阻断），只作最后兜底，单独
// 归类，不冒充局域网地址误导用户。
export function classifyAddresses(addresses: readonly string[]): { lan: string[]; cellular: string[]; tailnet: string[] } {
  const lan: string[] = []
  const cellular: string[] = []
  const tailnet: string[] = []
  for (const address of addresses) {
    if (address.includes(':')) { cellular.push(address); continue }
    const parts = address.split('.')
    if (parts.length === 4 && Number(parts[0]) === 100) {
      const second = Number(parts[1])
      if (Number.isInteger(second) && second >= 64 && second <= 127) { tailnet.push(address); continue }
    }
    lan.push(address)
  }
  return { lan, cellular, tailnet }
}

export function RemoteCompanionPanel({ bridge = remoteCompanionBridge }: { bridge?: RemoteCompanionBridge }): React.JSX.Element {
  const [status, setStatus] = useState<RemoteAccessStatusResult | null>(null)
  const [devices, setDevices] = useState<DeviceRow[]>([])
  const [sessions, setSessions] = useState<SessionRow[]>([])
  const [audit, setAudit] = useState<AuditRow[]>([])
  const [pairInfo, setPairInfo] = useState<RemotePairCodeResult | null>(null)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  const [askNode, askConfirm] = useConfirmDialog()
  const refreshGeneration = useRef(0)

  const refresh = useCallback(async (withPairCode = false) => {
    const generation = ++refreshGeneration.current
    setBusy(true)
    const [statusResult, devicesResult, sessionsResult] = await Promise.allSettled([bridge.accessStatus(), bridge.devicesList(), bridge.sessionsList()])
    if (generation !== refreshGeneration.current) return
    const errors: string[] = []
    if (statusResult.status === 'fulfilled') setStatus(statusResult.value)
    else errors.push('远程访问状态加载失败，请刷新重试')
    if (devicesResult.status === 'fulfilled') setDevices(devicesResult.value.devices)
    else errors.push('设备列表暂时无法读取')
    if (sessionsResult.status === 'fulfilled') {
      setSessions(sessionsResult.value.sessions)
      setAudit(sessionsResult.value.audit)
    } else {
      errors.push('在线会话与审计暂时无法读取')
    }
    if (withPairCode && statusResult.status === 'fulfilled' && statusResult.value.enabled) {
      const code = await bridge.pairCode({ lang: desktopLang() }).catch(() => null)
      if (generation !== refreshGeneration.current) return
      if (code) setPairInfo(code)
      else errors.push('配对码生成失败，请重试')
    } else if (!withPairCode || statusResult.status !== 'fulfilled' || !statusResult.value.enabled) {
      setPairInfo(null)
    }
    setError(errors.join('；'))
    setBusy(false)
  }, [bridge])

  useEffect(() => { void refresh(); return () => { refreshGeneration.current++ } }, [refresh])

  const toggleAccess = async (next: boolean) => {
    setBusy(true); setError('')
    try {
      if (next) await bridge.accessEnable()
      else await bridge.accessDisable()
      await refresh(next)
    } catch (err) {
      setError(rcUserError(err, next ? '开启远程访问失败，请重试' : '关闭远程访问失败，请重试'))
      setBusy(false)
    }
  }

  const toggleKeepAwake = async (next: boolean) => {
    setBusy(true); setError('')
    try {
      await bridge.keepAwakeSet({ enabled: next })
      await refresh(false)
    } catch (err) {
      setError(rcUserError(err, '防休眠设置失败，请重试'))
      setBusy(false)
    }
  }

  const regenerateCode = async () => {
    setBusy(true); setError('')
    try {
      setPairInfo(await bridge.pairCode({ lang: desktopLang() }))
    } catch (err) {
      setError(rcUserError(err, '配对码生成失败，请重试'))
    }
    setBusy(false)
  }

  const revoke = async (deviceId: string, name: string) => {
    if (!await askConfirm({ title: `吊销「${name}」的访问授权？`, description: '吊销后该设备的连接立即断开，需重新配对才能连接。', confirmLabel: '吊销' })) return
    setBusy(true); setError('')
    try {
      await bridge.devicesRevoke({ deviceId })
      await refresh(false)
    } catch (err) {
      setError(rcUserError(err, '吊销失败，请重试'))
      setBusy(false)
    }
  }

  const enabled = status?.enabled ?? false
  const countdown = useCountdown(pairInfo?.expiresAt)
  const activeDevices = devices.filter(d => !d.revokedAt)
  const revokedDevices = devices.filter(d => d.revokedAt)
  const deviceNameOf = (id: string) => devices.find(d => d.deviceId === id)?.name ?? (id === 'anonymous' ? '未知来源' : id)
  const reachability = classifyAddresses(status?.addresses ?? [])

  return (
    <div className="governance-stack">
      {askNode}
      <div className="setting-group">
        <div className="setting-group-title">远程访问</div>
        <Toggle
          on={enabled}
          onChange={v => void toggleAccess(v)}
          label="允许手机连接本机"
          desc="开启后在局域网内提供加密（WSS）入口；手机扫描二维码完成配对即可使用本机的对话与全部工具。关闭即断开全部连接。"
        />
        {status && (
          <div className="setting-row" style={{ gridTemplateColumns: '1fr' }}>
            <div className="setting-desc">
              {enabled
                ? `监听端口 ${status.port} · 本机地址 ${status.addresses.length ? status.addresses.join(' / ') : '未检测到'} · 在线设备 ${status.activeDevices} 台`
                : '当前未开启远程访问，手机无法连接。'}
              {enabled && status.certFingerprint && <> · 身份指纹 <code>{status.certFingerprint}</code></>}
            </div>
          </div>
        )}
      </div>

      {enabled && (
        <div className="setting-group">
          <div className="setting-group-title">扫码配对</div>
          <div className="setting-row" style={{ gridTemplateColumns: 'auto 1fr', alignItems: 'start', gap: '20px' }}>
            {pairInfo ? (
              <img
                src={`data:image/png;base64,${pairInfo.qrPngBase64}`}
                alt="配对二维码"
                width={208}
                height={208}
                style={{ borderRadius: 12, border: '1px solid var(--border, #2a3245)', background: '#fff' }}
              />
            ) : (
              <div style={{ width: 208, height: 208, display: 'grid', placeItems: 'center', border: '1px dashed #2a3245', borderRadius: 12, color: '#8b93a7', fontSize: 13 }}>
                {busy ? '生成中…' : '点击下方按钮生成'}
              </div>
            )}
            <div style={{ display: 'grid', gap: 10, alignContent: 'start' }}>
              <div className="setting-desc">
                手机浏览器扫描二维码，确认页面显示的指纹与本页一致后输入配对码完成绑定。
              </div>
              {pairInfo && (
                <>
                  <div className="setting-desc">配对码 <code style={{ fontSize: 18, letterSpacing: 2 }}>{pairInfo.code}</code> · {countdown} 后过期</div>
                  <div className="setting-desc">连接地址：{pairInfo.addresses.length ? pairInfo.addresses.map(a => `https://${a}:${status?.port ?? 47651}`).join(' 或 ') : '未检测到本机地址'}</div>
                </>
              )}
              <div style={{ display: 'flex', gap: 8 }}>
                <button className="settings-save" onClick={() => void regenerateCode()} disabled={busy}>
                  {pairInfo ? '重新生成二维码' : '生成配对二维码'}
                </button>
              </div>
              <div className="setting-desc">配对码 5 分钟内有效且只能使用一次；设备令牌 180 天有效，可随时在下方吊销。</div>
            </div>
          </div>
        </div>
      )}

      <div className="setting-group">
        <div className="setting-group-title">外网可达性</div>
        <div className="setting-row" style={{ gridTemplateColumns: '1fr' }}>
          <div className="setting-desc">
            {reachability.cellular.length > 0 ? (
              <>
                本机已有公网 IPv6：<code>{reachability.cellular.join('、')}</code>。
                手机在外用蜂窝流量时，Lunitide APP 会自动切换到该地址直连家里电脑。
                {reachability.lan.length > 0 && <> 局域网地址（仅同一 Wi-Fi 可达）：{reachability.lan.join('、')}。</>}
                {reachability.tailnet.length > 0 && <> 尾网地址（需手机登录同一虚拟组网，仅作兜底）：{reachability.tailnet.join('、')}。</>}
                <br />若外网连不上：多为路由器拦截 IPv6 入站——在路由器设置中放行
                「IPv6 防火墙/入站过滤」对端口 {status?.port ?? 47651} 的 TCP 连接；也可先用手机浏览器
                在蜂窝网络下直接访问上述 IPv6 地址自检。
                <br />运营商重新分配 IPv6 前缀后地址会变化，APP 连不上时重新扫码配对即可。
              </>
            ) : (
              <>
                未检测到公网 IPv6 地址，手机仅能在同一 Wi-Fi（局域网）下连接。
                {reachability.tailnet.length > 0 && <>检测到尾网地址（{reachability.tailnet.join('、')}）：
                  仅在手机与电脑登录同一虚拟组网时可达（国内环境登录/中继常被阻断），不建议作为外网方案。</>}
                <br />需要外网直连：在光猫/路由器开启 IPv6（多数运营商默认下发），重新打开远程访问即可。
              </>
            )}
          </div>
        </div>
      </div>

      <div className="setting-group">
        <div className="setting-group-title">已配对设备（{activeDevices.length}）</div>
        {activeDevices.length === 0 ? (
          <div className="setting-row" style={{ gridTemplateColumns: '1fr' }}>
            <div className="setting-desc">{enabled ? '还没有手机配对。生成二维码并用手机扫描即可。' : '开启远程访问后可在这里管理已配对设备。'}</div>
          </div>
        ) : (
          <div className="setting-row" style={{ gridTemplateColumns: '1fr' }}>
            <table className="settings-table" style={{ width: '100%', borderCollapse: 'collapse', fontSize: 13 }}>
              <thead>
                <tr style={{ textAlign: 'left', color: '#8b93a7' }}>
                  <th style={{ padding: '6px 8px' }}>设备</th>
                  <th style={{ padding: '6px 8px' }}>平台</th>
                  <th style={{ padding: '6px 8px' }}>最近地址</th>
                  <th style={{ padding: '6px 8px' }}>授权至</th>
                  <th style={{ padding: '6px 8px' }} />
                </tr>
              </thead>
              <tbody>
                {activeDevices.map(device => (
                  <tr key={device.deviceId} style={{ borderTop: '1px solid #232b3f' }}>
                    <td style={{ padding: '8px' }}>{device.name}</td>
                    <td style={{ padding: '8px' }}>{device.platform}</td>
                    <td style={{ padding: '8px' }}>{device.lastIp ?? '—'}</td>
                    <td style={{ padding: '8px' }}>{formatTime(device.expiresAt)}</td>
                    <td style={{ padding: '8px' }}>
                      <button onClick={() => void revoke(device.deviceId, device.name)} disabled={busy}>吊销</button>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
        {revokedDevices.length > 0 && (
          <div className="setting-row" style={{ gridTemplateColumns: '1fr' }}>
            <div className="setting-desc">已吊销：{revokedDevices.map(d => d.name).join('、')}</div>
          </div>
        )}
      </div>

      <div className="setting-group">
        <div className="setting-group-title">在线会话与审计</div>
        <div className="setting-row" style={{ gridTemplateColumns: '1fr' }}>
          <div className="setting-desc">
            {sessions.length > 0
              ? `当前在线：${sessions.map(s => `${s.deviceName || s.deviceId}${s.ip ? `（${s.ip}）` : ''}`).join('、')}。吊销后连接立即断开。`
              : '当前没有手机在线。'}
          </div>
        </div>
        {audit.length > 0 && (
          <div className="setting-row" style={{ gridTemplateColumns: '1fr' }}>
            <table className="settings-table" style={{ width: '100%', borderCollapse: 'collapse', fontSize: 13 }}>
              <thead>
                <tr style={{ textAlign: 'left', color: '#8b93a7' }}>
                  <th style={{ padding: '6px 8px' }}>时间</th>
                  <th style={{ padding: '6px 8px' }}>设备</th>
                  <th style={{ padding: '6px 8px' }}>事件</th>
                  <th style={{ padding: '6px 8px' }}>方法/详情</th>
                </tr>
              </thead>
              <tbody>
                {audit.slice(0, 20).map(entry => (
                  <tr key={entry.id} style={{ borderTop: '1px solid #232b3f' }}>
                    <td style={{ padding: '8px', whiteSpace: 'nowrap' }}>{formatTime(entry.at)}</td>
                    <td style={{ padding: '8px' }}>{deviceNameOf(entry.deviceId)}</td>
                    <td style={{ padding: '8px', whiteSpace: 'nowrap' }}>{AUDIT_KIND_LABELS[entry.kind] ?? entry.kind}</td>
                    <td style={{ padding: '8px' }}>{entry.method || entry.detail || '—'}</td>
                  </tr>
                ))}
              </tbody>
            </table>
            {audit.length > 20 && (
              <div className="setting-desc" style={{ marginTop: 6 }}>仅展示最近 20 条（共 {audit.length} 条）。</div>
            )}
          </div>
        )}
      </div>

      <div className="setting-group">
        <div className="setting-group-title">供电</div>
        <Toggle
          on={status?.keepAwake ?? false}
          onChange={v => void toggleKeepAwake(v)}
          label="远程使用时阻止电脑休眠"
          desc="开启后只要有手机连接，电脑保持唤醒（SetThreadExecutionState），离开时自动恢复系统默认策略。"
        />
      </div>

      {error && <div className="setting-desc" role="alert" style={{ color: '#ff8d8d' }}>{error}</div>}
    </div>
  )
}
