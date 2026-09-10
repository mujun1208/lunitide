import { useEffect, useRef, useState } from 'react';
import { Dialog } from '../ui/Dialog';
import { chartDecimalValid } from './officeChartForm';
import type { OfficeNode, OfficeStudioApi, OfficeTaskDetail } from './officeStudioApi';
import { officeStudioUserError } from './officeUserError';

export interface OfficeSheetChartTarget {
  taskId: string;
  artifactId: string;
  baseVersionId: string;
  expectedRevision: number;
  versionNo: number;
  node: OfficeNode;
  partDigest: string;
}

const decimal = /^-?(?:0|[1-9][0-9]*)(?:\.[0-9]+)?$/;

function rangeCells(range: string): string[] {
  const match = /^([A-Z]{1,3})([1-9][0-9]*)(?::([A-Z]{1,3})([1-9][0-9]*))?$/.exec(range);
  if (!match || (match[3] && match[1] !== match[3])) throw new Error('图表源范围不受支持。');
  const from = Number(match[2]), to = Number(match[4] || match[2]);
  if (to < from || to - from >= 100 || to > 1048576) throw new Error('图表源范围超出可编辑范围。');
  return Array.from({ length: to - from + 1 }, (_, index) => `${match[1]}${from + index}`);
}

export function sheetRangeError(ranges: string[], columns: string[][], original?: string[][]): string {
  if (!ranges.length) return '此图表没有可编辑的本表源范围。';
  if (ranges.length !== columns.length) return '源范围与填写列不一致。';
  for (const [index, rows] of columns.entries()) {
    let addresses: string[];
    try { addresses = rangeCells(ranges[index]); } catch (cause) { return String(cause); }
    if (rows.length !== addresses.length) return `${ranges[index]} 需要 ${addresses.length} 个单元格，保持原范围大小。`;
    if (index === 0) {
      if (rows.some(value => !value || new TextEncoder().encode(value).length > 1024)) return `${ranges[0]} 类别不能为空或超过 1024 字节。`;
      continue;
    }
    if (rows.some((value, row) => value !== original?.[index]?.[row] && !decimal.test(value))) return `${ranges[index]} 需要显式十进制数值，不能填写公式。`;
    if (rows.some((value,row)=>value !== original?.[index]?.[row] && !chartDecimalValid(value))) return `${ranges[index]} 超出 Excel 的 15 位数值精度，不会自动舍入。`;
  }
  return '';
}

export function OfficeSheetChartDialog({
  api,
  target,
  onChanged,
  onClose,
}: {
  api: OfficeStudioApi;
  target?: OfficeSheetChartTarget;
  onChanged: (detail: OfficeTaskDetail) => void;
  onClose: () => void;
}) {
  const ranges = target?.node.chart?.sourceRanges ?? [];
  const [columns, setColumns] = useState<string[][]>(ranges.map(() => ['']));
  const [original, setOriginal] = useState<string[][]>();
  const [loading, setLoading] = useState(false);
  const request = useRef(0);
  useEffect(() => {
    const generation = ++request.current;
    setColumns([]);
    setOriginal(undefined);
    setError('');
    setBusy(false);
    setLoading(!!target);
    if (!target) return;
    const load = async () => {
      const sourceRanges = target.node.chart?.sourceRanges ?? [];
      const addresses = sourceRanges.map(rangeCells);
      const remaining = new Set(addresses.flat());
      const cells = new Map<string, string>();
      let offset = 0;
      for (let page = 0; page < 128 && remaining.size; page++) {
        const data = await api.preview({ taskId: target.taskId, versionId: target.baseVersionId, ...(offset ? { nodeOffset: offset } : {}) });
        if (request.current !== generation) return;
        if (data.versionId !== target.baseVersionId || data.kind !== 'xlsx') throw new Error('读取到的文件版本不一致，请重新选择图表。');
        for (const node of data.nodes) {
          if (node.location !== target.node.location) continue;
          const address = / · cell:([A-Z]+[1-9][0-9]*)$/.exec(node.label)?.[1];
          if (address && remaining.has(address)) {
            if (node.text.includes('\n') || node.text.includes('\r') || node.valueType === 'cell:richtext') throw new Error('源单元格包含多行或富文本，请使用原文件编辑，保留其格式。');
            cells.set(address, node.text); remaining.delete(address);
          }
        }
        if (!remaining.size) break;
        if (!data.nextNodeOffset || data.nextNodeOffset <= offset) break;
        offset = data.nextNodeOffset;
      }
      if (remaining.size) throw new Error('未完整读取图表源数据，已停止修改。请刷新后重试。');
      const values = addresses.map(column => column.map(address => cells.get(address)!));
      setColumns(values); setOriginal(values);
    };
    void load().catch(cause => { if (request.current === generation) setError(officeStudioUserError(cause, '源数据读取失败。')); })
      .finally(() => { if (request.current === generation) setLoading(false); });
    return () => { request.current++; };
  }, [api, target]);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');
  const invalid = target && original ? sheetRangeError(ranges, columns, original) : '';
  const changed = !!original && columns.some((column, index) => column.some((value, row) => value !== original[index]?.[row]));
  const apply = async () => {
    if (!target || invalid || busy || loading || !original || !changed) return;
    const generation = request.current;
    setBusy(true);
    setError('');
    try {
      const writes = new Map<string, { part: string; range: string; expectedDigest: string; rows: { type: 'text' | 'number'; value: string }[][] }>();
      ranges.forEach((range, index) => rangeCells(range).forEach((address, row) => {
        const value = columns[index][row];
        if (value === original[index][row]) return;
        const write = { part: target.node.location || '', range: address, expectedDigest: target.partDigest, rows: [[{ type: index === 0 ? 'text' as const : 'number' as const, value }]] };
        const previous = writes.get(address);
        if (previous && JSON.stringify(previous) !== JSON.stringify(write)) throw new Error('重叠源单元格的填写值不一致。');
        writes.set(address, write);
      }));
      const combined: Array<(typeof writes extends Map<string, infer T> ? T : never)> = [];
      for (const write of [...writes.values()].sort((a,b) => a.range.localeCompare(b.range, 'en', { numeric: true }))) {
        const last = combined.at(-1);
        const currentAddress = /^([A-Z]+)(\d+)$/.exec(write.range)!;
        const lastAddress = last && /^([A-Z]+)(\d+)$/.exec(last.range.split(':').at(-1)!);
        if (last && lastAddress && lastAddress[1] === currentAddress[1] && Number(lastAddress[2]) + 1 === Number(currentAddress[2])) {
          last.range = `${last.range.split(':')[0]}:${write.range}`;
          last.rows.push(...write.rows);
        } else combined.push({ ...write, rows: [...write.rows] });
      }
      if (combined.length > 32) throw new Error('修改分散在超过 32 个独立区域，请分两次保存。当前修改尚未写入。');
      const next = await api.patchRange({
          taskId: target.taskId,
          artifactId: target.artifactId,
          baseVersionId: target.baseVersionId,
          expectedRevision: target.expectedRevision,
          ranges: combined,
        });
      if (request.current !== generation) return;
      onChanged(next);
      onClose();
    } catch (cause) {
      if (request.current === generation) setError(officeStudioUserError(cause, '图表源范围未保存。'));
    } finally {
      if (request.current === generation) setBusy(false);
    }
  };
  return (
    <Dialog open={!!target} title="修改图表源范围" onClose={() => { if (!busy) onClose(); }} wide>
      <div className="os-chart-dialog">
        <p>
          <b>{target?.node.label}</b> · v{target?.versionNo}
        </p>
        <p className="os-muted">
          修改类别和数值后另存为新版本，图表随源数据更新。原文件保留。
          {target?.node.chart?.cacheState === 'requires-recalculation' ? ' 当前图表需要重新计算。' : ''}
        </p>
        {loading && <p role="status">正在读取图表源数据…</p>}
        {error && <p role="alert">{error}</p>}
        {ranges.map((range, index) => (
          <label key={range}>
            {index === 0 ? '类别' : `系列 ${index}`} {range}
            <textarea
              aria-label={`${range} 的单元格`}
              disabled={busy || loading || !original}
              rows={Math.min(8, Math.max(3, columns[index]?.length || 3))}
              value={(columns[index] ?? []).join('\n')}
              onChange={event =>
                setColumns(current => current.map((column, i) => (i === index ? event.target.value.split(/\r?\n/) : column)))
              }
            />
          </label>
        ))}
        {invalid && (
          <p className="os-preview-notice" role="status">
            {invalid}
          </p>
        )}
        <div className="dialog-actions">
          <button disabled={busy} onClick={onClose}>
            关闭
          </button>
          <button className="primary" disabled={busy || loading || !original || !changed || !!invalid} onClick={() => void apply()}>
            {busy ? '正在保存…' : '按源范围保存为新版本'}
          </button>
        </div>
      </div>
    </Dialog>
  );
}
