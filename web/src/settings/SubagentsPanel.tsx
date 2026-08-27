import React, { useEffect, useState } from 'react'
import { providerBridge } from '../bridge/client'
import type { ProviderDTO } from '../generated/bridge'
import { ChoiceTiles } from './ChoiceTiles'
import {
  BUILTIN_SUBAGENT_IDS,
  BUILTIN_SUBAGENT_META,
  READ_CAP_OPTIONS,
  configuredModelOptions,
  defaultSubagentSettings,
  inheritModelLabel,
  loadSubagentSettings,
  saveSubagentSettings,
  type CustomSubagentProfile,
  type SubagentDelegationMode,
  type SubagentSettings,
} from './subagentSettings'

const DELEGATION_OPTIONS = [
  { value: 'proactive', label: '主动委派（推荐）', desc: '复杂只读调研优先派出，结果汇总到主对话。' },
  { value: 'explicit', label: '按需委派', desc: '暴露工具，由模型自行决定是否派出。' },
  { value: 'disabled', label: '关闭', desc: '隐藏 subagent.spawn / join。' },
] as const

function Switch({ on, onChange, label }: { on: boolean; onChange: (v: boolean) => void; label: string }): React.JSX.Element {
  return (
    <button
      type="button"
      className={`toggle ${on ? 'on' : ''}`}
      onClick={() => onChange(!on)}
      role="switch"
      aria-checked={on}
      aria-label={label}
    >
      <span className="toggle-knob" />
    </button>
  )
}

export function SubagentsPanel({ onSaved }: { onSaved?: () => void }): React.JSX.Element {
  const [settings, setSettings] = useState<SubagentSettings>(() => loadSubagentSettings())
  const [providers, setProviders] = useState<ProviderDTO[]>([])
  const [draft, setDraft] = useState<CustomSubagentProfile>({
    id: '', displayName: '', description: '', systemPrompt: '', readCaps: ['web.search', 'web.fetch'], maxSteps: 4, budgetTokens: 8192,
  })

  useEffect(() => {
    void providerBridge.list().then(r => setProviders(r.items)).catch(() => {})
  }, [])

  const persist = (next: SubagentSettings) => {
    setSettings(next)
    saveSubagentSettings(next)
    onSaved?.()
  }

  const setMode = (delegationMode: SubagentDelegationMode) => persist({ ...settings, delegationMode })

  const setOverride = (id: string, patch: Partial<SubagentSettings['overrides'][string]>) => {
    persist({ ...settings, overrides: { ...settings.overrides, [id]: { ...settings.overrides[id], ...patch } } })
  }

  const models = configuredModelOptions(providers)

  const addCustom = () => {
    const id = draft.id.trim().replace(/\s+/g, '-').slice(0, 48)
    if (!id || !draft.systemPrompt.trim()) return
    if (settings.customProfiles.some(p => p.id === id) || BUILTIN_SUBAGENT_IDS.includes(id as typeof BUILTIN_SUBAGENT_IDS[number])) return
    persist({
      ...settings,
      customProfiles: [...settings.customProfiles, {
        id,
        displayName: draft.displayName.trim() || id,
        description: draft.description?.trim(),
        systemPrompt: draft.systemPrompt.trim(),
        readCaps: draft.readCaps.length ? draft.readCaps : ['web.search'],
        maxSteps: draft.maxSteps ?? 4,
        budgetTokens: draft.budgetTokens ?? 8192,
      }],
    })
    setDraft({ id: '', displayName: '', description: '', systemPrompt: '', readCaps: ['web.search', 'web.fetch'], maxSteps: 4, budgetTokens: 8192 })
  }

  const removeCustom = (id: string) => persist({ ...settings, customProfiles: settings.customProfiles.filter(p => p.id !== id) })

  return (
    <div className="setting-group subagent-settings">
      <ChoiceTiles
        legend="委派策略"
        name="delegation-mode"
        value={settings.delegationMode}
        options={DELEGATION_OPTIONS}
        onChange={setMode}
      />

      <h3>内置子智能体 · {BUILTIN_SUBAGENT_IDS.length} 项</h3>
      <p className="setting-desc">对齐 Zed / Cursor 常见分型；可为每个 profile 指定更便宜的模型。</p>
      <div className="subagent-profile-list">
        {BUILTIN_SUBAGENT_IDS.map(id => {
          const meta = BUILTIN_SUBAGENT_META[id]
          const ov = settings.overrides[id] ?? { enabled: true }
          const modelValue = ov.providerId && ov.modelId ? `${ov.providerId}\u0000${ov.modelId}` : ''
          return (
            <article key={id} className="subagent-profile-row">
              <div className="subagent-profile-id">
                <b>{meta.label}</b>
                <code>{id}</code>
                <small>{meta.desc} · {meta.tools}</small>
              </div>
              <label className="subagent-model-row">
                模型
                <select
                  value={modelValue}
                  onChange={e => {
                    if (!e.target.value) {
                      setOverride(id, { providerId: '', modelId: '' })
                      return
                    }
                    const hit = models.find(m => m.value === e.target.value)
                    if (hit) setOverride(id, { providerId: hit.providerId, modelId: hit.modelId })
                  }}
                >
                  <option value="">{inheritModelLabel()}</option>
                  {models.map(m => <option key={m.value} value={m.value}>{m.label}</option>)}
                </select>
              </label>
              <Switch
                on={ov.enabled !== false}
                onChange={enabled => setOverride(id, { enabled })}
                label={`启用 ${meta.label}`}
              />
            </article>
          )
        })}
      </div>

      <h3>自定义子智能体 · {settings.customProfiles.length} 项</h3>
      <p className="setting-desc">保存到本机；随 chat.start 传给引擎（最多 16 个）。</p>
      {settings.customProfiles.length > 0 && (
        <ul className="subagent-custom-list">
          {settings.customProfiles.map(p => (
            <li key={p.id}>
              <b>{p.displayName}</b> <code>{p.id}</code>
              <button type="button" onClick={() => removeCustom(p.id)}>删除</button>
            </li>
          ))}
        </ul>
      )}
      <div className="subagent-custom-form">
        <label>ID<input value={draft.id} onChange={e => setDraft(v => ({ ...v, id: e.target.value }))} placeholder="my-research" /></label>
        <label>名称<input value={draft.displayName} onChange={e => setDraft(v => ({ ...v, displayName: e.target.value }))} /></label>
        <label>系统提示<textarea rows={3} value={draft.systemPrompt} onChange={e => setDraft(v => ({ ...v, systemPrompt: e.target.value }))} /></label>
        <fieldset>
          <legend>readCaps</legend>
          {READ_CAP_OPTIONS.map(cap => (
            <label key={cap} className="subagent-cap-chip">
              <input
                type="checkbox"
                checked={draft.readCaps.includes(cap)}
                onChange={e => setDraft(v => ({
                  ...v,
                  readCaps: e.target.checked ? [...v.readCaps, cap] : v.readCaps.filter(x => x !== cap),
                }))}
              />
              {cap}
            </label>
          ))}
        </fieldset>
        <button type="button" className="primary" onClick={addCustom}>添加自定义 profile</button>
      </div>

      <button type="button" onClick={() => persist(defaultSubagentSettings())}>恢复默认</button>
    </div>
  )
}
