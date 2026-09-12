import { createRoot } from 'react-dom/client';
import { OfficeStudioPage } from './OfficeStudioPage';
import type { OfficePreview, OfficeStudioApi, OfficeTaskDetail } from './officeStudioApi';

const taskId = '01ARZ3NDEKTSV4RRFFQ69G5FA0';

const fixture = (): OfficeTaskDetail => ({
  task: {
    id: taskId,
    sessionId: '01ARZ3NDEKTSV4RRFFQ69G5FA1',
    projectId: '01ARZ3NDEKTSV4RRFFQ69G5FA2',
    title: '季度汇报',
    goal: '根据已提供的数据整理季度汇报',
    revision: 42,
    status: 'succeeded',
    createdAt: '2026-09-07T00:00:00Z',
    updatedAt: '2026-09-07T00:00:00Z',
  },
  artifacts: [
    {
      id: 'doc',
      role: 'deliverable',
      revision: 7,
      name: '季度汇报.docx',
      kind: 'docx',
      headVersionId: 'v2',
      versions: [
        {
          id: 'v2',
          versionNo: 2,
          quality: 'partial',
          mode: 'imported',
          size: 130,
          sha256: 'b'.repeat(64),
          createdAt: '2026-09-07T00:00:00Z',
          validations: [
            { id: 'structure', label: '结构检查', status: 'passed', severity: 'info', message: '结构完整' },
            {
              id: 'layout',
              label: '排版检查',
              status: 'unavailable',
              severity: 'warning',
              message: '排版组件尚未配置',
            },
          ],
        },
      ],
    },
  ],
  steps: [],
  sources: [],
});

const preview = (versionId: string): OfficePreview => ({
  versionId,
  kind: 'docx',
  content: '',
  previewBasis: '结构预览',
  pdfReady: false,
  truncated: false,
  nodes: [{ id: `node-${versionId}`, label: '经营总结', text: `正文 ${versionId}`, editable: true, digest: 'c'.repeat(64) }],
});

const detail = fixture();
const api: OfficeStudioApi = {
  storageUsage: async () => ({
    limitBytes: 2147483648,
    usedBytes: 0,
    reservedBytes: 0,
    totalBytes: 0,
    referencedBytes: 0,
    unmanagedBytes: 0,
    blobCount: 0,
    activeLeaseCount: 0,
    overLimit: false,
  }),
  sweepStorage: async () => ({
    dryRun: true,
    candidates: 0,
    removed: 0,
    freedBytes: 0,
    releasedReservationBytes: 0,
    hasMore: false,
    errors: [],
  }),
  replaceImage: async () => detail,
  refreshNative: async () => detail,
  readChart: async () => ({ type: 'bar', title: '', categories: [], series: [], x: 0, y: 0, width: 0, height: 0, legend: false }),
  patchChart: async () => detail,
  patchRange: async () => detail,
  list: async () => ({ items: [detail.task] }),
  get: async () => detail,
  create: async () => detail,
  update: async () => detail,
  sync: async () => detail,
  importArtifact: async () => detail,
  preview: async (p) => preview(p.versionId),
  patch: async () => detail,
  validate: async () => detail,
  accept: async () => detail,
  restore: async () => detail,
  exportArtifact: async () => ({ path: 'exports/季度汇报.docx' }),
  probe: async () => ({ components: [] }),
  readChunk: async () => ({ contentBase64: '', nextOffset: 0, total: 0, eof: true }),
  cancel: async () => ({ ...detail, task: { ...detail.task, status: 'cancelled' } }),
  openExport: async () => ({ opened: 'exports/季度汇报.docx' }),
  listMetrics: async () => ({ items: [] }),
  captureMetric: async () => detail,
  applyMetric: async () => detail,
  listBundles: async () => ({ items: [] }),
  createBundle: async () => ({ schemaVersion: 1, id: '01ARZ3NDEKTSV4RRFFQ69G5FA3', taskId, title: '预览', files: [], createdAt: '2026-09-07T00:00:00Z' }),
  exportBundle: async () => ({ path: 'exports/bundle.zip' }),
  diff: async () => ({ parts: [], nodes: [] }),
} as OfficeStudioApi;

localStorage.setItem('lunitide:office-studio:last-task', taskId);
createRoot(document.getElementById('root')!).render(
  <OfficeStudioPage
    initialTaskId={taskId}
    api={api}
    renderConversation={() => <div>原会话输入框</div>}
    onOpenSession={() => undefined}
  />,
);
