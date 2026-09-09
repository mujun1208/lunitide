import React from 'react';
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { afterEach, expect, it, vi } from 'vitest';
import { OfficeMetricPanel, type OfficeMetricActions } from './OfficeMetricPanel';
import { OfficeBundleDialog, type OfficeBundleActions } from './OfficeBundleDialog';
import type {
  OfficeArtifact,
  OfficeBundle,
  OfficeBundleExport,
  OfficeMetric,
  OfficeTaskDetail,
} from './officeStudioApi';

const stamp = '2026-09-07T00:00:00Z';
const artifact: OfficeArtifact = {
  id: 'doc',
  name: '报告.docx',
  kind: 'docx',
  revision: 7,
  headVersionId: 'v2',
  acceptedVersionId: 'v1',
  versions: [
    {
      id: 'v1',
      versionNo: 1,
      quality: 'unverified',
      mode: 'imported',
      size: 100,
      sha256: 'a'.repeat(64),
      createdAt: stamp,
    },
    { id: 'v2', versionNo: 2, quality: 'passed', mode: 'managed', size: 110, sha256: 'b'.repeat(64), createdAt: stamp },
  ],
};
const metric: OfficeMetric = {
  id: 'metric',
  taskId: 'task',
  name: '实际收入',
  sourceVersionId: 'xlsx-v1',
  sourceSha256: 'c'.repeat(64),
  sourceNodeId: 'cell-A1',
  sourceNodeDigest: 'd'.repeat(64),
  rawValue: '12.3456',
  valueType: 'number',
  unit: '万元',
  currency: 'CNY',
  period: '2026 Q2',
  aggregation: 'identity',
  roundingDigits: 2,
  roundingPolicy: 'half_away_from_zero',
  displayValue: '12.35',
  createdAt: stamp,
};
const detail: OfficeTaskDetail = {
  task: {
    id: 'task',
    sessionId: 'session',
    title: '季度报告',
    goal: '',
    revision: 42,
    status: 'draft',
    createdAt: stamp,
    updatedAt: stamp,
  },
  artifacts: [artifact],
  steps: [],
  sources: [],
};
const node = { id: 'paragraph-1', label: '经营总结', text: '收入待填', editable: true, digest: 'e'.repeat(64) };
function metricActions(): OfficeMetricActions {
  return {
    list: vi.fn(async () => ({ items: [metric] })),
    capture: vi.fn(async () => detail),
    apply: vi.fn(async () => detail),
  };
}
const bundle: OfficeBundle = {
  schemaVersion: 1,
  id: 'bundle-1',
  taskId: 'task',
  title: '季度报告交付包',
  createdAt: stamp,
  files: [
    {
      versionId: 'v1',
      artifactId: 'doc',
      name: '报告.docx',
      kind: 'docx',
      sha256: 'a'.repeat(64),
      size: 100,
      quality: 'unverified',
      accepted: true,
    },
  ],
};
const exported: OfficeBundleExport = {
  bundleId: 'bundle-1',
  directory: 'task/exports/bundle-1',
  manifestPath: 'task/exports/bundle-1/manifest.json',
  complete: true,
  files: [
    {
      versionId: 'v1',
      name: '报告.docx',
      path: 'task/exports/bundle-1/报告.docx',
      sha256: 'a'.repeat(64),
      reused: false,
    },
  ],
};
function bundleActions(): OfficeBundleActions {
  return {
    list: vi.fn(async () => ({ items: [] })),
    create: vi.fn(async () => bundle),
    export: vi.fn(async () => exported),
    open: vi.fn(async () => {}),
  };
}
afterEach(cleanup);

it('captures immutable source identity and formatting rather than an editable replacement value', async () => {
  const actions = metricActions(),
    onChanged = vi.fn();
  render(
    <OfficeMetricPanel
      taskId="task"
      artifact={artifact}
      version={artifact.versions[0]}
      node={{ ...node, text: '00012', editable: false }}
      actions={actions}
      onChanged={onChanged}
    />,
  );
  await screen.findByRole('button', { name: /实际收入/ });
  fireEvent.click(screen.getByRole('button', { name: '记录选中内容的指标' }));
  expect(screen.getByText('00012')).toBeTruthy();
  fireEvent.change(screen.getByLabelText('指标名称'), { target: { value: '客户编号' } });
  expect(screen.getByLabelText('小数位数（可留空）')).toHaveProperty('disabled', true);
  fireEvent.click(screen.getByRole('button', { name: '保留来源并记录' }));
  await waitFor(() =>
    expect(actions.capture).toHaveBeenCalledExactlyOnceWith({
      versionId: 'v1',
      nodeId: node.id,
      nodeDigest: node.digest,
      name: '客户编号',
    }),
  );
  expect(onChanged).toHaveBeenCalledWith(detail, false);
  expect(actions.apply).not.toHaveBeenCalled();
});

it('retains a selected source when the target changes and uses the target artifact revision', async () => {
  const actions = metricActions(),
    onChanged = vi.fn();
  const props = { taskId: 'task', artifact, version: artifact.versions[0], node, actions, onChanged };
  const { rerender } = render(<OfficeMetricPanel {...props} />);
  fireEvent.click(await screen.findByRole('button', { name: /实际收入/ }));
  fireEvent.change(screen.getByLabelText('更新后的内容模板'), { target: { value: '本期收入 {{value}}{{unit}}。' } });
  rerender(
    <OfficeMetricPanel
      {...props}
      version={artifact.versions[1]}
      node={{ ...node, id: 'new-target', digest: 'f'.repeat(64) }}
    />,
  );
  fireEvent.click(screen.getByRole('button', { name: '应用指标并生成草稿' }));
  await waitFor(() =>
    expect(actions.apply).toHaveBeenCalledExactlyOnceWith({
      metricId: 'metric',
      targetVersionId: 'v2',
      targetNodeId: 'new-target',
      targetNodeDigest: 'f'.repeat(64),
      template: '本期收入 {{value}}{{unit}}。',
      expectedRevision: 7,
    }),
  );
  expect(onChanged).toHaveBeenCalledWith(detail, true);
});

it('allows explicit rounding only for numeric cells and rejects formula cache as a source', async () => {
  const actions = metricActions();
  const props = {
    taskId: 'task',
    artifact,
    version: artifact.versions[0],
    node: { ...node, editable: false, valueType: 'cell:number', text: '12.3456' },
    actions,
    onChanged: vi.fn(),
  };
  const { rerender } = render(<OfficeMetricPanel {...props} />);
  await screen.findByRole('button', { name: /实际收入/ });
  fireEvent.click(screen.getByRole('button', { name: '记录选中内容的指标' }));
  fireEvent.change(screen.getByLabelText('小数位数（可留空）'), { target: { value: '2' } });
  fireEvent.click(screen.getByRole('button', { name: '保留来源并记录' }));
  await waitFor(() => expect(actions.capture).toHaveBeenCalledWith(expect.objectContaining({ roundingDigits: 2 })));
  rerender(<OfficeMetricPanel {...props} node={{ ...props.node, valueType: 'cell:formula' }} />);
  expect(screen.getByRole('button', { name: '记录选中内容的指标' })).toHaveProperty('disabled', true);
});

it('rejects ambiguous templates and noneditable target nodes, then retains edits on conflict', async () => {
  const actions = metricActions();
  vi.mocked(actions.apply).mockRejectedValue(new Error('版本已变更，请同步后核对'));
  const props = { taskId: 'task', artifact, version: artifact.versions[1], node, actions, onChanged: vi.fn() };
  const { rerender } = render(<OfficeMetricPanel {...props} />);
  fireEvent.click(await screen.findByRole('button', { name: /实际收入/ }));
  fireEvent.change(screen.getByLabelText('更新后的内容模板'), { target: { value: '{{value}} + {{value}}' } });
  expect(screen.getByRole('button', { name: '应用指标并生成草稿' })).toHaveProperty('disabled', true);
  fireEvent.change(screen.getByLabelText('更新后的内容模板'), { target: { value: '{{value}}' } });
  rerender(<OfficeMetricPanel {...props} node={{ ...node, editable: false }} />);
  expect(screen.getByRole('button', { name: '应用指标并生成草稿' })).toHaveProperty('disabled', true);
  rerender(<OfficeMetricPanel {...props} />);
  fireEvent.click(screen.getByRole('button', { name: '应用指标并生成草稿' }));
  expect(await screen.findByRole('alert')).toHaveProperty('textContent', '版本已变更，请同步后核对');
  expect(screen.getByLabelText('更新后的内容模板')).toHaveProperty('value', '{{value}}');
  expect(props.onChanged).not.toHaveBeenCalled();
});

it('exports the accepted older version by default and accurately labels its missing checks', async () => {
  const actions = bundleActions();
  render(
    <OfficeBundleDialog
      open
      taskId="task"
      taskTitle="季度报告"
      artifacts={[artifact]}
      actions={actions}
      onClose={vi.fn()}
    />,
  );
  await screen.findByText(/其中 1 份尚未通过全部检查/);
  expect(screen.getByLabelText('报告.docx 导出版本')).toHaveProperty('value', 'v1');
  fireEvent.click(screen.getByRole('button', { name: '固定版本并导出' }));
  await screen.findByRole('heading', { name: '交付包已保存' });
  expect(actions.create).toHaveBeenCalledExactlyOnceWith({ title: '季度报告交付包', versionIds: ['v1'] });
  expect(actions.export).toHaveBeenCalledWith({ bundleId: 'bundle-1' });
  fireEvent.click(screen.getByRole('button', { name: '打开交付包所在文件夹' }));
  await waitFor(() => expect(actions.open).toHaveBeenCalledWith(exported.manifestPath));
});

it('retries the same fixed manifest after export failure and never labels incomplete output as saved', async () => {
  const actions = bundleActions();
  vi.mocked(actions.export)
    .mockRejectedValueOnce(new Error('写入失败'))
    .mockResolvedValueOnce({ ...exported, complete: false, files: [] });
  render(
    <OfficeBundleDialog
      open
      taskId="task"
      taskTitle="季度报告"
      artifacts={[artifact]}
      actions={actions}
      onClose={vi.fn()}
    />,
  );
  fireEvent.click(screen.getByRole('button', { name: '固定版本并导出' }));
  await screen.findByRole('alert');
  fireEvent.click(screen.getByRole('button', { name: '固定版本并导出' }));
  await screen.findByRole('heading', { name: '交付包尚未完整导出' });
  expect(actions.create).toHaveBeenCalledTimes(1);
  expect(actions.export).toHaveBeenCalledTimes(2);
  expect(screen.queryByRole('heading', { name: '交付包已保存' })).toBeNull();
});

it('keeps explicit version choices through background polling and requires at least one file', async () => {
  const actions = bundleActions();
  const props = { open: true, taskId: 'task', taskTitle: '季度报告', artifacts: [artifact], actions, onClose: vi.fn() };
  const { rerender } = render(<OfficeBundleDialog {...props} />);
  fireEvent.change(screen.getByLabelText('报告.docx 导出版本'), { target: { value: 'v2' } });
  rerender(<OfficeBundleDialog {...props} artifacts={[{ ...artifact, revision: 8 }]} />);
  expect(screen.getByLabelText('报告.docx 导出版本')).toHaveProperty('value', 'v2');
  fireEvent.click(screen.getByRole('checkbox', { name: '报告.docx' }));
  expect(screen.getByRole('button', { name: '固定版本并导出' })).toHaveProperty('disabled', true);
  expect(screen.getByText('请选择需要放入交付包的文件。')).toBeTruthy();
  expect(actions.create).not.toHaveBeenCalled();
});
