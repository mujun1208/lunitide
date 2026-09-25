import React, { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { asUserBridgeError } from '../bridge/bridgeUserError'
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
  checklistFromBase64,
  checklistToBase64,
  TEST_ITEM_STATUSES,
  type ChecklistDoc,
  type ChecklistItem,
  type ChecklistItemStatus,
} from './checklistTypes'
import { rollbackTestFailToDev } from './checklistStore'

type DeliverableItem = DeliverableListResult['items'][number]

function checklistUserError(err: unknown, fallback: string): string {
  const detail = err instanceof Error ? err.message.trim() : ''
  return /[\u4e00-\u9fff]/.test(detail) ? detail : fallback
}
const problem = (e: unknown) =>
  e instanceof BridgeClientError
    ? asUserBridgeError(e, '请求失败')
    : new BridgeClientError(checklistUserError(e, '请求失败'), 'CLIENT_ERROR', false, 'renderer')

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
  onOpenTask,
  currentTaskId,
  onGoDevItem,
  autoImport = false,
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
  onOpenTask?: (itemId: string, executor?: ChecklistItem['executor']) => void
  currentTaskId?: string
  onGoDevItem?: (itemId: string) => void
  autoImport?: boolean
}): React.JSX.Element {
  const [deliverable, setDeliverable] = useState<DeliverableItem | undefined>()
  const [doc, setDoc] = useState<ChecklistDoc>(emptyChecklist())
  const [dirty, setDirty] = useState(false)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  const [loadError, setLoadError] = useState('')
  const [failTarget, setFailTarget] = useState<string>()
  const [failReason, setFailReason] = useState('')
  const autoImported = useRef(false)

  const load = useCallback(async () => {
    setLoadError('')
    try {
      const result = await deliverables.list({ projectId: project.id, phase })
      const saved = result.items.find(i => i.documentType === documentType)
      setDeliverable(saved)
      if (!saved?.attachmentId) {
        setDoc(emptyChecklist())
        setDirty(false)
        if (autoImport && !readOnly && importFrom && !autoImported.current) {
          const sourceList = await deliverables.list({ projectId: project.id, phase: importFrom.phase })
          const sourceDeliverable = sourceList.items.find(i => i.documentType === importFrom.documentType)
          if (sourceDeliverable?.attachmentId) {
            const sourceFile = await attachments.get({ projectId: project.id, attachmentId: sourceDeliverable.attachmentId })
            const imported = importFrom.mapItems(parseChecklist(checklistFromBase64(sourceFile.contentBase64)), emptyChecklist())
            if (imported.length) {
              autoImported.current = true
              const merged = { version: 1 as const, items: imported }
              const ingested = await attachments.ingest({
                projectId: project.id,
                phase,
                category: 'checklist',
                fileName: `${documentType}.json`,
                mimeType: 'application/json',
                contentBase64: checklistToBase64(serializeChecklist(merged)),
              })
              const payload = {
                projectId: project.id,
                phase,
                documentType,
                title,
                attachmentId: ingested.attachmentId,
                status: 'review' as const,
                digest: `items:${merged.items.length}`,
              }
              const savedDoc = await deliverables.upsert(payload, { attempt: createMutationAttempt('deliverable.upsert', payload) })
              setDeliverable({
                ...savedDoc,
                createdAt: new Date().toISOString(),
                updatedAt: new Date().toISOString(),
              })
              setDoc(merged)
              setDirty(false)
              onSaved?.()
            }
          }
        }
        return
      }
      const file = await attachments.get({ projectId: project.id, attachmentId: saved.attachmentId })
      setDoc(parseChecklist(checklistFromBase64(file.contentBase64)))
      setDirty(false)
    } catch (e) {
      setLoadError(problem(e).message)
    }
  }, [attachments, autoImport, deliverables, documentType, importFrom, onSaved, phase, project.id, readOnly, title])

  useEffect(() => { void load() }, [load])

  const persist = async (next: ChecklistDoc, nextStatus: DeliverableItem['status'] = 'review') => {
    setBusy(true)
    setError('')
    try {
      const contentBase64 = checklistToBase64(serializeChecklist(next))
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
      setFailTarget(id)
      setFailReason('')
      setDoc(current => ({
        ...current,
        items: current.items.map(item => (item.id === id ? { ...item, status: prev.status } : item)),
      }))
      setDirty(false)
    }
  }

  const submitTestFail = async () => {
    if (!failTarget || busy) return
    const reason = failReason.trim()
    if (!reason) {
      setError('请填写测试不通过原因')
      return
    }
    const item = doc.items.find(i => i.id === failTarget)
    if (!item?.sourceId) {
      setError('这条测试没有对应开发条目，不能退回。')
      return
    }
    setBusy(true)
    setError('')
    try {
      const ok = await rollbackTestFailToDev(project, failTarget, item.sourceId, reason, deliverables, attachments)
      if (ok) {
        setError(`已退回开发清单 ${item.sourceId}`)
        setFailTarget(undefined)
        setFailReason('')
        await load()
        onSaved?.()
      }
    } catch (e) {
      setError(problem(e).message)
    } finally {
      setBusy(false)
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
      const sourceDoc = parseChecklist(checklistFromBase64(file.contentBase64))
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
                            onChange={e => {
                              const next = e.target.value as ChecklistItemStatus
                              if (enableTestRollback && next === 'test_fail') {
                                if (!item.sourceId) {
                                  setError('这条测试没有对应开发条目，不能标不通过。')
                                  return
                                }
                                setFailTarget(item.id)
                                setFailReason('')
                                return
                              }
                              patchItem(item.id, { status: next })
                            }}
                          >
                            {statusOptions.map(s => <option key={s} value={s}>{STATUS_LABEL[s]}</option>)}
                          </select>
                        )}
                    </td>
                    {!readOnly && (
                      <td>
                        {documentType === 'dev_checklist' && onOpenTask && (
                          <>
                            <select
                              aria-label={`执行器 ${item.id}`}
                              value={item.executor ?? ''}
                              disabled={busy}
                              onChange={e => patchItem(item.id, { executor: (e.target.value || undefined) as ChecklistItem['executor'] })}
                            >
                              <option value="">默认</option>
                              <option value="lunitide">月汐</option>
                              <option value="cursor">Cursor</option>
                              <option value="codex">Codex</option>
                            </select>
                            <button type="button" disabled={busy || !(project.rootPath ?? '').trim()} title={(project.rootPath ?? '').trim() ? undefined : '请先在项目里选择项目目录'} onClick={() => onOpenTask(item.id, item.executor)}>进入开发</button>
                          </>
                        )}
                        {documentType === 'test_checklist' && item.status === 'test_fail' && item.sourceId && onGoDevItem && (
                          <button type="button" disabled={busy} onClick={() => onGoDevItem(item.sourceId!)}>去开发改这一条</button>
                        )}
                        {currentTaskId === item.id && <small>当前任务</small>}
                        {item.executor && <small>{item.executor}</small>}
                        {item.lastResultSummary && <small title={item.lastResultSummary}>已回写</small>}
                        {item.testReturn?.reason && <small title={item.testReturn.reason}>测试退回</small>}
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
      {failTarget && (
        <div className="pm-confirm" role="dialog" aria-label="测试不通过原因">
          <p>不通过原因（必填）</p>
          <textarea className="pm-reason" rows={3} maxLength={2000} value={failReason} onChange={e => setFailReason(e.target.value)} placeholder="说明失败原因，将退回同一条开发任务" />
          <div className="dialog-actions">
            <button type="button" disabled={busy} onClick={() => { setFailTarget(undefined); setFailReason('') }}>取消</button>
            <button type="button" className="primary" disabled={busy || !failReason.trim()} onClick={() => void submitTestFail()}>退回开发</button>
          </div>
        </div>
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
