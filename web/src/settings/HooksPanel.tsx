import React, { useEffect, useState } from 'react'
import { hooksPolicyBridge, type HooksPolicyBridge } from '../bridge/client'
import type { ToolsHooksPolicySetPayload } from '../generated/bridge'

// P3-B Hooks — 工具调用拦截规则（tools.hooksPolicy.*）。规则在工具执行前
// 评估：block 拒绝并回显原因；requireApproval 强制审批（即使自动模式）；
// allow 免除审批往返（不放宽命令白名单）。多规则命中时 block 最优先。
// 保存即校验并热生效（fail-closed：非法文档整体拒绝，不影响现运行规则）。
const HOOK_TOOLS = ['workspace.list', 'workspace.read', 'workspace.write', 'workspace.search', 'workspace.edit', 'todo.write', 'command.run', 'web.fetch', 'web.search', 'excel.gen', 'excel.parse', 'docx.gen', 'pptx.gen', 'pdf.gen', 'html.gen', 'desktop.open'] as const
const HOOK_DECISIONS = [
  { value: 'block', label: '拦截（拒绝执行）' },
  { value: 'requireApproval', label: '强制审批' },
  { value: 'allow', label: '免审批放行' },
] as const
interface HookEntry { id: string; events: string[]; tools: string[]; decision: string; message: string }
export function HooksPanel({ bridge = hooksPolicyBridge }: { bridge?: HooksPolicyBridge }): React.JSX.Element {
  const [entries, setEntries] = useState<HookEntry[]>([])
  const [events, setEvents] = useState<{ sessionId: string; toolName: string; hookId: string; event: string; decision: string; createdAt: string }[]>([])
  const [status, setStatus] = useState('')
  const [busy, setBusy] = useState(false)
  const [loaded, setLoaded] = useState(false)

  useEffect(() => {
    const load = async () => {
      setBusy(true)
      try {
        const [policy, recent] = await Promise.all([bridge.getHooksPolicy(), bridge.listHookEvents({ limit: 20 })])
        setEntries(policy.hooks.map(h => ({ id: h.id, events: [...h.events], tools: [...h.tools], decision: h.decision, message: h.message ?? '' })))
        setEvents(recent.events)
        setLoaded(true)
      } catch (e) { setStatus(e instanceof Error ? e.message : 'Hooks 规则读取失败') } finally { setBusy(false) }
    }
    void load()
  }, [])

  const refreshEvents = async () => {
    try { const r = await bridge.listHookEvents({ limit: 20 }); setEvents(r.events) } catch { /* 保持现状 */ }
  }

  const save = async () => {
    setBusy(true); setStatus('')
    try {
      const hooks = entries
        .map(e => ({ ...e, id: e.id.trim(), tools: e.tools.filter(Boolean) }))
        .filter(e => e.id && e.tools.length > 0)
      const r = await bridge.setHooksPolicy({ hooks: hooks as ToolsHooksPolicySetPayload['hooks'] })
      setStatus(`已保存并热生效：${r.applied} 条 Hook 规则。`)
      setEntries(hooks)
    } catch (e) { setStatus(e instanceof Error ? e.message : 'Hooks 规则保存失败（文档被整体拒绝，现运行规则不变）') } finally { setBusy(false) }
  }

  const toggleIn = (list: string[], value: string): string[] => list.includes(value) ? list.filter(x => x !== value) : [...list, value]
  const update = (i: number, patch: Partial<HookEntry>) => setEntries(entries.map((x, j) => j === i ? { ...x, ...patch } : x))

  return (
    <div className="setting-group">
      <div className="setting-group-title">Hooks 拦截规则（工具调用）</div>
      <div className="setting-row" style={{ gridTemplateColumns: '1fr' }}>
        <div className="setting-desc">在工具执行前拦截：拦截=拒绝并回显原因；强制审批=即使自动编辑/完全访问也走审批；免审批=跳过审批往返（命令白名单仍生效）。多条规则命中时按 拦截 &gt; 强制审批 &gt; 免审批 取最严。保存即校验并热生效，非法文档整体拒绝（fail-closed）。</div>
      </div>
      {entries.map((entry, i) => (
        <div className="setting-row" key={i} style={{ gridTemplateColumns: '1fr auto' }}>
          <div style={{ display: 'flex', gap: 8, flexWrap: 'wrap', alignItems: 'center' }}>
            <input className="setting-input" style={{ width: 150 }} placeholder="规则 ID（如 no-docx）" value={entry.id} maxLength={64} onChange={e => update(i, { id: e.target.value })} aria-label={`规则 ${i + 1} ID`} />
            <select className="setting-input" style={{ width: 150 }} value={entry.decision} onChange={e => update(i, { decision: e.target.value, message: e.target.value === 'block' ? entry.message : '' })} aria-label={`规则 ${i + 1} 动作`}>
              {HOOK_DECISIONS.map(d => <option key={d.value} value={d.value}>{d.label}</option>)}
            </select>
            {entry.decision === 'block' && <input className="setting-input" style={{ width: 220 }} placeholder="拒绝原因（必填）" value={entry.message} maxLength={256} onChange={e => update(i, { message: e.target.value })} aria-label={`规则 ${i + 1} 拒绝原因`} />}
          </div>
          <div style={{ display: 'flex', gap: 8, flexWrap: 'wrap', alignItems: 'center' }}>
            {HOOK_TOOLS.map(tool => (
              <label key={tool} style={{ display: 'inline-flex', gap: 4, alignItems: 'center', fontSize: 12 }}>
                <input type="checkbox" checked={entry.tools.includes(tool)} onChange={() => update(i, { tools: toggleIn(entry.tools, tool) })} aria-label={`规则 ${i + 1} 工具 ${tool}`} />
                {tool}
              </label>
            ))}
            <label style={{ display: 'inline-flex', gap: 4, alignItems: 'center', fontSize: 12 }}>
              <input type="checkbox" checked={entry.events.includes('afterToolCall')} onChange={() => update(i, { events: toggleIn(entry.events.includes('beforeToolCall') ? entry.events : ['beforeToolCall', ...entry.events], 'afterToolCall') })} aria-label={`规则 ${i + 1} 执行后审计`} />
              执行后审计
            </label>
            <button disabled={busy} onClick={() => setEntries(entries.filter((_, j) => j !== i))} aria-label={`删除规则 ${i + 1}`}>删除</button>
          </div>
        </div>
      ))}
      <div className="setting-row">
        <div className="setting-desc">{loaded ? `共 ${entries.length} 条规则` : '正在读取当前 Hooks 规则…'}</div>
        <div style={{ display: 'flex', gap: 8 }}>
          <button disabled={busy} onClick={() => setEntries([...entries, { id: '', events: ['beforeToolCall'], tools: [], decision: 'block', message: '' }])}>添加规则</button>
          <button className="primary" disabled={busy || !loaded} onClick={() => void save()}>保存并热生效</button>
        </div>
      </div>
      {loaded && (
        <div className="setting-row" style={{ gridTemplateColumns: '1fr' }}>
          <div className="setting-label">最近命中记录</div>
          <div style={{ display: 'flex', justifyContent: 'flex-end' }}>
            <button disabled={busy} onClick={() => void refreshEvents()}>刷新</button>
          </div>
          {events.length === 0 ? <div className="setting-desc">暂无命中记录。</div> : (
            <ul style={{ margin: 0, paddingLeft: 18, fontSize: 12, lineHeight: 1.9 }}>
              {events.map((ev, k) => (
                <li key={k}>{ev.createdAt} · {ev.hookId} · {ev.toolName} · {ev.event}{ev.decision ? ` · ${ev.decision}` : ''} · 会话 {ev.sessionId.slice(-6)}</li>
              ))}
            </ul>
          )}
        </div>
      )}
      {status && <p role="status" className="notice">{status}</p>}
    </div>
  )
}
