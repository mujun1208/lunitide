import React, { useEffect, useRef, useState } from 'react';
import { Dialog } from '../ui/Dialog';
import type { OfficeArtifact, OfficeBundle, OfficeBundleExport } from './officeStudioApi';
import { officeDate, officeQualityLabel } from './officePresentation';
import { officeStudioUserError } from './officeUserError';

export interface OfficeBundleActions {
  list: () => Promise<{ items: OfficeBundle[] }>;
  create: (input: { title: string; versionIds: string[] }) => Promise<OfficeBundle>;
  export: (input: { bundleId: string }) => Promise<OfficeBundleExport>;
  open: (path: string) => Promise<void>;
}
export function OfficeBundleDialog({
  open,
  taskId,
  taskTitle,
  artifacts,
  actions,
  onClose,
  readOnly = false,
}: {
  open: boolean;
  taskId: string;
  taskTitle: string;
  artifacts: OfficeArtifact[];
  actions: OfficeBundleActions;
  onClose: () => void;
  readOnly?: boolean;
}): React.JSX.Element {
  const [title, setTitle] = useState('');
  const [selected, setSelected] = useState<Record<string, string>>({});
  const [bundles, setBundles] = useState<OfficeBundle[]>([]);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');
  const [result, setResult] = useState<OfficeBundleExport>();
  const [loading, setLoading] = useState(false);
  const actionsRef = useRef(actions);
  actionsRef.current = actions;
  const initial = useRef({ taskTitle, artifacts });
  initial.current = { taskTitle, artifacts };
  const retained = useRef<{ signature: string; bundle: OfficeBundle } | undefined>(undefined);
  const mounted = useRef(true);
  const operation = useRef(false);
  useEffect(() => {
    mounted.current = true;
    return () => {
      mounted.current = false;
    };
  }, []);
  useEffect(() => {
    if (!open) return;
    let active = true;
    const values = initial.current;
    setTitle(`${values.taskTitle}交付包`);
    setError('');
    setResult(undefined);
    retained.current = undefined;
    setSelected(
      Object.fromEntries(
        values.artifacts.map((artifact) => [artifact.id, artifact.acceptedVersionId || artifact.headVersionId]),
      ),
    );
    setLoading(true);
    void actionsRef.current
      .list()
      .then((list) => {
        if (active) setBundles(list.items);
      })
      .catch((cause) => {
        if (active) setError(officeStudioUserError(cause, '历史交付包读取失败。'));
      })
      .finally(() => {
        if (active) setLoading(false);
      });
    return () => {
      active = false;
    };
  }, [open, taskId]);
  const versionIds = Object.values(selected).filter(Boolean);
  const draftCount = artifacts.filter(
    (artifact) =>
      selected[artifact.id] &&
      artifact.versions.find((version) => version.id === selected[artifact.id])?.quality !== 'passed',
  ).length;
  const run = async (work: () => Promise<void>) => {
    if (operation.current || readOnly) return;
    operation.current = true;
    setBusy(true);
    setError('');
    try {
      await work();
    } catch (cause) {
      if (mounted.current) setError(officeStudioUserError(cause, '成套导出失败，可重试同一交付包。'));
    } finally {
      operation.current = false;
      if (mounted.current) setBusy(false);
    }
  };
  const createAndExport = () =>
    run(async () => {
      const signature = JSON.stringify({ title: title.trim(), versionIds: [...versionIds].sort() });
      const bundle =
        retained.current?.signature === signature
          ? retained.current.bundle
          : await actions.create({ title: title.trim(), versionIds });
      retained.current = { signature, bundle };
      if (mounted.current) setBundles((items) => [bundle, ...items.filter((item) => item.id !== bundle.id)]);
      const exported = await actions.export({ bundleId: bundle.id });
      if (mounted.current) setResult(exported);
    });
  return (
    <Dialog
      open={open}
      title="成套导出"
      description="固定每份文件的具体版本，保存交付清单与副本。默认优先选择已接受版本，可调整为历史版本。"
      onClose={() => {
        if (!busy) onClose();
      }}
      wide
    >
      <div className="os-bundle-content">
        {error && (
          <p className="os-metric-error" role="alert">
            {error}
          </p>
        )}
        <form
          onSubmit={(event) => {
            event.preventDefault();
            void createAndExport();
          }}
        >
          <label>
            交付包名称
            <input value={title} disabled={busy} onChange={(event) => setTitle(event.target.value)} maxLength={200} />
          </label>
          <div className="os-bundle-files">
            {artifacts.map((artifact) => (
              <div className="os-bundle-file" key={artifact.id}>
                <label>
                  <input
                    type="checkbox"
                    checked={!!selected[artifact.id]}
                    disabled={busy}
                    onChange={(event) =>
                      setSelected((values) => ({
                        ...values,
                        [artifact.id]: event.target.checked ? artifact.acceptedVersionId || artifact.headVersionId : '',
                      }))
                    }
                  />
                  <span>{artifact.name}</span>
                </label>
                <select
                  aria-label={`${artifact.name} 导出版本`}
                  value={selected[artifact.id] || ''}
                  disabled={busy || !selected[artifact.id]}
                  onChange={(event) => setSelected((values) => ({ ...values, [artifact.id]: event.target.value }))}
                >
                  {!selected[artifact.id] && <option value="">不包含此文件</option>}
                  {[...artifact.versions]
                    .sort((a, b) => b.versionNo - a.versionNo)
                    .map((version) => (
                      <option key={version.id} value={version.id}>
                        v{version.versionNo}
                        {version.id === artifact.acceptedVersionId ? ' · 已接受' : ''}
                        {version.id === artifact.headVersionId ? ' · 最新' : ''} · {officeQualityLabel(version.quality)}
                      </option>
                    ))}
                </select>
              </div>
            ))}
          </div>
          <p className="os-muted">
            {!versionIds.length
              ? '请选择需要放入交付包的文件。'
              : `已选 ${versionIds.length} 份文件${draftCount ? `，其中 ${draftCount} 份尚未通过全部检查，将明确标为草稿。` : '，全部所需检查已通过。检查通过不是已接受为正式版。'}`}
          </p>
          <div className="dialog-actions">
            <button type="button" disabled={busy} onClick={onClose}>
              关闭
            </button>
            <button className="primary" disabled={busy || readOnly || !title.trim() || !versionIds.length}>
              {busy ? '正在保存交付包…' : '固定版本并导出'}
            </button>
          </div>
        </form>
        {result && (
          <section className="os-export-result" aria-label="交付包导出结果">
            <h3>{result.complete ? '交付包已保存' : '交付包尚未完整导出'}</h3>
            <p>{result.directory}</p>
            <p>
              实际文件：{result.files.length} 份 · 清单：{result.manifestPath}
            </p>
            <button disabled={busy} onClick={() => void run(() => actions.open(result.manifestPath))}>
              打开交付包所在文件夹
            </button>
          </section>
        )}
        <details className="os-bundle-history">
          <summary>历史交付包（{bundles.length}）</summary>
          {loading ? (
            <p role="status">正在读取…</p>
          ) : bundles.length ? (
            bundles.map((bundle) => (
              <article key={bundle.id}>
                <div>
                  <b>{bundle.title}</b>
                  <small>
                    {officeDate(bundle.createdAt)} · {bundle.files.length} 份文件
                  </small>
                </div>
                <ul>
                  {bundle.files.map((file) => (
                    <li key={file.versionId}>
                      {file.name} · {officeQualityLabel(file.quality)}
                      {file.accepted ? ' · 已接受' : ''}
                    </li>
                  ))}
                </ul>
                <button
                  disabled={busy}
                  onClick={() =>
                    void run(async () => {
                      const exported = await actions.export({ bundleId: bundle.id });
                      if (mounted.current) setResult(exported);
                    })
                  }
                >
                  再次导出这套固定版本
                </button>
              </article>
            ))
          ) : (
            <p className="os-muted">尚无历史交付包。</p>
          )}
        </details>
      </div>
    </Dialog>
  );
}
