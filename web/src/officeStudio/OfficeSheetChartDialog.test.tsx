import { act, cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { afterEach, expect, it, vi } from 'vitest';
import { OfficeSheetChartDialog, sheetRangeError, type OfficeSheetChartTarget } from './OfficeSheetChartDialog';
import type { OfficeStudioApi, OfficeTaskDetail } from './officeStudioApi';
afterEach(cleanup);
const preview = async () => ({ versionId:'v8',kind:'xlsx',nodes: [
  ...['旧项目一','旧项目二'].map((text,index)=>({label:`xl/worksheets/sheet1.xml · cell:A${index+2}`,text,location:'xl/worksheets/sheet1.xml',valueType:'cell:text'})),
  ...['001.00','=SUM(B2)'].map((text,index)=>({label:`xl/worksheets/sheet1.xml · cell:B${index+2}`,text,location:'xl/worksheets/sheet1.xml',valueType:index?'cell:formula':'cell:number'})),
],nextNodeOffset:4,totalNodes:4 });

const target: OfficeSheetChartTarget = {
  taskId: 'task-a',
  artifactId: 'book-a',
  baseVersionId: 'v8',
  expectedRevision: 4,
  versionNo: 8,
  partDigest: 'd'.repeat(64),
  node: {
    id: 'chart-sheet',
    label: '每周 数据 · 图表',
    text: '',
    location: 'xl/worksheets/sheet1.xml',
    digest: 'n'.repeat(64),
    editable: false,
    valueType: 'chart',
    chart: {
      chartPart: 'xl/charts/chart1.xml',
      chartSha256: 'c'.repeat(64),
      seriesCount: 1,
      categoryCount: 2,
      x: 0,
      y: 0,
      width: 1,
      height: 1,
      sourceRanges: ['A2:A3', 'B2:B3'],
      cacheState: 'requires-recalculation',
    },
  },
};

it('rejects formula text in numeric chart sources', () => {
  expect(sheetRangeError(['A2:A3', 'B2:B3'], [['项目','另一个'], ['=SUM(B2)','1']])).toContain('十进制数值');
});

it('patches the worksheet ranges and keeps the chart object read-only', async () => {
  const result = {} as OfficeTaskDetail;
  const patchRange = vi.fn(async () => result);
  const onChanged = vi.fn();
  render(
    <OfficeSheetChartDialog
      api={{ patchRange, preview: vi.fn(preview) } as unknown as OfficeStudioApi}
      target={target}
      onChanged={onChanged}
      onClose={vi.fn()}
    />,
  );
  expect(screen.getByText(/当前图表需要重新计算/)).toBeInTheDocument();
  await waitFor(() => expect(screen.getByLabelText('A2:A3 的单元格')).toHaveValue('旧项目一\n旧项目二'));
  fireEvent.change(screen.getByLabelText('A2:A3 的单元格'), { target: { value: '本月\n上月' } });
  fireEvent.change(screen.getByLabelText('B2:B3 的单元格'), { target: { value: '12.50\n8' } });
  fireEvent.click(screen.getByRole('button', { name: '按源范围保存为新版本' }));
  expect(patchRange).toHaveBeenCalledWith({
    taskId: 'task-a',
    artifactId: 'book-a',
    baseVersionId: 'v8',
    expectedRevision: 4,
    ranges: [
      { part: 'xl/worksheets/sheet1.xml', range: 'A2:A3', expectedDigest: 'd'.repeat(64), rows: [[{ type: 'text', value: '本月' }], [{ type: 'text', value: '上月' }]] },
      { part: 'xl/worksheets/sheet1.xml', range: 'B2:B3', expectedDigest: 'd'.repeat(64), rows: [[{ type: 'number', value: '12.50' }], [{ type: 'number', value: '8' }]] },
    ],
  });
  await vi.waitFor(() => expect(onChanged).toHaveBeenCalledWith(result));
});

it('leaves unchanged formulas and numeric category types out of the write set', async () => {
  const patchRange=vi.fn(async (_payload: Parameters<OfficeStudioApi['patchRange']>[0])=>({} as OfficeTaskDetail));
  render(<OfficeSheetChartDialog api={{patchRange,preview:vi.fn(preview)} as unknown as OfficeStudioApi} target={target} onChanged={vi.fn()} onClose={vi.fn()}/>);
  await waitFor(()=>expect(screen.getByLabelText('B2:B3 的单元格')).toHaveValue('001.00\n=SUM(B2)'));
  expect(screen.getByRole('button',{name:'按源范围保存为新版本'})).toBeDisabled();
  fireEvent.change(screen.getByLabelText('A2:A3 的单元格'),{target:{value:'新项目\n旧项目二'}});
  fireEvent.click(screen.getByRole('button',{name:'按源范围保存为新版本'}));
  await waitFor(()=>expect(patchRange).toHaveBeenCalledOnce());
  expect(patchRange.mock.calls[0][0].ranges).toEqual([{part:target.node.location,range:'A2',expectedDigest:target.partDigest,rows:[[{type:'text',value:'新项目'}]]}]);
});

it('ignores source data returning after the selected chart has closed', async () => {
  let finish!: (value: Awaited<ReturnType<typeof preview>>) => void;
  const api={preview:vi.fn(()=>new Promise(resolve=>{finish=resolve;})),patchRange:vi.fn()} as unknown as OfficeStudioApi;
  const view=render(<OfficeSheetChartDialog api={api} target={target} onChanged={vi.fn()} onClose={vi.fn()}/>);
  view.rerender(<OfficeSheetChartDialog api={api} target={undefined} onChanged={vi.fn()} onClose={vi.fn()}/>);
  await act(async()=>finish(await preview()));
  expect(screen.queryByRole('dialog')).not.toBeInTheDocument();
  expect(api.patchRange).not.toHaveBeenCalled();
});
