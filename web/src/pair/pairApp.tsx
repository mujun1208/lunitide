// 配对落地页：手机扫描桌面端二维码后打开（https://<电脑IP>:47651/pair#c=<码>&fp=<短指纹>）。
// 流程：解析 hash 中的配对码与证书短指纹 →（TOFU）展示指纹供人工比对 →
// POST /api/pair 换取设备令牌 → 存入 localStorage → 引导进入主界面。
// 配对成功即注册 Service Worker，之后 PWA 图标打开的是完整产品界面。
// 挂载逻辑在 pairEntry.tsx（本模块保持可测试的纯导出）。
import React, { useEffect, useMemo, useState } from 'react'
import { saveRemoteCredentials } from '../bridge/wsTransport'

export interface PairHash { code: string; fingerprint: string; lang?: 'zh-CN' | 'en' }

export function parsePairHash(hash: string): PairHash | undefined {
  const raw = hash.startsWith('#') ? hash.slice(1) : hash
  if (!raw) return undefined
  const params = new URLSearchParams(raw)
  const code = (params.get('c') ?? '').trim()
  if (!/^\d{8}$/.test(code)) return undefined
  const lang = (params.get('lang') ?? '').trim()
  return { code, fingerprint: (params.get('fp') ?? '').trim(), lang: lang === 'zh-CN' || lang === 'en' ? lang : undefined }
}

// 把桌面语言写入本机 localStorage：lunitide:language 是全局语言键，
// lunitide:language-default-en 标记「首次默认英文」流程已走过（见
// i18n/language.tsx），不写它的话下次启动仍会被强制回英文。
export function applyPairLanguage(lang: 'zh-CN' | 'en' | undefined, storage: Storage | null = safeStorage()): void {
  if (!lang || !storage) return
  try {
    storage.setItem('lunitide:language', lang)
    storage.setItem('lunitide:language-default-en', '1')
  } catch { /* storage-denied：语言保持默认 */ }
}

function safeStorage(): Storage | null {
  try { return localStorage } catch { return null }
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

// Android 壳 APP 的 WebView 会向 UA 追加「 LunitideApp/<version>」。
export function isShellApp(userAgent: string): boolean {
  return /LunitideApp\//.test(userAgent)
}

export type InstallGuidance =
  | { kind: 'prompt' } // 浏览器原生安装按钮可用（beforeinstallprompt 已触发）
  | { kind: 'ios-home-screen' } // iOS Safari：分享 → 添加到主屏幕
  | { kind: 'shortcut'; reason: 'self-signed' | 'unknown' } // 只能添加快捷方式

// 安装引导分派：Android Chrome 对自签证书（内网直连）不提供 WebAPK
// 安装也不触发 beforeinstallprompt——这是平台安全模型，不是产品缺陷。
// 此时退化为「添加到主屏幕」快捷方式，功能完整。
export function installGuidance(platform: string, promptAvailable: boolean): InstallGuidance {
  if (promptAvailable) return { kind: 'prompt' }
  if (platform === 'ios-pwa') return { kind: 'ios-home-screen' }
  return { kind: 'shortcut', reason: platform === 'android-pwa' ? 'self-signed' : 'unknown' }
}

export interface PairSuccess {
  deviceToken: string
  expiresAt: string
  /** 网关候选地址（局域网 IPv4 + 公网 IPv6，蜂窝直连用）；旧版网关无此字段。 */
  addresses?: string[]
  /** 网关证书短指纹；旧版网关无此字段。 */
  fingerprint?: string
}
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

// beforeinstallprompt 事件的鸭子类型：只用到 prompt()。
interface InstallPromptEvent extends Event { prompt(): Promise<void> }

export function PairApp() {
  const fromHash = useMemo(() => parsePairHash(location.hash), [])
  const [code, setCode] = useState(fromHash?.code ?? '')
  const [deviceName, setDeviceName] = useState(() => detectDeviceName(navigator.userAgent))
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  const [done, setDone] = useState(false)
  const [pairInfo, setPairInfo] = useState<PairSuccess | null>(null)
  const [installPrompt, setInstallPrompt] = useState<InstallPromptEvent | null>(null)
  // Android 浏览器（非壳）场景探测网关是否带 APP 安装包（/app/lunitide.apk）。
  const [apkAvailable, setApkAvailable] = useState(false)

  useEffect(() => {
    // 桌面语言随二维码带来，立即写入本机（用户扫完码语言即就位，
    // 无需等待点「完成配对」）。
    applyPairLanguage(fromHash?.lang)
    // 清掉地址栏中的配对码（一次性敏感值，避免刷新/分享泄露）。
    if (fromHash) history.replaceState(null, '', '/pair')
  }, [fromHash])

  useEffect(() => {
    if (isShellApp(navigator.userAgent)) return
    fetch('/app/lunitide.apk', { method: 'HEAD' })
      .then(response => { if (response.ok) setApkAvailable(true) })
      .catch(() => { /* 网关无安装包：保持快捷方式引导 */ })
  }, [])

  useEffect(() => {
    // 壳内配对成功：把网关多候选地址与指纹交给壳保存（蜂窝网络自动
    // 切换 IPv6 直连用；token 已在壳 WebView 的 localStorage 里）。
    if (!done || !pairInfo || !isShellApp(navigator.userAgent)) return
    const bridge = (window as unknown as { LunitideShell?: { saveGateway(json: string): void } }).LunitideShell
    try {
      bridge?.saveGateway?.(JSON.stringify({
        origin: location.origin,
        fingerprint: pairInfo.fingerprint ?? fromHash?.fingerprint ?? '',
        addresses: pairInfo.addresses ?? [],
      }))
    } catch { /* 壳桥异常不阻塞配对结果 */ }
  }, [done, pairInfo, fromHash])

  useEffect(() => {
    // 可安装（可信证书环境）时浏览器会触发该事件；自签证书下 Android
    // Chrome 不触发，引导自动退化为快捷方式（见 installGuidance）。
    const handler = (event: Event) => {
      event.preventDefault()
      setInstallPrompt(event as InstallPromptEvent)
    }
    window.addEventListener('beforeinstallprompt', handler)
    return () => window.removeEventListener('beforeinstallprompt', handler)
  }, [])

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
    setPairInfo(outcome.result)
    setDone(true); setBusy(false)
  }

  // 「在 Lunitide APP 中打开」：把本设备授权（token）与候选地址经本机
  // intent 交给已安装的壳（配对码是一次性的，凭据转移避免二次扫码）。
  const shellHandoffUrl = done && pairInfo && !isShellApp(navigator.userAgent) && apkAvailable
    ? `lunitide://open?origin=${encodeURIComponent(location.origin)}`
      + `&fp=${encodeURIComponent(pairInfo.fingerprint ?? fromHash?.fingerprint ?? '')}`
      + `&token=${encodeURIComponent(pairInfo.deviceToken)}`
      + `&addresses=${encodeURIComponent((pairInfo.addresses ?? []).join(','))}`
    : ''

  if (done) {
    const shell = isShellApp(navigator.userAgent)
    const guidance = installGuidance(detectPlatform(navigator.userAgent), installPrompt !== null)
    return (
      <div className="pair-card">
        <img className="pair-logo" src="/brand/icon-192.png" alt="Lunitide" />
        <h1>配对成功</h1>
        <p className="pair-sub">这台设备已关联你的电脑。</p>
        {shell ? (
          <p className="pair-sub">
            网关地址与证书指纹已保存到 Lunitide APP。<br />
            在家走 Wi-Fi、在外走蜂窝流量，自动切换直连你的电脑。
          </p>
        ) : apkAvailable ? (
          <>
            <a className="pair-btn" href="/app/lunitide.apk" download="Lunitide.apk">安装 Lunitide APP（推荐）</a>
            <p className="pair-hint">
              下载后打开安装（需允许「未知来源/安装未知应用」）；安装完成后回到本页，
              点下方按钮把本设备授权带入 APP，无需再次扫码。
            </p>
            {shellHandoffUrl && <a className="pair-open" href={shellHandoffUrl}>已在 APP 中打开（带入授权）</a>}
            <p className="pair-hint">也可以不装 APP：本页继续使用网页版，或按下面的快捷方式指引使用。</p>
          </>
        ) : (
          <>
            {guidance.kind === 'prompt' && installPrompt && (
              <>
                <button
                  className="pair-btn"
                  onClick={() => { void installPrompt.prompt(); setInstallPrompt(null) }}
                >
                  安装到手机主屏幕
                </button>
                <p className="pair-hint">安装后像 App 一样全屏打开，无需浏览器地址栏。</p>
              </>
            )}
            {guidance.kind === 'ios-home-screen' && (
              <p className="pair-sub">
                建议添加到主屏幕：Safari 底部分享按钮 →「添加到主屏幕」，<br />即可像 App 一样打开完整界面。
              </p>
            )}
            {guidance.kind === 'shortcut' && (
              <p className="pair-sub">
                建议添加到主屏幕：点浏览器右上角 ⋮ 菜单 →「安装并创建快捷方式」→ 选「创建快捷方式」（旧版菜单叫「添加到主屏幕」）。
                {guidance.reason === 'self-signed' && (
                  <>
                    <br />弹窗里「安装」显示「无法安装此应用」属正常：内网直连使用自签证书，
                    Chrome 安全模型不允许此类网站安装成应用（任何网站都无法绕过，属平台限制）。
                    <br />「创建快捷方式」后功能完整可用，图标与名称同应用一致。
                  </>
                )}
              </p>
            )}
          </>
        )}
        <a className="pair-open" href="/">进入 Lunitide</a>
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
      {apkAvailable && !isShellApp(navigator.userAgent) && (
        <>
          <a className="pair-open" href="/app/lunitide.apk" download="Lunitide.apk">先安装 Lunitide APP（推荐）</a>
          <p className="pair-hint">先装 APP 再配对：安装后重新扫码，选择用 Lunitide 打开，配对与使用都在 APP 内完成。</p>
        </>
      )}
      <p className="pair-hint">配对码 5 分钟内有效且只能使用一次；设备授权 180 天，可随时在电脑端吊销。</p>
    </div>
  )
}
