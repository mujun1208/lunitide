import React, { useState } from 'react'
import type {
  GenerationSummary,
  MemoryExportResult,
  MemoryGenerationPreviewResult,
  MemoryImportPreviewResult,
  MemoryPurgePrepareResult,
  MemoryReviewListResult,
} from '../generated/bridge'

type ReviewItem = MemoryReviewListResult['items'][number]
type AdvancedSection = 'recent' | 'review' | 'privacy' | 'import-export' | 'purge'
type AdvancedTab = 'create' | 'review' | 'import-export' | 'purge' | 'generation'

const GENERATION_STATE: Record<GenerationSummary['state'], string> = {
  building: '整理中',
  ready: '可启用',
  active: '当前生效',
  discarded: '已丢弃',
  failed: '失败',
  archived: '已归档',
}

const CHANGE_LABEL: Record<MemoryGenerationPreviewResult['changes'][number]['change'], string> = {
  add: '新增',
  remove: '移除',
  replace: '替换',
}

export function isFabricExport(result: MemoryExportResult): result is Extract<MemoryExportResult, { format: 'fabric_v2' }> {
  return 'format' in result && result.format === 'fabric_v2'
}

export function MemoryAdvancedPanel({
  section,
  createText,
  onCreateText,
  onCreate,
  creating,
  error,
  reviews = [],
  onAcceptReview,
  onRejectReview,
  reviewBusy,
  purgePreview,
  onPreparePurge,
  onConfirmPurge,
  purgeBusy,
  fabricExport,
  importPreview,
  artifactId,
  onArtifactId,
  onExportFabric,
  onExportLegacy,
  onPreviewImport,
  onCommitImport,
  archiveBusy,
  generations = [],
  generationPreview,
  generationBusy,
  onPreviewGeneration,
  onActivateGeneration,
  onDiscardGeneration,
}: {
  section: AdvancedSection
  createText: string
  onCreateText: (value: string) => void
  onCreate: () => void
  creating?: boolean
  error?: string
  reviews?: ReviewItem[]
  onAcceptReview?: (reviewId: string) => void
  onRejectReview?: (reviewId: string) => void
  reviewBusy?: boolean
  purgePreview?: MemoryPurgePrepareResult | null
  onPreparePurge?: () => void
  onConfirmPurge?: () => void
  purgeBusy?: boolean
  fabricExport?: Extract<MemoryExportResult, { format: 'fabric_v2' }> | null
  importPreview?: MemoryImportPreviewResult | null
  artifactId?: string
  onArtifactId?: (value: string) => void
  onExportFabric?: () => void
  onExportLegacy?: () => void
  onPreviewImport?: () => void
  onCommitImport?: () => void
  archiveBusy?: boolean
  generations?: GenerationSummary[]
  generationPreview?: MemoryGenerationPreviewResult | null
  generationBusy?: boolean
  onPreviewGeneration?: (generationId: string) => void
  onActivateGeneration?: (generationId: string, revision: number) => void
  onDiscardGeneration?: (generationId: string, revision: number) => void
}): React.JSX.Element {
  const [tab, setTab] = useState<AdvancedTab>(
    section === 'review' ? 'review' : section === 'purge' ? 'purge' : section === 'import-export' ? 'import-export' : 'create',
  )
  return (
    <div className="memory-advanced">
      <div className="memory-advanced-nav">
        <button type="button" className="ui-btn" aria-pressed={tab === 'create'} onClick={() => setTab('create')}>手动新增</button>
        <button type="button" className="ui-btn" aria-pressed={tab === 'review'} onClick={() => setTab('review')}>待审</button>
        <button type="button" className="ui-btn" aria-pressed={tab === 'import-export'} onClick={() => setTab('import-export')}>导入/导出</button>
        <button type="button" className="ui-btn" aria-pressed={tab === 'purge'} onClick={() => setTab('purge')}>清除</button>
        <button type="button" className="ui-btn" aria-pressed={tab === 'generation'} onClick={() => setTab('generation')}>记忆世代</button>
      </div>
      {tab === 'create' ? (
        <>
          <h3>手动新增</h3>
          <p>只写稳定偏好或身份事实。提交走 canonical 保存，不会生成待确认横幅。</p>
          <label>
            记忆正文
            <textarea aria-label="记忆正文" rows={3} value={createText} onChange={event => onCreateText(event.target.value)} />
          </label>
          <button type="button" className="ui-btn primary" disabled={creating || !createText.trim()} onClick={onCreate}>
            {creating ? '保存中…' : '保存为记忆'}
          </button>
          {error ? <p role="alert">{error}</p> : null}
          <p className="setting-desc">忘记一条记忆只会擦除记忆正文和索引，不会删除原来的对话。</p>
        </>
      ) : null}
      {tab === 'review' ? (
        <>
          <h3>待审记忆</h3>
          {reviews.length === 0 ? <p className="setting-desc">没有需要审核的记忆。</p> : (
            <ul className="memory-list">
              {reviews.map(item => (
                <li key={item.reviewId}>
                  <p>{item.text || '（无正文）'}</p>
                  <div className="org-bound-actions">
                    <button type="button" className="ui-btn primary" disabled={reviewBusy} onClick={() => onAcceptReview?.(item.reviewId)}>接受</button>
                    <button type="button" className="ui-btn" disabled={reviewBusy} onClick={() => onRejectReview?.(item.reviewId)}>拒绝</button>
                  </div>
                </li>
              ))}
            </ul>
          )}
          {error ? <p role="alert">{error}</p> : null}
        </>
      ) : null}
      {tab === 'import-export' ? (
        <>
          <h3>导入/导出</h3>
          <p className="setting-desc">完整档案只返回本机编号和摘要，不会把记忆正文送到 Bridge。旧格式仍可下载 JSON。</p>
          <div className="org-bound-actions">
            <button type="button" className="ui-btn primary" disabled={archiveBusy} onClick={onExportFabric}>导出完整档案</button>
            <button type="button" className="ui-btn" disabled={archiveBusy} onClick={onExportLegacy}>导出旧格式</button>
          </div>
          {fabricExport ? (
            <p>
              档案 {fabricExport.artifactId} · 当前 {fabricExport.counts.current} 条 · 墓碑 {fabricExport.counts.tombstones} 条 · 修订 {fabricExport.databaseRevision}
            </p>
          ) : null}
          <label>
            档案编号
            <input aria-label="档案编号" value={artifactId ?? ''} onChange={event => onArtifactId?.(event.target.value.trim())} />
          </label>
          <div className="org-bound-actions">
            <button type="button" className="ui-btn" disabled={archiveBusy || !(artifactId ?? '').trim()} onClick={onPreviewImport}>预览导入</button>
            <button type="button" className="ui-btn primary" disabled={archiveBusy || !importPreview} onClick={onCommitImport}>确认导入</button>
          </div>
          {importPreview ? (
            <p>
              可写入 {importPreview.counts.accepted} 条，冲突 {importPreview.counts.conflicts} 条，墓碑 {importPreview.counts.tombstones} 条。关闭抽屉不会提交。
            </p>
          ) : null}
          {error ? <p role="alert">{error}</p> : null}
        </>
      ) : null}
      {tab === 'purge' ? (
        <>
          <h3>清除记忆</h3>
          <p>清除不会删除原聊天记录。需要先准备一次性确认令牌，再确认执行。</p>
          {purgePreview ? (
            <>
              <p role="status">将清除事实 {purgePreview.counts.facts} 条、候选 {purgePreview.counts.candidates} 条。令牌 {purgePreview.expiresAt} 过期。</p>
              <button type="button" className="ui-btn danger" disabled={purgeBusy} onClick={onConfirmPurge}>确认清除</button>
            </>
          ) : (
            <button type="button" className="ui-btn" disabled={purgeBusy || !onPreparePurge} onClick={onPreparePurge}>准备清除</button>
          )}
          {error ? <p role="alert">{error}</p> : null}
        </>
      ) : null}
      {tab === 'generation' ? (
        <>
          <h3>记忆世代</h3>
          <p className="setting-desc">世代由后台整理产生，这里只能预览、启用或丢弃，不能手动创建。</p>
          {generations.length === 0 ? <p className="setting-desc">还没有记忆世代。</p> : (
            <ul className="memory-list">
              {generations.map(item => (
                <li key={item.generationId}>
                  <p>{GENERATION_STATE[item.state]} · {item.memberCount} 条 · {item.createdAt.slice(0, 10)}</p>
                  <div className="org-bound-actions">
                    <button type="button" className="ui-btn" disabled={generationBusy} onClick={() => onPreviewGeneration?.(item.generationId)}>预览</button>
                    {item.state === 'ready' || item.state === 'archived' ? (
                      <button type="button" className="ui-btn primary" disabled={generationBusy} onClick={() => onActivateGeneration?.(item.generationId, item.revision)}>启用</button>
                    ) : null}
                    {item.state === 'building' || item.state === 'ready' || item.state === 'failed' ? (
                      <button type="button" className="ui-btn" disabled={generationBusy} onClick={() => onDiscardGeneration?.(item.generationId, item.revision)}>丢弃</button>
                    ) : null}
                  </div>
                </li>
              ))}
            </ul>
          )}
          {generationPreview ? (
            <section aria-label="世代预览">
              <h4>变更预览</h4>
              {generationPreview.changes.length === 0 ? <p className="setting-desc">没有可见变更。</p> : (
                <ul>
                  {generationPreview.changes.map(change => (
                    <li key={`${change.factId}-${change.change}-${change.toVersion ?? 0}`}>
                      {CHANGE_LABEL[change.change]} · {change.afterText || change.beforeText || change.factId}
                    </li>
                  ))}
                </ul>
              )}
            </section>
          ) : null}
          {error ? <p role="alert">{error}</p> : null}
        </>
      ) : null}
    </div>
  )
}
