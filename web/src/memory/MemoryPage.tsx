import React, { useCallback, useEffect, useState } from 'react'
import { createMutationAttempt, getMemoryBridge, getMemoryOpsBridge, newBridgeULID, type MemoryBridge, type MemoryOpsBridge } from '../bridge/client'
import type { GenerationSummary, MemoryExportResult, MemoryGenerationPreviewResult, MemoryImportPreviewResult, MemoryItemDTO, MemoryPurgePrepareResult, MemoryReviewListResult } from '../generated/bridge'
import { isFabricExport, MemoryAdvancedPanel } from './MemoryAdvancedPanel'
import { MemoryDrawer, type MemoryDrawerState } from './MemoryDrawer'
import { MemoryList } from './MemoryList'
import { MemorySettingsPanel } from './MemorySettingsPanel'
import { MemoryStatusHeader } from './MemoryStatusHeader'
import { loadMemorySettings, saveMemorySettings, type MemorySettingsDraft } from './memorySettings'

function userError(err: unknown, fallback: string): string {
  const detail = err instanceof Error ? err.message.trim() : ''
  return /[\u4e00-\u9fff]/.test(detail) ? detail : fallback
}

export function MemoryPage({
  projectId,
  bridge = getMemoryBridge(),
  ops = getMemoryOpsBridge(),
}: {
  projectId?: string
  bridge?: MemoryBridge
  ops?: MemoryOpsBridge
}): React.JSX.Element {
  const [draft, setDraft] = useState<MemorySettingsDraft | null>(null)
  const [items, setItems] = useState<MemoryItemDTO[]>([])
  const [query, setQuery] = useState('')
  const [drawer, setDrawer] = useState<MemoryDrawerState>({ kind: 'closed' })
  const [error, setError] = useState('')
  const [notice, setNotice] = useState('')
  const [busy, setBusy] = useState(false)
  const [createText, setCreateText] = useState('')
  const [creating, setCreating] = useState(false)
  const [forgetBusy, setForgetBusy] = useState(false)
  const [correctText, setCorrectText] = useState('')
  const [correctBusy, setCorrectBusy] = useState(false)
  const [reviews, setReviews] = useState<MemoryReviewListResult['items']>([])
  const [reviewRevision, setReviewRevision] = useState(0)
  const [reviewBusy, setReviewBusy] = useState(false)
  const [purgePreview, setPurgePreview] = useState<MemoryPurgePrepareResult | null>(null)
  const [purgeBusy, setPurgeBusy] = useState(false)
  const [databaseRevision, setDatabaseRevision] = useState(0)
  const [artifactId, setArtifactId] = useState('')
  const [fabricExport, setFabricExport] = useState<Extract<MemoryExportResult, { format: 'fabric_v2' }> | null>(null)
  const [importPreview, setImportPreview] = useState<MemoryImportPreviewResult | null>(null)
  const [archiveBusy, setArchiveBusy] = useState(false)
  const [itemHistory, setItemHistory] = useState<MemoryItemDTO[]>([])
  const [detailItem, setDetailItem] = useState<MemoryItemDTO | null>(null)
  const [undoOperationId, setUndoOperationId] = useState('')
  const [generations, setGenerations] = useState<GenerationSummary[]>([])
  const [generationPreview, setGenerationPreview] = useState<MemoryGenerationPreviewResult | null>(null)
  const [generationBusy, setGenerationBusy] = useState(false)

  const showNotice = (value: string, undoId = '') => {
    setNotice(value)
    setUndoOperationId(undoId)
  }

  const loadSettings = useCallback(async () => {
    try {
      setDraft(await loadMemorySettings(ops))
    } catch (err) {
      setError(userError(err, '记忆设置载入失败'))
    }
  }, [ops])

  const loadItems = useCallback(async () => {
    if (!bridge.itemList) return
    try {
      const listed = await bridge.itemList({ scopeKind: 'user' })
      let merged = listed.items
      if (projectId && projectId.length === 26 && bridge.itemList) {
        try {
          const project = await bridge.itemList({ scopeKind: 'project', scopeId: projectId })
          merged = [...listed.items, ...project.items]
        } catch {
          /* keep personal items when project scope is unavailable */
        }
      }
      setItems(merged.filter(item => !item.forgotten))
      setDatabaseRevision(listed.databaseRevision)
    } catch (err) {
      setError(userError(err, '记忆列表载入失败'))
    }
  }, [bridge, projectId])

  const loadReviews = useCallback(async () => {
    if (!bridge.reviewList) return
    try {
      const listed = await bridge.reviewList({ scopeKind: 'user' })
      setReviews(listed.items)
      setReviewRevision(listed.databaseRevision)
    } catch (err) {
      setError(userError(err, '待审记忆载入失败'))
    }
  }, [bridge])

  const loadGenerations = useCallback(async () => {
    if (!bridge.generationList) return
    try {
      const listed = await bridge.generationList({ scopeKind: 'user' })
      setGenerations(listed.items)
    } catch (err) {
      setError(userError(err, '记忆世代载入失败'))
    }
  }, [bridge])

  useEffect(() => {
    void loadSettings()
    void loadItems()
    void loadReviews()
  }, [loadSettings, loadItems, loadReviews])

  useEffect(() => {
    if (drawer.kind === 'advanced') void loadGenerations()
  }, [drawer.kind, loadGenerations])

  useEffect(() => {
    if (drawer.kind !== 'item') {
      setItemHistory([])
      setDetailItem(null)
      return
    }
    let cancelled = false
    if (bridge.itemGet) {
      void bridge.itemGet({ factId: drawer.factId }).then(item => {
        if (cancelled) return
        setDetailItem(item)
        if (item.text) setCorrectText(item.text)
      }).catch(err => {
        if (!cancelled) setError(userError(err, '记忆详情载入失败'))
      })
    }
    if (bridge.itemHistory) {
      void bridge.itemHistory({ factId: drawer.factId, limit: 20 }).then(listed => {
        if (!cancelled) setItemHistory(listed.items)
      }).catch(err => {
        if (!cancelled) setError(userError(err, '记忆历史载入失败'))
      })
    }
    return () => { cancelled = true }
  }, [bridge, drawer])

  const selected = drawer.kind === 'item' ? (detailItem ?? items.find(item => item.factId === drawer.factId)) : undefined

  const createItem = async (text: string) => {
    const trimmed = text.trim()
    if (!trimmed || !bridge.itemCreate) return
    setCreating(true)
    setError('')
    try {
      const payload = { scopeKind: 'user' as const, text: trimmed, operationId: newBridgeULID() }
      const created = await bridge.itemCreate(payload, { attempt: createMutationAttempt('memory.item.create', payload) })
      setCreateText('')
      showNotice('已保存为记忆', bridge.captureUndo ? created.undoOperationId : '')
      await loadItems()
    } catch (err) {
      setError(userError(err, '保存记忆失败'))
    } finally {
      setCreating(false)
    }
  }

  const undoCapture = async () => {
    if (!bridge.captureUndo || !undoOperationId) return
    setCreating(true)
    setError('')
    try {
      const payload = { undoOperationId, operationId: newBridgeULID() }
      await bridge.captureUndo(payload, { attempt: createMutationAttempt('memory.capture.undo', payload) })
      showNotice('已撤销这次记忆')
      await loadItems()
    } catch (err) {
      setError(userError(err, '撤销记忆失败'))
    } finally {
      setCreating(false)
    }
  }

  const previewGeneration = async (generationId: string) => {
    if (!bridge.generationPreview) return
    setGenerationBusy(true)
    setError('')
    try {
      const preview = await bridge.generationPreview({ generationId })
      setGenerationPreview(preview)
    } catch (err) {
      setError(userError(err, '世代预览失败'))
    } finally {
      setGenerationBusy(false)
    }
  }

  const activateGeneration = async (generationId: string, revision: number) => {
    if (!bridge.generationActivate) return
    setGenerationBusy(true)
    setError('')
    try {
      const payload = { generationId, expectedRevision: Math.max(1, revision), operationId: newBridgeULID() }
      await bridge.generationActivate(payload, { attempt: createMutationAttempt('memory.generation.activate', payload) })
      setGenerationPreview(null)
      showNotice('已启用这个记忆世代')
      await Promise.all([loadGenerations(), loadItems()])
    } catch (err) {
      setError(userError(err, '启用记忆世代失败'))
    } finally {
      setGenerationBusy(false)
    }
  }

  const discardGeneration = async (generationId: string, revision: number) => {
    if (!bridge.generationDiscard) return
    setGenerationBusy(true)
    setError('')
    try {
      const payload = { generationId, expectedRevision: Math.max(1, revision), operationId: newBridgeULID() }
      await bridge.generationDiscard(payload, { attempt: createMutationAttempt('memory.generation.discard', payload) })
      setGenerationPreview(null)
      showNotice('已丢弃这个记忆世代')
      await loadGenerations()
    } catch (err) {
      setError(userError(err, '丢弃记忆世代失败'))
    } finally {
      setGenerationBusy(false)
    }
  }

  const forgetItem = async (item: MemoryItemDTO) => {
    if (!bridge.itemForget) return
    setForgetBusy(true)
    setError('')
    try {
      const payload = {
        factId: item.factId,
        mode: 'fact_history' as const,
        expectedRevision: item.revision,
        operationId: newBridgeULID(),
      }
      await bridge.itemForget(payload, { attempt: createMutationAttempt('memory.item.forget', payload) })
      setDrawer({ kind: 'closed' })
      showNotice('已忘记这条记忆')
      await loadItems()
    } catch (err) {
      setError(userError(err, '忘记记忆失败'))
    } finally {
      setForgetBusy(false)
    }
  }

  const correctItem = async (item: MemoryItemDTO) => {
    const trimmed = correctText.trim()
    if (!trimmed || !bridge.itemCorrect) return
    setCorrectBusy(true)
    setError('')
    try {
      const payload = {
        factId: item.factId,
        replacementText: trimmed,
        reason: '更准确',
        expectedRevision: item.revision,
        operationId: newBridgeULID(),
      }
      await bridge.itemCorrect(payload, { attempt: createMutationAttempt('memory.item.correct', payload) })
      setCorrectText('')
      setDrawer({ kind: 'closed' })
      showNotice('已更正这条记忆')
      await loadItems()
    } catch (err) {
      setError(userError(err, '更正记忆失败'))
    } finally {
      setCorrectBusy(false)
    }
  }

  const resolveReview = async (reviewId: string, decision: 'accept' | 'reject') => {
    if (!bridge.reviewResolve) return
    setReviewBusy(true)
    setError('')
    try {
      const payload = { reviewId, decision, expectedRevision: Math.max(1, reviewRevision), operationId: newBridgeULID() }
      await bridge.reviewResolve(payload, { attempt: createMutationAttempt('memory.review.resolve', payload) })
      showNotice(decision === 'accept' ? '已接受待审记忆' : '已拒绝待审记忆')
      await Promise.all([loadReviews(), loadItems()])
    } catch (err) {
      setError(userError(err, '处理待审记忆失败'))
    } finally {
      setReviewBusy(false)
    }
  }

  const preparePurge = async () => {
    setPurgeBusy(true)
    setError('')
    try {
      const payload = { scopeKind: 'user' as const, expectedDatabaseRevision: databaseRevision, operationId: newBridgeULID() }
      const grant = await ops.purgePrepare(payload, { attempt: createMutationAttempt('memory.purge.prepare', payload) })
      setPurgePreview(grant)
    } catch (err) {
      setError(userError(err, '准备清除失败'))
    } finally {
      setPurgeBusy(false)
    }
  }

  const confirmPurge = async () => {
    if (!purgePreview) return
    setPurgeBusy(true)
    setError('')
    try {
      const payload = {
        confirmationToken: purgePreview.confirmationToken,
        snapshotDigest: purgePreview.snapshotDigest,
        expectedDatabaseRevision: databaseRevision,
        operationId: newBridgeULID(),
      }
      await ops.purge(payload, { attempt: createMutationAttempt('memory.purge', payload) })
      setPurgePreview(null)
      setDrawer({ kind: 'closed' })
      showNotice('已清除本机记忆')
      await Promise.all([loadItems(), loadReviews()])
    } catch (err) {
      setError(userError(err, '记忆清除失败'))
    } finally {
      setPurgeBusy(false)
    }
  }

  const exportFabric = async () => {
    setArchiveBusy(true)
    setError('')
    try {
      const result = await ops.export({ format: 'fabric_v2' })
      if (!isFabricExport(result)) {
        setError('完整档案导出失败')
        return
      }
      setFabricExport(result)
      setArtifactId(result.artifactId)
      showNotice(`已生成本机档案 ${result.artifactId}`)
    } catch (err) {
      setError(userError(err, '完整档案导出失败'))
    } finally {
      setArchiveBusy(false)
    }
  }

  const exportLegacy = async () => {
    setArchiveBusy(true)
    setError('')
    try {
      const bundle = await ops.export({})
      if (isFabricExport(bundle) || !('facts' in bundle)) {
        setError('旧格式导出失败')
        return
      }
      const blob = new Blob([JSON.stringify({ exportedAt: new Date().toISOString(), ...bundle }, null, 2)], { type: 'application/json' })
      const url = URL.createObjectURL(blob)
      const link = document.createElement('a')
      link.href = url
      link.download = `lunitide-memory-export-${Date.now()}.json`
      link.click()
      URL.revokeObjectURL(url)
      showNotice('已下载旧格式记忆数据')
    } catch (err) {
      setError(userError(err, '旧格式导出失败'))
    } finally {
      setArchiveBusy(false)
    }
  }

  const previewImport = async () => {
    if (!bridge.importPreview || !artifactId) return
    setArchiveBusy(true)
    setError('')
    try {
      const payload = { sourceArtifactId: artifactId, operationId: newBridgeULID() }
      const preview = await bridge.importPreview(payload, { attempt: createMutationAttempt('memory.import.preview', payload) })
      setImportPreview(preview)
      showNotice('导入预览未写入正式记忆')
    } catch (err) {
      setError(userError(err, '导入预览失败'))
    } finally {
      setArchiveBusy(false)
    }
  }

  const commitImport = async () => {
    if (!bridge.importCommit || !importPreview) return
    setArchiveBusy(true)
    setError('')
    try {
      const payload = {
        previewId: importPreview.previewId,
        archiveDigest: importPreview.archiveDigest,
        manifestDigest: importPreview.manifestDigest,
        expectedDatabaseRevision: importPreview.databaseRevision,
        operationId: newBridgeULID(),
      }
      await bridge.importCommit(payload, { attempt: createMutationAttempt('memory.import.commit', payload) })
      setImportPreview(null)
      showNotice('已导入记忆档案')
      await loadItems()
    } catch (err) {
      setError(userError(err, '导入提交失败'))
    } finally {
      setArchiveBusy(false)
    }
  }

  return (
    <div className="memory-center memory-center-v2">
      <MemoryStatusHeader draft={draft} onOpenSettings={() => setDrawer({ kind: 'settings' })} />
      <div className="memory-toolbar">
        <input type="search" aria-label="搜索记忆" placeholder="搜索记忆…" value={query} onChange={event => setQuery(event.target.value)} />
      </div>
      {error ? <p role="alert">{error}</p> : null}
      {notice ? (
        <p role="status">
          {notice}
          {undoOperationId ? <button type="button" className="ui-btn" onClick={() => void undoCapture()}>撤销</button> : null}
        </p>
      ) : null}
      <MemoryList
        items={items}
        query={query}
        onOpen={factId => setDrawer({ kind: 'item', factId })}
        onForget={factId => {
          const target = items.find(item => item.factId === factId)
          if (target) void forgetItem(target)
        }}
      />
      <button type="button" className="smart-cap-disclose" onClick={() => setDrawer({ kind: 'advanced', section: 'recent' })}>高级管理</button>
      <MemoryDrawer state={drawer} onClose={() => setDrawer({ kind: 'closed' })}>
        {drawer.kind === 'settings' && draft ? (
          <MemorySettingsPanel
            draft={draft}
            busy={busy}
            error={error}
            onChange={setDraft}
            onSave={() => {
              if (!draft || busy) return
              setBusy(true)
              setError('')
              void saveMemorySettings(ops, draft).then(next => {
                setDraft(next)
                showNotice('记忆设置已保存')
                setDrawer({ kind: 'closed' })
              }).catch(err => {
                setError(userError(err, '记忆设置保存失败'))
              }).finally(() => setBusy(false))
            }}
          />
        ) : null}
        {drawer.kind === 'item' ? (
          selected ? (
            <section className="memory-item-detail">
              <p>{selected.text}</p>
              <label>
                更正正文
                <textarea aria-label="更正正文" rows={3} value={correctText} onChange={event => setCorrectText(event.target.value)} />
              </label>
              <button type="button" className="ui-btn" disabled={correctBusy || !correctText.trim()} onClick={() => void correctItem(selected)}>更正</button>
              <button type="button" className="ui-btn danger" disabled={forgetBusy} onClick={() => void forgetItem(selected)}>忘记</button>
              <p className="setting-desc">忘记会擦除记忆正文和检索索引，不会删除原来的对话。</p>
              {itemHistory.length > 0 ? (
                <section aria-label="版本历史">
                  <h3>版本历史</h3>
                  <ol>
                    {itemHistory.map(row => (
                      <li key={`${row.factId}-${row.version}`}>v{row.version} · {row.updatedAt.slice(0, 10)}{row.text ? ` · ${row.text}` : ''}</li>
                    ))}
                  </ol>
                </section>
              ) : null}
            </section>
          ) : <p>这条记忆已不存在。</p>
        ) : null}
        {drawer.kind === 'advanced' ? (
          <MemoryAdvancedPanel
            section={drawer.section}
            createText={createText}
            onCreateText={setCreateText}
            creating={creating}
            error={error}
            onCreate={() => void createItem(createText)}
            reviews={reviews}
            reviewBusy={reviewBusy}
            onAcceptReview={reviewId => void resolveReview(reviewId, 'accept')}
            onRejectReview={reviewId => void resolveReview(reviewId, 'reject')}
            purgePreview={purgePreview}
            purgeBusy={purgeBusy}
            onPreparePurge={() => void preparePurge()}
            onConfirmPurge={() => void confirmPurge()}
            fabricExport={fabricExport}
            importPreview={importPreview}
            artifactId={artifactId}
            onArtifactId={setArtifactId}
            archiveBusy={archiveBusy}
            onExportFabric={() => void exportFabric()}
            onExportLegacy={() => void exportLegacy()}
            onPreviewImport={() => void previewImport()}
            onCommitImport={() => void commitImport()}
            generations={generations}
            generationPreview={generationPreview}
            generationBusy={generationBusy}
            onPreviewGeneration={generationId => void previewGeneration(generationId)}
            onActivateGeneration={(generationId, revision) => void activateGeneration(generationId, revision)}
            onDiscardGeneration={(generationId, revision) => void discardGeneration(generationId, revision)}
          />
        ) : null}
      </MemoryDrawer>
    </div>
  )
}
