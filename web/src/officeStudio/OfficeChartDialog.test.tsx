import { act, cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { afterEach, expect, it, vi } from 'vitest';
import { OfficeChartDialog, type OfficeChartTarget } from './OfficeChartDialog';
import type { OfficeStudioApi, OfficeTaskDetail } from './officeStudioApi';
import type { OfficeChartForm } from './officeChartForm';
afterEach(cleanup);
const target: OfficeChartTarget = { taskId: 'task-a', artifactId: 'ppt-a', baseVersionId: 'v4', expectedRevision: 17, versionNo: 4, node: { id: 'chart-a', label: '第二页 · 图表 1', digest: 'node-digest', valueType: 'chart', text: '', editable: true } };
const chart = (): OfficeChartForm => ({ type: 'column', title: '实际数据', categories: ['本月'], series: [{ name: '金额', values: ['12345.1250000000'] }], x: 11000, y: 22000, width: 33000, height: 44000, legend: true });
it('edits a real loaded table and preserves decimal strings and the original frame in its patch', async () => {
  const readChart = vi.fn(async () => chart()), result = {} as OfficeTaskDetail, patchChart = vi.fn(async () => result), onChanged = vi.fn();
  render(<OfficeChartDialog api={{ readChart, patchChart } as unknown as OfficeStudioApi} target={target} onChanged={onChanged} onClose={vi.fn()}/>);
  const input = await screen.findByLabelText('系列 1 第 1 类数值');
  expect(input).toHaveValue('12345.1250000000');
  expect(readChart).toHaveBeenCalledWith({ taskId: 'task-a', versionId: 'v4', nodeId: 'chart-a', nodeDigest: 'node-digest' });
  fireEvent.change(input, { target: { value: '12345.1250000001' } });
  fireEvent.click(screen.getByRole('button', { name: '保存图表为新版本' }));
  await waitFor(() => expect(patchChart).toHaveBeenCalledOnce());
  expect(patchChart).toHaveBeenCalledWith({ taskId: 'task-a', artifactId: 'ppt-a', baseVersionId: 'v4', expectedRevision: 17, nodeId: 'chart-a', nodeDigest: 'node-digest', chart: { ...chart(), series: [{ name: '金额', values: ['12345.1250000001'] }] } });
  expect(onChanged).toHaveBeenCalledWith(result);
});
it('adds empty cells for user data and prevents saving incomplete new series', async () => {
  const patchChart = vi.fn(); render(<OfficeChartDialog api={{ readChart: vi.fn(async () => chart()), patchChart } as unknown as OfficeStudioApi} target={target} onChanged={vi.fn()} onClose={vi.fn()}/>);
  await screen.findByLabelText('系列 1 第 1 类数值'); fireEvent.click(screen.getByRole('button', { name: '添加系列' }));
  expect(screen.getByLabelText('系列 2 第 1 类数值')).toHaveValue('');
  expect(screen.getByRole('button', { name: '保存图表为新版本' })).toBeDisabled(); expect(patchChart).not.toHaveBeenCalled();
});
it('never populates the new selection with an old chart response', async () => {
  let resolve!: (data: OfficeChartForm) => void;
  const readChart = vi.fn().mockImplementationOnce(() => new Promise(done => { resolve = done; })).mockResolvedValueOnce({ ...chart(), title: '新图表' });
  const props = { api: { readChart, patchChart: vi.fn() } as unknown as OfficeStudioApi, onChanged: vi.fn(), onClose: vi.fn() };
  const view = render(<OfficeChartDialog {...props} target={target}/>);
  view.rerender(<OfficeChartDialog {...props} target={{ ...target, node: { ...target.node, id: 'chart-b' } }}/>);
  await waitFor(() => expect(screen.getByLabelText('图表标题')).toHaveValue('新图表'));
  await act(async () => resolve(chart())); expect(screen.getByLabelText('图表标题')).toHaveValue('新图表');
});
