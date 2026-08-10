import React, { useEffect, useState, useCallback } from 'react'
import { skillBridge, type SkillBridge } from '../bridge/client'
import type { SkillDTO, SkillMatchDTO, SkillStatus, SkillPermission } from '../generated/bridge'

const SKILL_STATUS_LABELS: Record<SkillStatus, string> = {
  draft: '草稿', published: '已发布', deprecated: '已弃用', disabled: '已禁用'
}
const PERMISSION_LABELS: Record<SkillPermission, string> = {
  read_only: '只读', read_write: '读写', network: '网络', file_system: '文件系统', shell: 'Shell', admin: '管理员'
}

const statusColor = (s: SkillStatus): string => {
  if (s === 'published') return '#34d399'
  if (s === 'draft') return '#8fa3bf'
  if (s === 'deprecated') return '#fbbf24'
  return '#f87171'
}

export function SkillPage({ bridge = skillBridge }: { bridge?: SkillBridge }): React.JSX.Element {
  const [skills, setSkills] = useState<SkillDTO[]>([])
  const [matches, setMatches] = useState<SkillMatchDTO[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string>()
  const [busy, setBusy] = useState(false)
  const [searchQuery, setSearchQuery] = useState('')
  const [statusFilter, setStatusFilter] = useState<SkillStatus | ''>('')

  const load = useCallback(async () => {
    setLoading(true); setError(undefined)
    try { const r = await bridge.list({ ...(statusFilter ? { status: statusFilter } : {}) }); setSkills(r.items) }
    catch (e) { setError(e instanceof Error ? e.message : '加载失败') }
    finally { setLoading(false) }
  }, [bridge, statusFilter])

  useEffect(() => { load() }, [load])

  const doMatch = async () => {
    if (!searchQuery.trim()) return
    setLoading(true); setError(undefined)
    try { const r = await bridge.match({ query: searchQuery.trim() }); setMatches(r.items) }
    catch (e) { setError(e instanceof Error ? e.message : '匹配失败') }
    finally { setLoading(false) }
  }

  const doOp = async (op: 'publish' | 'deprecate' | 'disable', id: string) => {
    setBusy(true); setError(undefined)
    try {
      if (op === 'publish') await bridge.publish({ id })
      else if (op === 'deprecate') await bridge.deprecate({ id })
      else await bridge.disable({ id })
      await load()
    } catch (e) { setError(e instanceof Error ? e.message : '操作失败') }
    finally { setBusy(false) }
  }

  const panelStyle: React.CSSProperties = { border: '1px solid #1f2937', borderRadius: '16px', background: '#0e1c30', padding: '20px' }
  const cardStyle: React.CSSProperties = { padding: '16px', border: '1px solid #1f2937', borderRadius: '12px', background: '#111827' }

  const renderSkillCard = (skill: SkillDTO, matchInfo?: SkillMatchDTO) => (
    <div key={skill.id} style={cardStyle}>
      <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'flex-start', gap: '8px' }}>
        <div>
          <strong style={{ fontSize: '15px' }}>{skill.displayName}</strong>
          <span style={{ marginLeft: '8px', fontSize: '11px', color: '#8fa3bf', fontFamily: 'monospace' }}>v{skill.version}</span>
        </div>
        <span style={{ color: statusColor(skill.status), fontSize: '12px', whiteSpace: 'nowrap' }}>{SKILL_STATUS_LABELS[skill.status]}</span>
      </div>
      <p style={{ margin: '6px 0 0', color: '#8fa3bf', fontSize: '12px' }}>{skill.description}</p>
      {matchInfo && (
        <div style={{ marginTop: '8px', padding: '8px 10px', border: '1px solid #1f2937', borderRadius: '8px', background: 'rgba(96,165,250,0.06)', fontSize: '12px' }}>
          <span style={{ color: '#60a5fa' }}>匹配度: {(matchInfo.score * 100).toFixed(0)}%</span>
          <span style={{ marginLeft: '8px', color: '#8fa3bf' }}>{matchInfo.reason}</span>
        </div>
      )}
      <div style={{ marginTop: '8px', display: 'flex', gap: '6px', flexWrap: 'wrap' }}>
        {skill.permissions.map(p => <span key={p} style={{ fontSize: '10px', padding: '2px 7px', border: '1px solid #1f2937', borderRadius: '99px', color: '#8fa3bf' }}>{PERMISSION_LABELS[p]}</span>)}
      </div>
      <div style={{ marginTop: '6px', fontSize: '11px', color: '#8fa3bf', fontFamily: 'monospace' }}>{skill.entryPoint}</div>
      <div style={{ marginTop: '10px', display: 'flex', gap: '6px', flexWrap: 'wrap' }}>
        {skill.status === 'draft' && <button className="primary" disabled={busy} onClick={() => void doOp('publish', skill.id)}>发布</button>}
        {skill.status === 'published' && <button disabled={busy} onClick={() => void doOp('deprecate', skill.id)}>弃用</button>}
        {(skill.status === 'published' || skill.status === 'deprecated') && <button disabled={busy} onClick={() => void doOp('disable', skill.id)} style={{ color: '#f87171' }}>禁用</button>}
      </div>
    </div>
  )

  return (
    <div className="shell">
      <header className="brand"><div><p className="eyebrow">SKILL MARKETPLACE</p><h1>技能市场</h1><p>发现、匹配与管理引擎技能。</p></div></header>
      {error && <div className="error" role="alert"><b>{error}</b></div>}
      <div style={{ display: 'flex', gap: '10px', marginBottom: '18px', flexWrap: 'wrap' }}>
        <input value={searchQuery} onChange={e => setSearchQuery(e.target.value)} placeholder="描述需求，匹配技能…" style={{ flex: 1, minWidth: '200px' }} onKeyDown={e => { if (e.key === 'Enter') void doMatch() }} />
        <select value={statusFilter} onChange={e => setStatusFilter(e.target.value as SkillStatus | '')} style={{ minWidth: '140px' }}>
          <option value="">全部状态</option>
          <option value="draft">草稿</option>
          <option value="published">已发布</option>
          <option value="deprecated">已弃用</option>
          <option value="disabled">已禁用</option>
        </select>
        <button onClick={() => void doMatch()} disabled={loading}>匹配</button>
        <button onClick={() => void load()} disabled={loading} aria-label="刷新">↻</button>
      </div>
      {matches.length > 0 && (
        <section style={{ ...panelStyle, marginBottom: '20px' }}>
          <h2 style={{ margin: '0 0 14px', fontSize: '18px' }}>匹配结果</h2>
          <div style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fit,minmax(320px,1fr))', gap: '12px' }}>
            {matches.map(m => renderSkillCard(m.skill, m))}
          </div>
        </section>
      )}
      <section style={panelStyle}>
        <h2 style={{ margin: '0 0 14px', fontSize: '18px' }}>技能列表</h2>
        {loading ? <p style={{ color: '#8fa3bf' }}>正在载入…</p> :
         skills.length === 0 ? <div className="empty"><b>暂无技能</b></div> : (
          <div style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fit,minmax(320px,1fr))', gap: '12px' }}>
            {skills.map(s => renderSkillCard(s))}
          </div>
        )}
      </section>
    </div>
  )
}
