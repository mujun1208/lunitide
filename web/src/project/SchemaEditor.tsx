import React, { useEffect, useState } from 'react'
import type { ProjectDTO } from '../generated/bridge'
import { projectFactoryApi } from './projectFactoryApi'

const emptySchema = `{
  "version": 1,
  "dialect": "sqlite",
  "tables": []
}`

const emptyHint = '至少需要一张表才能保存或核齐'

function schemaHasTables(text: string): boolean {
  try {
    const parsed = JSON.parse(text) as { tables?: unknown }
    return Array.isArray(parsed.tables) && parsed.tables.length > 0
  } catch {
    return false
  }
}

export function SchemaEditor({
  project,
  readOnly = false,
  onProjectUpdated,
}: {
  project: ProjectDTO
  readOnly?: boolean
  onProjectUpdated?: (project: ProjectDTO) => void
}): React.JSX.Element {
  const [text, setText] = useState(emptySchema)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  const [note, setNote] = useState(project.dbStatus === 'ready' ? '库表已核齐' : '')

  useEffect(() => {
    let cancelled = false
    void projectFactoryApi.schemaGet({ projectId: project.id }).then(got => {
      if (cancelled) return
      setText(JSON.stringify(got.schema ?? { version: 1, dialect: 'sqlite', tables: [] }, null, 2))
      setNote(got.dbStatus === 'ready' ? '库表已核齐' : '')
    }).catch(() => {
      if (!cancelled) setText(emptySchema)
    })
    return () => { cancelled = true }
  }, [project.id, project.dbStatus])

  const parse = () => {
    const schema = JSON.parse(text) as unknown
    if (!schema || typeof schema !== 'object') throw new Error('模式不是对象')
    return schema
  }

  const hasTables = schemaHasTables(text)
  const canWrite = !readOnly && hasTables
  const status = hasTables ? note : emptyHint

  const save = async () => {
    if (readOnly || busy || !canWrite) return
    setBusy(true)
    setError('')
    try {
      await projectFactoryApi.schemaPut({ projectId: project.id, schema: parse() })
      setNote('模式已保存')
    } catch (e) {
      setError(e instanceof Error ? e.message : '无法保存模式')
    } finally {
      setBusy(false)
    }
  }

  const materialize = async () => {
    if (readOnly || busy || !canWrite) return
    setBusy(true)
    setError('')
    try {
      await projectFactoryApi.schemaPut({ projectId: project.id, schema: parse() })
      const got = await projectFactoryApi.schemaMaterialize({ projectId: project.id, version: project.version })
      onProjectUpdated?.(got.project)
      setNote('已按设计建表（不删表）')
    } catch (e) {
      setError(e instanceof Error ? e.message : '建表失败')
    } finally {
      setBusy(false)
    }
  }

  const verify = async () => {
    if (readOnly || busy || !canWrite) return
    setBusy(true)
    setError('')
    try {
      const got = await projectFactoryApi.schemaVerify({ projectId: project.id, version: project.version })
      onProjectUpdated?.(got.project)
      setNote('库表已核齐')
    } catch (e) {
      setError(e instanceof Error ? e.message : '核齐失败')
    } finally {
      setBusy(false)
    }
  }

  return (
    <section className="schema-editor" aria-label="数据库模式">
      <header className="checklist-head">
        <div>
          <b>数据库模式</b>
          <small>{project.dbStatus === 'ready' ? 'ready' : project.dbStatus || 'none'}{project.dbPath ? ` · ${project.dbPath}` : ''}</small>
        </div>
      </header>
      <textarea className="pm-reason" rows={10} value={text} disabled={readOnly || busy} onChange={e => setText(e.target.value)} />
      {!readOnly && (
        <div className="checklist-actions">
          <button type="button" disabled={busy || !canWrite} onClick={() => void save()}>保存模式</button>
          <button type="button" disabled={busy || !canWrite} onClick={() => void materialize()}>物化建表</button>
          <button type="button" className="primary" disabled={busy || !canWrite} onClick={() => void verify()}>核齐库表</button>
        </div>
      )}
      {status && <p className="release-result" role="status">{status}</p>}
      {error && <p className="error" role="alert"><b>{error}</b></p>}
    </section>
  )
}
