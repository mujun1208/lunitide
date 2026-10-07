// 配对落地页：手机扫描桌面端二维码后打开（https://<电脑IP>:47651/pair#c=<码>&fp=<短指纹>）。
// 流程：解析 hash 中的配对码与证书短指纹 →（TOFU）展示指纹供人工比对 →
// POST /api/pair 换取设备令牌 → 存入 localStorage → 引导进入主界面。
// 配对成功即注册 Service Worker，之后 PWA 图标打开的是完整产品界面。
// 挂载逻辑在 pairEntry.tsx（本模块保持可测试的纯导出）。
import React, { useEffect, useMemo, useState } from 'react'
import { saveRemoteCredentials } from '../bridge/wsTransport'

export interface PairHash { code: string; fingerprint: string }

export function parsePairHash(hash: string): PairHash | undefined {
  const raw = hash.startsWith('#') ? hash.slice(1) : hash
  if (!raw) return undefined
  const params = new URLSearchParams(raw)
  const code = (params.get('c') ?? '').trim()
  if (!/^\d{8}$/.test(code)) return undefined
  return { code, fingerprint: (params.get('fp') ?? '').trim() }
}

export function detectDeviceName(userAgent: string): string {
  if (/iPhone/i.test(userAgent)) return 'iPhone'
  if (/iPad/i.test(userAgent)) return 'iPad'
  if (/Android/i.test(userAgent)) return 'Android 手机'
  if (/Macintosh/i.test(userAgent)) return 'Mac'
  if (/Windows/i.test(userAgent)) return 'Windows 设备'
  return '手机'
}

export function detectPlatform(userAgent: string): string {
  if (/iPhone|iPad|iPod/i.test(userAgent)) return 'ios-pwa'
  if (/Android/i.test(userAgent)) return 'android-pwa'
  return 'mobile-web'
}

export interface PairSuccess { deviceToken: string; expiresAt: string }
type PairOutcome = { ok: true; result: PairSuccess } | { ok: false; error: string }

// pairWithGateway 调网关配对端点。独立导出便于单测注入 fetch。
export async function pairWithGateway(input: { code: string; deviceName: string; platform: string }, fetchFn: typeof fetch = fetch): Promise<PairOutcome> {
  let response: Response
  try {
    response = await fetchFn('/api/pair', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ code: input.code, deviceName: input.deviceName, platform: input.platform }),
    })
  } catch {
    return { ok: false, error: '无法连接电脑，请确认电脑端 Lunitide 已打开且手机与电脑在同一网络。' }
  }
  if (response.ok) {
    const result = (await response.json()) as PairSuccess
    if (typeof result?.deviceToken !== 'string' || !result.deviceToken) return { ok: false, error: '配对响应无效，请重试。' }
    return { ok: true, result }
  }
  if (response.status === 503) return { ok: false, error: '电脑端已关闭远程访问，请在桌面「移动伴侣」设置中重新开启。' }
  if (response.status === 403) {
    const text = await response.text().catch(() => '')
    return { ok: false, error: text.includes('锁定') ? text : '配对码无效或已过期，请在电脑端重新生成二维码。' }
  }
  return { ok: false, error: `配对失败（${response.status}），请重试。` }
}

export function PairApp() {
  const fromHash = useMemo(() => parsePairHash(location.hash), [])
  const [code, setCode] = useState(fromHash?.code ?? '')
  const [deviceName, setDeviceName] = useState(() => detectDeviceName(navigator.userAgent))
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  const [done, setDone] = useState(false)

  useEffect(() => {
    // 清掉地址栏中的配对码（一次性敏感值，避免刷新/分享泄露）。
    if (fromHash) history.replaceState(null, '', '/pair')
  }, [fromHash])

  const submit = async () => {
    const normalized = code.trim()
    if (!/^\d{8}$/.test(normalized)) { setError('请输入 8 位配对码。'); return }
    if (!deviceName.trim()) { setError('请输入设备名称。'); return }
    setBusy(true); setError('')
    const outcome = await pairWithGateway({ code: normalized, deviceName: deviceName.trim(), platform: detectPlatform(navigator.userAgent) })
    if (!outcome.ok) { setError(outcome.error); setBusy(false); return }
    saveRemoteCredentials({ wsUrl: `wss://${location.host}/bridge`, token: outcome.result.deviceToken })
    try { await navigator.serviceWorker?.register('/sw.js') } catch { /* SW 失败不阻塞配对结果 */ }
    // 轮询通知（M4）需要浏览器通知权限；拒绝不阻塞配对，仅无通知。
    try { if (typeof Notification !== 'undefined' && Notification.permission === 'default') void Notification.requestPermission() } catch { /* 权限请求失败忽略 */ }
    setDone(true); setBusy(false)
  }

  if (done) {
    return (
      <div className="pair-card">
        <img className="pair-logo" src="/brand/icon-192.png" alt="Lunitide" />
        <h1>配对成功</h1>
        <p className="pair-sub">这台设备已关联你的电脑。建议现在「添加到主屏幕」：<br />浏览器菜单 → 添加到主屏幕，即可像 App 一样打开。</p>
        <a className="pair-open" href="/">进入 Lunitide</a>
        <p className="pair-hint">添加到主屏幕后，图标将直接打开完整界面。</p>
      </div>
    )
  }

  return (
    <div className="pair-card">
      <img className="pair-logo" src="/brand/icon-192.png" alt="Lunitide" />
      <h1>Lunitide 移动伴侣</h1>
      <p className="pair-sub">扫描电脑端「移动伴侣」二维码来到这里。<br />确认下方指纹与电脑端显示一致，然后完成配对。</p>
      {fromHash?.fingerprint && (
        <div className="fp-box">
          电脑身份指纹<br />
          <code>{fromHash.fingerprint}</code><br />
          请与电脑端显示的指纹比对一致后再配对
        </div>
      )}
      <input
        type="text"
        inputMode="numeric"
        placeholder="8 位配对码"
        value={code}
        maxLength={8}
        onChange={event => setCode(event.target.value.replace(/\D/g, ''))}
        disabled={busy}
      />
      <input
        type="text"
        placeholder="设备名称"
        value={deviceName}
        maxLength={40}
        onChange={event => setDeviceName(event.target.value)}
        disabled={busy}
      />
      <div className="pair-error" role="alert">{error}</div>
      <button className="pair-btn" onClick={() => void submit()} disabled={busy || !/^\d{8}$/.test(code)}>
        {busy && <span className="spinner" aria-hidden="true" />}
        {busy ? '配对中…' : '完成配对'}
      </button>
      <p className="pair-hint">配对码 5 分钟内有效且只能使用一次；设备授权 180 天，可随时在电脑端吊销。</p>
    </div>
  )
}
