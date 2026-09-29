import React, { useEffect, useState } from 'react'
import { BridgeClientError, createMutationAttempt, getCapabilityRolesBridge, getProviderBridge, type CapabilityRoleName, type CapabilityRoleRow, type CapabilityRolesBridge, type ProviderBridge } from '../bridge/client'
import type { ProviderDTO } from '../generated/bridge'
import { modelKind } from '../provider/modelKind'

const ROLES: { id: CapabilityRoleName; label: string; hint: string }[] = [
  { id: 'chat', label: '对话缺省', hint: '空=会话选择器；只作无会话模型时的缺省' },
  { id: 'flash', label: 'Flash', hint: '空=启发式且不跑分类；绑定后才用该模型做路由分类' },
  { id: 'vision', label: '视觉', hint: '描述与 SoM 读号' },
  { id: 'embed', label: '向量', hint: '空=自动用目录里的 embedding；绑定后只走该模型' },
  { id: 'judge', label: 'Judge', hint: '验证 Complete；与 chat 相同必须勾选允许' },
  { id: 'gui', label: 'GUI', hint: '只作兜底，模型看不见' },
]

type Option = { value: string; label: string; providerId: string; modelId: string }

function optionsFor(role: CapabilityRoleName, providers: ProviderDTO[]): Option[] {
  const out: Option[] = []
  for (const p of providers) {
    for (const m of p.models) {
      const kind = modelKind(m)
      const ok =
        (role === 'chat' || role === 'flash' || role === 'judge') && kind === 'llm' ||
        (role === 'vision' && (kind === 'vision' || (kind === 'llm' && !!m.supportsVision))) ||
        (role === 'embed' && kind === 'embedding') ||
        (role === 'gui' && kind === 'gui')
      if (!ok) continue
      out.push({ value: `${p.id}\u0000${m.modelId}`, label: `${p.name} / ${m.displayName || m.modelId}`, providerId: p.id, modelId: m.modelId })
    }
  }
  return out
}

export function CapabilityRouting({ providers, roles }: { providers?: ProviderBridge; roles?: CapabilityRolesBridge }): React.JSX.Element {
  const providerApi = providers ?? getProviderBridge()
  const roleApi = roles ?? getCapabilityRolesBridge()
  const [items, setItems] = useState<ProviderDTO[]>([])
  const [rows, setRows] = useState<CapabilityRoleRow[]>([])
  const [error, setError] = useState('')
  const [notice, setNotice] = useState('')
  const [busy, setBusy] = useState(false)

  const load = async () => {
    try {
      const [listed, got] = await Promise.all([providerApi.list(), roleApi.get()])
      setItems(listed.items)
      setRows(got.roles)
      setError('')
    } catch (e) {
      setError(e instanceof BridgeClientError ? e.message : '能力路由载入失败')
    }
  }

  useEffect(() => { void load() }, [])

  const patch = (role: CapabilityRoleName, next: Partial<CapabilityRoleRow>) => {
    setRows(current => current.map(row => row.role === role ? { ...row, ...next } : row))
  }

  const save = async () => {
    setBusy(true)
    setNotice('')
    setError('')
    try {
      const payload = { roles: rows.map(row => ({
        role: row.role,
        ...(row.providerId && row.modelId ? { providerId: row.providerId, modelId: row.modelId } : {}),
        allowJudgeEqChat: row.allowJudgeEqChat,
      })) }
      const saved = await roleApi.set(payload, { attempt: createMutationAttempt('capability.roles.set', payload) })
      setRows(saved.roles)
      setNotice('能力路由已保存')
    } catch (e) {
      setError(e instanceof BridgeClientError ? e.message : '能力路由保存失败')
    } finally {
      setBusy(false)
    }
  }

  return (
    <section className="capability-routing" aria-label="能力路由">
      <h3>能力路由</h3>
      <p className="setting-desc">六个角色空着就是自动。不新增设置页，只挂在模型与供应商上面。</p>
      {rows.map(row => {
        const opts = optionsFor(row.role, items)
        const value = row.providerId && row.modelId ? `${row.providerId}\u0000${row.modelId}` : ''
        const meta = ROLES.find(r => r.id === row.role)
        return (
          <label key={row.role} className="capability-role-row">
            <span>{meta?.label ?? row.role}</span>
            <select
              aria-label={meta?.label ?? row.role}
              value={value}
              disabled={busy}
              onChange={e => {
                const hit = opts.find(o => o.value === e.target.value)
                patch(row.role, { providerId: hit?.providerId ?? '', modelId: hit?.modelId ?? '' })
              }}
            >
              <option value="">自动</option>
              {opts.map(o => <option key={o.value} value={o.value}>{o.label}</option>)}
            </select>
            <small>{meta?.hint}</small>
            {row.role === 'judge' && (
              <label className="default">
                <input
                  type="checkbox"
                  checked={row.allowJudgeEqChat}
                  disabled={busy}
                  onChange={e => patch('judge', { allowJudgeEqChat: e.target.checked })}
                />
                允许 judge 与 chat 相同
              </label>
            )}
          </label>
        )
      })}
      {error && <p role="alert">{error}</p>}
      {notice && <p role="status">{notice}</p>}
      <button type="button" className="primary" disabled={busy || rows.length !== 6} onClick={() => void save()}>保存能力路由</button>
    </section>
  )
}
