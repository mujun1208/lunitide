import React, { useEffect, useState } from 'react'
import { ConfirmDialog, Dialog } from '../ui/Dialog'
import { useZh } from '../i18n/language'
import { composeDatasourceDsn, datasourceStatus, FIXED_DATABASE, type DatasourceKind } from './datasourceDsn'

export type DatasourceRow = {
  id: string
  name: string
  kind: DatasourceKind
  state: 'active' | 'disabled'
  readonlyVerified: boolean
  createdAt: string
}

export type BrowseItem = { name: string; schema?: string }

export type DatasourcePanelApi = {
  list: () => Promise<{ items: DatasourceRow[] }>
  create: (input: { name: string; kind: DatasourceKind; dsn: string }) => Promise<DatasourceRow>
  probe: (id: string) => Promise<{ id: string; readonlyVerified: boolean }>
  browse: (input: { id: string; scope: 'catalog' | 'schema' | 'table' | 'column'; schema?: string; table?: string }) => Promise<{ items: BrowseItem[] }>
  disable: (id: string) => Promise<{ id: string; state: 'disabled' }>
}

const emptyForm = { name: '', host: '127.0.0.1', port: '', database: '', user: '', password: '', ssl: false }

export function DataSourcePanel({ api }: { api?: DatasourcePanelApi }): React.JSX.Element {
  const zh = useZh()
  const [items, setItems] = useState<DatasourceRow[]>([])
  const [kind, setKind] = useState<DatasourceKind | null>(null)
  const [form, setForm] = useState(emptyForm)
  const [saved, setSaved] = useState(false)
  const [error, setError] = useState('')
  const [busy, setBusy] = useState(false)
  const [disableId, setDisableId] = useState('')
  const [browse, setBrowse] = useState<{ id: string; schemas: BrowseItem[]; tables: BrowseItem[]; columns: BrowseItem[] } | null>(null)

  const reload = async () => {
    if (!api) return
    const got = await api.list().catch(() => ({ items: [] as DatasourceRow[] }))
    setItems(got.items)
  }

  useEffect(() => { void reload() }, [])

  const submit = async () => {
    if (!api || !kind) return
    setError('')
    setBusy(true)
    try {
      const host = form.host.trim() || '127.0.0.1'
      const name = form.name.trim() || `${FIXED_DATABASE}@${host}`
      await api.create({
        name,
        kind,
        dsn: composeDatasourceDsn(kind, { ...form, host }),
      })
      setForm(emptyForm)
      setKind(null)
      setSaved(true)
      await reload()
    } catch (e) {
      setError(e instanceof Error ? e.message : (zh ? '保存失败' : 'Could not save'))
    } finally {
      setBusy(false)
    }
  }

  const runProbe = async (id: string) => {
    if (!api) return
    setError('')
    try {
      await api.probe(id)
      setSaved(true)
      await reload()
    } catch (e) {
      setError(e instanceof Error ? e.message : (zh ? '探测失败' : 'Probe failed'))
    }
  }

  const runBrowse = async (id: string) => {
    if (!api) return
    setError('')
    try {
      const schemas = await api.browse({ id, scope: 'schema' })
      setBrowse({ id, schemas: schemas.items, tables: [], columns: [] })
    } catch (e) {
      setError(e instanceof Error ? e.message : (zh ? '浏览失败' : 'Browse failed'))
    }
  }

  const openTables = async (schema: string) => {
    if (!api || !browse) return
    const tables = await api.browse({ id: browse.id, scope: 'table', schema })
    setBrowse({ ...browse, tables: tables.items, columns: [] })
  }

  const openColumns = async (schema: string, table: string) => {
    if (!api || !browse) return
    const columns = await api.browse({ id: browse.id, scope: 'column', schema, table })
    setBrowse({ ...browse, columns: columns.items })
  }

  const confirmDisable = async () => {
    if (!api || !disableId) return
    try {
      await api.disable(disableId)
      setDisableId('')
      await reload()
    } catch (e) {
      setError(e instanceof Error ? e.message : (zh ? '禁用失败' : 'Disable failed'))
    }
  }

  const statusLabel = (row: DatasourceRow) => {
    const status = datasourceStatus(row)
    if (status === 'disabled') return zh ? '不可查询' : 'Not queryable'
    if (status === 'unverified') return zh ? '未探测' : 'Not probed'
    return zh ? '已探测 · 只读' : 'Probed · read-only'
  }

  return (
    <div className="ds-panel">
      <p className="setting-desc">
        {zh
          ? '月汐自己的库仍是本机 SQLite。这里连接外部 PostgreSQL / MySQL；本机连接可读写，远程连接只读。'
          : 'Lunitide itself stays on local SQLite. This connects external PostgreSQL / MySQL; local connections are read-write, remote connections read-only.'}
      </p>
      <p className="setting-desc">{zh ? `本机连接只填账号密码即可：库名固定为 ${FIXED_DATABASE}，不存在会自动创建（需可建库账号，如 root）。远程连接建议只读账号。` : `For a local connection just enter account + password: the database is fixed to ${FIXED_DATABASE} and auto-created if missing (needs an account that can create databases, e.g. root). Prefer a read-only account for remote.`}</p>
      <div className="ds-add-row">
        <button type="button" onClick={() => { setKind('postgres'); setSaved(false) }}>{zh ? '添加 PostgreSQL' : 'Add PostgreSQL'}</button>
        <button type="button" onClick={() => { setKind('mysql'); setSaved(false) }}>{zh ? '添加 MySQL' : 'Add MySQL'}</button>
      </div>
      <table className="ds-table">
        <thead>
          <tr>
            <th>{zh ? '名称' : 'Name'}</th>
            <th>{zh ? '引擎' : 'Engine'}</th>
            <th>{zh ? '状态' : 'Status'}</th>
            <th />
          </tr>
        </thead>
        <tbody>
          {items.map(row => (
            <tr key={row.id}>
              <td>{row.name}</td>
              <td>{row.kind === 'postgres' ? 'PostgreSQL' : 'MySQL'}</td>
              <td>{statusLabel(row)}</td>
              <td>
                {row.state === 'active' && !row.readonlyVerified && (
                  <button type="button" onClick={() => void runProbe(row.id)}>{zh ? '探测' : 'Probe'}</button>
                )}
                {row.readonlyVerified && row.state === 'active' && (
                  <button type="button" onClick={() => void runBrowse(row.id)}>{zh ? '浏览' : 'Browse'}</button>
                )}
                {row.state === 'active' && (
                  <button type="button" onClick={() => setDisableId(row.id)}>{zh ? '禁用' : 'Disable'}</button>
                )}
              </td>
            </tr>
          ))}
        </tbody>
      </table>
      {saved && <p role="status">{zh ? '已保存 · 不回显' : 'Saved · not echoed'}</p>}
      {error && <p role="alert">{error}</p>}
      {browse && (
        <div className="ds-browse">
          <p>{zh ? '只显示 schema / 表 / 列名' : 'Schema / table / column names only'}</p>
          <ul>
            {browse.schemas.map(item => (
              <li key={item.name}>
                <button type="button" onClick={() => void openTables(item.name)}>{item.name}</button>
              </li>
            ))}
          </ul>
          {browse.tables.length > 0 && (
            <ul>
              {browse.tables.map(item => (
                <li key={`${item.schema ?? ''}.${item.name}`}>
                  <button type="button" onClick={() => void openColumns(item.schema ?? browse.schemas[0]?.name ?? '', item.name)}>{item.name}</button>
                </li>
              ))}
            </ul>
          )}
          {browse.columns.length > 0 && (
            <ul>
              {browse.columns.map(item => <li key={item.name}>{item.name}</li>)}
            </ul>
          )}
        </div>
      )}
      <Dialog open={kind !== null} title={kind === 'mysql' ? (zh ? '添加 MySQL' : 'Add MySQL') : (zh ? '添加 PostgreSQL' : 'Add PostgreSQL')} onClose={() => setKind(null)}>
        <form onSubmit={e => { e.preventDefault(); void submit() }}>
          <p className="ds-hint">{zh ? `只填账号和密码即可。默认连接本机 ${form.host || '127.0.0.1'}，库名固定为 ${FIXED_DATABASE}，不存在会自动创建。` : `Just enter the account and password. Connects to local ${form.host || '127.0.0.1'}; the database is fixed to ${FIXED_DATABASE} and auto-created if missing.`}</p>
          <label>{zh ? '用户' : 'User'}<input value={form.user} onChange={e => setForm(v => ({ ...v, user: e.target.value }))} autoComplete="off" placeholder={zh ? '如 root' : 'e.g. root'} /></label>
          <label>{zh ? '密码' : 'Password'}<input type="password" value={form.password} onChange={e => setForm(v => ({ ...v, password: e.target.value }))} autoComplete="new-password" placeholder={zh ? '数据库密码' : 'database password'} /></label>
          <details className="ds-advanced">
            <summary>{zh ? '更多设置（本机可跳过）' : 'More settings (skip for local)'}</summary>
            <label>{zh ? '名称' : 'Name'}<input value={form.name} maxLength={128} onChange={e => setForm(v => ({ ...v, name: e.target.value }))} placeholder={zh ? '不填自动命名' : 'auto if blank'} /></label>
            <label>{zh ? '主机' : 'Host'}<input value={form.host} onChange={e => setForm(v => ({ ...v, host: e.target.value }))} autoComplete="off" placeholder="127.0.0.1" /></label>
            <label>{zh ? '端口' : 'Port'}<input value={form.port} onChange={e => setForm(v => ({ ...v, port: e.target.value }))} placeholder={kind === 'mysql' ? '3306' : '5432'} /></label>
            <label><input type="checkbox" checked={form.ssl} onChange={e => setForm(v => ({ ...v, ssl: e.target.checked }))} /> {zh ? 'SSL（本机通常不需要）' : 'SSL (usually off for local)'}</label>
          </details>
          <div className="dialog-actions">
            <button type="button" onClick={() => setKind(null)}>{zh ? '取消' : 'Cancel'}</button>
            <button className="primary" disabled={busy || !form.user.trim() || !form.password.trim() || !form.host.trim()}>{zh ? '保存' : 'Save'}</button>
          </div>
        </form>
      </Dialog>
      <ConfirmDialog
        open={disableId !== ''}
        title={zh ? '禁用连接' : 'Disable connection'}
        description={zh ? '禁用后不可查询，需重新探测。' : 'Disabled connections cannot be queried until probed again.'}
        confirmLabel={zh ? '禁用' : 'Disable'}
        onCancel={() => setDisableId('')}
        onConfirm={() => void confirmDisable()}
      />
    </div>
  )
}
