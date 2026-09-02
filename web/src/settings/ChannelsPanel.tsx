import React, { useEffect, useState } from 'react'
import { imBridge, type ImBridge } from '../bridge/client'
import type { ImChannelsGetResult, ImChannelsSetPayload } from '../generated/bridge'
import { detectImWebhookKind } from './imWebhook'

function inboundPaired(ch: { inboundEnabled: boolean; inboundAllowlist: string }): boolean {
  return ch.inboundEnabled && ch.inboundAllowlist.trim() !== ''
}

function inboundPairStatus(ch: { inboundEnabled: boolean; inboundAllowlist: string }): string {
  if (!ch.inboundEnabled) return '入站关闭'
  const first = ch.inboundAllowlist.split('\n').map(s => s.trim()).find(Boolean)
  return first ? `已配对：${first}` : '等待第一条消息配对'
}

export function ChannelsPanel({ bridge = imBridge }: { bridge?: ImBridge }): React.JSX.Element {
  const [channels, setChannels] = useState<ImChannelsGetResult['channels']>([])
  const [paste, setPaste] = useState('')
  const [drafts, setDrafts] = useState<Record<string, string>>({})
  const [appIdDrafts, setAppIdDrafts] = useState<Record<string, string>>({})
  const [secretDrafts, setSecretDrafts] = useState<Record<string, string>>({})
  const [status, setStatus] = useState('')
  const [busy, setBusy] = useState(false)

  const refresh = async () => {
    setBusy(true)
    try {
      const r = await bridge.get()
      setChannels(r.channels)
      setDrafts(Object.fromEntries(r.channels.map(ch => [ch.kind, ch.webhookUrl])))
      setAppIdDrafts(Object.fromEntries(r.channels.map(ch => [ch.kind, ch.inboundAppId])))
      setStatus('')
    } catch (e) {
      setStatus(e instanceof Error ? e.message : '消息通道加载失败')
    } finally { setBusy(false) }
  }
  useEffect(() => { void refresh() }, [])

  const apply = async (p: ImChannelsSetPayload, okMsg: string) => {
    setBusy(true); setStatus('')
    try {
      const r = await bridge.set(p)
      setChannels(r.channels)
      setDrafts(Object.fromEntries(r.channels.map(ch => [ch.kind, ch.webhookUrl])))
      setAppIdDrafts(Object.fromEntries(r.channels.map(ch => [ch.kind, ch.inboundAppId])))
      setSecretDrafts(d => ({ ...d, [p.kind]: '' }))
      setStatus(okMsg)
    } catch (e) {
      setStatus(e instanceof Error ? e.message : '保存失败')
    } finally { setBusy(false) }
  }

  const pairWebhook = async () => {
    const url = paste.trim()
    const kind = detectImWebhookKind(url)
    if (!kind) {
      setStatus('无法识别：请粘贴飞书 / 企微 / 钉钉的 https Webhook')
      return
    }
    await apply({ kind, enabled: true, webhookUrl: url, testSend: true }, `${kind === 'feishu' ? '飞书' : kind === 'wecom' ? '企业微信' : '钉钉'}已启用，并试发了「月汐已连上」`)
  }

  const webhookKind = (kind: string) => kind === 'feishu' || kind === 'wecom' || kind === 'dingtalk'
  const inboundKind = (kind: string) => kind === 'feishu' || kind === 'wecom' || kind === 'dingtalk'
  const modeLabel = (mode: string) => mode === 'webhook' ? '机器人 Webhook' : mode === 'desktop' ? '本机客户端' : '未启用'
  const webhookPlaceholder = (kind: string) => kind === 'wecom'
    ? 'https://qyapi.weixin.qq.com/cgi-bin/webhook/send?key=…'
    : kind === 'dingtalk'
      ? 'https://oapi.dingtalk.com/robot/send?access_token=…'
      : 'https://open.feishu.cn/open-apis/bot/v2/hook/…'

  return (
    <div className="setting-group">
      <div className="setting-group-title">消息通道</div>
      <div className="setting-row" style={{ gridTemplateColumns: '1fr' }}>
        <div className="setting-desc">粘贴飞书 / 企微 / 钉钉群机器人 https Webhook，点「识别并启用」。会先试发「月汐已连上」，成功才保存。没有 Webhook 不能启用，也不会改用本机打字。微信 / QQ 是本机已登录客户端代发，不能收消息。入站：飞书 / 企微 / 钉钉填 App ID 与 Secret，点「连接并等待配对」。名单为空时，第一条发来的人会写入白名单，之后只收这些人。不要在陌生人能发言的大群里开配对。本机向外长连接，不开放公网端口。关窗口 ≠ 退出助手。</div>
      </div>
      <div className="setting-row" style={{ gridTemplateColumns: '1fr' }}>
        <div style={{ display: 'flex', gap: 8, flexWrap: 'wrap' }}>
          <input className="setting-input" style={{ flex: 1, minWidth: 240, fontFamily: 'var(--mono)', fontSize: 12 }}
            value={paste} placeholder="https://open.feishu.cn/open-apis/bot/v2/hook/…"
            aria-label="消息通道 Webhook"
            onChange={ev => setPaste(ev.target.value)} />
          <button disabled={busy} onClick={() => void pairWebhook()}>识别并启用</button>
        </div>
      </div>
      {channels.map(ch => (
        <div key={ch.kind} className="setting-row im-channel-row" style={{ gridTemplateColumns: '1fr' }}>
          <div style={{ display: 'flex', justifyContent: 'space-between', gap: 12, alignItems: 'center', flexWrap: 'wrap' }}>
            <div>
              <div className="setting-label">{ch.label}</div>
              <div className="setting-desc">当前：{modeLabel(ch.mode)}{ch.desktopApp ? ` · 客户端 ${ch.desktopApp}` : ''}{inboundKind(ch.kind) ? ` · ${inboundPairStatus(ch)}` : ''}</div>
            </div>
            {webhookKind(ch.kind) ? (
              ch.enabled
                ? <button disabled={busy} onClick={() => void apply({ kind: ch.kind, enabled: false }, `${ch.label}已关闭`)}>停用</button>
                : <button disabled={busy || !(drafts[ch.kind] ?? '').trim()} onClick={() => void apply({ kind: ch.kind, enabled: true, webhookUrl: (drafts[ch.kind] ?? '').trim(), testSend: true }, `${ch.label}已启用`)}>启用</button>
            ) : (
              <button disabled={busy} onClick={() => void apply(
                ch.enabled
                  ? { kind: ch.kind, enabled: false }
                  : { kind: ch.kind, enabled: true, probeDesktop: true },
                ch.enabled ? `${ch.label}已关闭` : `${ch.label}已启用`,
              )}>{ch.enabled ? '停用' : '启用'}</button>
            )}
          </div>
          {webhookKind(ch.kind) ? (
            <div style={{ display: 'flex', gap: 8, flexWrap: 'wrap', marginTop: 8 }}>
              <input className="setting-input" style={{ flex: 1, minWidth: 240, fontFamily: 'var(--mono)', fontSize: 12 }}
                value={drafts[ch.kind] ?? ''} placeholder={webhookPlaceholder(ch.kind)}
                aria-label={`${ch.label} Webhook 地址`}
                onChange={ev => setDrafts(d => ({ ...d, [ch.kind]: ev.target.value }))} />
              <button disabled={busy} onClick={() => void apply({ kind: ch.kind, enabled: true, webhookUrl: (drafts[ch.kind] ?? '').trim(), testSend: true }, `${ch.label} Webhook 已保存`)}>保存{ch.label} Webhook</button>
            </div>
          ) : (
            <div className="setting-desc">本机已登录客户端代发，不能收消息。启用时会检测本机是否装了{ch.desktopApp}。没有安装或未登录时，发送会立刻说无法执行，不会假装发出去。</div>
          )}
          {inboundKind(ch.kind) ? (
            <div style={{ display: 'grid', gap: 8, marginTop: 10 }}>
              <div className="setting-desc">{inboundPairStatus(ch)}。填 App ID / Secret 后连接。名单为空时第一条发信人会写入白名单，之后只收已配对的人。都不监听公网端口。</div>
              <div style={{ display: 'flex', gap: 8, flexWrap: 'wrap' }}>
                <input className="setting-input" style={{ flex: 1, minWidth: 160, fontFamily: 'var(--mono)', fontSize: 12 }}
                  value={appIdDrafts[ch.kind] ?? ''} placeholder={ch.kind === 'wecom' ? '智能机器人 Bot ID' : ch.kind === 'dingtalk' ? '应用 AppKey' : '应用 App ID'}
                  aria-label={ch.kind === 'wecom' ? `${ch.label} 入站 Bot ID` : `${ch.label} 入站 App ID`}
                  onChange={ev => setAppIdDrafts(d => ({ ...d, [ch.kind]: ev.target.value }))} />
                <input className="setting-input" style={{ flex: 1, minWidth: 160, fontFamily: 'var(--mono)', fontSize: 12 }} type="password"
                  value={secretDrafts[ch.kind] ?? ''} placeholder={ch.inboundHasSecret ? '已保存，留空不改' : (ch.kind === 'wecom' ? 'Bot Secret' : '应用 App Secret')}
                  aria-label={ch.kind === 'wecom' ? `${ch.label} 入站 Bot Secret` : `${ch.label} 入站 App Secret`}
                  onChange={ev => setSecretDrafts(d => ({ ...d, [ch.kind]: ev.target.value }))} />
              </div>
              <label className="setting-desc" style={{ display: 'flex', gap: 8, alignItems: 'center' }}>
                <input type="checkbox" checked={ch.inboundAutoRun} disabled={busy || !inboundPaired(ch)}
                  aria-label={`${ch.label} 入站自动执行`}
                  onChange={ev => void apply({ kind: ch.kind, inboundAutoRun: ev.target.checked }, ev.target.checked ? `${ch.label}入站将自动执行` : `${ch.label}入站只写入会话`)} />
                配对后再自动跑一轮对话（默认关闭，只写入「{ch.label} · 入站」会话）
              </label>
              <div style={{ display: 'flex', gap: 8, flexWrap: 'wrap' }}>
                <button disabled={busy || (!ch.inboundEnabled && !(appIdDrafts[ch.kind] ?? '').trim() && !ch.inboundAppId)} onClick={() => void apply({
                  kind: ch.kind,
                  inboundEnabled: !ch.inboundEnabled,
                  inboundAppId: (appIdDrafts[ch.kind] ?? '').trim(),
                  ...(secretDrafts[ch.kind]?.trim() ? { inboundAppSecret: secretDrafts[ch.kind].trim() } : {}),
                }, ch.inboundEnabled ? `${ch.label}入站已关闭` : `${ch.label}已连接，等待第一条消息配对`)}>{ch.inboundEnabled ? '关闭入站' : '连接并等待配对'}</button>
                <button disabled={busy} onClick={() => void apply({
                  kind: ch.kind,
                  inboundAppId: (appIdDrafts[ch.kind] ?? '').trim(),
                  ...(secretDrafts[ch.kind]?.trim() ? { inboundAppSecret: secretDrafts[ch.kind].trim() } : {}),
                }, `${ch.label}入站凭证已保存`)}>保存入站凭证</button>
              </div>
            </div>
          ) : null}
        </div>
      ))}
      {status && <p className="notice" role="status">{status}</p>}
    </div>
  )
}
