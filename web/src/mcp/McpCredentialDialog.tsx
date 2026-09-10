import React, { useRef, useState } from 'react'
import type { McpCredentialSetPayload, McpCredentialSetResult, McpListResult } from '../generated/bridge'
import { Dialog } from '../ui/Dialog'

function mcpCredentialUserError(err: unknown, fallback: string): string {
  const detail = err instanceof Error ? err.message.trim() : ''
  return /[\u4e00-\u9fff]/.test(detail) ? detail : fallback
}

type Endpoint = McpListResult['endpoints'][number]
type Props = {
  endpoint: Endpoint
  suggestedEnvs?: readonly string[]
  save: (payload: McpCredentialSetPayload) => Promise<McpCredentialSetResult>
  onClose: () => void
  onSaved: () => void
}

export function McpCredentialDialog({ endpoint, suggestedEnvs = [], save, onClose, onSaved }: Props): React.JSX.Element {
  const [value, setValue] = useState('')
  const [env, setEnv] = useState(suggestedEnvs[0] ?? '')
  const [version, setVersion] = useState(endpoint.securityVersion)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  const [notice, setNotice] = useState('')
  const attempt = useRef<{ requestId: string; env: string; remove: boolean } | null>(null)
  const inFlight = useRef(false)

  const submit = async (remove: boolean, keepOpen = false) => {
    if (inFlight.current) return
    if (version === undefined) { setError('请刷新 MCP 清单后重试'); return }
    if (endpoint.transport === 'stdio' && !/^[A-Z][A-Z0-9_]{0,63}$/.test(env)) {
      setError('请填写凭据环境变量名称，例如 API_TOKEN'); return
    }
    if (!attempt.current || attempt.current.env !== env || attempt.current.remove !== remove) {
      attempt.current = { requestId: crypto.randomUUID(), env, remove }
    }
    const credential = value
    inFlight.current = true
    setValue(''); setBusy(true); setError(''); setNotice('')
    try {
      const result = await save({
        endpointId: endpoint.endpointId, expectedVersion: version, requestId: attempt.current.requestId,
        ...(endpoint.transport === 'stdio' ? { env } : {}),
        ...(remove ? { remove: true as const } : { credential }),
      })
      setVersion(result.securityVersion)
      attempt.current = null
      onSaved()
      if (keepOpen) {
        setNotice(`已保存 ${env}，可以继续配置下一项`)
        setEnv(suggestedEnvs[suggestedEnvs.indexOf(env) + 1] ?? '')
      } else onClose()
    } catch (e) {
      setError(`${mcpCredentialUserError(e, '凭据保存失败')}。如结果待确认，请重新输入相同凭据重试。`)
    } finally { inFlight.current = false; setBusy(false) }
  }

  return <Dialog open title="MCP 凭据" description="凭据保存在本机。配置完成后，在已安装列表中重新连接。" onClose={() => { if (!inFlight.current) onClose() }}>
    <form onSubmit={e => { e.preventDefault(); void submit(false) }}>
      <p>{endpoint.displayName || endpoint.endpointId} · {endpoint.credentialConfigured ? '已配置凭据' : '未配置凭据'}</p>
      {endpoint.transport === 'stdio' && <>
        <label>环境变量名称<input aria-label="凭据环境变量" list="mcp-credential-envs" value={env} disabled={busy} onChange={e => setEnv(e.target.value.trim())} placeholder="API_TOKEN" /></label>
        <datalist id="mcp-credential-envs">{suggestedEnvs.map(name => <option key={name} value={name} />)}</datalist>
        {suggestedEnvs.length > 0 && <p className="setting-desc">此服务使用：{suggestedEnvs.join('、')}。按官方说明填写必需项。</p>}
      </>}
      <label>{endpoint.transport === 'https' ? '服务访问令牌' : '环境变量值'}<input aria-label="MCP 凭据值" type="password" autoComplete="off" value={value} disabled={busy} onChange={e => setValue(e.target.value)} maxLength={16384} /></label>
      {error && <p role="alert">{error}</p>}
      {notice && <p role="status">{notice}</p>}
      <div className="dialog-actions">
        <button type="button" disabled={busy} onClick={onClose}>取消</button>
        <button type="button" disabled={busy || version === undefined} onClick={() => void submit(true)}>撤销凭据</button>
        {endpoint.transport === 'stdio' && <button type="button" disabled={busy || !value || version === undefined} onClick={() => void submit(false, true)}>保存并继续配置</button>}
        <button className="primary" disabled={busy || !value || version === undefined}>{busy ? '等待确认…' : '保存凭据'}</button>
      </div>
    </form>
  </Dialog>
}
