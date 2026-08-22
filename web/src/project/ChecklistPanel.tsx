import React, { useCallback, useEffect, useMemo, useState } from 'react'
import {
  BridgeClientError,
  createMutationAttempt,
  deliverableBridge as defaultDeliverableBridge,
  projectAttachmentBridge as defaultProjectAttachmentBridge,
  type DeliverableBridge,
  type ProjectAttachmentBridge,
} from '../bridge/client'
import type { DeliverableListResult, ProjectDTO } from '../generated/bridge'
import {
  checklistSummary,
  DEV_ITEM_STATUSES,
  emptyChecklist,
  nextChecklistId,
  parseChecklist,
  serializeChecklist,
  TEST_ITEM_STATUSES,
  type ChecklistDoc,
  type ChecklistItem,
  type ChecklistItemStatus,
} from './checklistTypes'
import { rollbackTestFailToDev } from './checklistStore'

type DeliverableItem = DeliverableListResult['items'][number]

const problem = (e: unknown) =>
  e instanceof BridgeClientError
    ? e
    : new BridgeClientError(e instanceof Error ? e.message : '请求失败', 'CLIENT_ERROR', false, 'renderer')

const STATUS_LABEL: Record<ChecklistItemStatus, string> = {
  pending: '待处理',
  in_progress: '进行中',
  dev_done: '开发完成',
  test_pass: '测试通过',
  test_fail: '测试不通过',
}

type ImportSpec = {
  label: string
  phase: number
  documentType: string
  mapItems: (source: ChecklistDoc, existing: ChecklistDoc) => ChecklistItem[]
}

export function ChecklistPanel({
  project,
  phase,
  documentType,
  title,
  readOnly = false,
  deliverables = defaultDeliverableBridge,
  attachments = defaultProjectAttachmentBridge,
  statusOptions,
  importFrom,
  onSaved,
  enableTestRollback = false,
}: {
  project: ProjectDTO
  phase: number
  documentType: string
  title: string
  readOnly?: boolean
  deliverables?: DeliverableBridge
  attachments?: ProjectAttachmentBridge
  statusOptions: ChecklistItemStatus[]
  importFrom?: ImportSpec
  onSaved?: () => void
  enableTestRollback?: boolean
}): React.JSX.Element {
  const [deliverable, setDeliverable] = useState<DeliverableItem | undefined>()
  const [doc, setDoc] = useState<ChecklistDoc>(emptyChecklist())
  const [dirty, setDirty] = useState(false)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  const [loadError, setLoadError] = useState('')

  const load = useCallback(async () => {
    setLoadError('')
    try {
      const result = await deliverables.list({ projectId: project.id, phase })
      const saved = result.items.find(i => i.documentType === documentType)
      setDeliverable(saved)
      if (!saved?.attachmentId) {
        setDoc(emptyChecklist())
        setDirty(false)
        return
      }
      const file = await attachments.get({ projectId: project.id, attachmentId: saved.attachmentId })
      const binary = atob(file.contentBase64)
      setDoc(parseChecklist(binary))
      setDirty(false)
    } catch (e) {
      setLoadError(problem(e).message)
    }
  }, [attachments, deliverables, documentType, phase, project.id])

  useEffect(() => { void load() }, [load])

  const persist = async (next: ChecklistDoc, nextStatus: DeliverableItem['status'] = 'review') => {
    setBusy(true)
    setError('')
    try {
      const contentBase64 = btoa(unescape(encodeURIComponent(serializeChecklist(next))))
      const ingested = await attachments.ingest({
        projectId: project.id,
        phase,
        category: 'checklist',
        fileName: `${documentType}.json`,
        mimeType: 'application/json',
        contentBase64,
      })
      const payload = {
        projectId: project.id,
        phase,
        documentType,
        title,
        attachmentId: ingested.attachmentId,
        status: (deliverable?.status === 'approved' || deliverable?.status === 'immutable' ? 'approved' : nextStatus) as 'review' | 'approved',
        digest: `items:${next.items.length}`,
      }
      const saved = await deliverables.upsert(payload, { attempt: createMutationAttempt('deliverable.upsert', payload) })
      setDeliverable({
        ...saved,
        createdAt: deliverable?.createdAt ?? new Date().toISOString(),
        updatedAt: new Date().toISOString(),
      })
      setDoc(next)
      setDirty(false)
      onSaved?.()
    } catch (e) {
      setError(problem(e).message)
    } finally {
      setBusy(false)
    }
  }

  const patchItem = (id: string, patch: Partial<ChecklistItem>) => {
    const prev = doc.items.find(i => i.id === id)
    setDoc(current => ({
      ...current,
      items: current.items.map(item => (item.id === id ? { ...item, ...patch } : item)),
    }))
    setDirty(true)
    if (
      enableTestRollback
      && documentType === 'test_checklist'
      && patch.status === 'test_fail'
      && prev?.sourceId
    ) {
      void rollbackTestFailToDev(project, id, prev.sourceId, patch.notes ?? '测试不通过').then(ok => {
        if (ok) setError('已退回开发清单，对应条目状态改为进行中。')
      }).catch(e => setError(problem(e).message))
    }
  }

  const addRow = () => {
    const prefix = documentType === 'feature_dev_list' ? 'F' : documentType === 'test_checklist' ? 'T' : 'D'
    const item: ChecklistItem = {
      id: nextChecklistId(doc.items, prefix),
      title: '新任务',
      status: 'pending',
      priority: 'P1',
    }
    setDoc(current => ({ ...current, items: [...current.items, item] }))
    setDirty(true)
  }

  const removeRow = (id: string) => {
    setDoc(current => ({ ...current, items: current.items.filter(item => item.id !== id) }))
    setDirty(true)
  }

  const importRows = async () => {
    if (!importFrom || readOnly || busy) return
    setBusy(true)
    setError('')
    try {
      const result = await deliverables.list({ projectId: project.id, phase: importFrom.phase })
      const sourceDeliverable = result.items.find(i => i.documentType === importFrom.documentType)
      if (!sourceDeliverable?.attachmentId) {
        setError(`未找到 ${importFrom.label} 清单数据，请先在对应阶段维护清单。`)
        return
      }
      const file = await attachments.get({ projectId: project.id, attachmentId: sourceDeliverable.attachmentId })
      const sourceDoc = parseChecklist(atob(file.contentBase64))
      const imported = importFrom.mapItems(sourceDoc, doc)
      if (!imported.length) {
        setError(`${importFrom.label} 中没有可导入的条目。`)
        return
      }
      const merged: ChecklistDoc = { version: 1, items: [...doc.items, ...imported] }
      await persist(merged)
    } catch (e) {
      setError(problem(e).message)
    } finally {
      setBusy(false)
    }
  }

  const summary = useMemo(() => checklistSummary(doc), [doc])
  const openCount = doc.items.filter(i => i.status === 'pending' || i.status === 'in_progress').length

  return (
    <section className="checklist-panel" aria-label={title}>
      <header className="checklist-head">
        <div>
          <b>{title}</b>
          <small>{summary} 完成 · {doc.items.length} 条 · {deliverable?.status ?? '未保存'}</small>
        </div>
        <div className="checklist-actions">
          {importFrom && !readOnly && (
            <button type="button" disabled={busy} onClick={() => void importRows()}>
              从{importFrom.label}导入
            </button>
          )}
          {!readOnly && (
            <button type="button" disabled={busy} onClick={addRow}>新增</button>
          )}
          {!readOnly && (
            <button type="button" className="primary" disabled={busy || !dirty} onClick={() => void persist(doc)}>
              {busy ? '保存中…' : '保存清单'}
            </button>
          )}
        </div>
      </header>
      {loadError && <p className="error" role="alert"><b>{loadError}</b></p>}
      {error && <p className="error" role="alert"><b>{error}</b></p>}
      {doc.items.length === 0
        ? <p className="checklist-empty">暂无清单条目。可新增，或从上游阶段导入。</p>
        : (
          <div className="checklist-table-wrap">
            <table className="checklist-table">
              <thead>
                <tr>
                  <th>编号</th>
                  <th>标题</th>
                  <th>模块</th>
                  <th>优先级</th>
                  <th>状态</th>
                  {!readOnly && <th />}
                </tr>
              </thead>
              <tbody>
                {doc.items.map(item => (
                  <tr key={item.id} className={`status-${item.status}`}>
                    <td><code>{item.id}</code></td>
                    <td>
                      {readOnly
                        ? item.title
                        : <input value={item.title} disabled={busy} onChange={e => patchItem(item.id, { title: e.target.value })} />}
                    </td>
                    <td>
                      {readOnly
                        ? (item.module ?? '—')
                        : <input value={item.module ?? ''} disabled={busy} placeholder="模块" onChange={e => patchItem(item.id, { module: e.target.value })} />}
                    </td>
                    <td>
                      {readOnly
                        ? (item.priority ?? '—')
                        : <input value={item.priority ?? ''} disabled={busy} placeholder="P0" onChange={e => patchItem(item.id, { priority: e.target.value })} />}
                    </td>
                    <td>
                      {readOnly
                        ? STATUS_LABEL[item.status]
                        : (
                          <select
                            value={item.status}
                            disabled={busy}
                            onChange={e => patchItem(item.id, { status: e.target.value as ChecklistItemStatus })}
                          >
                            {statusOptions.map(s => <option key={s} value={s}>{STATUS_LABEL[s]}</option>)}
                          </select>
                        )}
                    </td>
                    {!readOnly && (
                      <td>
                        <button type="button" className="checklist-remove" disabled={busy} onClick={() => removeRow(item.id)} aria-label={`删除 ${item.id}`}>×</button>
                      </td>
                    )}
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
      {openCount > 0 && documentType === 'dev_checklist' && (
        <p className="checklist-note">还有 {openCount} 条开发任务未完成。完成后可将条目流转到测试阶段。</p>
      )}
    </section>
  )
}

export function buildTestItemsFromDev(source: ChecklistDoc, existing: ChecklistDoc): ChecklistItem[] {
  const known = new Set(existing.items.map(i => i.sourceId ?? i.id))
  return source.items
    .filter(item => item.status === 'dev_done' && !known.has(item.id))
    .map(item => ({
      id: nextChecklistId(existing.items, 'T'),
      title: item.title,
      module: item.module,
      priority: item.priority,
      status: 'pending' as const,
      sourceId: item.id,
      notes: item.notes,
    }))
}

export { DEV_ITEM_STATUSES, TEST_ITEM_STATUSES }
