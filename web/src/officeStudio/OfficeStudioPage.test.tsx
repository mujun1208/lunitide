import React, { useEffect } from 'react';
import { act, cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { OfficeStudioPage, type OfficeConversationOptions } from './OfficeStudioPage';
import { OFFICE_PANEL_WIDTH_KEY } from './useOfficePanelResize';
import type { OfficeStudioApi, OfficeTaskDetail, OfficePreview } from './officeStudioApi';

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
          id: 'v1',
          versionNo: 1,
          quality: 'unverified',
          mode: 'imported',
          size: 128,
          sha256: 'a'.repeat(64),
          createdAt: '2026-09-06T00:00:00Z',
        },
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
  nodes: [
    { id: `node-${versionId}`, label: '经营总结', text: `正文 ${versionId}`, editable: true, digest: 'c'.repeat(64) },
  ],
});
const apiFor = (detail = fixture()): OfficeStudioApi => ({
  storageUsage: vi.fn(async () => ({limitBytes:2147483648,usedBytes:0,reservedBytes:0,totalBytes:0,referencedBytes:0,unmanagedBytes:0,blobCount:0,activeLeaseCount:0,overLimit:false})),
  sweepStorage: vi.fn(async () => ({dryRun:true,candidates:0,removed:0,freedBytes:0,releasedReservationBytes:0,hasMore:false,errors:[]})),
  replaceImage: vi.fn(async () => detail),
  refreshNative: vi.fn(async () => detail),
  readChart: vi.fn(),
  patchChart: vi.fn(async () => detail),
  patchRange: vi.fn(async () => detail),
  list: vi.fn(async () => ({ items: [detail.task] })),
  get: vi.fn(async () => detail),
  create: vi.fn(async () => detail),
  update: vi.fn(async () => detail),
  sync: vi.fn(async () => detail),
  importArtifact: vi.fn(async () => detail),
  preview: vi.fn(async (p) => preview(p.versionId)),
  patch: vi.fn(async () => detail),
  validate: vi.fn(async () => detail),
  accept: vi.fn(async () => detail),
  restore: vi.fn(async () => detail),
  exportArtifact: vi.fn(async () => ({ path: 'exports/季度汇报.docx' })),
  probe: vi.fn(async () => ({ components: [] })),
  readChunk: vi.fn(),
  cancel: vi.fn(async () => ({ ...detail, task: { ...detail.task, status: 'cancelled' as const } })),
  openExport: vi.fn(),
  listMetrics: vi.fn(async () => ({ items: [] })),
  captureMetric: vi.fn(async () => detail),
  applyMetric: vi.fn(async () => detail),
  listBundles: vi.fn(async () => ({ items: [] })),
  createBundle: vi.fn(),
  exportBundle: vi.fn(),
  diff: vi.fn(),
});
const open = async (
  api: OfficeStudioApi,
  renderConversation: (task: OfficeTaskDetail['task'], options: OfficeConversationOptions) => React.ReactNode = () => (
    <div>原会话输入框</div>
  ),
) => {
  localStorage.setItem('lunitide:office-studio:last-task', taskId);
  render(<OfficeStudioPage initialTaskId={taskId} api={api} renderConversation={renderConversation} onOpenSession={vi.fn()} />);
  await screen.findByRole('heading', { name: '季度汇报' });
  await screen.findByText('正文 v2');
};

it('keeps an uploaded source separate until a generated deliverable appears', async () => {
  const detail = fixture();
  detail.artifacts[0].role = 'reference';
  detail.sources = [{ id: 'source', name: '季度汇报.docx', versionId: 'v2', location: '上传的参考材料' }];
  const api = apiFor(detail);
  localStorage.setItem('lunitide:office-studio:last-task', taskId);
  render(<OfficeStudioPage initialTaskId={taskId} api={api} renderConversation={() => <div>原会话输入框</div>} onOpenSession={vi.fn()} />);
  await screen.findByRole('heading', { name: '尚未生成交付文件' });
  expect(api.preview).not.toHaveBeenCalled();
  expect(screen.getByText('参考材料已就绪，尚无交付文件')).toBeVisible();
  fireEvent.click(screen.getByRole('button', { name: '来源' }));
  expect(screen.getByText('上传的参考材料')).toBeVisible();
  fireEvent.click(screen.getByRole('button', { name: '对话' }));
  const references = screen.getByRole('region', { name: '对话参考附件' });
  fireEvent.click(within(references).getByRole('button', { name: '查看附件 季度汇报.docx' }));
  await waitFor(() => expect(api.openExport).toHaveBeenCalledWith({taskId, path: 'exports/季度汇报.docx', reveal: false}));
  expect(api.exportArtifact).toHaveBeenCalledWith({taskId, versionId: 'v2', name: '季度汇报.docx', draft: true});
  expect(api.preview).not.toHaveBeenCalled();
  expect(screen.getByRole('heading', { name: '尚未生成交付文件' })).toBeVisible();
  expect(within(screen.getByLabelText('任务与文件')).queryByText('季度汇报.docx')).not.toBeInTheDocument();
  expect(screen.queryByRole('heading', { name: '本次工作' })).not.toBeInTheDocument();
});

it('updates native fields only on request and binds the selected historical version and artifact revision', async () => {
  const api = apiFor(); await open(api);
  expect(api.refreshNative).not.toHaveBeenCalled();
  fireEvent.click(screen.getByRole('button', { name: '版本' }));
  fireEvent.click(screen.getByRole('button', { name: 'v1' }));
  await screen.findByText('正文 v1');
  fireEvent.click(screen.getByRole('button', { name: '更新目录与页码' }));
  expect(screen.getByRole('dialog')).toHaveTextContent('当前查看的 v1');
  fireEvent.click(screen.getByRole('button', { name: '更新并保存新版本' }));
  await waitFor(() => expect(api.refreshNative).toHaveBeenCalledWith({ taskId, artifactId: 'doc', versionId: 'v1', expectedRevision: 7 }));
  expect(api.accept).not.toHaveBeenCalled();
});

it('keeps native-update failures visible inside the dialog and retains the original file', async () => {
  const api = apiFor(); vi.mocked(api.refreshNative).mockRejectedValue(new Error('本机未安装 LibreOffice'));
  await open(api); fireEvent.click(screen.getByRole('button', { name: '更新目录与页码' }));
  fireEvent.click(screen.getByRole('button', { name: '更新并保存新版本' }));
  await waitFor(() => expect(within(screen.getByRole('dialog')).getByRole('alert')).toHaveTextContent('本机未安装 LibreOffice'));
  expect(screen.getByText('正文 v2')).toBeInTheDocument(); expect(api.restore).not.toHaveBeenCalled();
});

it('ignores a late stop error after the native update has already completed', async () => {
  const api = apiFor();
  let complete!: (detail: OfficeTaskDetail) => void;
  let failStop!: (reason: Error) => void;
  vi.mocked(api.refreshNative).mockImplementation(() => new Promise(resolve => { complete = resolve; }));
  vi.mocked(api.cancel).mockImplementation(() => new Promise((_, reject) => { failStop = reject; }));
  await open(api);
  fireEvent.click(screen.getByRole('button', { name: '更新目录与页码' }));
  fireEvent.click(screen.getByRole('button', { name: '更新并保存新版本' }));
  await waitFor(() => expect(api.refreshNative).toHaveBeenCalledTimes(1));
  fireEvent.click(screen.getByRole('button', { name: '停止更新' }));
  expect(api.cancel).toHaveBeenCalledWith({ taskId });
  await act(async () => complete(fixture()));
  await waitFor(() => expect(screen.queryByRole('dialog')).not.toBeInTheDocument());
  await act(async () => failStop(new Error('late stop acknowledgement')));
  expect(screen.queryByText('late stop acknowledgement')).not.toBeInTheDocument();
  expect(screen.getByText('正文 v2')).toBeInTheDocument();
  expect(api.refreshNative).toHaveBeenCalledTimes(1);
});
beforeEach(() => {
  Element.prototype.scrollIntoView = vi.fn();
});
afterEach(() => {
  cleanup();
  localStorage.clear();
  vi.restoreAllMocks();
});

describe('Office Studio production state', () => {
  it('drags the conversation boundary without reloading the artifact and remembers its width', async () => {
    localStorage.setItem(OFFICE_PANEL_WIDTH_KEY, '440');
    const api = apiFor();
    await open(api);
    const divider = screen.getByRole('separator', { name: '调整产物与对话区域宽度' });
    const pointer = (target: EventTarget, type: string, x: number) => act(() => {
      const event = new Event(type, { bubbles: true });
      Object.defineProperties(event, { clientX: { value: x }, button: { value: 0 } });
      target.dispatchEvent(event);
    });
    const calls = vi.mocked(api.preview).mock.calls.length;
    pointer(divider, 'pointerdown', 800);
    pointer(window, 'pointermove', 640);
    expect(divider).toHaveAttribute('aria-valuenow', '600');
    pointer(window, 'pointerup', 640);
    expect(localStorage.getItem(OFFICE_PANEL_WIDTH_KEY)).toBe('600');
    expect(api.preview).toHaveBeenCalledTimes(calls);
    fireEvent.click(screen.getByRole('button', { name: '关闭详情面板' }));
    expect(divider).not.toBeVisible();
    fireEvent.click(screen.getByRole('button', { name: '对话' }));
    expect(divider).toHaveAttribute('aria-valuenow', '600');
    cleanup();
    await open(api);
    expect(screen.getByRole('separator')).toHaveAttribute('aria-valuenow', '600');
  });

  it('supports keyboard bounds, double-click reset and cancellation of a resize', async () => {
    await open(apiFor());
    const divider = screen.getByRole('separator');
    fireEvent.keyDown(divider, { key: 'Home' });
    expect(divider).toHaveAttribute('aria-valuenow', '350');
    fireEvent.keyDown(divider, { key: 'ArrowRight' });
    expect(divider).toHaveAttribute('aria-valuenow', '350');
    fireEvent.keyDown(divider, { key: 'ArrowLeft', shiftKey: true });
    expect(divider).toHaveAttribute('aria-valuenow', '430');
    fireEvent.keyDown(divider, { key: 'End' });
    expect(divider.getAttribute('aria-valuenow')).toBe(divider.getAttribute('aria-valuemax'));
    fireEvent.doubleClick(divider);
    expect(divider).toHaveAttribute('aria-valuenow', String(Math.round(window.innerWidth * 0.35)));
  });

  it('keeps visible files after a committed incomplete snapshot and requires read-only refresh before another edit', async () => {
    const detail = fixture(),
      api = apiFor(detail);
    vi.mocked(api.accept).mockResolvedValue({
      ...detail,
      artifacts: [],
      committed: true,
      snapshotIncomplete: true,
      taskSnapshotStale: true,
      loadNotice: '操作已保存，最新记录暂时读不到。',
    });
    await open(api);
    const syncCalls = vi.mocked(api.sync).mock.calls.length;
    fireEvent.click(screen.getByRole('button', { name: '版本' }));
    const old = screen.getByRole('button', { name: 'v1' }).closest('li')!;
    fireEvent.click(within(old).getByRole('button', { name: '接受为草稿' }));
    await screen.findByText('操作已保存，最新记录暂时读不到。');
    expect(screen.getByText('正文 v2')).toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'v1' })).toBeInTheDocument();
    expect(screen.getByRole('button', { name: '修改任务信息' })).toBeDisabled();
    expect(within(old).getByRole('button', { name: '接受为草稿' })).toBeDisabled();
    vi.mocked(api.get).mockRejectedValueOnce(new Error('只读刷新暂不可用'));
    fireEvent.click(screen.getByRole('button', { name: '重新读取完整记录' }));
    await screen.findByText('只读刷新暂不可用');
    fireEvent.click(screen.getByRole('button', { name: '重新读取完整记录' }));
    await waitFor(() => expect(screen.queryByText('操作已保存，最新记录暂时读不到。')).not.toBeInTheDocument());
    expect(screen.getByRole('button', { name: '修改任务信息' })).toBeEnabled();
    expect(api.accept).toHaveBeenCalledOnce();
    expect(api.sync).toHaveBeenCalledTimes(syncCalls);
  });

  it('retains a newly committed task when its first view read fails without creating it or starting its chat twice', async () => {
    const api = apiFor(),
      conversation = vi.fn((_task: OfficeTaskDetail['task'], _options: OfficeConversationOptions) => <div>原会话输入框</div>);
    vi.mocked(api.list).mockResolvedValue({ items: [] });
    vi.mocked(api.create).mockResolvedValue({
      ...fixture(),
      artifacts: [],
      committed: true,
      snapshotIncomplete: true,
      taskSnapshotStale: true,
      loadNotice: '任务已创建，正在等待读取。',
    });
    vi.mocked(api.get).mockRejectedValue(new Error('详情读取失败'));
    render(<OfficeStudioPage api={api} renderConversation={conversation} onOpenSession={vi.fn()} />);
    await screen.findByText('还没有办公任务。从上面输入目标即可开始。');
    fireEvent.change(screen.getByLabelText('想完成什么工作'), { target: { value: '只创建一次的任务' } });
    fireEvent.click(screen.getByRole('button', { name: '开始工作 →' }));
    await screen.findByRole('heading', { name: '季度汇报' });
    await screen.findByRole('button', { name: '重新读取完整记录' });
    expect(conversation).not.toHaveBeenCalled();
    expect(screen.queryByText('详情读取失败')).not.toBeInTheDocument();
    vi.mocked(api.get).mockResolvedValue(fixture());
    fireEvent.click(screen.getByRole('button', { name: '重新读取完整记录' }));
    await screen.findByText('原会话输入框');
    expect(api.create).toHaveBeenCalledOnce();
    expect(api.sync).not.toHaveBeenCalled();
    expect(conversation.mock.calls[0][1].initialPrompt).toBe('只创建一次的任务');
  });

  it('reports a completed file import as saved when the following list read fails', async () => {
    const api = apiFor(),
      upload = vi.fn().mockResolvedValue(undefined);
    localStorage.setItem('lunitide:office-studio:last-task', taskId);
    render(
      <OfficeStudioPage initialTaskId={taskId} api={api} renderConversation={() => <div />} onOpenSession={vi.fn()} onImportFiles={upload} />,
    );
    await screen.findByText('正文 v2');
    vi.mocked(api.get).mockRejectedValueOnce(new Error('列表读取失败'));
    const file = new File(['actual upload handled by the supplied importer'], '修改版.docx');
    fireEvent.change(document.querySelector('input[type="file"][multiple]')!, { target: { files: [file] } });
    await screen.findByText(/操作已保存，最新记录暂时读不到。当前保留/);
    expect(screen.getByText('正文 v2')).toBeInTheDocument();
    expect(screen.queryByText('列表读取失败')).not.toBeInTheDocument();
    fireEvent.click(screen.getByRole('button', { name: '重新读取完整记录' }));
    await waitFor(() => expect(screen.queryByRole('button', { name: '重新读取完整记录' })).not.toBeInTheDocument());
    expect(upload).toHaveBeenCalledOnce();
  });
  it('keeps an open edit bound to its original version when a background sync changes the head', async () => {
    const original = fixture(),
      api = apiFor(original);
    vi.mocked(api.patch).mockRejectedValue(new Error('原编辑版本已有更新。'));
    let options!: OfficeConversationOptions;
    await open(api, (_, current) => {
      options = current;
      return <div />;
    });
    fireEvent.click(screen.getByRole('button', { name: '选择 经营总结' }));
    fireEvent.click(screen.getByRole('button', { name: '修改这部分' }));
    fireEvent.change(screen.getByLabelText('修改后的文字'), { target: { value: '仍基于原文修改' } });
    vi.mocked(api.sync).mockResolvedValue({
      ...original,
      artifacts: [
        {
          ...original.artifacts[0],
          revision: 8,
          headVersionId: 'v3',
          versions: [
            ...original.artifacts[0].versions,
            { ...original.artifacts[0].versions[1], id: 'v3', versionNo: 3 },
          ],
        },
      ],
    });
    await act(async () => {
      options.onActivityChange(false);
    });
    await screen.findByText('正文 v3');
    fireEvent.click(screen.getByRole('button', { name: '保存为新版本' }));
    await screen.findByText('原编辑版本已有更新。');
    expect(api.patch).toHaveBeenCalledWith(
      expect.objectContaining({ baseVersionId: 'v2', expectedRevision: 7, nodeId: 'node-v2', text: '仍基于原文修改' }),
    );
    expect(screen.getByLabelText('修改后的文字')).toHaveValue('仍基于原文修改');
  });
  it('shows a partial-read notice after a committed change and retries reading without replaying the mutation', async () => {
    const detail = fixture(),
      api = apiFor(detail);
    vi.mocked(api.accept).mockResolvedValue({ ...detail, loadNotice: '修改已保存，历史记录暂未读取完整。' });
    await open(api);
    fireEvent.click(screen.getByRole('button', { name: '版本' }));
    fireEvent.click(
      within(screen.getByRole('button', { name: 'v1' }).closest('li')!).getByRole('button', { name: '接受为草稿' }),
    );
    await screen.findByText('修改已保存，历史记录暂未读取完整。');
    fireEvent.click(screen.getByRole('button', { name: '重新读取完整记录' }));
    await waitFor(() => expect(screen.queryByText('修改已保存，历史记录暂未读取完整。')).toBeNull());
    expect(api.accept).toHaveBeenCalledTimes(1);
    expect(api.get).toHaveBeenCalledTimes(2);
  });
  it('loads variable-sized preview pages, edits tail nodes, and resets pagination when changing versions', async () => {
    const api = apiFor();
    vi.mocked(api.preview).mockImplementation(async (payload) => ({
      ...preview(payload.versionId),
      nodeOffset: payload.nodeOffset || 0,
      nextNodeOffset: payload.nodeOffset ? 9 : 3,
      totalNodes: 9,
      nodes: [
        {
          ...preview(payload.versionId).nodes[0],
          id: `node-${payload.nodeOffset || 0}`,
          text: payload.nodeOffset ? '尾部正文' : `正文 ${payload.versionId}`,
        },
      ],
    }));
    await open(api);
    fireEvent.click(screen.getByRole('button', { name: '下一批内容' }));
    await screen.findByText('尾部正文');
    expect(api.preview).toHaveBeenLastCalledWith({ taskId, versionId: 'v2', nodeOffset: 3 });
    expect(screen.getByRole('button', { name: '下一批内容' })).toBeDisabled();
    fireEvent.click(screen.getByRole('button', { name: '选择 经营总结' }));
    fireEvent.click(screen.getByRole('button', { name: '修改这部分' }));
    fireEvent.change(screen.getByLabelText('修改后的文字'), { target: { value: '尾部修改' } });
    fireEvent.click(screen.getByRole('button', { name: '保存为新版本' }));
    await waitFor(() =>
      expect(api.patch).toHaveBeenCalledWith(
        expect.objectContaining({ nodeId: 'node-3', baseVersionId: 'v2', text: '尾部修改' }),
      ),
    );
    await screen.findByText('正文 v2');
    fireEvent.click(screen.getByRole('button', { name: '下一批内容' }));
    await screen.findByText('尾部正文');
    fireEvent.click(screen.getByRole('button', { name: '上一批内容' }));
    await screen.findByText('正文 v2');
    expect(api.preview).toHaveBeenLastCalledWith({ taskId, versionId: 'v2' });
    fireEvent.click(screen.getByRole('button', { name: '下一批内容' }));
    await screen.findByText('尾部正文');
    fireEvent.click(screen.getByRole('button', { name: '版本' }));
    fireEvent.click(screen.getByRole('button', { name: 'v1' }));
    await screen.findByText('正文 v1');
    expect(api.preview).toHaveBeenLastCalledWith({ taskId, versionId: 'v1' });
    expect(screen.getByRole('button', { name: '上一批内容' })).toBeDisabled();
  });
  it('imports a modified file against the viewed historical version instead of creating a separate artifact', async () => {
    const api = apiFor(),
      importFiles = vi.fn(async () => {});
    localStorage.setItem('lunitide:office-studio:last-task', taskId);
    render(
      <OfficeStudioPage
        initialTaskId={taskId}
        api={api}
        renderConversation={() => <div />}
        onOpenSession={vi.fn()}
        onImportFiles={importFiles}
      />,
    );
    await screen.findByText('正文 v2');
    fireEvent.click(screen.getByRole('button', { name: '版本' }));
    fireEvent.click(screen.getByRole('button', { name: 'v1' }));
    await screen.findByText('正文 v1');
    fireEvent.click(screen.getByRole('button', { name: '导入当前文件的修改版' }));
    const file = new File(['PK12345'], '人工修改.docx');
    fireEvent.change(screen.getByLabelText('导入当前文件的修改版'), { target: { files: [file] } });
    await waitFor(() =>
      expect(importFiles).toHaveBeenCalledWith(
        expect.objectContaining({ id: taskId }),
        [file],
        expect.any(Function),
        expect.any(AbortSignal),
        { artifactId: 'doc', baseVersionId: 'v1', expectedRevision: 7, kind: 'docx' },
      ),
    );
    await screen.findByText('已导入为修改草稿，历史和已接受版本保持不变。');
  });
  it('shows an actual empty catalog and starts one request in the shared conversation', async () => {
    const api = apiFor();
    vi.mocked(api.list).mockResolvedValue({ items: [] });
    const prompts: Array<string | undefined> = [];
    function Conversation({ options }: { options: OfficeConversationOptions }) {
      useEffect(() => {
        prompts.push(options.initialPrompt);
        options.onReady();
      }, []);
      return <div>原会话输入框</div>;
    }
    render(
      <OfficeStudioPage
        api={api}
        renderConversation={(_, options) => <Conversation options={options} />}
        onOpenSession={vi.fn()}
      />,
    );
    expect(await screen.findByText('还没有办公任务。从上面输入目标即可开始。')).toBeInTheDocument();
    fireEvent.change(screen.getByLabelText('想完成什么工作'), { target: { value: '根据数据生成经营报告' } });
    fireEvent.click(screen.getByRole('button', { name: '开始工作 →' }));
    await screen.findByText('原会话输入框');
    expect(api.create).toHaveBeenCalledExactlyOnceWith({ title: '根据数据生成经营报告', goal: '根据数据生成经营报告' });
    fireEvent.click(screen.getByRole('button', { name: '检查' }));
    fireEvent.click(screen.getByRole('button', { name: '对话' }));
    expect(prompts).toEqual(['根据数据生成经营报告']);
  });

  it('creates from the single home form, imports references, then starts the shared conversation', async () => {
    const api = apiFor(),
      timeline: string[] = [],
      created = { ...fixture(), artifacts: [] },
      importFiles = vi.fn(async () => {
        timeline.push('import');
      });
    vi.mocked(api.list).mockResolvedValue({ items: [] });
    vi.mocked(api.create).mockImplementation(async () => {
      timeline.push('create');
      return created;
    });
    vi.mocked(api.get).mockImplementation(async () => {
      timeline.push('get');
      return fixture();
    });
    const conversation = vi.fn((_task: OfficeTaskDetail['task'], options: OfficeConversationOptions) => (
      <div>带参考文件的对话：{options.initialPrompt}</div>
    ));
    render(
      <OfficeStudioPage
        api={api}
        renderConversation={conversation}
        onOpenSession={vi.fn()}
        onImportFiles={importFiles}
      />,
    );
    await screen.findByText('还没有办公任务。从上面输入目标即可开始。');
    expect(screen.queryByRole('button', { name: '＋ 新建任务' })).not.toBeInTheDocument();
    const reference = new File(['reference'], '参考材料.docx', {
      type: 'application/vnd.openxmlformats-officedocument.wordprocessingml.document',
    });
    fireEvent.change(screen.getByLabelText('添加参考文件'), { target: { files: [reference] } });
    expect(screen.getByText('参考材料.docx')).toBeInTheDocument();
    fireEvent.change(screen.getByLabelText('想完成什么工作'), { target: { value: '参考附件制作汇报' } });
    fireEvent.click(screen.getByRole('button', { name: '开始工作 →' }));
    await screen.findByText('带参考文件的对话：参考附件制作汇报');
    expect(timeline.slice(0, 3)).toEqual(['create', 'import', 'get']);
    expect(importFiles).toHaveBeenCalledWith(
      expect.objectContaining({ id: taskId }),
      [reference],
      expect.any(Function),
      expect.any(AbortSignal),
    );
    fireEvent.click(screen.getByRole('button', { name: '＋ 新建任务' }));
    expect(screen.getByLabelText('想完成什么工作')).toHaveValue('');
    expect(screen.queryByRole('dialog', { name: '新建办公任务' })).not.toBeInTheDocument();
  });

  it('keeps the same mounted conversation when switching inspector tabs', async () => {
    const mounted = vi.fn(),
      unmounted = vi.fn();
    function Conversation() {
      useEffect(() => {
        mounted();
        return unmounted;
      }, []);
      return <textarea aria-label="原输入框" defaultValue="未发送草稿" />;
    }
    await open(apiFor(), () => <Conversation />);
    fireEvent.click(screen.getByRole('button', { name: '版本' }));
    expect(screen.queryByLabelText('原输入框')).not.toBeVisible();
    fireEvent.click(screen.getByRole('button', { name: '对话' }));
    expect(screen.getByLabelText('原输入框')).toHaveValue('未发送草稿');
    expect(mounted).toHaveBeenCalledOnce();
    expect(unmounted).not.toHaveBeenCalled();
  });

  it('accepts a historical draft using artifact revision without upgrading its quality', async () => {
    const detail = fixture(),
      api = apiFor(detail);
    vi.mocked(api.accept).mockResolvedValue({
      ...detail,
      artifacts: [{ ...detail.artifacts[0], acceptedVersionId: 'v1', revision: 8 }],
    });
    await open(api);
    fireEvent.click(screen.getByRole('button', { name: '版本' }));
    const oldVersion = screen.getByRole('button', { name: 'v1' }).closest('li')!;
    fireEvent.click(within(oldVersion).getByRole('button', { name: '接受为草稿' }));
    await screen.findByText('已接受为草稿，检查状态保持不变。');
    expect(api.accept).toHaveBeenCalledWith({ taskId, artifactId: 'doc', versionId: 'v1', expectedRevision: 7 });
    expect(within(oldVersion).getByText('尚未检查')).toBeInTheDocument();
    expect(screen.queryByText('所需检查已通过')).not.toBeInTheDocument();
  });

  it('allows stopping a pending validation and does not mark it verified', async () => {
    const api = apiFor();
    let finish!: (detail: OfficeTaskDetail) => void;
    vi.mocked(api.validate).mockImplementation(
      () =>
        new Promise((resolve) => {
          finish = resolve;
        }),
    );
    await open(api);
    fireEvent.click(screen.getByRole('button', { name: '检查' }));
    fireEvent.click(screen.getByRole('button', { name: '检查此版本' }));
    const stop = await screen.findByRole('button', { name: '停止检查' });
    expect(stop).toBeEnabled();
    fireEvent.click(stop);
    await waitFor(() => expect(api.cancel).toHaveBeenCalledWith({ taskId }));
    await act(async () => {
      finish({ ...fixture(), task: { ...fixture().task, status: 'cancelled' as const } });
    });
    expect(await screen.findByRole('button', { name: '检查此版本' })).toBeEnabled();
    expect(screen.queryByText('所需检查已通过')).not.toBeInTheDocument();
  });

  it('preserves an edit draft and shows conflicts without replacing the displayed version', async () => {
    const api = apiFor();
    vi.mocked(api.patch).mockRejectedValue(new Error('文件已有新版本，请基于新版重试。'));
    await open(api);
    fireEvent.click(screen.getByRole('button', { name: '选择 经营总结' }));
    fireEvent.click(screen.getByRole('button', { name: '修改这部分' }));
    fireEvent.change(screen.getByLabelText('修改后的文字'), { target: { value: '需要保留的修改' } });
    fireEvent.click(screen.getByRole('button', { name: '保存为新版本' }));
    expect(await screen.findByText('文件已有新版本，请基于新版重试。')).toBeInTheDocument();
    expect(screen.getByLabelText('修改后的文字')).toHaveValue('需要保留的修改');
    expect(api.patch).toHaveBeenCalledWith(
      expect.objectContaining({ baseVersionId: 'v2', nodeId: 'node-v2', expectedRevision: 7, text: '需要保留的修改' }),
    );
  });

  it('does not turn a late preview into the newly selected historical version', async () => {
    const api = apiFor();
    let stale!: (preview: OfficePreview) => void;
    vi.mocked(api.preview).mockImplementation((p) =>
      p.versionId === 'v2'
        ? new Promise((resolve) => {
            stale = resolve;
          })
        : Promise.resolve(preview(p.versionId)),
    );
    localStorage.setItem('lunitide:office-studio:last-task', taskId);
    render(<OfficeStudioPage initialTaskId={taskId} api={api} renderConversation={() => <div />} onOpenSession={vi.fn()} />);
    await screen.findByRole('heading', { name: '季度汇报' });
    fireEvent.click(screen.getByRole('button', { name: '版本' }));
    fireEvent.click(screen.getByRole('button', { name: 'v1' }));
    await screen.findByText('正文 v1');
    await act(async () => {
      stale(preview('v2'));
    });
    expect(screen.getByText('正文 v1')).toBeInTheDocument();
    expect(screen.queryByText('正文 v2')).not.toBeInTheDocument();
  });

  it('exports the selected historical version as a draft rather than exporting the latest', async () => {
    const api = apiFor();
    await open(api);
    fireEvent.click(screen.getByRole('button', { name: '版本' }));
    fireEvent.click(screen.getByRole('button', { name: 'v1' }));
    fireEvent.click(screen.getByRole('button', { name: '导出副本' }));
    fireEvent.click(screen.getByRole('button', { name: '保存副本' }));
    await waitFor(() =>
      expect(api.exportArtifact).toHaveBeenCalledWith({
        taskId,
        versionId: 'v1',
        name: '季度汇报_v1.docx',
        draft: true,
      }),
    );
    expect(api.accept).not.toHaveBeenCalled();
  });
});
