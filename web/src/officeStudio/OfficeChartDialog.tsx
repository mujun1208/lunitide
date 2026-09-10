import { useEffect, useRef, useState } from 'react';
import { Dialog } from '../ui/Dialog';
import type { OfficeNode, OfficeStudioApi, OfficeTaskDetail } from './officeStudioApi';
import { chartFormError, type OfficeChartForm } from './officeChartForm';
import { officeStudioUserError } from './officeUserError';

export interface OfficeChartTarget { taskId: string; artifactId: string; baseVersionId: string; expectedRevision: number; versionNo: number; node: OfficeNode }
export function OfficeChartDialog({ api, target, onChanged, onClose }: { api: OfficeStudioApi; target?: OfficeChartTarget; onChanged: (detail: OfficeTaskDetail) => void; onClose: () => void }) {
  const [chart, setChart] = useState<OfficeChartForm>(), [loading, setLoading] = useState(false), [busy, setBusy] = useState(false), [error, setError] = useState(''), [revision, setRevision] = useState(0);
  const generation = useRef(0);
  useEffect(() => {
    const token = ++generation.current; setChart(undefined); setError(''); setBusy(false);
    if (!target?.node.digest) return;
    setLoading(true);
    void api.readChart({ taskId: target.taskId, versionId: target.baseVersionId, nodeId: target.node.id, nodeDigest: target.node.digest }).then(value => { if (token === generation.current) setChart(value); }).catch(cause => { if (token === generation.current) setError(officeStudioUserError(cause, '图表数据读取失败。')); }).finally(() => { if (token === generation.current) setLoading(false); });
    return () => { generation.current++; };
  }, [target, api, revision]);
  const invalid = chart ? chartFormError(chart) : '';
  const apply = async () => {
    if (!target?.node.digest || !chart || invalid || busy || loading) return;
    const snapshot = target, token = generation.current;
    setBusy(true); setError('');
    try {
      const result = await api.patchChart({ taskId: snapshot.taskId, artifactId: snapshot.artifactId, baseVersionId: snapshot.baseVersionId, expectedRevision: snapshot.expectedRevision, nodeId: snapshot.node.id, nodeDigest: snapshot.node.digest!, chart });
      if (token === generation.current) { onChanged(result); onClose(); }
    } catch (cause) { if (token === generation.current) setError(officeStudioUserError(cause, '图表修改未保存。')); }
    finally { if (token === generation.current) setBusy(false); }
  };
  const seriesName = (index: number, name: string) => setChart(current => current && ({ ...current, series: current.series.map((series, i) => i === index ? { ...series, name } : series) }));
  const cell = (column: number, row: number, value: string) => setChart(current => current && ({ ...current, series: current.series.map((series, index) => column === index ? { ...series, values: series.values.map((original, i) => i === row ? value : original) } : series) }));
  return <Dialog open={!!target} title="编辑图表数据" onClose={() => { if (!busy) onClose(); }} wide>
    <div className="os-chart-dialog">
      <p><b>{target?.node.label}</b> · v{target?.versionNo}</p>
      <p className="os-muted">保留原图表的位置和大小，更新数据后生成新草稿。数值按原始十进制文本保存，不改写精度。</p>
      {loading && <p role="status">正在读取当前版本的图表数据…</p>}
      {error && <p role="alert">{error}{!chart && !loading && <button onClick={() => setRevision(value => value + 1)}>重试读取</button>}</p>}
      {chart && <>
        <div className="os-chart-fields"><label>图表标题<input value={chart.title} disabled={busy} onChange={event => setChart({ ...chart, title: event.target.value })}/></label><label>图表类型<select value={chart.type} disabled={busy} onChange={event => setChart({ ...chart, type: event.target.value as OfficeChartForm['type'] })}><option value="column">柱状图</option><option value="bar">条形图</option><option value="line">折线图</option><option value="pie">饼图</option></select></label><label className="os-chart-legend"><input type="checkbox" checked={chart.legend} disabled={busy} onChange={event => setChart({ ...chart, legend: event.target.checked })}/>显示图例</label></div>
        <div className="os-chart-table-scroll" tabIndex={0} aria-label="图表数据表格"><table className="os-chart-table"><thead><tr><th>类别</th>{chart.series.map((series, index) => <th key={index}><input aria-label={`系列 ${index + 1} 名称`} value={series.name} disabled={busy} onChange={event => seriesName(index, event.target.value)}/><button disabled={busy || chart.series.length === 1} onClick={() => setChart({ ...chart, series: chart.series.filter((_, i) => i !== index) })} aria-label={`删除系列 ${index + 1}`}>删除系列</button></th>)}<th>操作</th></tr></thead><tbody>{chart.categories.map((category, row) => <tr key={row}><th><input aria-label={`类别 ${row + 1}`} value={category} disabled={busy} onChange={event => setChart({ ...chart, categories: chart.categories.map((v, i) => i === row ? event.target.value : v) })}/></th>{chart.series.map((series, column) => <td key={column}><input aria-label={`系列 ${column + 1} 第 ${row + 1} 类数值`} inputMode="decimal" value={series.values[row] ?? ''} disabled={busy} onChange={event => cell(column, row, event.target.value)}/></td>)}<td><button disabled={busy || chart.categories.length === 1} onClick={() => setChart({ ...chart, categories: chart.categories.filter((_, i) => i !== row), series: chart.series.map(series => ({ ...series, values: series.values.filter((_, i) => i !== row) })) })} aria-label={`删除类别 ${row + 1}`}>删除</button></td></tr>)}</tbody></table></div>
        <div className="os-row-actions"><button disabled={busy || chart.categories.length >= 100} onClick={() => setChart({ ...chart, categories: [...chart.categories, ''], series: chart.series.map(series => ({ ...series, values: [...series.values, ''] })) })}>添加类别</button><button disabled={busy || chart.series.length >= 6} onClick={() => setChart({ ...chart, series: [...chart.series, { name: '', values: chart.categories.map(() => '') }] })}>添加系列</button><small>{chart.categories.length}/100 个类别 · {chart.series.length}/6 个系列</small></div>
        {invalid && <p className="os-preview-notice" role="status">{invalid}</p>}
      </>}
      <div className="dialog-actions"><button disabled={busy} onClick={onClose}>关闭</button><button className="primary" disabled={busy || loading || !chart || !!invalid} onClick={() => void apply()}>{busy ? '正在保存…' : '保存图表为新版本'}</button></div>
    </div>
  </Dialog>;
}
