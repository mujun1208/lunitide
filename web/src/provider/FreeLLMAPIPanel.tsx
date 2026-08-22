import React, { useCallback, useEffect, useState } from 'react'
import { providerBridge } from '../bridge/client'
import {
  FREE_LLM_API_DEFAULT_ORIGIN,
  freeLLMAPIDashboardUrl,
  installFreeLLMAPIProvider,
  loadFreeLLMAPIHub,
  probeFreeLLMAPI,
  saveFreeLLMAPIHub,
  type FreeLLMAPIHubConfig,
} from './freeLlmApi'

export function FreeLLMAPIPanel({ onInstalled }: { onInstalled?: () => void }): React.JSX.Element {
  const [hub, setHub] = useState<FreeLLMAPIHubConfig>(() => loadFreeLLMAPIHub())
  const [apiKey, setApiKey] = useState('')
  const [busy, setBusy] = useState(false)
  const [notice, setNotice] = useState('')
  const [error, setError] = useState('')

  const refreshHealth = useCallback(async (silent = false) => {
    if (!apiKey.trim() && !hub.providerId) return
    if (!silent) setBusy(true)
    try {
      const health = await probeFreeLLMAPI(hub.baseUrl, apiKey)
      const next = { ...hub, lastHealth: health }
      setHub(next)
      saveFreeLLMAPIHub(next)
      if (!silent) setNotice(health.ok ? health.message ?? '连接正常' : health.message ?? '连接失败')
      if (!silent && !health.ok) setError(health.message ?? '连接失败')
      else if (!silent) setError('')
    } finally {
      if (!silent) setBusy(false)
    }
  }, [apiKey, hub])

  useEffect(() => {
    const timer = window.setInterval(() => { void refreshHealth(true) }, 60_000)
    return () => window.clearInterval(timer)
  }, [refreshHealth])

  const install = async () => {
    setBusy(true)
    setError('')
    setNotice('')
    try {
      const result = await installFreeLLMAPIProvider(hub.baseUrl, apiKey, providerBridge, hub.providerId)
      setHub(result.hub)
      setNotice(`已安装到 Lunitide · ${result.provider.models.length} 个模型可在对话中选择`)
      onInstalled?.()
    } catch (e) {
      setError(e instanceof Error ? e.message : '安装失败')
    } finally {
      setBusy(false)
    }
  }

  const usage = hub.monthlyUsage
  const health = hub.lastHealth

  return (
    <section className="freellm-panel" aria-label="FreeLLMAPI 免费池">
      <header className="freellm-panel-head">
        <div>
          <h3>FreeLLMAPI 免费池</h3>
          <p>
            真实可用的开源路由：把 Groq、Google、Cerebras 等 29 家厂商的<strong>免费额度</strong>聚合成一个 OpenAI 兼容端点。
            需先在本地运行 FreeLLMAPI，并在其 Dashboard 填入各厂商 Key；Lunitide 只连接你的本地路由，不代管上游账号。
          </p>
        </div>
        <a className="freellm-link" href="https://github.com/tashfeenahmed/freellmapi" target="_blank" rel="noreferrer">
          项目文档 ↗
        </a>
      </header>

      <div className="freellm-grid">
        <label>
          路由地址
          <input
            value={hub.baseUrl}
            onChange={e => setHub(v => ({ ...v, baseUrl: e.target.value }))}
            placeholder={FREE_LLM_API_DEFAULT_ORIGIN}
          />
          <small>默认 Docker/本地服务：<code>{FREE_LLM_API_DEFAULT_ORIGIN}/v1</code></small>
        </label>
        <label>
          统一 API Key
          <input
            type="password"
            autoComplete="off"
            value={apiKey}
            onChange={e => setApiKey(e.target.value)}
            placeholder="freellmapi-…（Dashboard → Keys）"
          />
        </label>
      </div>

      <div className="freellm-actions">
        <button type="button" disabled={busy || !apiKey.trim()} onClick={() => void refreshHealth(false)}>
          检测连接
        </button>
        <button type="button" className="primary" disabled={busy || !apiKey.trim()} onClick={() => void install()}>
          {hub.providerId ? '更新 Lunitide 供应商' : '安装到 Lunitide'}
        </button>
        <button
          type="button"
          disabled={busy}
          onClick={() => {
            const url = freeLLMAPIDashboardUrl(hub.baseUrl)
            window.open(url, '_blank', 'noopener,noreferrer')
          }}
        >
          打开 Dashboard
        </button>
      </div>

      <div className="freellm-status">
        <div className={`freellm-health ${health?.ok ? 'ok' : health ? 'bad' : ''}`} role="status">
          <b>{health?.ok ? '在线' : health ? '离线' : '未检测'}</b>
          <span>{health?.message ?? '安装或检测后显示路由状态'}</span>
          {health?.checkedAt && <small>{new Date(health.checkedAt).toLocaleString()}</small>}
        </div>
        <div className="freellm-usage" role="status">
          <b>本月经 Lunitide 统计</b>
          {usage ? (
            <span>
              {usage.month} · 请求 {usage.requests} · 输入 {usage.inputTokens.toLocaleString()} · 输出 {usage.outputTokens.toLocaleString()} tokens
            </span>
          ) : (
            <span>选择 FreeLLMAPI 模型对话后自动累计（上游免费配额请在 FreeLLMAPI Dashboard 查看）</span>
          )}
        </div>
      </div>

      <ul className="freellm-notes">
        <li>自动切换与 failover 由 FreeLLMAPI 路由完成；模型选 <code>auto</code> / <code>auto:fast</code> / <code>auto:smart</code> 即可。</li>
        <li>「4B tokens/月」是各厂商免费额度<strong>叠加上限</strong>，需你在 FreeLLMAPI 中配置对应 Key 才会生效。</li>
        <li>个人实验用途；生产环境请使用付费 API。</li>
      </ul>

      {notice && <p className="notice" role="status">{notice}</p>}
      {error && <p className="error" role="alert">{error}</p>}
    </section>
  )
}
