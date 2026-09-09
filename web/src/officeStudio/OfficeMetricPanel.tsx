import React, { useEffect, useState } from 'react';
import type { OfficeArtifact, OfficeNode, OfficeTaskDetail, OfficeVersion, OfficeMetric } from './officeStudioApi';

export interface OfficeMetricActions {
  list: () => Promise<{ items: OfficeMetric[] }>;
  capture: (input: {
    versionId: string;
    nodeId: string;
    nodeDigest: string;
    name: string;
    unit?: string;
    currency?: string;
    period?: string;
    roundingDigits?: number;
  }) => Promise<OfficeTaskDetail>;
  apply: (input: {
    metricId: string;
    targetVersionId: string;
    targetNodeId: string;
    targetNodeDigest: string;
    template: string;
    expectedRevision: number;
  }) => Promise<OfficeTaskDetail>;
}
export function OfficeMetricPanel({
  taskId,
  artifact,
  version,
  node,
  actions,
  onChanged,
  readOnly = false,
}: {
  taskId: string;
  artifact?: OfficeArtifact;
  version?: OfficeVersion;
  node?: OfficeNode;
  actions: OfficeMetricActions;
  onChanged: (detail: OfficeTaskDetail, selectHead: boolean) => void;
  readOnly?: boolean;
}): React.JSX.Element {
  const [metrics, setMetrics] = useState<OfficeMetric[]>([]);
  const [revision, setRevision] = useState(0);
  const [captureOpen, setCaptureOpen] = useState(false);
  const [selectedId, setSelectedId] = useState('');
  const [name, setName] = useState('');
  const [unit, setUnit] = useState('');
  const [currency, setCurrency] = useState('');
  const [period, setPeriod] = useState('');
  const [digits, setDigits] = useState('');
  const [template, setTemplate] = useState('{{value}}{{unit}}');
  const [busy, setBusy] = useState(false);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState('');
  const [notice, setNotice] = useState('');
  const listRef = React.useRef(actions.list);
  const scopeRef = React.useRef(taskId);
  const mounted = React.useRef(true);
  const operation = React.useRef(false);
  useEffect(() => {
    mounted.current = true;
    return () => {
      mounted.current = false;
    };
  }, []);
  listRef.current = actions.list;
  scopeRef.current = taskId;
  useEffect(() => {
    let active = true;
    setLoading(true);
    void listRef
      .current()
      .then((result) => {
        if (active) setMetrics(result.items);
      })
      .catch((cause) => {
        if (active) setError(cause instanceof Error ? cause.message : '指标读取失败。');
      })
      .finally(() => {
        if (active) setLoading(false);
      });
    return () => {
      active = false;
    };
  }, [taskId, revision]);
  const run = async (work: () => Promise<OfficeTaskDetail>, selectHead: boolean) => {
    if (operation.current || readOnly) return;
    operation.current = true;
    const currentTaskId = taskId;
    setBusy(true);
    setError('');
    setNotice('');
    try {
      const detail = await work();
      if (!mounted.current || scopeRef.current !== currentTaskId) return;
      onChanged(detail, selectHead);
      setRevision((value) => value + 1);
      setCaptureOpen(false);
      setNotice(selectHead ? '已根据真实来源生成修改草稿，原接受版保持不变。' : '指标已记录，原始值和来源版本已保留。');
    } catch (cause) {
      if (mounted.current && scopeRef.current === currentTaskId)
        setError(cause instanceof Error ? cause.message : '指标操作失败。');
    } finally {
      operation.current = false;
      if (mounted.current && scopeRef.current === currentTaskId) setBusy(false);
    }
  };
  const selected = metrics.find((item) => item.id === selectedId);
  const nodeKind = node?.valueType?.replace(/^cell:/, '') || 'text';
  const numericSource = nodeKind === 'number';
  const canCapture = !readOnly && !!version && !!node?.digest && !['formula', 'error'].includes(nodeKind);
  const canApply =
    !readOnly &&
    !!selected &&
    !!artifact &&
    !!version &&
    node?.editable === true &&
    !!node.digest &&
    !['number', 'formula', 'error'].includes(nodeKind);
  const placeholders = template.match(/\{\{value\}\}/g)?.length || 0;
  return (
    <section className="os-metrics" aria-label="来源指标">
      <div className="os-section-heading">
        <h3>来源指标</h3>
        <button disabled={loading || busy} onClick={() => setRevision((value) => value + 1)}>
          刷新
        </button>
      </div>
      <p className="os-muted">选择表格单元格或文本节点，记录真实原值；再选择目标内容，将指标写入新草稿。</p>
      {error && (
        <p role="alert" className="os-metric-error">
          {error}
        </p>
      )}
      {notice && (
        <p role="status" className="os-muted">
          {notice}
        </p>
      )}
      <button
        disabled={!canCapture || busy}
        onClick={() => {
          setName(node?.label || '');
          setCaptureOpen((value) => !value);
        }}
      >
        记录选中内容的指标
      </button>
      {captureOpen && version && node && (
        <form
          className="os-metric-form"
          onSubmit={(event) => {
            event.preventDefault();
            void run(
              () =>
                actions.capture({
                  versionId: version.id,
                  nodeId: node.id,
                  nodeDigest: node.digest!,
                  name: name.trim(),
                  ...(unit.trim() ? { unit: unit.trim() } : {}),
                  ...(currency.trim() ? { currency: currency.trim() } : {}),
                  ...(period.trim() ? { period: period.trim() } : {}),
                  ...(numericSource && digits !== '' ? { roundingDigits: Number(digits) } : {}),
                }),
              false,
            );
          }}
        >
          <p>
            来源：{node.label} · v{version.versionNo}
          </p>
          <pre>{node.text}</pre>
          <p className="os-muted">整段文字会保留原值；数值格式化用于数字单元格，不自动猜测句子中的数字。</p>
          <label>
            指标名称
            <input value={name} onChange={(event) => setName(event.target.value)} maxLength={120} required />
          </label>
          <div className="os-form-columns">
            <label>
              单位
              <input
                value={unit}
                onChange={(event) => setUnit(event.target.value)}
                placeholder="例如 万元"
                maxLength={32}
              />
            </label>
            <label>
              币种
              <input
                value={currency}
                onChange={(event) => setCurrency(event.target.value)}
                placeholder="例如 CNY"
                maxLength={16}
              />
            </label>
          </div>
          <label>
            数据期间
            <input
              value={period}
              onChange={(event) => setPeriod(event.target.value)}
              placeholder="例如 2026年第二季度"
              maxLength={80}
            />
          </label>
          <label>
            小数位数（可留空）
            <input
              type="number"
              min="0"
              max="12"
              disabled={!numericSource}
              value={numericSource ? digits : ''}
              onChange={(event) => setDigits(event.target.value)}
            />
          </label>
          <div className="os-row-actions">
            <button type="button" disabled={busy} onClick={() => setCaptureOpen(false)}>
              取消
            </button>
            <button
              disabled={
                readOnly ||
                busy ||
                !name.trim() ||
                (numericSource &&
                  digits !== '' &&
                  (!Number.isInteger(Number(digits)) || Number(digits) < 0 || Number(digits) > 12))
              }
            >
              保留来源并记录
            </button>
          </div>
        </form>
      )}
      {loading ? (
        <p role="status" className="os-muted">
          正在读取已记录指标…
        </p>
      ) : metrics.length ? (
        <ul className="os-metric-list">
          {metrics.map((metric) => (
            <li key={metric.id} className={metric.id === selectedId ? 'is-selected' : ''}>
              <button
                aria-pressed={metric.id === selectedId}
                onClick={() => {
                  setSelectedId(metric.id);
                  setTemplate('{{value}}{{unit}}');
                }}
              >
                <strong>{metric.name}</strong>
                <b>
                  {metric.displayValue}
                  {metric.unit}
                </b>
              </button>
              <small>{[metric.currency, metric.period].filter(Boolean).join(' · ')}</small>
              <details>
                <summary>原值与来源</summary>
                <dl>
                  <dt>原值</dt>
                  <dd>
                    {metric.rawValue}
                    {metric.rawValueTruncated && <small>原值较长，此处显示摘要；应用时使用完整原值。</small>}
                  </dd>
                  <dt>显示值</dt>
                  <dd>
                    {metric.displayValue}
                    {metric.displayValueTruncated && <small>此处为显示值摘要。</small>}
                  </dd>
                  <dt>来源版本</dt>
                  <dd>{metric.sourceVersionId}</dd>
                  <dt>来源节点</dt>
                  <dd>{metric.sourceNodeId}</dd>
                  <dt>舍入规则</dt>
                  <dd>
                    {metric.roundingPolicy === 'none'
                      ? '保留原值'
                      : `四舍五入，保留 ${metric.roundingDigits ?? 0} 位小数`}
                  </dd>
                </dl>
              </details>
            </li>
          ))}
        </ul>
      ) : (
        <p className="os-muted">尚未记录指标。</p>
      )}
      {selected && (
        <form
          className="os-metric-form"
          onSubmit={(event) => {
            event.preventDefault();
            if (!canApply || !artifact || !version || !node?.digest) return;
            void run(
              () =>
                actions.apply({
                  metricId: selected.id,
                  targetVersionId: version.id,
                  targetNodeId: node.id,
                  targetNodeDigest: node.digest!,
                  template,
                  expectedRevision: artifact.revision,
                }),
              true,
            );
          }}
        >
          <h3>应用「{selected.name}」</h3>
          <p className="os-muted">
            {node && version ? `目标：${node.label} · v${version.versionNo}` : '请在文件中选择要更新的内容。'}
          </p>
          <label>
            更新后的内容模板
            <textarea
              value={template}
              onChange={(event) => setTemplate(event.target.value)}
              rows={4}
              maxLength={32000}
            />
          </label>
          <p className="os-muted">
            用 {'{{value}}'} 表示真实指标值，只能出现一次。可搭配 {'{{unit}}'}、{'{{currency}}'}、{'{{period}}'}。
          </p>
          {placeholders !== 1 && <p className="os-metric-error">模板需要且只能包含一个 {'{{value}}'}。</p>}
          <button disabled={busy || !canApply || placeholders !== 1}>应用指标并生成草稿</button>
        </form>
      )}
    </section>
  );
}
