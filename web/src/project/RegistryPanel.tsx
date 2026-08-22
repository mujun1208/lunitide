import React, { useCallback, useEffect, useMemo, useState } from 'react'
import {
  BridgeClientError,
  createMutationAttempt,
  deliverableBridge as defaultDeliverableBridge,
  projectAttachmentBridge as defaultProjectAttachmentBridge,
  registryBridge as defaultRegistryBridge,
  type DeliverableBridge,
  type ProjectAttachmentBridge,
  type RegistryBridge,
} from '../bridge/client'
import type { DbQueryResult, OpenapiParseResult, ProjectDTO } from '../generated/bridge'
import {
  dbRegistryPhase,
  interfaceRegistryPhase,
  registryDefaultTab,
} from './deliverableTypes'
import { createProjectCrRevision, writeStoredCrRevision } from './crRevision'
import { designPhaseForType, devPhaseForType } from './checklistTypes'

type EndpointRow = { method: string; path: string; operationId: string }
type Tab = 'openapi' | 'db'

const problem = (e: unknown) =>
  e instanceof BridgeClientError
    ? e
    : new BridgeClientError(e instanceof Error ? e.message : '请求失败', 'CLIENT_ERROR', false, 'renderer')

const sampleOpenAPISpec = `{"openapi":"3.0.3","info":{"title":"Sample API","version":"1.0.0","description":"Paste or edit your OpenAPI document here."},"paths":{"/health":{"get":{"operationId":"getHealth","summary":"Health check","responses":{"200":{"description":"ok"}}}},"/items":{"get":{"operationId":"listItems","responses":{"200":{"description":"ok"}}},"post":{"operationId":"createItem","responses":{"201":{"description":"created"}}}}}}`

const toBase64 = (text: string) => btoa(unescape(encodeURIComponent(text)))

function extractEndpoints(spec: string): EndpointRow[] {
  try {
    const doc = JSON.parse(spec) as { paths?: Record<string, Record<string, { operationId?: string }>> }
    const paths = doc.paths ?? {}
    const methods = ['get', 'post', 'put', 'patch', 'delete', 'head', 'options']
    const out: EndpointRow[] = []
    for (const [path, item] of Object.entries(paths)) {
      if (!item || typeof item !== 'object') continue
      for (const m of methods) {
        const op = item[m]
        if (op) out.push({ method: m.toUpperCase(), path, operationId: op.operationId ?? '' })
      }
    }
    return out.sort((a, b) => a.path.localeCompare(b.path) || a.method.localeCompare(b.method))
  } catch {
    return []
  }
}

export function RegistryPanel({
  project,
  phase,
  bridge = defaultRegistryBridge,
  deliverables = defaultDeliverableBridge,
  attachments = defaultProjectAttachmentBridge,
  readOnly = false,
  devGateReady = true,
  onGoDevPhase,
}: {
  project: ProjectDTO
  phase: number
  bridge?: RegistryBridge
  deliverables?: DeliverableBridge
  attachments?: ProjectAttachmentBridge
  readOnly?: boolean
  devGateReady?: boolean
  onGoDevPhase?: () => void
}): React.JSX.Element {
  const defaultTab = registryDefaultTab(phase, project.type)
  const [tab, setTab] = useState<Tab>(defaultTab)
  const [spec, setSpec] = useState(sampleOpenAPISpec)
  const [parseResult, setParseResult] = useState<OpenapiParseResult | null>(null)
  const [endpoints, setEndpoints] = useState<EndpointRow[]>([])
  const [sql, setSql] = useState("SELECT name, type FROM sqlite_master WHERE type IN ('table','view') ORDER BY name")
  const [dbResult, setDbResult] = useState<DbQueryResult | null>(null)
  const [bindNote, setBindNote] = useState('')
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  const runId = useMemo(() => `registry-${project.id}`, [project.id])
  const ifacePhase = interfaceRegistryPhase(project.type)
  const dbPhase = dbRegistryPhase(project.type)

  useEffect(() => { setTab(registryDefaultTab(phase, project.type)) }, [phase, project.type])

  const loadBoundSpec = useCallback(async () => {
    try {
      const result = await deliverables.list({ projectId: project.id, phase: ifacePhase })
      const bound = result.items.find(i => i.documentType === 'interface_list')
      if (!bound?.attachmentId) return
      const file = await attachments.get({ projectId: project.id, attachmentId: bound.attachmentId })
      setSpec(atob(file.contentBase64))
      setEndpoints(extractEndpoints(atob(file.contentBase64)))
      setBindNote(`已加载接口清单绑定 · digest ${bound.digest || '—'}`)
    } catch {
      /* optional preload */
    }
  }, [attachments, deliverables, ifacePhase, project.id])

  useEffect(() => { if (devGateReady && tab === 'openapi') void loadBoundSpec() }, [devGateReady, tab, loadBoundSpec])

  const bindDeliverable = async (
    targetPhase: number,
    documentType: string,
    title: string,
    fileName: string,
    content: string,
    digest: string,
  ) => {
    setBusy(true)
    setError('')
    try {
      const ingested = await attachments.ingest({
        projectId: project.id,
        phase: targetPhase,
        category: 'registry',
        fileName,
        mimeType: documentType === 'interface_list' ? 'application/json' : 'text/plain',
        contentBase64: toBase64(content),
      })
      const payload = {
        projectId: project.id,
        phase: targetPhase,
        documentType,
        title,
        attachmentId: ingested.attachmentId,
        status: 'review' as const,
        digest: digest.slice(0, 128),
      }
      await deliverables.upsert(payload, { attempt: createMutationAttempt('deliverable.upsert', payload) })
      setBindNote(`${title} 已绑定 · ${digest.slice(0, 12)}…`)
      if (documentType === 'interface_list' || documentType === 'db_design') {
        try {
          const cr = await createProjectCrRevision(project, `${title} 配置完成`, deliverables)
          writeStoredCrRevision(project.id, cr.crRevisionId, cr.digest)
          setBindNote(prev => `${prev} · CR #${cr.revisionNo}`)
        } catch { /* CR optional */ }
      }
    } catch (e) {
      setError(problem(e).message)
    } finally {
      setBusy(false)
    }
  }

  const parseOpenAPI = async () => {
    if (readOnly || busy || !devGateReady || spec.trim().length < 100) return
    setBusy(true)
    setError('')
    setParseResult(null)
    setEndpoints([])
    try {
      const result = await bridge.parseOpenAPI({ spec, name: project.projectCode })
      setParseResult(result)
      setEndpoints(extractEndpoints(spec))
    } catch (e) {
      setError(problem(e).message)
    } finally {
      setBusy(false)
    }
  }

  const bindOpenAPI = async () => {
    if (!parseResult || readOnly || busy || !devGateReady) return
    await bindDeliverable(ifacePhase, 'interface_list', '接口清单', 'openapi.json', spec, parseResult.digest)
  }

  const queryDb = async () => {
    if (readOnly || busy || !devGateReady || !sql.trim()) return
    setBusy(true)
    setError('')
    setDbResult(null)
    try {
      const result = await bridge.queryDb({ runId, sql: sql.trim(), maxRows: 200, timeoutMs: 8000 })
      setDbResult(result)
    } catch (e) {
      setError(problem(e).message)
    } finally {
      setBusy(false)
    }
  }

  const bindDb = async () => {
    if (!dbResult || readOnly || busy || !devGateReady) return
    const summary = `sql:${sql.trim().slice(0, 80)}\nrows:${dbResult.rowCount}`
    await bindDeliverable(dbPhase, 'db_design', '数据库设计文档', 'db-query.txt', summary, dbResult.resultDigest)
  }

  const importApiListHint = async () => {
    if (readOnly || busy || !devGateReady) return
    setBusy(true)
    setError('')
    try {
      const result = await deliverables.list({ projectId: project.id, phase: designPhaseForType(project.type) })
      const apiList = result.items.find(i => i.documentType === 'api_list')
      if (!apiList?.attachmentId) {
        setError('请先在「方案和UI设计」阶段维护接口清单。')
        return
      }
      setBindNote(`已关联方案阶段接口清单 · 请在 OpenAPI 中对照 api_list 条目配置`)
    } catch (e) {
      setError(problem(e).message)
    } finally {
      setBusy(false)
    }
  }

  const rows = dbResult?.rows ?? []
  const columns = dbResult?.columns ?? []
  const panelTitle = tab === 'openapi' ? '接口注册表' : '数据库注册表'
  const phaseHint = project.type === 'operations'
    ? (phase === 3 ? '阶段 3 · 接口' : phase === 2 ? '阶段 2 · 数据库' : `阶段 ${phase}`)
    : (phase === 4 ? '阶段 4 · 接口' : phase === 3 ? '阶段 3 · 数据库' : `阶段 ${phase}`)
  const devPhase = devPhaseForType(project.type)
  const showDbTab = phase === dbPhase
  const showApiTab = phase === ifacePhase

  if (!devGateReady) {
    return (
      <aside className="pm-registry-panel" aria-label="注册表门禁">
        <header className="pm-deliverable-head">
          <div><b>注册表暂不可用</b><small>需先完成开发阶段规范与检查清单</small></div>
        </header>
        <div className="registry-gate">
          <p>请先进入阶段 {devPhase}「开发」，保存并确认 <b>开发检查清单</b>，建立项目规范与目录结构后，再配置数据库与接口。</p>
          {onGoDevPhase && <button type="button" className="primary" onClick={onGoDevPhase}>前往开发阶段</button>}
        </div>
      </aside>
    )
  }

  return (
    <aside className="pm-registry-panel" aria-label="OpenAPI 与数据库注册表">
      <header className="pm-deliverable-head">
        <div><b>{panelTitle}</b><small>{phaseHint}{bindNote ? ` · ${bindNote}` : ''}</small></div>
      </header>
      <div className="registry-tabs" role="tablist" aria-label="注册表类型">
        {showDbTab && (
          <button type="button" role="tab" aria-selected={tab === 'db'} className={tab === 'db' ? 'on' : ''} onClick={() => setTab('db')}>数据库</button>
        )}
        {showApiTab && (
          <button type="button" role="tab" aria-selected={tab === 'openapi'} className={tab === 'openapi' ? 'on' : ''} onClick={() => setTab('openapi')}>OpenAPI / 接口</button>
        )}
      </div>
      <div className="registry-body">
        {error && <p className="error" role="alert"><b>{error}</b></p>}
        {tab === 'openapi' ? (
          <div className="registry-pane">
            <div className="registry-actions">
              <button type="button" disabled={readOnly || busy} onClick={() => void importApiListHint()}>关联方案接口清单</button>
              <button type="button" disabled={readOnly || busy} onClick={() => void loadBoundSpec()}>重新加载绑定</button>
            </div>
            <label>OpenAPI 规范<textarea className="registry-spec" value={spec} onChange={e => setSpec(e.target.value)} rows={10} disabled={readOnly || busy} spellCheck={false} /></label>
            <button className="primary" disabled={readOnly || busy || spec.trim().length < 100} onClick={() => void parseOpenAPI()}>{busy ? '解析中…' : '解析规范'}</button>
            {parseResult && (
              <>
                <div className="registry-meta" role="status">
                  <span>title: {parseResult.title ?? '—'}</span>
                  <span>operations: {parseResult.operationCount}</span>
                  <span>digest: <code>{parseResult.digest.slice(0, 12)}…</code></span>
                </div>
                <button type="button" className="primary" disabled={readOnly || busy} onClick={() => void bindOpenAPI()}>绑定到接口清单交付物</button>
              </>
            )}
            {endpoints.length > 0 && (
              <div className="registry-table-wrap">
                <table className="registry-table">
                  <thead><tr><th>Method</th><th>Path</th><th>Operation ID</th></tr></thead>
                  <tbody>{endpoints.map(row => <tr key={`${row.method}:${row.path}`}><td>{row.method}</td><td><code>{row.path}</code></td><td>{row.operationId || '—'}</td></tr>)}</tbody>
                </table>
              </div>
            )}
          </div>
        ) : (
          <div className="registry-pane">
            <label>SQL 查询<textarea className="registry-spec" value={sql} onChange={e => setSql(e.target.value)} rows={5} disabled={readOnly || busy} spellCheck={false} /></label>
            <button className="primary" disabled={readOnly || busy || !sql.trim()} onClick={() => void queryDb()}>{busy ? '查询中…' : '执行查询'}</button>
            {dbResult && (
              <>
                <div className="registry-meta" role="status">
                  <span>rows: {dbResult.rowCount}{dbResult.truncated ? ' (truncated)' : ''}</span>
                  <span>digest: <code>{dbResult.resultDigest.slice(0, 12)}…</code></span>
                </div>
                <button type="button" className="primary" disabled={readOnly || busy} onClick={() => void bindDb()}>绑定到数据库设计交付物</button>
              </>
            )}
            {columns.length > 0 && (
              <div className="registry-table-wrap">
                <table className="registry-table">
                  <thead><tr>{columns.map(c => <th key={c}>{c}</th>)}</tr></thead>
                  <tbody>{rows.map((row, i) => <tr key={i}>{columns.map((_, j) => <td key={j}>{formatCell((row as unknown[])?.[j])}</td>)}</tr>)}</tbody>
                </table>
              </div>
            )}
          </div>
        )}
      </div>
    </aside>
  )
}

function formatCell(v: unknown): string {
  if (v == null) return '—'
  if (typeof v === 'object') return JSON.stringify(v)
  return String(v)
}
