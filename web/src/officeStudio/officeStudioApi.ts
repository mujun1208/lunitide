export type OfficeKind = 'pptx' | 'docx' | 'xlsx' | 'pdf'
export type OfficeQuality = 'unverified' | 'checking' | 'partial' | 'passed' | 'blocked' | 'stale'
export type OfficeRunStatus = 'draft' | 'queued' | 'planning' | 'running' | 'validating' | 'succeeded' | 'waiting_input' | 'waiting_approval' | 'cancelling' | 'cancelled' | 'failed' | 'interrupted'

export interface OfficeBriefFact {
  factId: string; value: string; unit?: string; period?: string; sourceId?: string
  locator?: string; status?: string; locked?: boolean
}
export interface OfficeNarrativeNode {
  nodeId?: string
  title?: string
  purpose?: string
  claim?: string
}
export interface OfficeBrief {
  audience?: string; purpose?: string; language?: string; targetLength?: number
  deliverables?: OfficeKind[]; facts?: OfficeBriefFact[]
  outline?: OfficeNarrativeNode[]
  confidentiality?: string
}
export interface OfficeBrandImport {
  brandId: string
  colors?: Record<string, string>
  fonts?: { latin?: string; east?: string }
  asset: { sourceUrl: string; author?: string; license: string; digest: string; logoDigest?: string; commercial?: boolean }
}
export interface OfficeTask {
  id: string; sessionId: string; projectId?: string; title: string; goal: string
  goalTruncated?: boolean
  revision: number; status: OfficeRunStatus; createdAt: string; updatedAt: string
  styleId?: string
  brief?: OfficeBrief
  brandId?: string
}
export interface OfficeCheck {
  id: string; label: string; status: 'passed' | 'failed' | 'unavailable' | 'pending' | 'missing' | 'unsupported'
  severity: 'info' | 'warning' | 'blocking'; message: string; nodeId?: string
  messageTruncated?: boolean
}
export interface OfficeVersion {
  id: string; versionNo: number; quality: OfficeQuality; mode: 'managed' | 'imported'
  size: number; sha256: string; createdAt: string; parentVersionId?: string
  summary?: string; path?: string; validations?: OfficeCheck[]
}
export interface OfficeArtifact {
  role?: 'reference' | 'deliverable'
  id: string; revision: number; name: string; kind: OfficeKind; headVersionId: string
  acceptedVersionId?: string; versions: OfficeVersion[]
}
export interface OfficeStep {
  id: string; label: string; status: string; summary?: string; createdAt?: string
  summaryTruncated?: boolean
}
export interface OfficeSource {
  id: string; name: string; versionId?: string; location?: string
  rawValue?: string; displayValue?: string; transform?: string; stale?: boolean
  transformTruncated?: boolean
}
export interface OfficeTaskDetail extends SnapshotPage {
  task: OfficeTask; artifacts: OfficeArtifact[]; steps: OfficeStep[]; sources: OfficeSource[]
  loadNotice?: string
  committed?: boolean; snapshotIncomplete?: boolean; taskSnapshotStale?: boolean
}
export interface OfficeNode {
  id: string; label: string; text: string; location?: string; digest?: string; editable?: boolean; valueType?: string
  image?: OfficeImageInfo
  chart?: OfficeChartInfo
}
export type OfficeChartInfo = import('../generated/bridge').OfficeChartInfoDTO
export type OfficeSlideChart = import('../generated/bridge').OfficeSlideChartDTO
export type OfficeImageInfo = import('../generated/bridge').OfficeImageInfoDTO
export type OfficeStorageUsage = import('../generated/bridge').OfficeStorageUsageResult
export type OfficeStorageSweepReport = import('../generated/bridge').OfficeStorageSweepResult
export type OfficeImageReplacement = import('../generated/bridge').OfficeArtifactReplaceImagePayload
export interface OfficePreview {
  versionId: string; kind: OfficeKind; content: string; notice?: string
  previewBasis: string; nodes: OfficeNode[]
  pdfReady: boolean; truncated: boolean
  nodeOffset?:number; nextNodeOffset?:number; totalNodes?:number
  parts?: Array<{ name: string; sha256: string; size: number }>
}
export interface OfficeRendererStatus {
  components: Array<{ id: string; label: string; status: 'ready' | 'unavailable' | 'error'; detail: string }>
}
export interface OfficeStudioApi {
  readChart(payload: {taskId:string;versionId:string;nodeId:string;nodeDigest:string}): Promise<OfficeSlideChart>
  patchChart(payload: {taskId:string;artifactId:string;baseVersionId:string;expectedRevision:number;nodeId:string;nodeDigest:string;chart:OfficeSlideChart}): Promise<OfficeTaskDetail>
  patchRange(payload: import('../generated/bridge').OfficeArtifactPatchRangePayload): Promise<OfficeTaskDetail>
  refreshNative(payload: {taskId:string;artifactId:string;versionId:string;expectedRevision:number}): Promise<OfficeTaskDetail>
  storageUsage(): Promise<OfficeStorageUsage>
  sweepStorage(payload: {dryRun:boolean;limit?:number}): Promise<OfficeStorageSweepReport>
  replaceImage(payload: OfficeImageReplacement): Promise<OfficeTaskDetail>
  diff(payload: {taskId:string;baseVersionId:string;versionId:string;nodeOffset?:number;partOffset?:number}):Promise<OfficeDiff>
  captureMetric(payload: import('../generated/bridge').OfficeMetricCapturePayload): Promise<OfficeTaskDetail>
  listMetrics(payload: {taskId:string}): Promise<{items:OfficeMetric[]}>
  applyMetric(payload: import('../generated/bridge').OfficeMetricApplyPayload): Promise<OfficeTaskDetail>
  createBundle(payload: {taskId:string;title:string;versionIds:string[]}): Promise<OfficeBundle>
  listBundles(payload: {taskId:string}): Promise<{items:OfficeBundle[]}>
  exportBundle(payload: {taskId:string;bundleId:string}): Promise<OfficeBundleExport>
  list(payload?: { query?: string; sessionId?: string }): Promise<{ items: OfficeTask[] }>
  create(payload: { title: string; goal?: string; sessionId?: string; includeHistory?: boolean }): Promise<OfficeTaskDetail>
  get(payload: { taskId: string }): Promise<OfficeTaskDetail>
  update(payload: { taskId: string; expectedRevision: number; title: string; goal: string; styleId?: string; brief?: OfficeBrief; brand?: OfficeBrandImport }): Promise<OfficeTaskDetail>
  sync(payload: { taskId: string; artifactPath?: string }): Promise<OfficeTaskDetail>
  importArtifact(payload: { taskId: string; attachmentId: string; name?: string; artifactId?:string; baseVersionId?:string; expectedRevision?:number }): Promise<OfficeTaskDetail>
  preview(payload: { taskId: string; versionId: string; nodeOffset?:number }): Promise<OfficePreview>
  patch(payload: { taskId: string; artifactId: string; baseVersionId: string; expectedRevision: number; nodeId: string; nodeDigest: string; text: string }): Promise<OfficeTaskDetail>
  validate(payload: { taskId: string; versionId: string }): Promise<OfficeTaskDetail>
  accept(payload: { taskId: string; artifactId: string; versionId: string; expectedRevision: number; formal?: boolean }): Promise<OfficeTaskDetail>
  restore(payload: { taskId: string; artifactId: string; versionId: string; expectedRevision: number }): Promise<OfficeTaskDetail>
  exportArtifact(payload: { taskId: string; versionId: string; name?: string; draft: boolean }): Promise<{ path: string; absolutePath?: string; notice?: string }>
  probe(): Promise<OfficeRendererStatus>
  readChunk(payload: { taskId: string; versionId: string; offset: number; limit?: number }): Promise<{contentBase64:string; nextOffset:number; total:number; eof:boolean}>
  cancel(payload: {taskId:string}): Promise<OfficeTaskDetail>
  openExport(payload: {taskId:string; path:string; reveal?:boolean}): Promise<{opened:string}>
}

// The production implementation is supplied by the shared typed bridge. Keeping
// this boundary explicit also lets component tests exercise real state changes.
export const officeStudioApi: OfficeStudioApi = {
  readChart: p => requestOffice('office.artifact.chart',p),
  patchChart: p => detail(requestOffice('office.artifact.patchChart',p)),
  patchRange: p => detail(requestOffice('office.artifact.patchRange',p)),
  refreshNative: p => detail(requestOffice('office.artifact.refresh',p)),
  storageUsage: () => requestOffice('office.storage.usage',{}),
  sweepStorage: p => requestOffice('office.storage.sweep',p),
  replaceImage: p => detail(requestOffice('office.artifact.replaceImage',p)),
  diff: p => requestOffice('office.artifact.diff',p),
  captureMetric: p => detail(requestOffice('office.metric.capture',p)),
  listMetrics: p => collectOfficeItems(cursor => requestOffice('office.metric.list',{...p,...cursor})),
  applyMetric: p => detail(requestOffice('office.metric.apply',p)),
  createBundle: p => requestOffice('office.bundle.create',p),
  listBundles: p => collectOfficeItems(cursor => requestOffice('office.bundle.list',{...p,...cursor})),
  exportBundle: p => requestOffice('office.bundle.export',p),
  list: p => collectOfficeItems(cursor => requestOffice('office.task.list', {...p,...cursor})),
  create: p => detail(requestOffice('office.task.create',p)),
  get: p => detail(requestOffice('office.task.get',p)),
  update: p => detail(requestOffice('office.task.update',p)),
  sync: async p => {
    try {
      return await detail(requestOffice('office.task.sync',p))
    } catch (error) {
      if (error instanceof BridgeClientError && error.code === 'OFFICE_BUSY' && !p.artifactPath) {
        return detail(requestOffice('office.task.get',{taskId:p.taskId}))
      }
      throw error
    }
  },
  importArtifact: p => detail(requestOffice('office.artifact.import',p)),
  preview: p => requestOffice('office.artifact.preview',p),
  patch: p => detail(requestOffice('office.artifact.patch',p)),
  validate: p => detail(requestOffice('office.artifact.validate',p)),
  accept: p => detail(requestOffice('office.artifact.accept',p)),
  restore: p => detail(requestOffice('office.artifact.restore',p)),
  exportArtifact: p => requestOffice('office.artifact.export',p),
  probe: () => requestOffice('office.renderer.probe',{}),
  readChunk: p => requestOffice('office.artifact.readChunk',p),
  cancel: p => detail(requestOffice('office.task.cancel',p)),
  openExport: p => requestOffice('office.artifact.open',p),
}
import { BridgeClientError, requestOffice } from '../bridge/client'
import { collectOfficeDetail, collectOfficeItems } from './officeSnapshot'
import type { SnapshotPage } from './officeSnapshot'
const detail = (result: Promise<OfficeTaskDetail>) => collectOfficeDetail(result,(taskId,cursor)=>requestOffice('office.task.get',{taskId,...cursor}))
export type OfficeMetric = import('../generated/bridge').OfficeMetricDTO
export type OfficeDiff = import('../generated/bridge').OfficeArtifactDiffResult
export type OfficeBundle = import('../generated/bridge').OfficeBundleDTO
export type OfficeBundleExport = import('../generated/bridge').OfficeBundleExportResult
