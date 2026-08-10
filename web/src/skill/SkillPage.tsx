import React, { useEffect, useState, useCallback } from 'react'
import { skillBridge, type SkillBridge } from '../bridge/client'
import type { SkillDTO, SkillMatchDTO, SkillStatus, SkillPermission } from '../generated/bridge'

const SKILL_STATUS_LABELS: Record<SkillStatus, string> = {
  draft: '草稿', published: '已发布', deprecated: '已弃用', disabled: '已禁用'
}
const PERMISSION_LABELS: Record<SkillPermission, string> = {
  read_only: '只读', read_write: '读写', network: '网络', file_system: '文件系统', shell: 'Shell', admin: '管理员'
}
const PERMISSION_OPTIONS: SkillPermission[] = ['read_only', 'read_write', 'network', 'file_system', 'shell', 'admin']

const statusColor = (s: SkillStatus): string => {
  if (s === 'published') return '#34d399'
  if (s === 'draft') return '#8fa3bf'
  if (s === 'deprecated') return '#fbbf24'
  return '#f87171'
}

const inputStyle: React.CSSProperties = { width: '100%', padding: '6px 8px', backgroundColor: '#0a0e1a', color: '#e5e7eb', border: '1px solid #334155', borderRadius: '4px', boxSizing: 'border-box' }
const btnStyle: React.CSSProperties = { padding: '6px 12px', backgroundColor: '#1e293b', color: '#e5e7eb', border: '1px solid #334155', borderRadius: '4px', cursor: 'pointer' }
const primaryBtnStyle: React.CSSProperties = { ...btnStyle, backgroundColor: '#2563eb', borderColor: '#3b82f6' }
const dangerBtnStyle: React.CSSProperties = { ...btnStyle, color: '#f87171' }

export function SkillPage({ bridge = skillBridge }: { bridge?: SkillBridge }): React.JSX.Element {
  const [skills, setSkills] = useState<SkillDTO[]>([])
  const [matches, setMatches] = useState<SkillMatchDTO[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string>()
  const [busy, setBusy] = useState(false)
  const [searchQuery, setSearchQuery] = useState('')
  const [statusFilter, setStatusFilter] = useState<SkillStatus | ''>('')

  const [showCreate, setShowCreate] = useState(false)
  const [cName, setCName] = useState('')
  const [cDisplayName, setCDisplayName] = useState('')
  const [cDescription, setCDescription] = useState('')
  const [cVersion, setCVersion] = useState('1.0.0')
  const [cPermissions, setCPermissions] = useState<SkillPermission[]>(['read_only'])
  const [cEntryPoint, setCEntryPoint] = useState('')
  const [cManifestJson, setCManifestJson] = useState('{}')

  const [editingId, setEditingId] = useState<string | null>(null)
  const [eDisplayName, setEDisplayName] = useState('')
  const [eDescription, setEDescription] = useState('')
  const [ePermissions, setEPermissions] = useState<SkillPermission[]>([])
  const [eEntryPoint, setEEntryPoint] = useState('')
  const [eManifestJson, setEManifestJson] = useState('{}')

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

  const togglePermission = (perm: SkillPermission, current: SkillPermission[], setter: (v: SkillPermission[]) => void) => {
    setter(current.includes(perm) ? current.filter(p => p !== perm) : [...current, perm])
  }

  const doCreate = async (e: React.FormEvent) => {
    e.preventDefault()
    if (!cName.trim() || !cDisplayName.trim() || !cEntryPoint.trim()) return
    setBusy(true); setError(undefined)
    try {
      await bridge.create({
        name: cName.trim(), displayName: cDisplayName.trim(), description: cDescription,
        version: cVersion, permissions: cPermissions, entryPoint: cEntryPoint.trim(), manifestJson: cManifestJson,
      })
      setCName(''); setCDisplayName(''); setCDescription(''); setCVersion('1.0.0'); setCPermissions(['read_only']); setCEntryPoint(''); setCManifestJson('{}')
      setShowCreate(false)
      await load()
    } catch (e) { setError(e instanceof Error ? e.message : '创建失败') }
    finally { setBusy(false) }
  }

  const startEdit = (skill: SkillDTO) => {
    setEditingId(skill.id)
    setEDisplayName(skill.displayName)
    setEDescription(skill.description)
    setEPermissions([...skill.permissions])
    setEEntryPoint(skill.entryPoint)
    setEManifestJson(skill.manifestJson)
  }

  const doUpdate = async (id: string) => {
    setBusy(true); setError(undefined)
    try {
      await bridge.update({
        id, displayName: eDisplayName, description: eDescription, permissions: ePermissions,
        entryPoint: eEntryPoint, manifestJson: eManifestJson, expectedVersion: 1,
      })
      setEditingId(null)
      await load()
    } catch (e) { setError(e instanceof Error ? e.message : '更新失败') }
    finally { setBusy(false) }
  }

  const doDelete = async (id: string, expectedVersion: number) => {
    setBusy(true); setError(undefined)
    try {
      await bridge.delete({ id, expectedVersion })
      await load()
    } catch (e) { setError(e instanceof Error ? e.message : '删除失败') }
    finally { setBusy(false) }
  }

  const panelStyle: React.CSSProperties = { border: '1px solid #1f2937', borderRadius: '16px', background: '#0e1c30', padding: '20px' }
  const cardStyle: React.CSSProperties = { padding: '16px', border: '1px solid #1f2937', borderRadius: '12px', background: '#111827' }

  const renderPermissionCheckboxes = (current: SkillPermission[], setter: (v: SkillPermission[]) => void) => (
    <div style={{ display: 'flex', gap: '8px', flexWrap: 'wrap' }}>
      {PERMISSION_OPTIONS.map(p => (
        <label key={p} style={{ display: 'flex', alignItems: 'center', gap: '4px', fontSize: '12px', cursor: 'pointer' }}>
          <input type="checkbox" checked={current.includes(p)} onChange={() => togglePermission(p, current, setter)} />
          {PERMISSION_LABELS[p]}
        </label>
      ))}
    </div>
  )

  const renderSkillCard = (skill: SkillDTO, matchInfo?: SkillMatchDTO) => (
    <div key={skill.id} style={cardStyle}>
      {editingId === skill.id ? (
        <form onSubmit={e => { e.preventDefault(); void doUpdate(skill.id) }} style={{ display: 'grid', gap: '10px' }}>
          <label style={{ display: 'grid', gap: '4px', fontSize: '13px' }}>显示名称
            <input style={inputStyle} value={eDisplayName} onChange={e => setEDisplayName(e.target.value)} aria-label="显示名称" />
          </label>
          <label style={{ display: 'grid', gap: '4px', fontSize: '13px' }}>描述
            <textarea style={{ ...inputStyle, resize: 'vertical', minHeight: '60px' }} value={eDescription} onChange={e => setEDescription(e.target.value)} aria-label="技能描述" />
          </label>
          <label style={{ display: 'grid', gap: '4px', fontSize: '13px' }}>入口
            <input style={inputStyle} value={eEntryPoint} onChange={e => setEEntryPoint(e.target.value)} aria-label="入口" />
          </label>
          <label style={{ display: 'grid', gap: '4px', fontSize: '13px' }}>权限
            {renderPermissionCheckboxes(ePermissions, setEPermissions)}
          </label>
          <label style={{ display: 'grid', gap: '4px', fontSize: '13px' }}>清单 JSON
            <textarea style={{ ...inputStyle, resize: 'vertical', minHeight: '60px', fontFamily: 'monospace', fontSize: '12px' }} value={eManifestJson} onChange={e => setEManifestJson(e.target.value)} aria-label="清单 JSON" />
          </label>
          <div style={{ display: 'flex', gap: '8px' }}>
            <button type="submit" style={primaryBtnStyle} disabled={busy}>{busy ? '更新中…' : '保存技能'}</button>
            <button type="button" style={btnStyle} onClick={() => setEditingId(null)}>取消</button>
          </div>
        </form>
      ) : (
        <>
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
            <button style={btnStyle} disabled={busy} onClick={() => startEdit(skill)}>编辑</button>
            <button style={dangerBtnStyle} disabled={busy} onClick={() => void doDelete(skill.id, 1)}>删除</button>
          </div>
        </>
      )}
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
        <button style={btnStyle} onClick={() => setShowCreate(v => !v)}>新建技能</button>
      </div>
      {showCreate && (
        <form onSubmit={e => void doCreate(e)} style={{ marginBottom: '20px', padding: '16px', border: '1px solid #334155', borderRadius: '12px', background: '#0a0e1a', display: 'grid', gap: '10px', gridTemplateColumns: '1fr 1fr' }}>
          <label style={{ display: 'grid', gap: '4px', fontSize: '13px' }}>技能名称
            <input style={inputStyle} value={cName} onChange={e => setCName(e.target.value)} aria-label="技能名称" placeholder="如 code-review" />
          </label>
          <label style={{ display: 'grid', gap: '4px', fontSize: '13px' }}>显示名称
            <input style={inputStyle} value={cDisplayName} onChange={e => setCDisplayName(e.target.value)} aria-label="显示名称" placeholder="如 代码审查" />
          </label>
          <label style={{ display: 'grid', gap: '4px', fontSize: '13px', gridColumn: '1 / -1' }}>描述
            <input style={inputStyle} value={cDescription} onChange={e => setCDescription(e.target.value)} aria-label="技能描述" placeholder="输入技能描述" />
          </label>
          <label style={{ display: 'grid', gap: '4px', fontSize: '13px' }}>版本
            <input style={inputStyle} value={cVersion} onChange={e => setCVersion(e.target.value)} aria-label="版本" placeholder="1.0.0" />
          </label>
          <label style={{ display: 'grid', gap: '4px', fontSize: '13px' }}>入口
            <input style={inputStyle} value={cEntryPoint} onChange={e => setCEntryPoint(e.target.value)} aria-label="入口" placeholder="如 skills/code-review/index.js" />
          </label>
          <label style={{ display: 'grid', gap: '4px', fontSize: '13px', gridColumn: '1 / -1' }}>权限
            {renderPermissionCheckboxes(cPermissions, setCPermissions)}
          </label>
          <label style={{ display: 'grid', gap: '4px', fontSize: '13px', gridColumn: '1 / -1' }}>清单 JSON
            <textarea style={{ ...inputStyle, resize: 'vertical', minHeight: '80px', fontFamily: 'monospace', fontSize: '12px' }} value={cManifestJson} onChange={e => setCManifestJson(e.target.value)} aria-label="清单 JSON" />
          </label>
          <div style={{ gridColumn: '1 / -1', display: 'flex', gap: '8px' }}>
            <button type="submit" style={primaryBtnStyle} disabled={busy || !cName.trim() || !cDisplayName.trim() || !cEntryPoint.trim()}>{busy ? '创建中…' : '创建技能'}</button>
            <button type="button" style={btnStyle} onClick={() => setShowCreate(false)}>取消</button>
          </div>
        </form>
      )}
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
