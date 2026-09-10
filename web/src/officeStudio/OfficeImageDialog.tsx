import { useEffect, useRef, useState } from 'react';
import { Dialog } from '../ui/Dialog';
import { readBoundedFile } from '../files/readBoundedFile';
import type { OfficeImageUpload } from './officeImageUpload';
import type { OfficeNode, OfficeStudioApi, OfficeTask, OfficeTaskDetail } from './officeStudioApi';
import { officeBytes } from './officePresentation';
import { officeStudioUserError } from './officeUserError';

export interface OfficeImageTarget {
  task: OfficeTask; artifactId: string; baseVersionId: string; expectedRevision: number;
  node: OfficeNode; versionNo: number;
}
export type OfficeImageUploader = (task: OfficeTask, file: File, progress: (text: string) => void, signal: AbortSignal) => Promise<OfficeImageUpload>;

export function OfficeImageDialog({ api, target, onUpload, onChanged, onClose }: { api: OfficeStudioApi; target?: OfficeImageTarget; onUpload: OfficeImageUploader; onChanged: (detail: OfficeTaskDetail) => void; onClose: () => void }) {
  const [file, setFile] = useState<File>(), [url, setURL] = useState('');
  const [fit, setFit] = useState<'contain' | 'cover'>('contain'), [alt, setAlt] = useState(''), [editAlt, setEditAlt] = useState(false);
  const [busy, setBusy] = useState(false), [committing, setCommitting] = useState(false), [error, setError] = useState(''), [notice, setNotice] = useState('');
  const generation = useRef(0), operation = useRef<AbortController | undefined>(undefined);
  useEffect(() => {
    generation.current++; setFile(undefined); setError(''); setNotice(''); setFit('contain'); setAlt(''); setEditAlt(false); setBusy(false); setCommitting(false);
    return () => { generation.current++; operation.current?.abort(); };
  }, [target]);
  useEffect(() => { if (!file) { setURL(''); return; } const next = URL.createObjectURL(file); setURL(next); return () => URL.revokeObjectURL(next); }, [file]);
  const choose = async (next?: File) => {
    if (!next || busy) return;
    const revision = ++generation.current;
    setFile(undefined); setError(''); setBusy(true);
    try {
      const bytes = new Uint8Array(await readBoundedFile(next, 8 * 1024 * 1024));
      if (![137,80,78,71,13,10,26,10].every((v, i) => bytes[i] === v) && !(bytes[0] === 255 && bytes[1] === 216 && bytes[2] === 255)) throw new Error('请选择文件内容有效的 PNG 或 JPEG 原图。');
      if (revision === generation.current) setFile(next);
    } catch (cause) { if (revision === generation.current) setError(officeStudioUserError(cause, '图片读取失败。')); }
    finally { if (revision === generation.current) setBusy(false); }
  };
  const replace = async () => {
    if (!target || !file || !target.node.digest || busy) return;
    const snapshot = target, revision = ++generation.current, controller = new AbortController(); operation.current = controller;
    setBusy(true); setCommitting(false); setError('');
    try {
      const uploaded = await onUpload(snapshot.task, file, text => { if (revision === generation.current) setNotice(text); }, controller.signal);
      if (revision !== generation.current || controller.signal.aborted) return;
      setCommitting(true); setNotice('正在替换选中的图片并保存为新版本…');
      const next = await api.replaceImage({ taskId: snapshot.task.id, artifactId: snapshot.artifactId, baseVersionId: snapshot.baseVersionId, expectedRevision: snapshot.expectedRevision, nodeId: snapshot.node.id, nodeDigest: snapshot.node.digest!, attachmentId: uploaded.attachmentId, sha256: uploaded.sha256, fit, ...(editAlt ? { alt } : {}) });
      if (revision !== generation.current) return;
      onChanged(next); onClose();
    } catch (cause) { if (revision === generation.current) setError(officeStudioUserError(cause, '图片替换未完成。')); }
    finally { if (revision === generation.current) { setBusy(false); setCommitting(false); operation.current = undefined; } }
  };
  return <Dialog open={!!target} title="替换选中的图片" onClose={() => { if (!busy) onClose(); }} wide>
    {target && <div className="os-image-dialog">
      <p><b>{target.node.label}</b> · v{target.versionNo}</p>
      <p className="os-muted">新图片沿用此处的位置和图片框，保存为新草稿，原版本仍可查看。上传保留 PNG / JPEG 原始字节，最大 8 MiB。</p>
      {target.node.image && <details className="os-image-source"><summary>当前图片来源</summary><p>{target.node.image.mediaPart}</p>{target.node.image.pixelWidth && target.node.image.pixelHeight && <p>{target.node.image.pixelWidth} × {target.node.image.pixelHeight} 像素</p>}{target.node.image.sourceId && <p>来源附件：{target.node.image.sourceId}</p>}<code>{target.node.image.mediaSha256}</code></details>}
      <label>选择替换图片<input type="file" accept=".png,.jpg,.jpeg,image/png,image/jpeg" disabled={busy} onChange={event => { const next = event.target.files?.[0]; event.target.value = ''; void choose(next); }} /></label>
      {url && file && <figure className="os-image-choice"><img src={url} alt="待替换原图预览" /><figcaption>{file.name} · {officeBytes(file.size)}</figcaption></figure>}
      <label>图片填充方式<select value={fit} disabled={busy} onChange={event => setFit(event.target.value as 'contain' | 'cover')}><option value="contain">完整显示（可能留白）</option><option value="cover">填满图片框（可能裁边）</option></select></label>
      <label><input type="checkbox" checked={editAlt} disabled={busy} onChange={event => setEditAlt(event.target.checked)} />修改替代说明</label>
      {editAlt && <label>图片替代说明<input value={alt} maxLength={2000} disabled={busy} onChange={event => setAlt(event.target.value)} /></label>}
      {notice && <p role="status">{notice}</p>}{error && <p role="alert">{error}</p>}
      <div className="dialog-actions"><button disabled={busy} onClick={onClose}>关闭</button>{busy && !committing && <button onClick={() => operation.current?.abort()}>取消上传</button>}<button className="primary" disabled={busy || !file || !target.node.digest} onClick={() => void replace()}>{busy ? '处理中…' : '替换并生成新版本'}</button></div>
    </div>}
  </Dialog>;
}
