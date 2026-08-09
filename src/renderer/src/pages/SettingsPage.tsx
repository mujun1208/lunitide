import { useEffect, useState } from 'react'
import type { ProviderConfig, ProviderInput, ProviderProtocol, ProviderTestResult } from '../../../shared/models'

const defaults: Record<ProviderProtocol, Pick<ProviderInput, 'baseUrl' | 'models' | 'defaultModel'>> = {
  openai: { baseUrl: 'https://api.openai.com/v1', models: ['gpt-4o-mini'], defaultModel: 'gpt-4o-mini' },
  anthropic: { baseUrl: 'https://api.anthropic.com', models: ['claude-3-5-haiku-latest'], defaultModel: 'claude-3-5-haiku-latest' }
}
const createEmptyForm = (): ProviderInput => ({ name: '', protocol: 'openai', ...defaults.openai, apiKey: '' })

export function SettingsPage(): React.JSX.Element {
  const [providers, setProviders] = useState<ProviderConfig[]>([])
  const [form, setForm] = useState<ProviderInput>(createEmptyForm)
  const [showApiKey, setShowApiKey] = useState(false)
  const [busy, setBusy] = useState(false)
  const [message, setMessage] = useState('')
  const [testModels, setTestModels] = useState<Record<string, string>>({})
  const [testResults, setTestResults] = useState<Record<string, ProviderTestResult>>({})
  const [testingIds, setTestingIds] = useState<Set<string>>(new Set())
  const [originalIdentity, setOriginalIdentity] = useState<{ protocol: ProviderProtocol; baseUrl: string }>()

  const refresh = (): void => {
    void window.lunitide.listProviders().then((items) => {
      setProviders(items)
      setTestModels(Object.fromEntries(items.map((provider) => [provider.id, provider.defaultModel])))
    }).catch((error: unknown) => setMessage(error instanceof Error ? error.message : '读取供应商配置失败'))
  }
  useEffect(refresh, [])

  const resetForm = (): void => {
    setForm(createEmptyForm())
    setShowApiKey(false)
    setOriginalIdentity(undefined)
    setMessage('')
  }

  const changeProtocol = (protocol: ProviderProtocol): void => {
    setForm({ ...form, protocol, ...defaults[protocol], apiKey: '' })
    setShowApiKey(false)
  }

  const edit = async (provider: ProviderConfig): Promise<void> => {
    setBusy(true); setMessage('正在安全读取 API Key…'); setShowApiKey(false)
    setForm({ id: provider.id, name: provider.name, protocol: provider.protocol, baseUrl: provider.baseUrl, models: [...provider.models], defaultModel: provider.defaultModel, apiKey: '' })
    setOriginalIdentity({ protocol: provider.protocol, baseUrl: provider.baseUrl })
    try {
      const secret = await window.lunitide.revealProviderApiKey(provider.id)
      setForm((current) => current.id === provider.id ? { ...current, apiKey: secret.apiKey } : current)
      setMessage('API Key 已读取并默认隐藏，点击眼睛可查看')
    } catch (error) {
      setMessage(`${error instanceof Error ? error.message : '读取 API Key 失败'}，请在下方重新输入`)
    } finally { setBusy(false) }
  }

  const updateModel = (index: number, value: string): void => {
    const models = form.models.map((model, current) => current === index ? value : model)
    const defaultModel = form.defaultModel === form.models[index] ? value : form.defaultModel
    setForm({ ...form, models, defaultModel })
  }
  const addModel = (): void => setForm({ ...form, models: [...form.models, ''] })
  const removeModel = (index: number): void => {
    if (form.models.length === 1) return
    const removed = form.models[index]
    const models = form.models.filter((_, current) => current !== index)
    setForm({ ...form, models, defaultModel: form.defaultModel === removed ? models[0] : form.defaultModel })
  }

  const fetchModels = async (): Promise<void> => {
    if (!form.id) return
    setBusy(true); setMessage('正在从上游获取模型列表…')
    try {
      const result = await window.lunitide.fetchProviderModels(form.id)
      setForm((current) => current.id === form.id ? { ...current, models: result.models, defaultModel: result.models.includes(current.defaultModel) ? current.defaultModel : result.models[0] } : current)
      setMessage(result.detail)
    } catch (error) {
      setMessage(error instanceof Error ? error.message : '获取模型列表失败')
    } finally { setBusy(false) }
  }

  const save = async (): Promise<void> => {
    setBusy(true); setMessage('')
    try {
      if (originalIdentity) {
        const previousOrigin = new URL(originalIdentity.baseUrl).origin
        const nextOrigin = new URL(form.baseUrl).origin
        if ((originalIdentity.protocol !== form.protocol || previousOrigin !== nextOrigin) && !confirm(`服务来源将从 ${previousOrigin} 改为 ${nextOrigin}。确认将当前 API Key 用于新服务吗？`)) return
      }
      const saved = await window.lunitide.saveProvider({ ...form, models: form.models.map((model) => model.trim()), defaultModel: form.defaultModel.trim() })
      setProviders((current) => [...current.filter((provider) => provider.id !== saved.id), saved])
      setTestModels((current) => ({ ...current, [saved.id]: saved.defaultModel }))
      resetForm()
      setMessage(saved.persistent ? '供应商、模型列表与 API Key 已加密保存' : '供应商已保存；API Key 仅在本次会话有效')
    } catch (error) {
      setMessage(error instanceof Error ? error.message : '保存失败')
    } finally { setBusy(false) }
  }

  const remove = async (id: string): Promise<void> => {
    if (!confirm('确定删除这个供应商配置吗？')) return
    try {
      await window.lunitide.deleteProvider(id)
      setProviders((current) => current.filter((provider) => provider.id !== id))
      if (form.id === id) resetForm()
    } catch (error) { setMessage(error instanceof Error ? error.message : '删除失败') }
  }

  const test = async (provider: ProviderConfig): Promise<void> => {
    setTestingIds((current) => new Set(current).add(provider.id))
    const model = testModels[provider.id] || provider.defaultModel
    setTestResults((current) => ({ ...current, [provider.id]: { ok: false, detail: `正在使用 ${model} 发起真实请求…` } }))
    try {
      const result = await window.lunitide.testProvider(provider.id, model)
      setTestResults((current) => ({ ...current, [provider.id]: result }))
    } catch (error) {
      setTestResults((current) => ({ ...current, [provider.id]: { ok: false, detail: error instanceof Error ? error.message : '连接测试失败', model } }))
    } finally { setTestingIds((current) => { const next = new Set(current); next.delete(provider.id); return next }) }
  }

  const validModels = form.models.map((model) => model.trim()).filter(Boolean)
  const canSave = !busy && form.name.trim() && validModels.length === form.models.length && new Set(validModels).size === validModels.length && validModels.includes(form.defaultModel.trim()) && form.apiKey?.trim()

  return <>
    <header className="topbar"><div><p className="eyebrow">MODEL GATEWAY · PROVIDERS</p><h1>供应商管理</h1><p className="page-subtitle">管理 API 供应商、密钥与模型列表</p></div></header>
    <section className="settings-grid">
      <article className="panel provider-list">
        <div className="panel-title"><span>供应商列表</span><small>{providers.length} 个配置</small></div>
        {providers.length === 0 && <div className="empty-state">尚未配置供应商，请在右侧添加。</div>}
        {providers.map((provider) => {
          const result = testResults[provider.id]
          return <div className="provider-card" key={provider.id}>
            <div><b>{provider.name}</b><span className="protocol-badge">{provider.protocol === 'openai' ? 'OpenAI Compatible' : 'Anthropic'}</span></div>
            <small>{provider.baseUrl}</small>
            <div className="provider-models">{provider.models.map((model) => <span className={model === provider.defaultModel ? 'model-chip default' : 'model-chip'} key={model}>{model}{model === provider.defaultModel ? ' · 默认' : ''}</span>)}</div>
            <div className="provider-meta"><span>{provider.hasApiKey ? '● 密钥已配置' : '○ 缺少密钥'}</span><span>{provider.persistent ? '系统加密存储' : '仅本次会话'}</span></div>
            <div className="test-row"><select value={testModels[provider.id] || provider.defaultModel} onChange={(event) => setTestModels({ ...testModels, [provider.id]: event.target.value })}>{provider.models.map((model) => <option key={model}>{model}</option>)}</select><button className="test-button" disabled={testingIds.has(provider.id)} onClick={() => void test(provider)}>{testingIds.has(provider.id) ? '测试中…' : '测试连接'}</button></div>
            {result && <div className={result.ok ? 'test-result ok' : 'test-result error'}><b>{result.ok ? '✓ 连接成功' : '✕ 连接失败'}</b><span>{result.detail}</span><small>{[result.model, result.httpStatus && `HTTP ${result.httpStatus}`, result.latencyMs !== undefined && `${result.latencyMs} ms`, result.checkedAt && new Date(result.checkedAt).toLocaleString()].filter(Boolean).join(' · ')}</small></div>}
            <div className="card-actions"><button onClick={() => void edit(provider)}>编辑</button><button className="danger" onClick={() => void remove(provider.id)}>删除</button></div>
          </div>
        })}
      </article>

      <article className="panel provider-form">
        <div className="panel-title"><span>{form.id ? '编辑供应商' : '添加供应商'}</span>{form.id && <button className="link-button" onClick={resetForm}>取消编辑</button>}</div>
        <label>显示名称<input value={form.name} placeholder="例如：DeepSeek" onChange={(event) => setForm({ ...form, name: event.target.value })} /></label>
        <label>协议类型<select value={form.protocol} onChange={(event) => changeProtocol(event.target.value as ProviderProtocol)}><option value="openai">OpenAI Compatible</option><option value="anthropic">Anthropic</option></select></label>
        <label>Base URL<input value={form.baseUrl} onChange={(event) => setForm({ ...form, baseUrl: event.target.value })} /></label>
        <label>API Key<div className="secret-field"><input type={showApiKey ? 'text' : 'password'} autoComplete="off" value={form.apiKey ?? ''} placeholder="输入 API Key" onChange={(event) => setForm({ ...form, apiKey: event.target.value })} /><button type="button" aria-label={showApiKey ? '隐藏 API Key' : '显示 API Key'} onClick={() => setShowApiKey(!showApiKey)}>{showApiKey ? '隐藏' : '👁'}</button></div></label>
        <p className="security-note">🔒 API Key 使用 Windows 系统凭据加密保存。编辑时会安全读取并默认隐藏，不写入日志。</p>
        <div className="models-heading"><b>模型列表</b><small>选择一个默认模型</small></div>
        <div className="model-editor">{form.models.map((model, index) => <div className="model-row" key={index}><input value={model} placeholder="模型 ID" onChange={(event) => updateModel(index, event.target.value)} /><label className="default-radio"><input type="radio" name="default-model" checked={form.defaultModel === model} disabled={!model.trim()} onChange={() => setForm({ ...form, defaultModel: model })} />默认</label><button type="button" disabled={form.models.length === 1} onClick={() => removeModel(index)}>删除</button></div>)}</div>
        <div className="model-actions"><button className="add-model" type="button" onClick={addModel}>＋ 添加模型</button><button className="add-model" type="button" disabled={!form.id || busy} onClick={() => void fetchModels()}>↓ 从上游获取</button></div>
        <button className="primary-button" disabled={!canSave} onClick={() => void save()}>{busy ? '处理中…' : '保存供应商'}</button>
        {message && <div className="form-message">{message}</div>}
      </article>
    </section>
  </>
}
