import React, { useRef, useState } from 'react';
import { ExternalLink, Paperclip, Plus, X } from 'lucide-react';
import { Dialog } from '../ui/Dialog';
import { OfficePDFViewer } from './OfficePDFViewer';
import type { OfficeArtifact, OfficeStudioApi, OfficeTask } from './officeStudioApi';
import { officeStudioUserError } from './officeUserError';

export function OfficeReferences({ api, task, files, onAdd, onOpenExport }: {
  api: OfficeStudioApi; task: OfficeTask; files: OfficeArtifact[]; onAdd?: () => void;
  onOpenExport?: (task: OfficeTask, path: string, reveal: boolean) => Promise<void>;
}): React.JSX.Element {
  const [pdf, setPdf] = useState<OfficeArtifact>();
  const [opening, setOpening] = useState('');
  const [notice, setNotice] = useState('');
  const [error, setError] = useState('');
  const busy = useRef(false);
  const open = async (file: OfficeArtifact) => {
    if (busy.current) return;
    busy.current = true;
    setOpening(file.id); setError(''); setNotice('');
    try {
      const copy = await api.exportArtifact({ taskId: task.id, versionId: file.headVersionId, name: file.name, draft: true });
      if (onOpenExport) await onOpenExport(task, copy.path, false);
      else await api.openExport({ taskId: task.id, path: copy.path, reveal: false });
      setNotice(`已打开 ${file.name} 的查看副本`);
    } catch (cause) {
      setError(officeStudioUserError(cause, '附件打开失败，请重试。'));
    } finally { busy.current = false; setOpening(''); }
  };
  return <section className="os-references" aria-label="对话参考附件">
    <div className="os-reference-heading"><strong>参考附件{files.length ? ` · ${files.length}` : ''}</strong>
      {onAdd && <button onClick={onAdd} title="添加参考附件" aria-label="添加参考附件"><Plus size={16}/></button>}
    </div>
    <div className="os-reference-files">{files.map(file => <button key={file.id} disabled={!!opening}
      title={`查看附件 ${file.name}`} aria-label={`查看附件 ${file.name}`}
      onClick={() => { if (file.kind === 'pdf') { setError(''); setPdf(file); } else void open(file); }}>
      <Paperclip size={15}/><span>{file.name}</span>{opening === file.id ? <small>正在打开…</small> : <ExternalLink size={14}/>}</button>)}</div>
    {notice && !pdf && <p role="status">{notice}</p>}{error && !pdf && <p role="alert">{error}</p>}
    <Dialog open={!!pdf} title={pdf?.name || '参考附件'} wide onClose={() => setPdf(undefined)}>
      {pdf && <div className="os-reference-preview"><OfficePDFViewer api={api} taskId={task.id} versionId={pdf.headVersionId} name={pdf.name}/></div>}
      {notice && <p role="status">{notice}</p>}{error && <p role="alert">{error}</p>}
      <div className="dialog-actions">{pdf && <button disabled={!!opening} onClick={() => void open(pdf)}><ExternalLink size={16}/>用本机应用打开</button>}
        <button onClick={() => setPdf(undefined)}><X size={16}/>关闭</button></div>
    </Dialog>
  </section>;
}
